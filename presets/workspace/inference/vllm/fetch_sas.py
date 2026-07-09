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
SAS token via the datarefs endpoint, and writes the SAS query string to a shared
volume path so the main inference container can read it via AZURE_STORAGE_SAS_TOKEN.

Required environment variables:
    STREAM_DATAREFS_URL  - datarefs endpoint URL
    STREAM_ASSET_ID      - asset identifier for the request body
    STREAM_BLOB_URI      - blob URI for the request body
    SAS_TOKEN_PATH       - file path to write the SAS token string
"""

import json
import os
import sys
import urllib.request

from azure.identity import DefaultAzureCredential


def main() -> int:
    datarefs_url = os.environ["STREAM_DATAREFS_URL"]
    asset_id = os.environ["STREAM_ASSET_ID"]
    blob_uri = os.environ["STREAM_BLOB_URI"]
    out_path = os.environ["SAS_TOKEN_PATH"]

    cred = DefaultAzureCredential()
    token = cred.get_token("https://management.azure.com/.default").token

    body = json.dumps({"assetId": asset_id, "blobUri": blob_uri}).encode()
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

    sas_uri = (
        payload.get("blobReferenceForConsumption", {})
        .get("credential", {})
        .get("sasUri", "")
    )
    if not sas_uri or "?" not in sas_uri:
        print("ERROR: datarefs response had no usable sasUri", file=sys.stderr)
        return 1

    sas_token = sas_uri.split("?", 1)[1]
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as f:
        f.write(sas_token)
    print(f"SAS token written to {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
