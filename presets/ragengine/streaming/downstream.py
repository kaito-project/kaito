import json
from typing import Any

from ragengine.streaming.upstream import SSE_DATA_PREFIX, SSE_DONE_MARKER


def build_sse_data_chunk(payload: dict[str, Any]) -> str:
    return f"{SSE_DATA_PREFIX} {json.dumps(payload, separators=(',', ':'))}\n\n"


def build_sse_done_chunk() -> str:
    return f"{SSE_DATA_PREFIX} {SSE_DONE_MARKER}\n\n"


def build_block_sse_chunk(message: str) -> str:
    payload = {
        "choices": [
            {
                "delta": {"content": message},
                "finish_reason": "content_filter",
            }
        ]
    }
    return build_sse_data_chunk(payload)