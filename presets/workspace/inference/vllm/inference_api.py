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
import copy
import gc
import logging
import os
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

import psutil
import torch
import uvloop
import vllm.entrypoints.openai.api_server as api_server
import vllm.envs as envs
import yaml
from vllm.engine.llm_engine import EngineArgs, LLMEngine, VllmConfig
from vllm.entrypoints.openai.serving_models import LoRAModulePath
from vllm.executor.executor_base import ExecutorBase
from vllm.utils import FlexibleArgumentParser

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get("DEBUG_MODE", "false").lower() == "true"
logging.basicConfig(
    level=logging.DEBUG if debug_mode else logging.INFO,
    format="%(levelname)s %(asctime)s %(filename)s:%(lineno)d] %(message)s",
    datefmt="%m-%d %H:%M:%S",
)


class KAITOArgumentParser(argparse.ArgumentParser):
    vllm_parser = FlexibleArgumentParser(description="vLLM serving server")

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)

        # Initialize vllm parser
        self.vllm_parser = api_server.make_arg_parser(self.vllm_parser)
        self._reset_vllm_defaults()

        # KAITO only args
        # They should start with "kaito-" prefix to avoid conflict with vllm args
        self.add_argument(
            "--kaito-adapters-dir",
            type=str,
            default="/mnt/adapter",
            help="Directory where adapters are stored in KAITO preset.",
        )
        self.add_argument(
            "--kaito-config-file",
            type=str,
            default="",
            help="Additional args for KAITO preset.",
        )
        self.add_argument(
            "--kaito-max-probe-steps",
            type=int,
            help="Maximum number of steps to find the max available seq len fitting in the GPU memory.",
        )
        self.add_argument(
            "--kaito-kv-cache-cpu-memory-utilization",
            type=float,
            default=0.5,
            help="KV cache CPU memory utilization.",
        )

    def _reset_vllm_defaults(self):
        local_rank = int(os.environ.get("LOCAL_RANK", 0))  # Default to 0 if not set
        port = 5000 + local_rank  # Adjust port based on local rank

        server_default_args = {
            "disable_frontend_multiprocessing": False,
            "port": port,
        }
        self.vllm_parser.set_defaults(**server_default_args)

        # See https://docs.vllm.ai/en/stable/serving/engine_args.html for more args
        engine_default_args = {
            "model": "/workspace/vllm/weights",
            "cpu_offload_gb": 0,
            "gpu_memory_utilization": 0.95,
            "swap_space": 4,
            "disable_log_stats": False,
            "uvicorn_log_level": "error",
        }
        self.vllm_parser.set_defaults(**engine_default_args)

    def parse_args(self, *args, **kwargs):
        args = super().parse_known_args(*args, **kwargs)
        kaito_args = args[0]
        runtime_args = args[1]  # Remaining args

        # Load KAITO config
        if kaito_args.kaito_config_file:
            file_config = KaitoConfig.from_yaml(kaito_args.kaito_config_file)
            if kaito_args.kaito_max_probe_steps is None:
                kaito_args.kaito_max_probe_steps = file_config.max_probe_steps
            if kaito_args.kaito_kv_cache_cpu_memory_utilization is None:
                kaito_args.kaito_kv_cache_cpu_memory_utilization = (
                    file_config.kv_cache_cpu_memory_utilization
                )

            for key, value in file_config.vllm.items():
                runtime_args.append(f"--{key}")
                runtime_args.append(str(value))

        vllm_args = self.vllm_parser.parse_args(runtime_args, **kwargs)
        # Merge KAITO and vLLM args
        return argparse.Namespace(**vars(kaito_args), **vars(vllm_args))

    def print_help(self, file=None):
        super().print_help(file)
        print("\norignal vLLM server arguments:\n")
        self.vllm_parser.print_help(file)


