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

"""Init-container script for SAS-authenticated blob streaming.

Given only the SAS-mint endpoint, a workload identity client id, and the source type, this
script resolves everything else at pod runtime using the workload identity:

  1. Mint an AAD token for the workload identity (audience selected by source type).
  2. Resolve the model (via a URL derived from the mint endpoint) -> blobUri (+ assetId for public).
  3. Derive the storage account and container from the blobUri.
  4. Mint a SAS at the mint endpoint with {blobUri[, assetId]} -> SAS token.
  5. List the container with the SAS to discover the safetensors subpath -> model streaming URI.
  6. For a bring-your-own model, verify the bundle's config.json against the digest the
     operator sized the deployment from.
  7. Write AZURE_STORAGE_SAS_TOKEN, AZURE_STORAGE_ACCOUNT_NAME, and STREAM_MODEL_URI to the
     shared env file so the main container's entrypoint wrapper can source them.

Required environment variables:
    STREAM_DATAREFS_URL       - model endpoint URL. For public: the datarefs (mint) URL. For byo:
                                the base model URL WITHOUT '/credentials' (KAITO appends it to mint).
    STREAM_IDENTITY_CLIENT_ID - workload identity client ID to resolve/mint as
    STREAM_SOURCE_TYPE        - model source flavor: "public" or "byo"
    STREAM_ENV_FILE           - file path to write the env file (KEY=value lines)

Optional environment variables:
    KAITO_MODEL_CONFIG_SHA256 - expected SHA-256 of the bundle's config.json. When set, the
                                bundle is verified before the model is loaded; when unset
                                (preset and HuggingFace models) verification is skipped.
"""

import hashlib
import json
import os
import re
import sys
import urllib.parse
import urllib.request
import xml.sax.saxutils

from azure.identity import WorkloadIdentityCredential

SOURCE_PUBLIC = "public"
SOURCE_BYO = "byo"

# Exit code returned when the streamed bundle's config.json is missing or does not
# match the digest the deployment was sized for. It is distinct from a generic
# failure (exit 1) so the controller can tell a bring-your-own artifact mismatch
# apart from a SAS token/mint failure. Keep in sync with SASFetchExitConfigMismatch
# in modelstreaming.go.
EXIT_CONFIG_MISMATCH = 3

# Token audience per source type (fixed Azure AAD resource identifiers).
AUDIENCE_BY_TYPE = {
    SOURCE_PUBLIC: "https://management.azure.com",
    SOURCE_BYO: "https://ai.azure.com",
}


def derive_urls(input_url: str, source_type: str) -> "tuple[str, str]":
    """Return (resolve_url, mint_url) from STREAM_DATAREFS_URL.

    The '/credentials' minting suffix is a byo detail owned by KAITO:
      byo:    input is the base model URL (.../models/{m}/versions/{v}, NO '/credentials').
              resolve = input as-is; mint = input + '/credentials'.
      public: input is the datarefs (mint) URL (.../registries/{r}/datarefs/{m}/versions/{v}).
              mint = input as-is; resolve = input with '/datarefs/' -> '/models/'.

    Query and fragment are preserved on both derived URLs.
    """
    parts = urllib.parse.urlsplit(input_url)
    path = parts.path.rstrip("/")
    if source_type == SOURCE_BYO:
        if path.endswith("/credentials"):
            raise ValueError(
                f"byo STREAM_DATAREFS_URL must be the base model URL without '/credentials': {path}"
            )
        resolve_path, mint_path = path, path + "/credentials"
    else:
        if "/datarefs/" not in path:
            raise ValueError(
                f"public STREAM_DATAREFS_URL must contain '/datarefs/': {path}"
            )
        mint_path, resolve_path = path, path.replace("/datarefs/", "/models/", 1)

    def build(p: str) -> str:
        return urllib.parse.urlunsplit(
            (parts.scheme, parts.netloc, p, parts.query, parts.fragment)
        )

    return build(resolve_path), build(mint_path)


