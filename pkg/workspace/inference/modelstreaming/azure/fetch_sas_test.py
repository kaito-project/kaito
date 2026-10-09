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

"""Unit tests for fetch_sas.py pure helpers.

NOTE: this file is under a Go package dir, so CI pytest globs (which target presets/)
do NOT run it automatically. Run manually during development:
    python3 pkg/workspace/inference/modelstreaming/azure/fetch_sas_test.py
It stubs azure.identity so azure-identity need not be installed locally; rfc8785 must be
installed (pip install rfc8785==0.1.4, matching rfc8785Version in sasblob.go).
"""

import importlib.util
import json
import os
import sys
import tempfile
import types

# Stub azure.identity BEFORE loading fetch_sas (its module-level import would otherwise fail).
_azure = types.ModuleType("azure")
_identity = types.ModuleType("azure.identity")
_identity.WorkloadIdentityCredential = object
_identity.DefaultAzureCredential = object
sys.modules.setdefault("azure", _azure)
sys.modules["azure.identity"] = _identity

_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "fetch_sas", os.path.join(_here, "fetch_sas.py")
)
fetch_sas = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(fetch_sas)


def test_derive_urls_byo_appends_credentials():
    # byo input is the BASE model URL (no /credentials); mint appends it, resolve is the base.
    base = "https://a.services.ai.azure.com/api/projects/p/models/m/versions/1?api-version=2025-11-15-preview"
    resolve_url, mint_url = fetch_sas.derive_urls(base, fetch_sas.SOURCE_BYO)
    assert resolve_url == base, resolve_url
    assert mint_url == (
        "https://a.services.ai.azure.com/api/projects/p/models/m/versions/1/credentials?api-version=2025-11-15-preview"
    ), mint_url


def test_derive_urls_public_swaps_datarefs_for_models():
    # public input is the datarefs (mint) URL; resolve swaps /datarefs/ -> /models/.
    mint = "https://mfep/mferp/managementfrontend/registries/r/datarefs/m/versions/1?api-version=2021-10-01-dataplanepreview"
    resolve_url, mint_url = fetch_sas.derive_urls(mint, fetch_sas.SOURCE_PUBLIC)
    assert mint_url == mint, mint_url
    assert resolve_url == (
        "https://mfep/mferp/managementfrontend/registries/r/models/m/versions/1?api-version=2021-10-01-dataplanepreview"
    ), resolve_url


def test_derive_urls_byo_rejects_credentials_suffix():
    # byo must NOT include /credentials (RP passes the base).
    try:
        fetch_sas.derive_urls(
            "https://a/models/m/versions/1/credentials", fetch_sas.SOURCE_BYO
        )
    except ValueError:
        return
    raise AssertionError("expected ValueError for byo URL that includes /credentials")


def test_derive_urls_public_requires_datarefs_segment():
    try:
        fetch_sas.derive_urls(
            "https://a/registries/r/models/m", fetch_sas.SOURCE_PUBLIC
        )
    except ValueError:
        return
    raise AssertionError("expected ValueError for public URL without /datarefs/")


def test_extract_blob_uri_public():
    payload = {
        "properties": {"modelUri": "https://acct.blob.core.windows.net/c/prefix"}
    }
    assert (
        fetch_sas.extract_blob_uri(payload)
        == "https://acct.blob.core.windows.net/c/prefix"
    )


def test_extract_blob_uri_foundry_byo_top_level():
    payload = {
        "id": "azureai://accounts/a/projects/p/models/m/versions/1",
        "name": "m",
        "version": "1",
        "blobUri": "https://acct.blob.core.windows.net/c/foundry",
    }
    assert (
        fetch_sas.extract_blob_uri(payload)
        == "https://acct.blob.core.windows.net/c/foundry"
    )


def test_extract_blob_uri_top_level_takes_precedence():
    payload = {
        "blobUri": "https://acct.blob.core.windows.net/c/top-level",
        "properties": {"modelUri": "https://acct.blob.core.windows.net/c/properties"},
        "blobReference": {"blobUri": "https://acct.blob.core.windows.net/c/reference"},
    }
    assert (
        fetch_sas.extract_blob_uri(payload)
        == "https://acct.blob.core.windows.net/c/top-level"
    )


