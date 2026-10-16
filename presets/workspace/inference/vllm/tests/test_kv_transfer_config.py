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

# Add parent directory to sys.path for inference_api imports
parent_dir = str(Path(__file__).resolve().parent.parent)
sys.path.insert(0, parent_dir)

from inference_api import (  # noqa: E402, I001
    _cpu_kv_cache_available_memory_gib,
    configure_cpu_kv_cache_space_if_applicable,
)
from vllm_kv_transfer import (  # noqa: E402, I001
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


class TestConfigureCPUKVCacheSpace:
    def test_sets_cpu_kv_cache_space_from_available_memory(self):
        args = _make_args(
            device="cpu",
            kaito_kv_cache_cpu_memory_utilization=0.5,
            tensor_parallel_size=2,
        )
        with (
            patch.dict(os.environ, {}, clear=True),
            patch("inference_api.psutil.virtual_memory") as vm,
            patch("inference_api._container_remaining_memory_bytes", return_value=None),
        ):
            vm.return_value = argparse.Namespace(available=64 * 1024**3)
            configure_cpu_kv_cache_space_if_applicable(args)
            assert os.environ["VLLM_CPU_KVCACHE_SPACE"] == "16"

    def test_caps_cpu_kv_cache_space_by_container_budget(self):
        args = _make_args(
            device="cpu",
            kaito_kv_cache_cpu_memory_utilization=0.5,
            tensor_parallel_size=1,
        )
        with (
            patch.dict(os.environ, {}, clear=True),
            patch("inference_api.psutil.virtual_memory") as vm,
            patch(
                "inference_api._container_remaining_memory_bytes",
                return_value=8 * 1024**3,
            ),
        ):
            vm.return_value = argparse.Namespace(available=64 * 1024**3)
            configure_cpu_kv_cache_space_if_applicable(args)
            assert os.environ["VLLM_CPU_KVCACHE_SPACE"] == "3"

    def test_respects_explicit_cpu_kv_cache_override(self):
        args = _make_args(device="cpu", kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {"VLLM_CPU_KVCACHE_SPACE": "8"}, clear=True):
            configure_cpu_kv_cache_space_if_applicable(args)
            assert os.environ["VLLM_CPU_KVCACHE_SPACE"] == "8"

    def test_skips_non_cpu_runtime(self):
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {}, clear=True):
            configure_cpu_kv_cache_space_if_applicable(args)
            assert "VLLM_CPU_KVCACHE_SPACE" not in os.environ


class TestCPUKVCacheMemoryBudget:
    def test_prefers_host_available_memory_without_cgroup_limit(self):
        with (
            patch("inference_api.psutil.virtual_memory") as vm,
            patch("inference_api._container_remaining_memory_bytes", return_value=None),
        ):
            vm.return_value = argparse.Namespace(available=10 * 1024**3)
            assert _cpu_kv_cache_available_memory_gib() == 10

    def test_reserves_headroom_inside_container_budget(self):
        with (
            patch("inference_api.psutil.virtual_memory") as vm,
            patch(
                "inference_api._container_remaining_memory_bytes",
                return_value=8 * 1024**3,
            ),
        ):
            vm.return_value = argparse.Namespace(available=64 * 1024**3)
            assert _cpu_kv_cache_available_memory_gib() == 6


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
