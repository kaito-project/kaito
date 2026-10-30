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

"""Unit tests for kv_transfer_config injection."""

import argparse
import os
import sys
from pathlib import Path
from unittest.mock import patch

import pytest

# Add parent directory to sys.path for inference_api imports
parent_dir = str(Path(__file__).resolve().parent.parent)
sys.path.insert(0, parent_dir)

from vllm_kv_transfer import (  # noqa: E402, I001
    configure_kv_transfer_and_lmcache,
    record_lmcache_server_generation,
    set_kv_transfer_config_if_applicable,
)


def _make_args(**kwargs):
    """Create a minimal argparse.Namespace for testing."""
    defaults = {
        "kv_transfer_config": None,
        "kaito_kv_cache_cpu_memory_utilization": None,
        "tensor_parallel_size": 1,
        "device": None,
        "kv_cache_memory_bytes": None,
    }
    defaults.update(kwargs)
    return argparse.Namespace(**defaults)


class TestSetKvTransferConfig:
    """Tests for set_kv_transfer_config_if_applicable()."""

    def test_nixl_connector_when_role_is_prefill(self):
        """When KAITO_INFERENCE_ROLE=prefill, should set NixlConnector."""
        args = _make_args()
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "prefill"}):
            set_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config == {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
        }

    def test_nixl_connector_when_role_is_decode(self):
        """When KAITO_INFERENCE_ROLE=decode, should set NixlConnector."""
        args = _make_args()
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_kv_transfer_config_if_applicable(args)
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
            set_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config is None

    def test_no_managed_connector_without_sidecar_mode(self):
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {}, clear=True):
            os.environ.pop("KAITO_INFERENCE_ROLE", None)
            set_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config is None

    def test_lmcache_sidecar_uses_isolated_ipc_without_local_server(self):
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(
            os.environ,
            {"KAITO_LMCACHE_DEPLOYMENT_MODE": "sidecar"},
            clear=True,
        ):
            set_kv_transfer_config_if_applicable(args)

        assert args.kv_transfer_config == {
            "kv_connector": "LMCacheMPConnector",
            "kv_connector_module_path": "lmcache.integration.vllm.lmcache_mp_connector",
            "kv_role": "kv_both",
            "kv_connector_extra_config": {
                "lmcache.mp.host": "tcp://127.0.0.1",
                "lmcache.mp.port": 5555,
                "lmcache.mp.isolated_ipc": True,
            },
        }

    def test_nixl_not_overridden_by_offload(self):
        """When role is set AND offload enabled, NixlConnector should not be overwritten."""
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}, clear=True):
            set_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config["kv_connector"] == "NixlConnector"

    def test_user_provided_config_not_overridden(self):
        """When user provides kv_transfer_config, it should not be overridden."""
        user_config = {"kv_connector": "CustomConnector", "kv_role": "kv_both"}
        args = _make_args(kv_transfer_config=user_config)
        with patch.dict(os.environ, {"KAITO_INFERENCE_ROLE": "decode"}):
            set_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config == user_config


class TestLMCacheMPServer:
    def test_gpu_still_configures_and_synchronizes_managed_sidecar(self):
        args = _make_args(device="cuda", kaito_kv_cache_cpu_memory_utilization=0.5)
        with (
            patch.dict(
                os.environ, {"KAITO_LMCACHE_DEPLOYMENT_MODE": "sidecar"}, clear=True
            ),
            patch("vllm_kv_transfer.record_lmcache_server_generation") as generation,
        ):
            configure_kv_transfer_and_lmcache(args)
            generation.assert_called_once_with()
        assert args.kv_transfer_config["kv_connector"] == "LMCacheMPConnector"

    def test_record_sidecar_generation(self, tmp_path):
        server_generation = tmp_path / "server-generation"
        client_generation = tmp_path / "client-generation"
        server_generation.write_text("generation-1")

        with (
            patch(
                "vllm_kv_transfer.LMCACHE_SERVER_GENERATION_FILE",
                str(server_generation),
            ),
            patch(
                "vllm_kv_transfer.LMCACHE_CLIENT_GENERATION_FILE",
                str(client_generation),
            ),
        ):
            record_lmcache_server_generation(timeout_seconds=1)

        assert client_generation.read_text() == "generation-1"


class TestCPUDoesNotUseGPUOffload:
    @pytest.mark.parametrize(
        "device,target", [("cpu", ""), (None, "cpu"), ("auto", "CPU"), (" CPU ", "")]
    )
    @pytest.mark.parametrize("role", ["", "prefill", "decode"])
    @pytest.mark.parametrize("utilization", [None, 0, 0.5])
    def test_no_automatic_transfer_connector(self, device, target, role, utilization):
        args = _make_args(
            device=device, kaito_kv_cache_cpu_memory_utilization=utilization
        )
        with patch.dict(
            os.environ,
            {
                "VLLM_TARGET_DEVICE": target,
                "KAITO_LMCACHE_DEPLOYMENT_MODE": "sidecar",
                "KAITO_INFERENCE_ROLE": role,
            },
            clear=True,
        ):
            set_kv_transfer_config_if_applicable(args)
        assert args.kv_transfer_config is None

    @pytest.mark.parametrize("space", [None, "8"])
    @pytest.mark.parametrize("memory_bytes", [None, 8 * 1024**3])
    @pytest.mark.parametrize("device,target", [("cpu", ""), ("auto", "cpu")])
    def test_cpu_skips_sidecar_setup_and_preserves_native_cache(
        self, space, memory_bytes, device, target
    ):
        args = _make_args(
            device=device,
            kaito_kv_cache_cpu_memory_utilization=0.5,
            kv_cache_memory_bytes=memory_bytes,
        )
        env = {"KAITO_LMCACHE_DEPLOYMENT_MODE": "sidecar", "VLLM_TARGET_DEVICE": target}
        if space is not None:
            env["VLLM_CPU_KVCACHE_SPACE"] = space
        with (
            patch.dict(os.environ, env, clear=True),
            patch("vllm_kv_transfer.set_kv_transfer_config_if_applicable") as configure,
            patch("vllm_kv_transfer.record_lmcache_server_generation") as generation,
        ):
            configure_kv_transfer_and_lmcache(args)
            configure.assert_not_called()
            generation.assert_not_called()
            assert dict(os.environ) == env
        assert args.kv_cache_memory_bytes == memory_bytes
        assert args.kv_transfer_config is None

    @pytest.mark.parametrize(
        "connector", ["CustomConnector", "NixlConnector", "LMCacheMPConnector"]
    )
    @pytest.mark.parametrize(
        "configure",
        [set_kv_transfer_config_if_applicable, configure_kv_transfer_and_lmcache],
    )
    def test_cpu_does_not_rewrite_explicit_connector(self, connector, configure):
        custom = {"kv_connector": connector, "kv_role": "kv_both"}
        args = _make_args(device="cpu", kv_transfer_config=custom)
        with (
            patch.dict(
                os.environ, {"KAITO_LMCACHE_DEPLOYMENT_MODE": "sidecar"}, clear=True
            ),
            patch("vllm_kv_transfer.record_lmcache_server_generation") as generation,
        ):
            configure(args)
            generation.assert_not_called()
        assert args.kv_transfer_config is custom