def test_extract_blob_uri_byo():
    payload = {"blobReference": {"blobUri": "https://acct.blob.core.windows.net/c"}}
    assert fetch_sas.extract_blob_uri(payload) == "https://acct.blob.core.windows.net/c"


def test_extract_blob_uri_missing():
    assert fetch_sas.extract_blob_uri({}) == ""


def test_extract_sas_uri_public_key():
    payload = {
        "blobReferenceForConsumption": {"credential": {"sasUri": "https://blob?sig=x"}}
    }
    assert fetch_sas.extract_sas_uri(payload) == "https://blob?sig=x"


def test_extract_sas_uri_byo_key():
    payload = {"blobReference": {"credential": {"sasUri": "https://blob?sig=y"}}}
    assert fetch_sas.extract_sas_uri(payload) == "https://blob?sig=y"


def test_extract_sas_uri_missing():
    assert fetch_sas.extract_sas_uri({}) == ""


def test_account_container_and_prefix_public():
    payload = {
        "properties": {
            "modelUri": "https://sacae6.blob.core.windows.net/public-models/qwen/version-2/"
        }
    }
    account, container, prefix = fetch_sas.account_container_and_prefix(
        fetch_sas.extract_blob_uri(payload)
    )
    assert account == "sacae6", account
    assert container == "public-models", container
    assert prefix == "qwen/version-2", prefix


def test_account_container_and_prefix_foundry_byo():
    payload = {
        "blobUri": "https://sacae6.blob.core.windows.net/private-models/projects/p/models/qwen/version-1"
    }
    account, container, prefix = fetch_sas.account_container_and_prefix(
        fetch_sas.extract_blob_uri(payload)
    )
    assert account == "sacae6", account
    assert container == "private-models", container
    assert prefix == "projects/p/models/qwen/version-1", prefix


def test_list_blob_names_parses_names():
    xml = (
        "<EnumerationResults><Blobs>"
        "<Blob><Name>model/a.safetensors</Name></Blob>"
        "<Blob><Name>model/config.json</Name></Blob>"
        "</Blobs></EnumerationResults>"
    )
    names = _with_urlopen(
        lambda url, timeout=30: _FakeResp(xml),
        lambda: fetch_sas.list_blob_names("https://blob/c?sig=x"),
    )
    assert names == ["model/a.safetensors", "model/config.json"], names


def test_list_blob_names_scopes_listing_to_model_prefix():
    xml = (
        "<EnumerationResults><Blobs>"
        "<Blob><Name>models/qwen/v1/model.safetensors</Name></Blob>"
        "</Blobs></EnumerationResults>"
    )
    requested_urls = []

    def responder(url, timeout=30):
        requested_urls.append(url)
        return _FakeResp(xml)

    names = _with_urlopen(
        responder,
        lambda: fetch_sas.list_blob_names("https://blob/c?sig=x", "models/qwen/v1"),
    )
    assert names == ["models/qwen/v1/model.safetensors"], names
    assert len(requested_urls) == 1, requested_urls
    assert "prefix=models%2Fqwen%2Fv1%2F" in requested_urls[0], requested_urls[0]


def test_list_blob_names_paginates_and_unescapes():
    # First page carries a NextMarker; the safetensors blob (with an '&amp;'
    # entity) only appears on page 2, so both pages must be fetched and unescaped.
    page1 = (
        "<EnumerationResults><Blobs>"
        "<Blob><Name>a&amp;b/config.json</Name></Blob>"
        "</Blobs><NextMarker>tok2</NextMarker></EnumerationResults>"
    )
    page2 = (
        "<EnumerationResults><Blobs>"
        "<Blob><Name>a&amp;b/model.safetensors</Name></Blob>"
        "</Blobs></EnumerationResults>"
    )
    pages = [page1, page2]
    names = _with_urlopen(
        lambda url, timeout=30: _FakeResp(pages.pop(0)),
        lambda: fetch_sas.list_blob_names("https://blob/c?sig=x"),
    )
    assert names == ["a&b/config.json", "a&b/model.safetensors"], names
    # The unescaped names feed discover_subpath, which finds the shared prefix.
    assert fetch_sas.discover_subpath(names) == "a&b"


def test_discover_subpath_nested():
    names = [
        "mlflow_model_folder/data/model/a.safetensors",
        "mlflow_model_folder/data/model/b.safetensors",
        "mlflow_model_folder/config.json",
    ]
    assert fetch_sas.discover_subpath(names) == "mlflow_model_folder/data/model", names


