import json
from typing import Any

SSE_DATA_PREFIX = "data:"
SSE_DONE_MARKER = "[DONE]"


def parse_sse_data_line(line: str) -> str | None:
    if not line.startswith(SSE_DATA_PREFIX):
        return None

    return line[len(SSE_DATA_PREFIX) :].lstrip()


def is_sse_done_event(data: str) -> bool:
    return data.strip() == SSE_DONE_MARKER


def extract_delta_content(payload: dict[str, Any]) -> str | None:
    choices = payload.get("choices")
    if not isinstance(choices, list) or not choices:
        return None

    delta = choices[0].get("delta")
    if not isinstance(delta, dict):
        return None

    content = delta.get("content")
    return content if isinstance(content, str) else None


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