def http_json(url: str, token: str, body: "bytes | None" = None) -> dict:
    """GET (body=None) or POST a JSON request with a bearer token, return parsed JSON."""
    req = urllib.request.Request(
        url,
        data=body,
        method="POST" if body is not None else "GET",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)


def extract_blob_uri(payload: dict) -> str:
    """Extract the blobUri from a model-resolve response. Public nests it under
    properties.modelUri; BYO under blobReference.blobUri (tolerate both wrapper keys)."""
    props = payload.get("properties") or {}
    if props.get("modelUri"):
        return props["modelUri"]
    ref = (
        payload.get("blobReference") or payload.get("blobReferenceForConsumption") or {}
    )
    return ref.get("blobUri", "")


def extract_sas_uri(payload: dict) -> str:
    """Extract the SAS URI from a datarefs/credentials response. Tolerates both wrapper
    keys: 'blobReferenceForConsumption' and 'blobReference'."""
    ref = (
        payload.get("blobReferenceForConsumption") or payload.get("blobReference") or {}
    )
    return ref.get("credential", {}).get("sasUri", "")


def account_and_container(blob_uri: str) -> "tuple[str, str]":
    """Parse the storage account (first host label) and container (first path segment)
    from a blob URI like https://<account>.blob.core.windows.net/<container>[/...]."""
    parts = urllib.parse.urlsplit(blob_uri)
    account = parts.netloc.split(".", 1)[0]
    container = parts.path.lstrip("/").split("/", 1)[0]
    return account, container


def list_blob_names(sas_uri: str) -> "list[str]":
    """List every blob name in the container via the SAS.

    Pages through the full listing (Azure returns at most 5000 blobs per page plus a
    NextMarker) and unescapes XML entities in blob names so paths with '&' etc. are correct.
    """
    base = sas_uri + "&restype=container&comp=list&include=metadata"
    names: list = []
    marker = ""
    while True:
        url = base + ("&marker=" + urllib.parse.quote(marker) if marker else "")
        with urllib.request.urlopen(url, timeout=30) as resp:
            body = resp.read().decode("utf-8", errors="replace")
        names.extend(
            xml.sax.saxutils.unescape(n)
            for n in re.findall(r"<Name>(.*?)</Name>", body, re.DOTALL)
        )
        m = re.search(r"<NextMarker>(.*?)</NextMarker>", body)
        marker = xml.sax.saxutils.unescape(m.group(1)) if m and m.group(1) else ""
        if not marker:
            break
    return names


def discover_subpath(names: "list[str]") -> str:
    """Return the common directory prefix of the safetensors files (empty string when
    they are at the container root)."""
    safetensors = [n for n in names if n.endswith(".safetensors")]
    if not safetensors:
        return ""
    if len(safetensors) == 1:
        return os.path.dirname(safetensors[0])
    return os.path.commonpath(safetensors)


def blob_url(sas_uri: str, blob_name: str) -> str:
    """Build a blob-scoped URL by inserting the blob path into a container SAS URI."""
    base, _, query = sas_uri.partition("?")
    return f"{base.rstrip('/')}/{urllib.parse.quote(blob_name)}" + (
        f"?{query}" if query else ""
    )


def fetch_blob(sas_uri: str, blob_name: str) -> bytes:
    """Download a single blob via the container SAS."""
    with urllib.request.urlopen(blob_url(sas_uri, blob_name), timeout=60) as resp:
        return resp.read()


