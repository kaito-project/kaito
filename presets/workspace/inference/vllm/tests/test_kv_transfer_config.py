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
from unittest.mock import MagicMock, patch

# Add parent directory to sys.path for inference_api imports
parent_dir = str(Path(__file__).resolve().parent.parent)
sys.path.insert(0, parent_dir)

from inference_api import (  # noqa: E402, I001
    set_kv_transfer_config_if_applicable,
    start_lmcache_mp_server,
    stop_lmcache_mp_server,
)


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

    def test_lmcache_default_when_offload_enabled_no_role(self):
        """When CPU offload is enabled, use the local LMCache MP connector."""
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        with patch.dict(os.environ, {}, clear=True):
            os.environ.pop("KAITO_INFERENCE_ROLE", None)
            use_local_server = set_kv_transfer_config_if_applicable(args)
        assert use_local_server is True
        assert args.kv_transfer_config == {
            "kv_connector": "LMCacheMPConnector",
            "kv_role": "kv_both",
            "kv_connector_extra_config": {
                "lmcache.mp.host": "tcp://127.0.0.1",
                "lmcache.mp.port": 5555,
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
            use_local_server = set_kv_transfer_config_if_applicable(args)
        assert use_local_server is False
        assert args.kv_transfer_config == user_config


class TestLMCacheMPServer:
    """Tests for the local LMCache MP server lifecycle."""

    def test_start_waits_for_server(self):
        args = _make_args(kaito_kv_cache_cpu_memory_utilization=0.5)
        memory = argparse.Namespace(total=100 * 1024**3, used=20 * 1024**3)
        process = MagicMock()
        process.poll.return_value = None

        with (
            patch("inference_api.psutil.virtual_memory", return_value=memory),
            patch("inference_api.subprocess.Popen", return_value=process) as popen,
            patch("inference_api.socket.create_connection"),
        ):
            assert start_lmcache_mp_server(args) is process

        popen.assert_called_once_with(
            [
                "lmcache",
                "server",
                "--host",
                "127.0.0.1",
                "--port",
                "5555",
                "--chunk-size",
                "256",
                "--l1-use-lazy",
                "--l1-init-size-gb",
                "1",
                "--l1-size-gb",
                "40.0",
                "--eviction-policy",
                "LRU",
            ]
        )

    def test_start_scales_l1_size_by_tensor_parallel_size(self):
        args = _make_args(
            kaito_kv_cache_cpu_memory_utilization=0.5,
            tensor_parallel_size=2,
        )
        memory = argparse.Namespace(total=500 * 1024**3, used=50 * 1024**3)
        process = MagicMock()
        process.poll.return_value = None

        with (
            patch("inference_api.psutil.virtual_memory", return_value=memory),
            patch("inference_api.subprocess.Popen", return_value=process) as popen,
            patch("inference_api.socket.create_connection"),
        ):
            assert start_lmcache_mp_server(args) is process

        command = popen.call_args.args[0]
        assert command[command.index("--l1-init-size-gb") + 1] == "1"
        assert command[command.index("--l1-size-gb") + 1] == "112.5"

    def test_stop_terminates_server(self):
        process = MagicMock()
        process.poll.return_value = None

        stop_lmcache_mp_server(process)

        process.terminate.assert_called_once_with()
        process.wait.assert_called_once_with(timeout=10)
