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

import argparse
import importlib
import os
import sys
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
inference_api = importlib.import_module("inference_api")
set_nixl_kv_transfer_config_if_applicable = (
    inference_api.set_nixl_kv_transfer_config_if_applicable
)


EXPECTED_NIXL_CONFIG = {
    "kv_connector": "NixlConnector",
    "kv_role": "kv_both",
    "kv_load_failure_policy": "fail",
}


class TestSetNixlKvTransferConfig:
    def _make_args(self, kv_transfer_config=None):
        return argparse.Namespace(kv_transfer_config=kv_transfer_config)

    def test_no_env_var_does_nothing(self):
        """Without KAITO_INFERENCE_ROLE, kv_transfer_config should not be set."""
        args = self._make_args()
        with mock.patch.dict(os.environ, {}, clear=True):
            set_nixl_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config is None

    def test_empty_env_var_does_nothing(self):
        args = self._make_args()
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": ""}):
            set_nixl_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config is None

    def test_invalid_role_does_nothing(self):
        args = self._make_args()
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "unknown"}):
            set_nixl_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config is None

    def test_prefill_role_injects_nixl_config(self):
        args = self._make_args()
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "prefill"}):
            set_nixl_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config == EXPECTED_NIXL_CONFIG

    def test_decode_role_injects_nixl_config(self):
        args = self._make_args()
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_nixl_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config == EXPECTED_NIXL_CONFIG

    def test_overrides_existing_lmcache_config(self):
        """NixlConnector should override LMCache config from KV cache offloading."""
        lmcache_config = {
            "kv_connector": "LMCacheConnectorV1",
            "kv_role": "kv_both",
        }
        args = self._make_args(kv_transfer_config=lmcache_config)
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "prefill"}):
            set_nixl_kv_transfer_config_if_applicable(
                args, user_provided_kv_config=False
            )
        assert args.kv_transfer_config == EXPECTED_NIXL_CONFIG

    def test_respects_user_provided_config(self):
        """User-provided kv-transfer-config from inference configmap is respected."""
        user_config = {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
        }
        args = self._make_args(kv_transfer_config=user_config)
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_nixl_kv_transfer_config_if_applicable(
                args, user_provided_kv_config=True
            )
        assert args.kv_transfer_config == user_config

    def test_user_provided_custom_config_preserved(self):
        """User-provided custom kv-transfer-config is preserved even if different."""
        user_config = {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
            "custom_field": "custom_value",
        }
        args = self._make_args(kv_transfer_config=user_config)
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "prefill"}):
            set_nixl_kv_transfer_config_if_applicable(
                args, user_provided_kv_config=True
            )
        assert args.kv_transfer_config == user_config

    def test_no_user_config_still_injects_nixl(self):
        """When user did not provide config, NixlConnector is still injected."""
        args = self._make_args()
        with mock.patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "prefill"}):
            set_nixl_kv_transfer_config_if_applicable(
                args, user_provided_kv_config=False
            )
        assert args.kv_transfer_config == EXPECTED_NIXL_CONFIG