def verify_bundle(
    sas_uri: str, names: "list[str]", subpath: str, expected_sha256: str
) -> None:
    """Verify the bundle's config.json against the digest the deployment was sized for.

    Raises ValueError when config.json is missing or does not match. This is the one
    check worth blocking on: a mismatch means the source served weights the deployment
    was never sized or argument-rendered for. Anything else the bundle lacks (tokenizer,
    weight files) is left for the runtime to report when it loads, so this stays a
    verification of identity rather than a re-implementation of the loader's own checks.

    Only the small configuration blob is hashed; the weights themselves are not, since
    the source is required to be versioned and write-once.
    """
    prefix = f"{subpath}/" if subpath else ""
    in_bundle = {n[len(prefix) :] for n in names if n.startswith(prefix)}

    if "config.json" not in in_bundle:
        raise ValueError(f"model bundle at '{prefix}' does not contain config.json")

    actual = hashlib.sha256(fetch_blob(sas_uri, prefix + "config.json")).hexdigest()
    if actual != expected_sha256:
        raise ValueError(
            "model bundle config.json does not match the configuration this deployment "
            f"was sized and configured from (expected sha256 {expected_sha256}, found {actual}); "
            "the model source must be versioned and write-once, and serving different "
            "weights requires a new deployment"
        )


def write_env_file(out_path: str, values: dict) -> None:
    """Write KEY='value' lines (single-quoted for safe shell sourcing) to the env file."""
    parent = os.path.dirname(out_path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as f:
        for key, value in values.items():
            escaped = value.replace("'", "'\\''")
            f.write(f"{key}='{escaped}'\n")


def main() -> int:
    datarefs_url = os.environ["STREAM_DATAREFS_URL"]
    client_id = os.environ["STREAM_IDENTITY_CLIENT_ID"]
    source_type = os.environ["STREAM_SOURCE_TYPE"]
    out_path = os.environ["STREAM_ENV_FILE"]

    if source_type not in AUDIENCE_BY_TYPE:
        print(
            f"ERROR: STREAM_SOURCE_TYPE must be one of {sorted(AUDIENCE_BY_TYPE)}, "
            f"got {source_type!r}",
            file=sys.stderr,
        )
        return 1
    audience = AUDIENCE_BY_TYPE[source_type]

    cred = WorkloadIdentityCredential(client_id=client_id)
    token = cred.get_token(f"{audience}/.default").token

    resolve_url, mint_url = derive_urls(datarefs_url, source_type)

    # Resolve the model to get its blobUri (and assetId for public).
    model = http_json(resolve_url, token)
    blob_uri = extract_blob_uri(model)
    if not blob_uri:
        print("ERROR: model resolve response had no blobUri", file=sys.stderr)
        return 1
    asset_id = model.get("id", "") if source_type == SOURCE_PUBLIC else ""

    account, container = account_and_container(blob_uri)

    # Mint the SAS token at the mint endpoint.
    body = {"blobUri": blob_uri}
    if asset_id:
        body["assetId"] = asset_id
    mint = http_json(mint_url, token, json.dumps(body).encode())
    sas_uri = extract_sas_uri(mint)
    if not sas_uri or "?" not in sas_uri:
        print("ERROR: datarefs response had no usable sasUri", file=sys.stderr)
        return 1
    sas_token = sas_uri.split("?", 1)[1]

    # Discover the safetensors subpath and build the az:// model URI.
    names = list_blob_names(sas_uri)
    subpath = discover_subpath(names)
    model_uri = f"az://{container}/{subpath}" if subpath else f"az://{container}"

    # Bring-your-own models carry an expected configuration digest; verify the bundle
    # matches it before the main container is allowed to start loading.
    expected_sha256 = os.environ.get("KAITO_MODEL_CONFIG_SHA256", "").strip()
    if expected_sha256:
        try:
            verify_bundle(sas_uri, names, subpath, expected_sha256)
        except ValueError as err:
            print(f"ERROR: {err}", file=sys.stderr)
            return EXIT_CONFIG_MISMATCH
        print(f"Model bundle verified against config.json sha256 {expected_sha256}")

    write_env_file(
        out_path,
        {
            "AZURE_STORAGE_SAS_TOKEN": sas_token,
            "AZURE_STORAGE_ACCOUNT_NAME": account,
            "STREAM_MODEL_URI": model_uri,
        },
    )
    print(f"SAS env file written to {out_path} (model_uri={model_uri})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
