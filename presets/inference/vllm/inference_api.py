# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import logging
import os

import uvloop
from vllm.utils import FlexibleArgumentParser
import vllm.entrypoints.openai.api_server as api_server

# Initialize logger
logger = logging.getLogger(__name__)
debug_mode = os.environ.get('DEBUG_MODE', 'false').lower() == 'true'
logging.basicConfig(level=logging.DEBUG if debug_mode else logging.INFO)

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
        "cpu-offload-gb": 0,
        "gpu-memory-utilization": 0.9,
        "swap-space": 4,
        "disable-log-stats": False,
    }
    parser.set_defaults(**engine_default_args)

    return parser


if __name__ == "__main__":
    parser = FlexibleArgumentParser(description='vLLM serving server')
    parser = api_server.make_arg_parser(parser)
    parser = make_arg_parser(parser)
    args = parser.parse_args()

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
