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
import json
import os
import sys
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


def test_build_request_body_public():
    b = json.loads(fetch_sas.build_request_body("asset-1", "https://a/b"))
    assert b == {"blobUri": "https://a/b", "assetId": "asset-1"}, b


def test_build_request_body_byo():
    b = json.loads(fetch_sas.build_request_body("", "https://a/b"))
    assert b == {"blobUri": "https://a/b"}, b


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


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print(f"PASS {name}")
    print("all fetch_sas helper tests passed")
