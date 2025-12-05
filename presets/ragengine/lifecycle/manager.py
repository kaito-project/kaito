# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Manager functions for PostStart and PreStop lifecycle hooks.
Handles FAISS index persistence and restoration using existing RAG service APIs.
"""

import json
import os
import shutil
import time
from contextlib import suppress
from datetime import datetime
from pathlib import Path

import requests


def wait_for_service(
    service_url: str = "http://localhost:5000/indexes", timeout: int = 60
) -> bool:
    """
    Wait for the RAG service to be ready.

    Args:
        service_url: URL to check for service health
        timeout: Maximum wait time in seconds

    Returns:
        True if service is ready, False otherwise
    """
    print(f"Waiting for service at {service_url}...")
    max_attempts = timeout // 2

    for attempt in range(max_attempts):
        try:
            response = requests.get(service_url, timeout=2)
            if response.status_code == 200:
                print(f"Service ready after {attempt * 2}s")
                return True
        except Exception:
            if attempt == 0:
                print(f"Waiting for service (attempt {attempt + 1}/{max_attempts})...")

        time.sleep(2)

    print(f"Service not ready after {timeout}s")
    return False


def get_indexes(service_url: str = "http://localhost:5000") -> list[str]:
    """
    Get list of indexes from the RAG service.

    Args:
        service_url: Base URL of the RAG service

    Returns:
        List of index names
    """
    try:
        response = requests.get(f"{service_url}/indexes", timeout=5)
        return response.json()
    except Exception as e:
        print(f"Failed to get indexes: {e}")
        return []


def load_index(
    index_name: str, path: str, service_url: str = "http://localhost:5000"
) -> bool:
    """
    Load an index from disk via the RAG service API.

    Args:
        index_name: Name of the index to load
        path: Path to the persisted index
        service_url: Base URL of the RAG service

    Returns:
        True if successful, False otherwise
    """
    try:
        url = f"{service_url}/load/{index_name}?path={path}&overwrite=true"
        response = requests.post(url, timeout=30)
        return response.status_code == 200
    except Exception as e:
        print(f"Failed to load index {index_name}: {e}")
        return False


def persist_index(
    index_name: str, path: str, service_url: str = "http://localhost:5000"
) -> bool:
    """
    Persist an index to disk via the RAG service API.

    Args:
        index_name: Name of the index to persist
        path: Path where the index should be saved
        service_url: Base URL of the RAG service

    Returns:
        True if successful, False otherwise
    """
    try:
        url = f"{service_url}/persist/{index_name}?path={path}"
        response = requests.post(url, timeout=30)
        return response.status_code == 200
    except Exception as e:
        print(f"Failed to persist index {index_name}: {e}")
        return False


def poststart_handler(base_dir: str | None = None) -> int:
    """
    PostStart lifecycle hook handler.
    Restores indexes from the latest snapshot.

    Args:
        base_dir: Base directory for vector database persistence

    Returns:
        0 on success, 1 on failure
    """
    print("=== PostStart Handler Started ===")

    # Get base directory
    if base_dir is None:
        base_dir = os.getenv("DEFAULT_VECTOR_DB_PERSIST_DIR", "/mnt/vector-db")

    base = Path(base_dir)
    latest_link = base / "LATEST"

    # Wait for service
    if not wait_for_service():
        print("ERROR: Service did not become ready")
        return 1

    # Check for existing snapshots
    if not latest_link.exists() or not latest_link.is_symlink():
        # LATEST link doesn't exist, try to find the most recent snapshot
        snapshots_dir = base / "systemsnapshots"
        if not snapshots_dir.exists():
            print("No previous snapshots found")
            return 0

        # Find all snapshot directories
        all_snapshots = sorted(
            [d for d in snapshots_dir.iterdir() if d.is_dir()],
            key=lambda x: x.name,
            reverse=True,
        )

        if not all_snapshots:
            print("No previous snapshots found")
            return 0

        # Use the most recent snapshot
        latest = all_snapshots[0]
        print(f"LATEST link missing, using most recent snapshot: {latest.name}")

        # Recreate LATEST symlink
        latest_link.symlink_to(latest.relative_to(base))
        print(f"✓ Recreated LATEST link -> {latest.name}")
    else:
        # LATEST link exists, resolve it
        latest = latest_link.resolve()

    # Read snapshot metadata
    meta_file = latest / "metadata.json"

    if not meta_file.exists():
        print("No metadata found in snapshot")
        return 0

    try:
        with open(meta_file) as f:
            metadata = json.load(f)

        index_names = metadata.get("index_names", [])
        print(f"Found {len(index_names)} indexes in snapshot {latest.name}")

        # Load each index
        loaded_count = 0
        for index_name in index_names:
            index_path = str(latest / index_name)
            print(f"Loading index: {index_name} from {index_path}")

            if load_index(index_name, index_path):
                loaded_count += 1
                print(f"✓ Loaded: {index_name}")
            else:
                print(f"✗ Failed to load: {index_name}")

            time.sleep(0.5)  # Rate limiting

        print(
            f"=== PostStart Complete: {loaded_count}/{len(index_names)} indexes loaded ==="
        )
        return 0

    except Exception as e:
        print(f"ERROR in PostStart handler: {e}")
        import traceback

        traceback.print_exc()
        return 1


def prestop_handler(base_dir: str | None = None, keep_snapshots: int = 5) -> int:
    """
    PreStop lifecycle hook handler.
    Persists all indexes to a timestamped snapshot.

    Args:
        base_dir: Base directory for vector database persistence
        keep_snapshots: Number of snapshots to keep (delete older ones)

    Returns:
        0 on success, 1 on failure
    """
    print("=== PreStop Handler Started ===")

    # Get base directory
    if base_dir is None:
        base_dir = os.getenv("DEFAULT_VECTOR_DB_PERSIST_DIR", "/mnt/vector-db")

    base = Path(base_dir)
    snapshots_dir = base / "systemsnapshots"
    snapshots_dir.mkdir(parents=True, exist_ok=True)

    # Create snapshot directory
    pod_uid = os.getenv("POD_UID", "unknown")[:8]
    timestamp = datetime.now().strftime("%Y-%m-%dT%H-%M-%S")
    snap_dir = snapshots_dir / f"{timestamp}_pod-{pod_uid}"
    snap_dir.mkdir(exist_ok=True)

    print(f"Snapshot directory: {snap_dir}")

    try:
        # Step 1: Get list of indexes
        index_names = get_indexes()
        print(f"Found {len(index_names)} indexes: {index_names}")

        if not index_names:
            print("No indexes to persist")
            shutil.rmtree(snap_dir)
            return 0

        # Step 2: Persist each index directly to snapshot directory
        saved = []
        for index_name in index_names:
            persist_path = str(snap_dir / index_name)
            print(f"Persisting index: {index_name} to {persist_path}")

            if persist_index(index_name, persist_path):
                saved.append(index_name)
                print(f"✓ Persisted: {index_name}")
            else:
                print(f"✗ Failed to persist: {index_name}")

            time.sleep(0.5)  # Rate limiting

        # Step 3: Save metadata and update LATEST link
        if saved:
            metadata = {
                "timestamp": datetime.now().isoformat(),
                "pod_name": os.getenv("POD_NAME", "unknown"),
                "pod_uid": pod_uid,
                "index_names": saved,
                "version": 1,
            }

            meta_file = snap_dir / "metadata.json"
            with open(meta_file, "w") as f:
                json.dump(metadata, f, indent=2)

            # Update LATEST symlink to point to the new snapshot
            latest_link = base / "LATEST"
            if latest_link.exists():
                latest_link.unlink()
            latest_link.symlink_to(snap_dir.relative_to(base))

            print("✓ Metadata saved, LATEST updated")

            # Step 4: Cleanup old snapshots (keep only the most recent ones)
            all_snapshots = sorted(
                [d for d in snapshots_dir.iterdir() if d.is_dir()],
                key=lambda x: x.name,
                reverse=True,
            )

            old_snapshots = all_snapshots[keep_snapshots:]
            for old_snap in old_snapshots:
                shutil.rmtree(old_snap)
                print(f"✓ Deleted old snapshot: {old_snap.name}")

            print(f"=== PreStop Complete: {len(saved)} indexes saved to snapshot ===")
            return 0
        else:
            print("No indexes were successfully saved")
            shutil.rmtree(snap_dir)
            return 1

    except Exception as e:
        print(f"ERROR in PreStop handler: {e}")
        import traceback

        traceback.print_exc()
        # Try to clean up incomplete snapshot
        if snap_dir.exists():
            with suppress(BaseException):
                shutil.rmtree(snap_dir)
        return 1
