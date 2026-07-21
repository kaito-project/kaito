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

"""Init-container script: obtain a short-lived SAS token via SAS-authenticated blob streaming.

Reads pod environment variables, exchanges a Workload Identity AAD token for a
SAS token via the datarefs endpoint, and writes it as an env file (KEY=value) to a
shared volume so the main inference container's entrypoint wrapper can source it and
export AZURE_STORAGE_SAS_TOKEN.

Required environment variables:
    STREAM_DATAREFS_URL       - SAS-mint endpoint URL (datarefs for public, /credentials for BYO)
    STREAM_BLOB_URI           - blob URI for the request body
    STREAM_IDENTITY_CLIENT_ID - workload identity client ID to mint the SAS as
    STREAM_ENV_FILE           - file path to write the env file (KEY=value lines)

Optional environment variables:
    STREAM_ASSET_ID           - request body {assetId}; omitted when empty
    STREAM_TOKEN_AUDIENCE     - AAD token audience; defaults to https://management.azure.com
"""

import json
import os
import sys
import urllib.request

from azure.identity import WorkloadIdentityCredential


def build_request_body(asset_id: str, blob_uri: str) -> bytes:
    """Build the SAS-mint POST body. Includes assetId only when non-empty"""
    body = {"blobUri": blob_uri}
    if asset_id:
        body["assetId"] = asset_id
    return json.dumps(body).encode()


def extract_sas_uri(payload: dict) -> str:
    """Extract the SAS URI from a datarefs/credentials response. Tolerates both wrapper
    keys: 'blobReferenceForConsumption' and 'blobReference'."""
    ref = (
        payload.get("blobReferenceForConsumption") or payload.get("blobReference") or {}
    )
    return ref.get("credential", {}).get("sasUri", "")


def main() -> int:
    datarefs_url = os.environ["STREAM_DATAREFS_URL"]
    blob_uri = os.environ["STREAM_BLOB_URI"]
    asset_id = os.environ.get("STREAM_ASSET_ID", "")
    client_id = os.environ["STREAM_IDENTITY_CLIENT_ID"]
    audience = os.environ.get("STREAM_TOKEN_AUDIENCE") or "https://management.azure.com"
    out_path = os.environ["STREAM_ENV_FILE"]

    cred = WorkloadIdentityCredential(client_id=client_id)
    token = cred.get_token(f"{audience}/.default").token

    body = build_request_body(asset_id, blob_uri)
    req = urllib.request.Request(
        datarefs_url,
        data=body,
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        payload = json.load(resp)

    sas_uri = extract_sas_uri(payload)
    if not sas_uri or "?" not in sas_uri:
        print("ERROR: datarefs response had no usable sasUri", file=sys.stderr)
        return 1

    sas_token = sas_uri.split("?", 1)[1]
    # Single-quote the value for safe shell sourcing
    escaped = sas_token.replace("'", "'\\''")
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as f:
        f.write(f"AZURE_STORAGE_SAS_TOKEN='{escaped}'\n")
    print(f"SAS env file written to {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
