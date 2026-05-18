#!/usr/bin/env python3
# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0.
"""Cache prewarm script: downloads model from HuggingFace, uploads to Azure Blob Storage.

Environment variables:
    MODEL_ID              HuggingFace model identifier (e.g., "microsoft/phi-4")
    MODEL_REVISION        Model revision (commit hash or "main")
    BLOB_ENDPOINT         Azure Blob Storage endpoint
    BLOB_CONTAINER        Blob container name
    BLOB_PATH             Relative path within container for model files
    TACHYON_DISCOVERY_ENDPOINT  Cache server discovery endpoint (for future use)
    HF_TOKEN              (optional) HuggingFace access token for gated models
"""

import logging
import os
import sys
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

from azure.identity import DefaultAzureCredential
from azure.storage.blob import BlobServiceClient
from huggingface_hub import snapshot_download

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
logger = logging.getLogger("cache-prewarm")

# Maximum concurrent blob uploads.
MAX_UPLOAD_WORKERS = 8


def get_required_env(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        logger.error(f"Required environment variable {name} is not set")
        sys.exit(1)
    return value


def download_model(model_id: str, revision: str, token: str | None) -> Path:
    """Download model snapshot from HuggingFace to a local directory."""
    logger.info(f"Downloading model {model_id} (revision: {revision})")
    local_dir = Path("/tmp/model-download") / model_id.replace("/", "--") / revision
    local_dir.mkdir(parents=True, exist_ok=True)

    path = snapshot_download(
        repo_id=model_id,
        revision=revision,
        local_dir=str(local_dir),
        token=token,
    )
    logger.info(f"Download complete: {path}")
    return Path(path)


def upload_to_blob(
    local_dir: Path,
    blob_endpoint: str,
    container_name: str,
    blob_path_prefix: str,
) -> int:
    """Upload all files from local_dir to blob storage under blob_path_prefix."""
    credential = DefaultAzureCredential()
    blob_service = BlobServiceClient(account_url=blob_endpoint, credential=credential)
    container_client = blob_service.get_container_client(container_name)

    # Collect files to upload.
    files = [f for f in local_dir.rglob("*") if f.is_file()]
    logger.info(
        f"Uploading {len(files)} files to {blob_endpoint}/{container_name}/{blob_path_prefix}"
    )

    uploaded = 0
    errors = []

    def upload_file(local_file: Path) -> str:
        relative = local_file.relative_to(local_dir)
        blob_name = f"{blob_path_prefix}/{relative}"
        blob_client = container_client.get_blob_client(blob_name)
        with open(local_file, "rb") as f:
            blob_client.upload_blob(f, overwrite=True)
        return blob_name

    with ThreadPoolExecutor(max_workers=MAX_UPLOAD_WORKERS) as executor:
        futures = {executor.submit(upload_file, f): f for f in files}
        for future in as_completed(futures):
            local_file = futures[future]
            try:
                blob_name = future.result()
                uploaded += 1
                if uploaded % 10 == 0 or uploaded == len(files):
                    logger.info(f"  Uploaded {uploaded}/{len(files)} files")
            except Exception as e:
                errors.append((local_file, e))
                logger.error(f"  Failed to upload {local_file}: {e}")

    if errors:
        logger.error(f"{len(errors)} files failed to upload")
        sys.exit(1)

    logger.info(f"Upload complete: {uploaded} files")
    return uploaded


def main():
    model_id = get_required_env("MODEL_ID")
    revision = os.environ.get("MODEL_REVISION", "main")
    blob_endpoint = get_required_env("BLOB_ENDPOINT")
    blob_container = get_required_env("BLOB_CONTAINER")
    blob_path = get_required_env("BLOB_PATH")
    hf_token = os.environ.get("HF_TOKEN")

    logger.info(f"Cache prewarm starting for {model_id}@{revision}")
    logger.info(f"Target: {blob_endpoint}/{blob_container}/{blob_path}")

    # Step 1: Download from HuggingFace.
    local_dir = download_model(model_id, revision, hf_token)

    # Step 2: Upload to blob storage.
    upload_to_blob(local_dir, blob_endpoint, blob_container, blob_path)

    logger.info("Prewarm complete")


if __name__ == "__main__":
    main()
