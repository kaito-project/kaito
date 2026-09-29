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
import os
import sys
import urllib.error
import urllib.request
import uuid
from pathlib import Path

import psutil
from kaito_inference_config import KaitoInferenceConfig

logger = logging.getLogger(__name__)

LMCACHE_BIND_HOST = "127.0.0.1"
LMCACHE_HEALTH_HOST = "127.0.0.1"
LMCACHE_PORT = 5555
LMCACHE_HTTP_PORT = 8080
LMCACHE_L1_INITIAL_SIZE_GB = 1
DEFAULT_CPU_MEMORY_UTILIZATION = 0.5
DEFAULT_GENERATION_DIR = "/var/run/kaito-lmcache"
DEFAULT_SERVER_GENERATION_FILE = f"{DEFAULT_GENERATION_DIR}/server-generation"
DEFAULT_CLIENT_GENERATION_FILE = f"{DEFAULT_GENERATION_DIR}/client-generation"


def resolve_cpu_memory_utilization(
    config_file: str, explicit_utilization: float | None
) -> float:
    if explicit_utilization is not None:
        return explicit_utilization
    if config_file:
        return KaitoInferenceConfig.from_yaml(
            config_file
        ).kv_cache_cpu_memory_utilization
    return DEFAULT_CPU_MEMORY_UTILIZATION


def calculate_l1_size_gb(utilization: float, tensor_parallel_size: int) -> float:
    memory = psutil.virtual_memory()
    available_memory_gb = (memory.total - memory.used) / (1024**3)
    return available_memory_gb * utilization / tensor_parallel_size


def write_server_generation(path: str) -> None:
    generation_path = Path(path)
    generation_path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path = generation_path.with_suffix(".tmp")
    temporary_path.write_text(str(uuid.uuid4()))
    os.replace(temporary_path, generation_path)


def check_endpoint(url: str) -> int:
    try:
        with urllib.request.urlopen(url, timeout=2) as response:
            return 0 if response.status == 200 else 1
    except (OSError, urllib.error.URLError):
        return 1


def check_server_health() -> int:
    return check_endpoint(
        f"http://{LMCACHE_HEALTH_HOST}:{LMCACHE_HTTP_PORT}/healthcheck"
    )


def check_health(server_generation_file: str, client_generation_file: str) -> int:
    try:
        server_generation = Path(server_generation_file).read_text().strip()
        client_generation = Path(client_generation_file).read_text().strip()
        if not server_generation or server_generation != client_generation:
            return 1
    except OSError:
        return 1
    if check_server_health() != 0:
        return 1
    return check_endpoint("http://127.0.0.1:5000/health")


def build_server_command(args: argparse.Namespace) -> list[str]:
    utilization = resolve_cpu_memory_utilization(
        args.config_file, args.cpu_memory_utilization
    )
    if utilization <= 0:
        raise ValueError("cpu memory utilization must be greater than zero")
    if args.tensor_parallel_size <= 0:
        raise ValueError("tensor parallel size must be greater than zero")

    l1_size_gb = calculate_l1_size_gb(utilization, args.tensor_parallel_size)
    logger.info(
        "Offload KV cache to LMCache MP server: "
        f"{LMCACHE_L1_INITIAL_SIZE_GB:.2f} GB initial, {l1_size_gb:.2f} GB final "
        f"(utilization={utilization}, tp={args.tensor_parallel_size})"
    )
    return [
        "lmcache",
        "server",
        "--host",
        LMCACHE_BIND_HOST,
        "--port",
        str(LMCACHE_PORT),
        "--http-port",
        str(LMCACHE_HTTP_PORT),
        "--chunk-size",
        "256",
        "--isolated-ipc",
        "--l1-use-lazy",
        "--l1-init-size-gb",
        str(LMCACHE_L1_INITIAL_SIZE_GB),
        "--l1-size-gb",
        str(l1_size_gb),
        "--eviction-policy",
        "LRU",
    ]


def validate_gpu_visibility() -> None:
    import torch

    if not torch.cuda.is_available() or torch.cuda.device_count() < 1:
        raise RuntimeError("LMCache sidecar cannot access an NVIDIA GPU")
    for device_index in range(torch.cuda.device_count()):
        torch.cuda.get_device_properties(device_index)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="KAITO LMCache MP sidecar")
    parser.add_argument("--config-file", default="")
    parser.add_argument("--cpu-memory-utilization", type=float)
    parser.add_argument("--tensor-parallel-size", type=int, default=1)
    parser.add_argument("--isolated-ipc", action="store_true")
    parser.add_argument("--generation-file", default=DEFAULT_SERVER_GENERATION_FILE)
    parser.add_argument(
        "--client-generation-file", default=DEFAULT_CLIENT_GENERATION_FILE
    )
    # Sidecar probes check only LMCache; the main-container probe also verifies
    # the generation handshake and vLLM, which cannot be required during startup.
    parser.add_argument("--server-health-check", action="store_true")
    parser.add_argument("--health-check", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.server_health_check:
        return check_server_health()
    if args.health_check:
        return check_health(args.generation_file, args.client_generation_file)

    validate_gpu_visibility()
    command = build_server_command(args)
    write_server_generation(args.generation_file)
    os.execvp(command[0], command)
    return 1


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    sys.exit(main())
