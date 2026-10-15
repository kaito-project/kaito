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
import shutil
import time

logger = logging.getLogger(__name__)

LMCACHE_DEPLOYMENT_MODE_ENV = "KAITO_LMCACHE_DEPLOYMENT_MODE"
LMCACHE_DEPLOYMENT_MODE_SIDECAR = "sidecar"
LMCACHE_HOST = "127.0.0.1"
LMCACHE_PORT = 5555
LMCACHE_SERVER_GENERATION_FILE = "/var/run/kaito-lmcache/server-generation"
LMCACHE_CLIENT_GENERATION_FILE = "/var/run/kaito-lmcache/client-generation"


def set_kv_transfer_config_if_applicable(args: argparse.Namespace) -> None:
    """Configure NIXL or managed LMCache KV transfer when applicable."""
    inference_role = os.environ.get("KAITO_INFERENCE_ROLE", "")
    if args.kv_transfer_config is None and inference_role in ("prefill", "decode"):
        args.kv_transfer_config = {
            "kv_connector": "NixlConnector",
            "kv_role": "kv_both",
            "kv_load_failure_policy": "fail",
        }

    if (
        args.kaito_kv_cache_cpu_memory_utilization is None
        or args.kaito_kv_cache_cpu_memory_utilization <= 0
    ):
        logger.info(
            "kv_cache_cpu_memory_utilization is not set, do not use KV cache offload to CPU RAM."
        )
        return

    cli_device = str(getattr(args, "device", "") or "").strip().lower()
    env_device = str(os.environ.get("VLLM_TARGET_DEVICE", "") or "").strip().lower()
    if cli_device == "cpu" or env_device == "cpu":
        logger.info(
            "CPU runtime detected, skip LMCache KV offload because the CPU image does not include the LMCache GPU stack."
        )
        return

    if os.environ.get(LMCACHE_DEPLOYMENT_MODE_ENV) != LMCACHE_DEPLOYMENT_MODE_SIDECAR:
        logger.info("Managed LMCache sidecar is unavailable; CPU KV offload disabled.")
        return

    if args.kv_transfer_config is None:
        args.kv_transfer_config = {
            "kv_connector": "LMCacheMPConnector",
            "kv_connector_module_path": "lmcache.integration.vllm.lmcache_mp_connector",
            "kv_role": "kv_both",
            "kv_connector_extra_config": {
                "lmcache.mp.host": f"tcp://{LMCACHE_HOST}",
                "lmcache.mp.port": LMCACHE_PORT,
                "lmcache.mp.isolated_ipc": True,
            },
        }


def record_lmcache_server_generation(timeout_seconds: float = 10) -> None:
    """Record the generation that this vLLM process registered against."""
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        try:
            shutil.copyfile(
                LMCACHE_SERVER_GENERATION_FILE,
                LMCACHE_CLIENT_GENERATION_FILE,
            )
            return
        except FileNotFoundError:
            time.sleep(0.1)
    raise TimeoutError(
        f"LMCache server generation was not available within {timeout_seconds} seconds"
    )


def configure_kv_transfer_and_lmcache(args: argparse.Namespace) -> None:
    """Configure KV transfer and synchronize vLLM with a managed LMCache sidecar."""
    set_kv_transfer_config_if_applicable(args)
    if (
        args.kv_transfer_config is not None
        and os.environ.get(LMCACHE_DEPLOYMENT_MODE_ENV)
        == LMCACHE_DEPLOYMENT_MODE_SIDECAR
    ):
        record_lmcache_server_generation()
