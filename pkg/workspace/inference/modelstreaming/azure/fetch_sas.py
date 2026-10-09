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
  3. Derive the storage account, container, and selected model prefix from the blobUri.
  4. Mint a SAS at the mint endpoint with {blobUri[, assetId]} -> SAS token.
  5. List only the selected model prefix with the SAS and discover its safetensors
     subpath -> model streaming URI.
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
    KAITO_MODEL_CONFIG_SHA256 - expected SHA-256 of the RFC 8785 (JCS) canonical form of the
                                bundle's config.json. When set, the bundle is verified before
                                the model is loaded; when unset (preset and HuggingFace models)
                                verification is skipped.

Every stage logs its start, outcome, and duration to stderr; failures include the HTTP
status and response body. SAS signatures are redacted from all log output.
"""

import contextlib
import hashlib
import json
import logging
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.sax.saxutils

import rfc8785
from azure.identity import WorkloadIdentityCredential

log = logging.getLogger("fetch_sas")

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

REQUIRED_ENV = (
    "STREAM_DATAREFS_URL",
    "STREAM_IDENTITY_CLIENT_ID",
    "STREAM_SOURCE_TYPE",
    "STREAM_ENV_FILE",
)

TOTAL_STEPS = 6

# Upper bound on how much of an HTTP error body is logged; Azure error payloads
# are small, and the useful part (error code and message) comes first.
MAX_ERROR_BODY = 2048

_SIG_RE = re.compile(r"(sig=)[^&\s\"'<>]+", re.IGNORECASE)


def redact(text: str) -> str:
    """Mask SAS signatures anywhere in a log line, including tracebacks."""
    return _SIG_RE.sub(r"\1<redacted>", text)


def redact_url(url: str) -> str:
    """Replace the query of a SAS-bearing URL; other URLs are returned unchanged."""
    parts = urllib.parse.urlsplit(url)
    if "sig=" not in parts.query.lower():
        return url
    return urllib.parse.urlunsplit(
        (parts.scheme, parts.netloc, parts.path, "<redacted>", "")
    )


class _RedactingFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        return redact(super().format(record))


def configure_logging() -> None:
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(
        _RedactingFormatter(
            "%(asctime)s %(levelname)s fetch_sas: %(message)s",
            datefmt="%Y-%m-%dT%H:%M:%S%z",
        )
    )
    log.handlers[:] = [handler]
    log.setLevel(logging.INFO)
    log.propagate = False


def describe_error(err: BaseException) -> str:
    """Render an exception as one diagnostic line, keeping the HTTP status and
    response body that a bare traceback would discard."""
    if isinstance(err, urllib.error.HTTPError):
        try:
            body = err.read().decode("utf-8", errors="replace")
        except Exception:  # noqa: BLE001 - the body is best-effort context
            body = ""
        if len(body) > MAX_ERROR_BODY:
            body = body[:MAX_ERROR_BODY] + "...(truncated)"
        url = redact_url(err.geturl() or "")
        return (
            f"HTTP {err.code} {err.reason} from {url}: {body.strip() or '<empty body>'}"
        )
    if isinstance(err, urllib.error.URLError):
        return f"network error: {err.reason}"
    return f"{type(err).__name__}: {err}"


class StepError(Exception):
    """Wraps a failure that a step has already logged, so main does not log it twice."""

    def __init__(self, cause: BaseException):
        super().__init__(str(cause))
        self.cause = cause


@contextlib.contextmanager
def step(number: int, description: str):
    """Log the start, outcome, and duration of one stage of the script."""
    label = f"[{number}/{TOTAL_STEPS}] {description}"
    log.info("%s ...", label)
    start = time.monotonic()
    try:
        yield
    except Exception as err:
        elapsed = int((time.monotonic() - start) * 1000)
        log.error("%s failed (%dms): %s", label, elapsed, describe_error(err))
        raise StepError(err) from err
    log.info("%s ok (%dms)", label, int((time.monotonic() - start) * 1000))


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
    """Extract the model URI from supported model-resolve response shapes.

    Microsoft Foundry BYO responses return blobUri at the top level. Public responses
    nest the URI under properties.modelUri, while other BYO responses use
    blobReference.blobUri or blobReferenceForConsumption.blobUri.
    """
    if payload.get("blobUri"):
        return payload["blobUri"]
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


def account_container_and_prefix(blob_uri: str) -> "tuple[str, str, str]":
    """Parse the account, container, and model prefix from a resolved blob URI."""
    parts = urllib.parse.urlsplit(blob_uri)
    account = parts.netloc.split(".", 1)[0]
    container, _, prefix = urllib.parse.unquote(parts.path).strip("/").partition("/")
    return account, container, prefix.rstrip("/")


def list_blob_names(sas_uri: str, model_prefix: str = "") -> "list[str]":
    """List blob names under the selected model prefix via the container SAS.

    Pages through the full listing (Azure returns at most 5000 blobs per page plus a
    NextMarker) and unescapes XML entities in blob names so paths with '&' etc. are correct.
    """
    base = sas_uri + "&restype=container&comp=list&include=metadata"
    normalized_prefix = model_prefix.strip("/")
    if normalized_prefix:
        listing_prefix = normalized_prefix + "/"
        base += "&prefix=" + urllib.parse.quote(listing_prefix, safe="")
    names: list = []
    marker = ""
    page = 0
    while True:
        url = base + ("&marker=" + urllib.parse.quote(marker) if marker else "")
        with urllib.request.urlopen(url, timeout=30) as resp:
            body = resp.read().decode("utf-8", errors="replace")
        page_names = [
            xml.sax.saxutils.unescape(n)
            for n in re.findall(r"<Name>(.*?)</Name>", body, re.DOTALL)
        ]
        names.extend(page_names)
        page += 1
        m = re.search(r"<NextMarker>(.*?)</NextMarker>", body)
        marker = xml.sax.saxutils.unescape(m.group(1)) if m and m.group(1) else ""
        log.info(
            "listed page %d: %d blobs (more pages: %s)",
            page,
            len(page_names),
            bool(marker),
        )
        if not marker:
            break
    return names


def discover_subpath(names: "list[str]", model_prefix: str = "") -> str:
    """Return the safetensors directory within the selected model prefix."""
    normalized_prefix = model_prefix.strip("/")
    listing_prefix = normalized_prefix + "/" if normalized_prefix else ""
    scoped_names = [
        name for name in names if not listing_prefix or name.startswith(listing_prefix)
    ]
    safetensors = [name for name in scoped_names if name.endswith(".safetensors")]
    if not safetensors:
        return normalized_prefix
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


def _reject_constant(name: str) -> None:
    raise ValueError(f"config.json contains non-finite number {name}")


def _reject_duplicate_keys(pairs: list) -> dict:
    obj = {}
    for key, value in pairs:
        if key in obj:
            raise ValueError(f"config.json contains duplicate key {key!r}")
        obj[key] = value
    return obj


def canonical_config_digest(raw: bytes) -> str:
    """Return the SHA-256 of the RFC 8785 (JCS) canonical form of a config.json.

    Must match ConfigDigest in presets/workspace/models/custom.go; both are pinned by
    presets/workspace/models/testdata/config_digest_vectors.json. Integers are parsed as
    floats because JCS models every number as an IEEE 754 double (as Go does), and
    duplicate keys and NaN/Infinity are rejected because the Go side rejects them too.
    """
    try:
        obj = json.loads(
            raw,
            parse_int=float,
            parse_constant=_reject_constant,
            object_pairs_hook=_reject_duplicate_keys,
        )
        canonical = rfc8785.dumps(obj)
    except ValueError as err:  # rfc8785.CanonicalizationError is a ValueError
        raise ValueError(
            f"config.json is not valid canonicalizable JSON: {err}"
        ) from err
    return hashlib.sha256(canonical).hexdigest()


def verify_bundle(
    sas_uri: str, names: "list[str]", subpath: str, expected_sha256: str
) -> None:
    """Verify the bundle's config.json against the digest the deployment was sized for.

    Raises ValueError when config.json is missing or does not match. This is the one
    check worth blocking on: a mismatch means the source served weights the deployment
    was never sized or argument-rendered for. Anything else the bundle lacks (tokenizer,
    weight files) is left for the runtime to report when it loads, so this stays a
    verification of identity rather than a re-implementation of the loader's own checks.

    Only the small configuration blob is hashed (in its canonical form, so formatting
    differences from the operator's copy do not matter); the weights themselves are not,
    since the source is required to be versioned and write-once.
    """
    prefix = f"{subpath}/" if subpath else ""
    in_bundle = {n[len(prefix) :] for n in names if n.startswith(prefix)}

    if "config.json" not in in_bundle:
        raise ValueError(f"model bundle at '{prefix}' does not contain config.json")

    actual = canonical_config_digest(fetch_blob(sas_uri, prefix + "config.json"))
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
    configure_logging()
    try:
        return run()
    except StepError:
        return 1
    except Exception:
        log.exception("unexpected failure")
        return 1


def run() -> int:
    missing = [k for k in REQUIRED_ENV if not os.environ.get(k)]
    if missing:
        log.error("missing required environment variables: %s", ", ".join(missing))
        return 1
    datarefs_url = os.environ["STREAM_DATAREFS_URL"]
    client_id = os.environ["STREAM_IDENTITY_CLIENT_ID"]
    source_type = os.environ["STREAM_SOURCE_TYPE"]
    out_path = os.environ["STREAM_ENV_FILE"]
    expected_sha256 = os.environ.get("KAITO_MODEL_CONFIG_SHA256", "").strip()

    if source_type not in AUDIENCE_BY_TYPE:
        log.error(
            "STREAM_SOURCE_TYPE must be one of %s, got %r",
            sorted(AUDIENCE_BY_TYPE),
            source_type,
        )
        return 1
    audience = AUDIENCE_BY_TYPE[source_type]
    resolve_url, mint_url = derive_urls(datarefs_url, source_type)
    log.info(
        "starting: source_type=%s client_id=%s resolve_url=%s mint_url=%s "
        "env_file=%s config_digest=%s",
        source_type,
        client_id,
        resolve_url,
        mint_url,
        out_path,
        expected_sha256 or "<none>",
    )

    with step(1, f"acquiring workload identity token (audience {audience})"):
        cred = WorkloadIdentityCredential(client_id=client_id)
        access = cred.get_token(f"{audience}/.default")
        token = access.token
        log.info(
            "token acquired, expires at %s",
            time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(access.expires_on)),
        )

    with step(2, f"resolving model at {resolve_url}"):
        model = http_json(resolve_url, token)
        blob_uri = extract_blob_uri(model)
        if not blob_uri:
            raise ValueError(
                "model resolve response had no blobUri "
                f"(top-level keys: {sorted(model)})"
            )
        asset_id = model.get("id", "") if source_type == SOURCE_PUBLIC else ""
        account, container, model_prefix = account_container_and_prefix(blob_uri)
        log.info(
            "blobUri=%s account=%s container=%s prefix=%s assetId=%s",
            redact_url(blob_uri),
            account,
            container,
            model_prefix or "<none>",
            asset_id or "<none>",
        )

    with step(3, f"minting SAS at {mint_url}"):
        body = {"blobUri": blob_uri}
        if asset_id:
            body["assetId"] = asset_id
        mint = http_json(mint_url, token, json.dumps(body).encode())
        sas_uri = extract_sas_uri(mint)
        if not sas_uri or "?" not in sas_uri:
            raise ValueError(
                f"mint response had no usable sasUri (top-level keys: {sorted(mint)})"
            )
        sas_token = sas_uri.split("?", 1)[1]
        sas_params = urllib.parse.parse_qs(sas_token)
        log.info(
            "SAS minted for %s: permissions=%s expires=%s",
            redact_url(sas_uri),
            sas_params.get("sp", ["?"])[0],
            sas_params.get("se", ["?"])[0],
        )

    with step(4, f"listing model prefix in container {container}"):
        # The credentials endpoint returns a container-level SAS that may also cover
        # sibling models or versions, so keep discovery scoped to the resolved path.
        names = list_blob_names(sas_uri, model_prefix)
        subpath = discover_subpath(names, model_prefix)
        safetensors = sum(1 for n in names if n.endswith(".safetensors"))
        model_uri = f"az://{container}/{subpath}" if subpath else f"az://{container}"
        log.info(
            "found %d blobs, %d safetensors; subpath=%r model_uri=%s",
            len(names),
            safetensors,
            subpath,
            model_uri,
        )
        if not safetensors:
            log.warning(
                "no .safetensors files found in container %s; the runtime will "
                "likely fail to load weights from %s",
                container,
                model_uri,
            )

    # Bring-your-own models carry an expected configuration digest; verify the bundle
    # matches it before the main container is allowed to start loading.
    if expected_sha256:
        try:
            with step(5, f"verifying config.json against sha256 {expected_sha256}"):
                verify_bundle(sas_uri, names, subpath, expected_sha256)
        except StepError as err:
            if isinstance(err.cause, ValueError):
                return EXIT_CONFIG_MISMATCH
            raise
    else:
        log.info(
            "[5/%d] verifying config.json skipped (KAITO_MODEL_CONFIG_SHA256 unset)",
            TOTAL_STEPS,
        )

    with step(6, f"writing env file {out_path}"):
        write_env_file(
            out_path,
            {
                "AZURE_STORAGE_SAS_TOKEN": sas_token,
                "AZURE_STORAGE_ACCOUNT_NAME": account,
                "STREAM_MODEL_URI": model_uri,
            },
        )
    log.info("done: model_uri=%s", model_uri)
    return 0


if __name__ == "__main__":
    sys.exit(main())
