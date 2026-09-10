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

"""Unit tests for fetch_sas.py helpers and asset-prefetch startup.

NOTE: this file is under a Go package dir, so CI pytest globs (which target presets/)
do NOT run it automatically. Run manually during development:
    .venv/bin/python pkg/workspace/inference/modelstreaming/azure/fetch_sas_test.py
It stubs azure.identity so azure-identity need not be installed locally.
"""

import hashlib
import importlib.util
import io
import os
import subprocess
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

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
        names = fetch_sas.list_blobs("https://blob/c?sig=x")
        got = fetch_sas.discover_subpath(names)
    finally:
        fetch_sas.urllib.request.urlopen = orig
    # entity unescaped ('a&b') and the second page was fetched via the marker.
    assert got == "a&b", got


def _assert_subpath(expected):
    got = fetch_sas.discover_subpath(fetch_sas.list_blobs("https://blob/c?sig=x"))
    assert got == expected, f"got {got!r} want {expected!r}"


class _FakeResp(io.BytesIO):
    def __init__(self, data, headers=None):
        super().__init__(data.encode("utf-8") if isinstance(data, str) else data)
        self.headers = headers or {}


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


class TestAssetPrefetch(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.cache = self.root / "assets"
        self.model_uri = "az://c/model"
        self.sas_uri = "https://acct.blob.core.windows.net/c?sv=1&sig=a%2Bb"
        self.destination = (
            self.cache
            / "model_streamer"
            / hashlib.sha256(self.model_uri.encode()).hexdigest()[:8]
        )

    def download(self, names, model_uri=None, subpath="model"):
        fetch_sas.download_model_assets(
            self.sas_uri, model_uri or self.model_uri, subpath, names, str(self.cache)
        )

    def test_downloads_all_non_weight_assets_under_model_prefix(self):
        assets = [
            "config.json",
            "tokenizer.json",
            "tokenizer_config.json",
            "preprocessor_config.json",
            "tiktoken.model",
            "tokenization_custom.py",
            "merges.txt",
            "vocab.txt",
            "chat_template.jinja",
            "templates/chat.jinja",
            "model.safetensors.index.json",
        ]
        names = [f"model/{name}" for name in assets]
        names += [
            "model/pytorch_model.bin",
            "model/consolidated.safetensors",
            "model/weights.pt",
            "model/weights.pth",
            "model/weights.tensors",
        ]
        names += ["other/config.json", "model-other/config.json", "config.json"]
        with patch.object(
            fetch_sas.urllib.request,
            "urlopen",
            side_effect=lambda url, timeout: _FakeResp(b"asset"),
        ) as request:
            self.download(names)
        self.assertEqual(request.call_count, len(assets))
        for name in assets:
            self.assertEqual((self.destination / name).read_bytes(), b"asset")
        self.assertEqual(
            sorted(
                str(p.relative_to(self.destination))
                for p in self.destination.rglob("*")
                if p.is_file()
            ),
            sorted(assets),
        )

    def test_container_root_uses_exact_uri_hash(self):
        model_uri = "az://c"
        with patch.object(
            fetch_sas.urllib.request, "urlopen", return_value=_FakeResp("{}")
        ):
            self.download(["config.json", "model.safetensors"], model_uri, subpath="")
        destination = (
            self.cache
            / "model_streamer"
            / hashlib.sha256(model_uri.encode()).hexdigest()[:8]
        )
        self.assertEqual((destination / "config.json").read_text(), "{}")

    def test_blob_url_encodes_names_and_preserves_sas(self):
        name = "tokenizer a&b#c?d%2F.json"
        with patch.object(
            fetch_sas.urllib.request, "urlopen", return_value=_FakeResp("{}")
        ) as request:
            self.download([f"model/{name}"])
        request.assert_called_once_with(
            "https://acct.blob.core.windows.net/c/model/"
            "tokenizer%20a%26b%23c%3Fd%252F.json?sv=1&sig=a%2Bb",
            timeout=30,
        )
        self.assertTrue((self.destination / name).is_file())

    def test_rejects_unsafe_paths(self):
        names = [
            "model/../env",
            "model/sub/../../env",
            "model//config.json",
            "model/./config.json",
            "model/a\\b.json",
            "model/config\x00.json",
        ]
        for name in names:
            with (
                self.subTest(name=name),
                patch.object(fetch_sas.urllib.request, "urlopen") as request,
                self.assertRaisesRegex(ValueError, "Unsafe model asset path"),
            ):
                self.download([name])
            request.assert_not_called()

    def test_rejects_symlink_outside_cache(self):
        self.destination.mkdir(parents=True)
        (self.destination / "outside").symlink_to(self.root, target_is_directory=True)
        with (
            patch.object(fetch_sas.urllib.request, "urlopen") as request,
            self.assertRaisesRegex(ValueError, "escapes cache directory"),
        ):
            self.download(["model/outside/env"])
        request.assert_not_called()

    def test_failure_propagates_and_retry_redownloads(self):
        class InterruptedResponse(_FakeResp):
            def read(self, size=-1):
                if self.tell():
                    raise OSError("download interrupted")
                return super().read(size)

        names = ["model/config.json", "model/tokenizer.json"]
        with (
            patch.object(
                fetch_sas.urllib.request,
                "urlopen",
                side_effect=[_FakeResp("old config"), InterruptedResponse("partial")],
            ),
            self.assertRaisesRegex(OSError, "download interrupted"),
        ):
            self.download(names)
        self.assertEqual(
            list(self.destination.iterdir()), [self.destination / "config.json"]
        )
        with patch.object(
            fetch_sas.urllib.request,
            "urlopen",
            side_effect=[_FakeResp("new config"), _FakeResp("complete tokenizer")],
        ) as request:
            self.download(names)
        self.assertEqual(request.call_count, 2)
        self.assertEqual((self.destination / "config.json").read_text(), "new config")
        self.assertEqual(
            (self.destination / "tokenizer.json").read_text(), "complete tokenizer"
        )

    def test_truncated_response_is_not_published(self):
        with (
            patch.object(
                fetch_sas.urllib.request,
                "urlopen",
                return_value=_FakeResp("partial", {"Content-Length": "100"}),
            ),
            self.assertRaisesRegex(OSError, "Incomplete model asset download"),
        ):
            self.download(["model/config.json"])
        self.assertEqual(list(self.destination.iterdir()), [])

    def test_rejects_missing_assets(self):
        with (
            patch.object(fetch_sas.urllib.request, "urlopen") as request,
            self.assertRaisesRegex(ValueError, "No non-weight model assets"),
        ):
            self.download(["model/a.safetensors", "other/config.json"])
        request.assert_not_called()

    def test_listing_decodes_entities_and_paginates(self):
        pages = [
            _FakeResp(
                "<EnumerationResults><Blobs><Blob>"
                "<Name>model/chat&#32;template.jinja</Name>"
                "</Blob></Blobs><NextMarker>a+/=&amp;b</NextMarker></EnumerationResults>"
            ),
            _FakeResp(
                "<EnumerationResults><Blobs><Blob>"
                "<Name>model/a.safetensors</Name>"
                "</Blob></Blobs></EnumerationResults>"
            ),
        ]
        with patch.object(
            fetch_sas.urllib.request, "urlopen", side_effect=pages
        ) as request:
            names = fetch_sas.list_blobs(self.sas_uri)
        self.assertEqual(names, ["model/chat template.jinja", "model/a.safetensors"])
        self.assertTrue(request.call_args.args[0].endswith("&marker=a%2B%2F%3D%26b"))
        self.assertEqual(fetch_sas.discover_subpath(names), "model")

    def run_main(self):
        env_file = self.root / "env"
        env = {
            "STREAM_DATAREFS_URL": "https://a/models/m/versions/1",
            "STREAM_IDENTITY_CLIENT_ID": "test-client",
            "STREAM_SOURCE_TYPE": fetch_sas.SOURCE_BYO,
            "STREAM_ENV_FILE": str(env_file),
        }
        responses = [
            {"blobReference": {"blobUri": "https://acct.blob.core.windows.net/c"}},
            {"blobReference": {"credential": {"sasUri": self.sas_uri}}},
        ]
        with (
            patch.dict(os.environ, env),
            patch.object(fetch_sas, "WorkloadIdentityCredential"),
            patch.object(fetch_sas, "http_json", side_effect=responses),
        ):
            self.assertEqual(fetch_sas.main(), 0)
        return env_file

    def test_main_prefetches_before_exporting_cache_and_credentials(self):
        listing = (
            "<EnumerationResults><Blobs>"
            "<Blob><Name>model/a.safetensors</Name></Blob>"
            "<Blob><Name>model/config.json</Name></Blob>"
            "<Blob><Name>model/tokenizer.json</Name></Blob>"
            "</Blobs></EnumerationResults>"
        )

        def respond(url, timeout):
            self.assertFalse((self.root / "env").exists())
            if "comp=list" in url:
                return _FakeResp(listing)
            return _FakeResp("{}")

        with patch.object(
            fetch_sas.urllib.request, "urlopen", side_effect=respond
        ) as request:
            env_file = self.run_main()
        self.assertEqual(request.call_count, 3)  # one listing, two non-weight files
        self.assertTrue((self.destination / "config.json").is_file())
        self.assertTrue((self.destination / "tokenizer.json").is_file())
        wrapper = (
            Path(_here).parents[4]
            / "presets/workspace/inference/vllm/export_sas_token_for_streaming.sh"
        )
        # The real wrapper must export the cache settings to either Ray launch branch.
        result = subprocess.run(
            [
                "/bin/sh",
                str(wrapper),
                "/bin/sh",
                "-c",
                'printf "%s\\n" "$STREAM_MODEL_URI" "$VLLM_ASSETS_CACHE" '
                '"$VLLM_ASSETS_CACHE_MODEL_CLEAN" "$AZURE_STORAGE_SAS_TOKEN"',
            ],
            env={**os.environ, "STREAM_ENV_FILE": str(env_file)},
            check=True,
            capture_output=True,
            text=True,
        )
        self.assertEqual(
            result.stdout.splitlines(),
            [self.model_uri, str(self.cache), "0", "sv=1&sig=a%2Bb"],
        )

    def test_main_does_not_publish_env_on_download_failure(self):
        with (
            patch.object(
                fetch_sas,
                "list_blobs",
                return_value=["model/a.safetensors", "model/config.json"],
            ),
            patch.object(
                fetch_sas.urllib.request, "urlopen", side_effect=OSError("offline")
            ),
            self.assertRaisesRegex(OSError, "offline"),
        ):
            self.run_main()
        self.assertFalse((self.root / "env").exists())


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print(f"PASS {name}")
    result = unittest.TextTestRunner().run(
        unittest.defaultTestLoader.loadTestsFromTestCase(TestAssetPrefetch)
    )
    sys.exit(0 if result.wasSuccessful() else 1)