@dataclass
class KaitoConfig:
    # Extra arguments for the vllm serving server, will be forwarded to the vllm server.
    # This should be in key-value format.
    vllm: dict[str, Any]

    # Maximum number of steps to find the max available seq len fitting in the GPU memory.
    max_probe_steps: int

    # Optional: CPU memory utilization for the vllm engine in kv cache offload mode. (default: 0.5, set to 0 to disable)
    kv_cache_cpu_memory_utilization: float

    @staticmethod
    def from_yaml(yaml_file: str) -> "KaitoConfig":
        with open(yaml_file) as file:
            config_data = yaml.safe_load(file)
        return KaitoConfig(
            vllm=config_data.get("vllm", {}),
            max_probe_steps=config_data.get("max_probe_steps", 6),
            kv_cache_cpu_memory_utilization=config_data.get(
                "kv_cache_cpu_memory_utilization", 0.5
            ),
        )

    def to_yaml(self) -> str:
        return yaml.dump(self.__dict__)


def load_lora_adapters(adapters_dir: str) -> LoRAModulePath | None:
    lora_list: list[LoRAModulePath] = []

    if not os.path.exists(adapters_dir):
        return lora_list

    logger.info(f"Loading LoRA adapters from {adapters_dir}")
    for adapter in os.listdir(adapters_dir):
        adapter_path = os.path.join(adapters_dir, adapter)
        if os.path.isdir(adapter_path):
            lora_list.append(LoRAModulePath(adapter, adapter_path))

    return lora_list


def find_max_available_seq_len(vllm_config: VllmConfig, max_probe_steps: int) -> int:
    """
    Load model and run profiler to find max available seq len.
    """
    executor_class = LLMEngine._get_executor_cls(vllm_config)
    if vllm_config.scheduler_config.enable_chunked_prefill:
        logger.info("Chunked Prefill is enabled, skip probing.")
        return vllm_config.model_config.max_model_len
    executor = executor_class(vllm_config=vllm_config)

    res = binary_search_with_limited_steps(
        vllm_config.model_config.max_model_len,
        max_probe_steps,
        lambda x: is_context_length_safe(executor, x),
    )

    # release memory
    del executor
    gc.collect()
    torch.cuda.empty_cache()

    return res


def binary_search_with_limited_steps(
    upper: int, max_probe_steps: int, is_valid_fn: Callable[[int], bool]
) -> int:
    """
    Finds the maximum valid value with limited number of steps.

    Parameters:
    - upper (int): The upper bound of the search space([0, upper]).
    - max_probe_steps (int): Maximum number of steps to try.
    - is_valid_fn (Callable[[int], bool]): A function that checks if a given value is valid.

    Returns: - int: The maximum valid value.
    """
    probe_steps = 0
    low = 0
    # double the upper bound and firstly search at upper value later.
    # because the valid value is likely to be close to the upper bound.
    high = upper * 2
    while low < high and probe_steps < max_probe_steps:
        mid = (low + high + 1) // 2
        if mid > upper:
            break

        if is_valid_fn(mid):
            low = mid
        else:
            high = mid - 1

        probe_steps += 1

    return low


def is_context_length_safe(executor: ExecutorBase, context_length: int) -> bool:
    """
    Check if the avilable gpu blocks is enough for the given num_gpu_blocks.
    """
    # Profile memory usage with max_num_sequences sequences and the total
    # number of tokens equal to max_num_batched_tokens.
    executor.scheduler_config.max_num_batched_tokens = context_length
    try:
        logger.info(
            f"Try to determine available gpu blocks for context length {context_length}"
        )
        # see https://github.com/vllm-project/vllm/blob/v0.7.2/vllm/engine/llm_engine.py#L416
        available_gpu_blocks, _ = executor.determine_num_available_blocks()
    except torch.OutOfMemoryError:
        return False

    num_gpu_blocks = context_length // executor.cache_config.block_size
    return available_gpu_blocks >= num_gpu_blocks