def test_discover_subpath_root():
    assert fetch_sas.discover_subpath(["a.safetensors", "b.safetensors"]) == ""


def test_discover_subpath_none():
    assert fetch_sas.discover_subpath(["config.json"]) == ""


def test_discover_subpath_ignores_sibling_models_outside_prefix():
    names = [
        "models/qwen/v1/weights/a.safetensors",
        "models/qwen/v1/weights/b.safetensors",
        "models/llama/v1/model.safetensors",
    ]
    assert (
        fetch_sas.discover_subpath(names, "models/qwen/v1") == "models/qwen/v1/weights"
    )


def test_discover_subpath_preserves_prefix_without_safetensors():
    names = ["models/qwen/v1/config.json", "models/qwen/v1/tokenizer.json"]
    assert fetch_sas.discover_subpath(names, "models/qwen/v1") == "models/qwen/v1"


def test_blob_url_inserts_path_before_query():
    assert (
        fetch_sas.blob_url(
            "https://acct.blob.core.windows.net/c?sig=x", "sub/config.json"
        )
        == "https://acct.blob.core.windows.net/c/sub/config.json?sig=x"
    )


def test_blob_url_without_query():
    assert (
        fetch_sas.blob_url("https://acct.blob.core.windows.net/c", "config.json")
        == "https://acct.blob.core.windows.net/c/config.json"
    )


_VECTORS_PATH = os.path.join(
    _here,
    "..",
    "..",
    "..",
    "..",
    "..",
    "presets",
    "workspace",
    "models",
    "testdata",
    "config_digest_vectors.json",
)


def _load_vectors():
    with open(_VECTORS_PATH, encoding="utf-8") as f:
        return json.load(f)


def test_canonical_config_digest_shared_vectors():
    # The same vectors pin models.ConfigDigest in Go, so both sides agree byte-for-byte.
    vectors = _load_vectors()
    assert vectors["equivalent"] and vectors["invalid"]
    for group in vectors["equivalent"]:
        for raw in group["inputs"]:
            got = fetch_sas.canonical_config_digest(raw.encode("utf-8"))
            assert got == group["sha256"], (group["name"], raw, got)
    for case in vectors["invalid"]:
        _expect_valueerror(
            "config.json",
            lambda: fetch_sas.canonical_config_digest(case["input"].encode("utf-8")),
        )


def test_verify_bundle_accepts_matching_config():
    # The operator hashed a reserialized copy (0.0 -> 0, keys reordered, compact); the
    # bundle holds the original pretty-printed file. They must still verify.
    operator_copy = '{"attention_dropout":0,"architectures":["LlamaForCausalLM"]}'
    bundle_config = (
        '{\n  "architectures": ["LlamaForCausalLM"],\n  "attention_dropout": 0.0\n}\n'
    )
    sha = fetch_sas.canonical_config_digest(operator_copy.encode("utf-8"))
    names = ["model/config.json", "model/model.safetensors"]
    # No exception means the bundle's config.json matched what was sized for.
    _with_urlopen(
        lambda url, timeout=60: _FakeResp(bundle_config),
        lambda: fetch_sas.verify_bundle("https://blob/c?sig=x", names, "model", sha),
    )


def test_verify_bundle_rejects_invalid_config_json():
    _with_urlopen(
        lambda url, timeout=60: _FakeResp('{"a": 1, "a": 2}'),
        lambda: _expect_valueerror(
            "duplicate key",
            lambda: fetch_sas.verify_bundle(
                "https://blob/c?sig=x", ["config.json"], "", "0" * 64
            ),
        ),
    )


def test_verify_bundle_rejects_missing_config():
    # config.json absence is caught before any fetch is attempted.
    _expect_valueerror(
        "config.json",
        lambda: fetch_sas.verify_bundle(
            "https://blob/c?sig=x", ["model/model.safetensors"], "model", "deadbeef"
        ),
    )


def test_verify_bundle_rejects_mismatched_config():
    config = '{"architectures": ["LlamaForCausalLM"]}'
    _with_urlopen(
        lambda url, timeout=60: _FakeResp(config),
        lambda: _expect_valueerror(
            "does not match",
            lambda: fetch_sas.verify_bundle(
                "https://blob/c?sig=x", ["config.json"], "", "0" * 64
            ),
        ),
    )


