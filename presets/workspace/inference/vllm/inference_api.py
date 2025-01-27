# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import logging
import gc
import os
from typing import Callable

import uvloop
import torch
from vllm.utils import FlexibleArgumentParser
import vllm.entrypoints.openai.api_server as api_server
from vllm.engine.llm_engine import (LLMEngine, EngineArgs, EngineConfig)
from vllm.executor.executor_base import ExecutorBase

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get('DEBUG_MODE', 'false').lower() == 'true'
logging.basicConfig(
    level=logging.DEBUG if debug_mode else logging.INFO,
    format='%(levelname)s %(asctime)s %(filename)s:%(lineno)d] %(message)s',
    datefmt='%m-%d %H:%M:%S')

def make_arg_parser(parser: FlexibleArgumentParser) -> FlexibleArgumentParser:
    local_rank = int(os.environ.get("LOCAL_RANK",
                                    0))  # Default to 0 if not set
    port = 5000 + local_rank  # Adjust port based on local rank

    server_default_args = {
        "disable-frontend-multiprocessing": False,
        "port": port
    }
    parser.set_defaults(**server_default_args)

    # See https://docs.vllm.ai/en/latest/models/engine_args.html for more args
    engine_default_args = {
        "model": "/workspace/vllm/weights",
        "cpu_offload_gb": 0,
        "gpu_memory_utilization": 0.95,
        "swap_space": 4,
        "disable_log_stats": False,
        "uvicorn_log_level": "error"
    }
    parser.set_defaults(**engine_default_args)

    return parser

def find_max_available_seq_len(engine_config: EngineConfig) -> int:
    """
    Load model and run profiler to find max available seq len.
    """
    # see https://github.com/vllm-project/vllm/blob/v0.6.3/vllm/engine/llm_engine.py#L335
    executor_class = LLMEngine._get_executor_cls(engine_config)
    executor = executor_class(
        model_config=engine_config.model_config,
        cache_config=engine_config.cache_config,
        parallel_config=engine_config.parallel_config,
        scheduler_config=engine_config.scheduler_config,
        device_config=engine_config.device_config,
        lora_config=engine_config.lora_config,
        speculative_config=engine_config.speculative_config,
        load_config=engine_config.load_config,
        prompt_adapter_config=engine_config.prompt_adapter_config,
        observability_config=engine_config.observability_config,
    )

    max_probe_steps = 6
    if os.getenv("MAX_PROBE_STEPS") is not None:
        try:
            max_probe_steps = int(os.getenv("MAX_PROBE_STEPS"))
        except ValueError:
            raise ValueError("MAX_PROBE_STEPS must be an integer.")

    model_max_blocks = int(engine_config.model_config.max_model_len / engine_config.cache_config.block_size)
    res = binary_search_with_limited_steps(model_max_blocks, max_probe_steps, lambda x: is_context_length_safe(executor, x))

    # release memory
    del executor
    gc.collect()
    torch.cuda.empty_cache()

    return engine_config.cache_config.block_size * res

def binary_search_with_limited_steps(upper: int, max_probe_steps: int, is_valid_fn: Callable[[int], bool]) -> int:
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

def is_context_length_safe(executor: ExecutorBase, num_gpu_blocks: int) -> bool:
    """
    Check if the avilable gpu blocks is enough for the given num_gpu_blocks.
    """
    context_length = executor.cache_config.block_size * num_gpu_blocks
    executor.scheduler_config.max_num_batched_tokens = context_length

    try:
        # see https://github.com/vllm-project/vllm/blob/v0.6.3/vllm/engine/llm_engine.py#L477
        available_gpu_blocks, _ = executor.determine_num_available_blocks()
    except torch.OutOfMemoryError as e:
        return False    

    return available_gpu_blocks >= num_gpu_blocks

if __name__ == "__main__":
    parser = FlexibleArgumentParser(description='vLLM serving server')
    parser = api_server.make_arg_parser(parser)
    parser = make_arg_parser(parser)
    args = parser.parse_args()

    if args.max_model_len is None:
        engine_args = EngineArgs.from_cli_args(args)
        # read the model config from hf weights path.
        # vllm will perform different parser for different model architectures
        # and read it into a unified EngineConfig.
        engine_config = engine_args.create_engine_config()

        logger.info("Try run profiler to find max available seq len")
        available_seq_len = find_max_available_seq_len(engine_config)
        # see https://github.com/vllm-project/vllm/blob/v0.6.3/vllm/worker/worker.py#L262
        if available_seq_len <= 0:
            raise ValueError("No available memory for the cache blocks. "
                         "Try increasing `gpu_memory_utilization` when "
                         "initializing the engine.")
        max_model_len = engine_config.model_config.max_model_len
        if available_seq_len > max_model_len:
            available_seq_len = max_model_len

        if available_seq_len != max_model_len:
            logger.info(f"Set max_model_len from {max_model_len} to {available_seq_len}")
            args.max_model_len = available_seq_len
        else:
            logger.info(f"Using model default max_model_len {max_model_len}")

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
