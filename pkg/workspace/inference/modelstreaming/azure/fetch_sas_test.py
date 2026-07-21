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
It stubs azure.identity so azure-identity need not be installed locally.
"""

import importlib.util
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


def test_resolve_url_byo_strips_credentials():
    url = "https://a.services.ai.azure.com/api/projects/p/models/m/versions/1/credentials?api-version=2025-11-15-preview"
    got = fetch_sas.resolve_url_from_datarefs(url, fetch_sas.SOURCE_BYO)
    assert got == (
        "https://a.services.ai.azure.com/api/projects/p/models/m/versions/1?api-version=2025-11-15-preview"
    ), got


def test_resolve_url_public_swaps_datarefs_for_models():
    url = "https://mfep/mferp/managementfrontend/registries/r/datarefs/m/versions/1?api-version=2021-10-01-dataplanepreview"
    got = fetch_sas.resolve_url_from_datarefs(url, fetch_sas.SOURCE_PUBLIC)
    assert got == (
        "https://mfep/mferp/managementfrontend/registries/r/models/m/versions/1?api-version=2021-10-01-dataplanepreview"
    ), got


def test_resolve_url_byo_requires_credentials_suffix():
    try:
        fetch_sas.resolve_url_from_datarefs(
            "https://a/models/m/versions/1", fetch_sas.SOURCE_BYO
        )
    except ValueError:
        return
    raise AssertionError("expected ValueError for BYO URL without /credentials")


def test_resolve_url_public_requires_datarefs_segment():
    try:
        fetch_sas.resolve_url_from_datarefs(
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


def test_account_and_container():
    account, container = fetch_sas.account_and_container(
        "https://sacae6.blob.core.windows.net/private-mo-abc/sub/dir"
    )
    assert account == "sacae6", account
    assert container == "private-mo-abc", container


def test_discover_subpath_nested():
    xml = (
        "<EnumerationResults><Blobs>"
        "<Blob><Name>mlflow_model_folder/data/model/a.safetensors</Name></Blob>"
        "<Blob><Name>mlflow_model_folder/data/model/b.safetensors</Name></Blob>"
        "<Blob><Name>mlflow_model_folder/config.json</Name></Blob>"
        "</Blobs></EnumerationResults>"
    )
    _with_stub_urlopen(xml, lambda: _assert_subpath("mlflow_model_folder/data/model"))


def test_discover_subpath_root():
    xml = (
        "<EnumerationResults><Blobs>"
        "<Blob><Name>a.safetensors</Name></Blob>"
        "<Blob><Name>b.safetensors</Name></Blob>"
        "</Blobs></EnumerationResults>"
    )
    _with_stub_urlopen(xml, lambda: _assert_subpath(""))


def test_discover_subpath_none():
    xml = "<EnumerationResults><Blobs><Blob><Name>config.json</Name></Blob></Blobs></EnumerationResults>"
    _with_stub_urlopen(xml, lambda: _assert_subpath(""))


def test_discover_subpath_paginates_and_unescapes():
    # First page carries a NextMarker; safetensors (with an '&amp;' entity) only appear on page 2.
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
    orig = fetch_sas.urllib.request.urlopen
    fetch_sas.urllib.request.urlopen = lambda url, timeout=30: _FakeResp(pages.pop(0))
    try:
        got = fetch_sas.discover_subpath("https://blob/c?sig=x")
    finally:
        fetch_sas.urllib.request.urlopen = orig
    # entity unescaped ('a&b') and the second page was fetched via the marker.
    assert got == "a&b", got


def _assert_subpath(expected):
    got = fetch_sas.discover_subpath("https://blob/c?sig=x")
    assert got == expected, f"got {got!r} want {expected!r}"


class _FakeResp:
    def __init__(self, data):
        self._data = data

    def read(self):
        return self._data.encode("utf-8")

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


def _with_stub_urlopen(xml, fn):
    orig = fetch_sas.urllib.request.urlopen
    fetch_sas.urllib.request.urlopen = lambda url, timeout=30: _FakeResp(xml)
    try:
        fn()
    finally:
        fetch_sas.urllib.request.urlopen = orig


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


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print(f"PASS {name}")
    print("all fetch_sas helper tests passed")