class _FakeResp:
    def __init__(self, data):
        self._data = data

    def read(self):
        return self._data.encode("utf-8")

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


def _with_urlopen(responder, fn):
    """Run fn with urllib.request.urlopen replaced by responder, then restore it."""
    orig = fetch_sas.urllib.request.urlopen
    fetch_sas.urllib.request.urlopen = responder
    try:
        return fn()
    finally:
        fetch_sas.urllib.request.urlopen = orig


def _expect_valueerror(substr, fn):
    try:
        fn()
    except ValueError as e:
        assert substr in str(e), f"{substr!r} not in {e!r}"
        return
    raise AssertionError(f"expected ValueError containing {substr!r}")


def test_write_env_file():
    with tempfile.TemporaryDirectory() as d:
        out = os.path.join(d, "sub", "env")
        fetch_sas.write_env_file(
            out,
            {
                "AZURE_STORAGE_SAS_TOKEN": "sv=1&sig=ab'cd",
                "AZURE_STORAGE_ACCOUNT_NAME": "acct",
                "STREAM_MODEL_URI": "az://c/sub",
            },
        )
        with open(out, encoding="utf-8") as f:
            content = f.read()
    assert "AZURE_STORAGE_ACCOUNT_NAME='acct'\n" in content, content
    assert "STREAM_MODEL_URI='az://c/sub'\n" in content, content
    # single quote in the token value is shell-escaped
    assert "sv=1&sig=ab'\\''cd" in content, content


def test_redact_masks_signature_everywhere():
    line = "GET https://a.blob.core.windows.net/c?sv=1&sig=SECRET%2F&se=x failed; sig=OTHER"
    out = fetch_sas.redact(line)
    assert "SECRET" not in out and "OTHER" not in out, out
    assert "se=x" in out, out


def test_redact_url_only_touches_sas_urls():
    assert (
        fetch_sas.redact_url("https://a.blob.core.windows.net/c/x?sv=1&sig=abc")
        == "https://a.blob.core.windows.net/c/x?<redacted>"
    )
    plain = "https://a/models/m/versions/1?api-version=2025-11-15-preview"
    assert fetch_sas.redact_url(plain) == plain


def test_describe_error_includes_http_status_and_body():
    import io
    import urllib.error

    err = urllib.error.HTTPError(
        "https://a.blob.core.windows.net/c?sig=abc",
        403,
        "Forbidden",
        {},
        io.BytesIO(b'{"error":{"code":"AuthorizationFailed"}}'),
    )
    msg = fetch_sas.describe_error(err)
    assert "HTTP 403" in msg and "AuthorizationFailed" in msg, msg
    assert "abc" not in msg, msg


def test_describe_error_truncates_large_body():
    import io
    import urllib.error

    body = b"x" * (fetch_sas.MAX_ERROR_BODY + 100)
    err = urllib.error.HTTPError("https://a/m", 500, "err", {}, io.BytesIO(body))
    assert "...(truncated)" in fetch_sas.describe_error(err)


def test_step_logs_and_wraps_failure():
    import logging

    records = []

    class _Capture(logging.Handler):
        def emit(self, record):
            records.append(fetch_sas.redact(record.getMessage()))

    fetch_sas.configure_logging()
    fetch_sas.log.addHandler(_Capture())
    try:
        with fetch_sas.step(1, "ok step"):
            pass
        try:
            with fetch_sas.step(2, "bad step"):
                raise ValueError("boom sig=leak")
        except fetch_sas.StepError as err:
            assert isinstance(err.cause, ValueError)
        else:
            raise AssertionError("expected StepError")
    finally:
        fetch_sas.configure_logging()
    joined = "\n".join(records)
    assert "ok step ok (" in joined, joined
    assert "bad step failed (" in joined and "boom" in joined, joined
    assert "leak" not in joined, joined


def test_main_reports_missing_env():
    saved = {k: os.environ.pop(k, None) for k in fetch_sas.REQUIRED_ENV}
    try:
        assert fetch_sas.main() == 1
    finally:
        for k, v in saved.items():
            if v is not None:
                os.environ[k] = v


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print(f"PASS {name}")
    print("all fetch_sas helper tests passed")
