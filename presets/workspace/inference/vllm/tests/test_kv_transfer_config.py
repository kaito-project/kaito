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

"""Unit tests for kv_transfer_config injection and KAITO_VLLM_PORT override."""

import argparse
import os
import sys
from pathlib import Path
from unittest.mock import patch

# Add parent directory to sys.path for inference_api imports
parent_dir = str(Path(__file__).resolve().parent.parent)
sys.path.append(parent_dir)

from inference_api import KAITOArgumentParser, set_kv_transfer_config_if_appliable  # noqa: E402


def _make_args(**kwargs):
    """Create a minimal argparse.Namespace for testing."""
    defaults = {
        "kv_transfer_config": None,
        "kaito_kv_cache_cpu_memory_utilization": None,
        "tensor_parallel_size": 1,
    }
    defaults.update(kwargs)
    return argparse.Namespace(**defaults)


class TestSetKvTransferConfig:
    """Tests for set_kv_transfer_config_if_appliable()."""

    def test_nixl_connector_when_role_is_prefill(self):
        """When KAITO_INFERENCE_ROLE=prefill, should set NixlConnector."""
        args = _make_args()
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "prefill"}):
            set_kv_transfer_config_if_appliable(args)
        assert args.kv_transfer_config == {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
        }

    def test_nixl_connector_when_role_is_decode(self):
        """When KAITO_INFERENCE_ROLE=decode, should set NixlConnector."""
        args = _make_args()
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_kv_transfer_config_if_appliable(args)
        assert args.kv_transfer_config == {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
        }

    def test_no_config_when_no_role_and_no_offload(self):
        """When no role and no CPU offload, kv_transfer_config stays None."""
        args = _make_args()
        with patch.dict(os.environ, {}, clear=True):
            os.environ.pop("KAITO_INFERENCE_ROLE", None)
            set_kv_transfer_config_if_appliable(args)
        assert args.kv_transfer_config is None

    def test_lmcache_default_when_offload_enabled_no_role(self):
        """When CPU offload enabled but no role, should default to LMCacheConnectorV1."""
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {}, clear=True):
            os.environ.pop("KAITO_INFERENCE_ROLE", None)
            set_kv_transfer_config_if_appliable(args)
        assert args.kv_transfer_config == {
            "kv_connector": "LMCacheConnectorV1",
            "kv_role": "kv_both",
        }

    def test_nixl_not_overridden_by_offload(self):
        """When role is set AND offload enabled, NixlConnector should not be overwritten."""
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_kv_transfer_config_if_appliable(args)
        assert args.kv_transfer_config["kv_connector"] == "NixlConnector"

    def test_user_provided_config_not_overridden(self):
        """When user provides kv_transfer_config, it should not be overridden."""
        user_config = {"kv_connector": "CustomConnector", "kv_role": "kv_both"}
        args = _make_args(kv_transfer_config=user_config)
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_kv_transfer_config_if_appliable(args)
        assert args.kv_transfer_config == user_config


class TestKaitoVllmPortOverride:
    """Tests for KAITO_VLLM_PORT env var overriding --port."""

    def test_port_override_via_env(self):
        """KAITO_VLLM_PORT should override CLI --port value."""
        with patch.dict(os.environ, {"KAITO_VLLM_PORT": "5001"}):
            parser = KAITOArgumentParser(description="test")
            args = parser.parse_args(["--port", "5000"])
        assert args.port == 5001

    def test_port_override_over_config_file(self, tmp_path):
        """KAITO_VLLM_PORT should override config file vllm.port."""
        config_file = tmp_path / "config.yaml"
        config_file.write_text("vllm:\n  port: 8000\n")
        with patch.dict(os.environ, {"KAITO_VLLM_PORT": "5001"}):
            parser = KAITOArgumentParser(description="test")
            args = parser.parse_args(["--kaito-config-file", str(config_file)])
        assert args.port == 5001

    def test_no_override_when_env_unset(self):
        """Without KAITO_VLLM_PORT, CLI --port should be used as-is."""
        with patch.dict(os.environ, {}, clear=True):
            os.environ.pop("KAITO_VLLM_PORT", None)
            parser = KAITOArgumentParser(description="test")
            args = parser.parse_args(["--port", "5000"])
        assert args.port == 5000