def try_get_max_available_seq_len(args: argparse.Namespace) -> int | None:
    if args.max_model_len is not None:
        logger.info(f"max_model_len is set to {args.max_model_len}, skip probing.")
        return None

    if args.tensor_parallel_size > 1 or args.pipeline_parallel_size > 1:
        logger.info("Multi-GPU serving is enabled, skip probing.")
        return None

    max_probe_steps = 0
    if args.kaito_max_probe_steps is not None:
        try:
            max_probe_steps = int(args.kaito_max_probe_steps)
        except ValueError:
            raise ValueError("kaito_max_probe_steps must be an integer.")

    if max_probe_steps <= 0:
        return None

    engine_args = EngineArgs.from_cli_args(args)
    # read the model config from hf weights path.
    # vllm will perform different parser for different model architectures
    # and read it into a unified EngineConfig.
    vllm_config = engine_args.create_engine_config()

    max_model_len = vllm_config.model_config.max_model_len
    available_seq_len = max_model_len
    logger.info("Try run profiler to find max available seq len")
    available_seq_len = find_max_available_seq_len(vllm_config, max_probe_steps)
    # see https://github.com/vllm-project/vllm/blob/v0.7.2/vllm/worker/worker.py#L539
    if available_seq_len <= 0:
        raise ValueError(
            "No available memory for the cache blocks. "
            "Try increasing `gpu_memory_utilization` when "
            "initializing the engine."
        )

    if available_seq_len != max_model_len:
        logger.info(f"Set max_model_len from {max_model_len} to {available_seq_len}")
        return available_seq_len
    else:
        logger.info(f"Using model default max_model_len {max_model_len}")
        return None


def set_kv_cache_offloading_if_appliable(args: argparse.Namespace) -> None:
    """
    Set KV cache offloading to CPU RAM if applicable.
    This is only applicable when VLLM_USE_V1 is enabled and
    kaito_kv_cache_cpu_memory_utilization is set.
    """
    if (
        not envs.is_set("VLLM_USE_V1")
        and args.kaito_kv_cache_cpu_memory_utilization > 0
    ):
        logger.info(
            f"VLLM_USE_V1 is not set, but kaito_kv_cache_cpu_memory_utilization is set as {args.kaito_kv_cache_cpu_memory_utilization}, "
            "run create_engine_config to check whether VLLM_USE_V1 should be set."
        )
        EngineArgs.from_cli_args(copy.deepcopy(args)).create_engine_config()

    if (
        envs.is_set("VLLM_USE_V1")
        and envs.VLLM_USE_V1
        and args.kaito_kv_cache_cpu_memory_utilization > 0
    ):
        os.environ["LMCACHE_CHUNK_SIZE"] = "256"
        os.environ["LMCACHE_LOCAL_CPU"] = "True"
        available_memory_gb = (
            psutil.virtual_memory().total - psutil.virtual_memory().used
        ) / (1024**3)
        logger.info(
            f"VLLM_USE_V1 is set, Offload KV cache to CPU RAM, size limit: {available_memory_gb} * {args.kaito_kv_cache_cpu_memory_utilization} GB"
        )
        os.environ["LMCACHE_MAX_LOCAL_CPU_SIZE"] = (
            f"{available_memory_gb * args.kaito_kv_cache_cpu_memory_utilization}"
        )

        if args.kv_transfer_config is None:
            args.kv_transfer_config = {
                "kv_connector": "LMCacheConnectorV1",
                "kv_role": "kv_both",
            }
    else:
        logger.info(
            "VLLM_USE_V1 or kv_cache_cpu_memory_utilization is not set, do not use KV cache offload to CPU RAM."
        )


if __name__ == "__main__":
    parser = KAITOArgumentParser(description="KAITO wrapper of vLLM serving server")
    args = parser.parse_args()

    # set LoRA adapters
    if args.lora_modules is None:
        args.lora_modules = load_lora_adapters(args.kaito_adapters_dir)

    # notes: avoid dirty args that breaks vllm runtime check. deepcopy here.
    max_available_seq_len = try_get_max_available_seq_len(copy.deepcopy(args))
    if max_available_seq_len is not None:
        args.max_model_len = max_available_seq_len

    set_kv_cache_offloading_if_appliable(args)

    # Run the serving server
    logger.info(f"Starting server on port {args.port}")
    # See https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html for more
    # details about serving server.
    # endpoints:
    # - /health
    # - /tokenize
    # - /detokenize
    # - /v1/models
    # - /version
    # - /v1/chat/completions
    # - /v1/completions
    # - /v1/embeddings
    uvloop.run(api_server.run_server(args))
