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
import logging
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

parent_dir = str(Path(__file__).resolve().parent.parent)
sys.path.insert(0, parent_dir)

from lmcache_server import (  # noqa: E402
    build_server_command,
    check_health,
    check_server_health,
    parse_args,
    resolve_cpu_memory_utilization,
    validate_gpu_visibility,
    write_server_generation,
)


def test_parser_accepts_controller_isolated_ipc_flag():
    with patch.object(sys, "argv", ["lmcache_server.py", "--isolated-ipc"]):
        args = parse_args()

    assert args.isolated_ipc is True


def test_config_file_resolves_cpu_utilization(tmp_path):
    config = tmp_path / "inference_config.yaml"
    config.write_text("kv_cache_cpu_memory_utilization: 0.7\n")

    assert resolve_cpu_memory_utilization(str(config), None) == 0.7
    assert resolve_cpu_memory_utilization(str(config), 0.4) == 0.4


def test_server_command_uses_isolated_ipc_and_lazy_l1(caplog):
    args = argparse.Namespace(
        config_file="",
        cpu_memory_utilization=0.5,
        tensor_parallel_size=2,
    )
    memory = argparse.Namespace(total=100 * 1024**3, used=20 * 1024**3)

    with (
        patch("lmcache_server.psutil.virtual_memory", return_value=memory),
        caplog.at_level(logging.INFO, logger="lmcache_server"),
    ):
        command = build_server_command(args)

    assert "--isolated-ipc" in command
    assert "--l1-use-lazy" in command
    assert command[command.index("--host") + 1] == "127.0.0.1"
    assert command[command.index("--l1-init-size-gb") + 1] == "1"
    assert command[command.index("--l1-size-gb") + 1] == "20.0"
    assert caplog.messages == [
        "Offload KV cache to LMCache MP server: 1.00 GB initial, 20.00 GB final "
        "(utilization=0.5, tp=2)"
    ]


def test_generation_health_requires_matching_files_and_endpoints(tmp_path):
    server_file = tmp_path / "server-generation"
    client_file = tmp_path / "client-generation"
    write_server_generation(str(server_file))
    client_file.write_text(server_file.read_text())

    response = MagicMock()
    response.status = 200
    response.__enter__.return_value = response
    with patch("lmcache_server.urllib.request.urlopen", return_value=response) as get:
        assert check_health(str(server_file), str(client_file)) == 0

    assert get.call_count == 2
    client_file.write_text("stale")
    assert check_health(str(server_file), str(client_file)) == 1


def test_server_health_only_checks_lmcache_endpoint():
    response = MagicMock()
    response.status = 200
    response.__enter__.return_value = response
    with patch("lmcache_server.urllib.request.urlopen", return_value=response) as get:
        assert check_server_health() == 0

    get.assert_called_once_with("http://127.0.0.1:8080/healthcheck", timeout=2)


def test_gpu_visibility_requires_cuda_devices():
    torch = MagicMock()
    torch.cuda.is_available.return_value = True
    torch.cuda.device_count.return_value = 1
    with patch.dict(sys.modules, {"torch": torch}):
        validate_gpu_visibility()

    torch.cuda.get_device_properties.assert_called_once_with(0)
