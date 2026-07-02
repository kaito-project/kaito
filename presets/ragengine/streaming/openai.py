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

import json
from dataclasses import dataclass
from enum import StrEnum
from typing import Any

from ragengine.streaming.sse import SSEEvent


class OpenAIChatChunkParseStatus(StrEnum):
    PARSED = "parsed"
    DONE = "done"
    MALFORMED_JSON = "malformed_json"
    NO_DATA = "no_data"


@dataclass(frozen=True)
class OpenAIChatChoiceDelta:
    choice_index: int
    content: str | None = None
    finish_reason: str | None = None
    passthrough: bool = False


@dataclass(frozen=True)
class OpenAIChatChunkParseResult:
    status: OpenAIChatChunkParseStatus
    payload: dict[str, Any] | None = None
    choice_deltas: tuple[OpenAIChatChoiceDelta, ...] = ()
    contents: tuple[str, ...] = ()
    finish_reasons: tuple[str, ...] = ()
    error: str | None = None


def parse_openai_chat_sse_event(event: SSEEvent) -> OpenAIChatChunkParseResult:
    if event.data is None:
        return OpenAIChatChunkParseResult(
            status=OpenAIChatChunkParseStatus.NO_DATA,
        )

    data = event.data.strip()
    if data == "[DONE]":
        return OpenAIChatChunkParseResult(
            status=OpenAIChatChunkParseStatus.DONE,
        )

    try:
        payload = json.loads(data)
    except json.JSONDecodeError as json_error:
        return OpenAIChatChunkParseResult(
            status=OpenAIChatChunkParseStatus.MALFORMED_JSON,
            error=str(json_error),
        )

    if not isinstance(payload, dict):
        return OpenAIChatChunkParseResult(
            status=OpenAIChatChunkParseStatus.MALFORMED_JSON,
            error="OpenAI chat stream data must be a JSON object.",
        )

    choice_deltas: list[OpenAIChatChoiceDelta] = []
    choices = payload.get("choices", [])
    if isinstance(choices, list):
        for choice in choices:
            if not isinstance(choice, dict):
                continue

            choice_index = _coerce_choice_index(choice.get("index"))
            delta = choice.get("delta", {})
            if isinstance(delta, dict):
                content = delta.get("content")
                if isinstance(content, str):
                    choice_deltas.append(
                        OpenAIChatChoiceDelta(
                            choice_index=choice_index,
                            content=content,
                        )
                    )

                passthrough_keys = set(delta) - {"content"}
                if passthrough_keys or (
                    "content" in delta and not isinstance(content, str)
                ):
                    choice_deltas.append(
                        OpenAIChatChoiceDelta(
                            choice_index=choice_index,
                            passthrough=True,
                        )
                    )

            finish_reason = choice.get("finish_reason")
            if isinstance(finish_reason, str):
                choice_deltas.append(
                    OpenAIChatChoiceDelta(
                        choice_index=choice_index,
                        finish_reason=finish_reason,
                    )
                )

    return OpenAIChatChunkParseResult(
        status=OpenAIChatChunkParseStatus.PARSED,
        payload=payload,
        choice_deltas=tuple(choice_deltas),
        contents=tuple(
            delta.content for delta in choice_deltas if delta.content is not None
        ),
        finish_reasons=tuple(
            delta.finish_reason
            for delta in choice_deltas
            if delta.finish_reason is not None
        ),
    )


def _coerce_choice_index(value: Any) -> int:
    if isinstance(value, int):
        return value
    return 0


def build_openai_chat_delta_sse_chunk(
    content: str,
    *,
    choice_index: int = 0,
) -> str:
    payload = {
        "choices": [
            {
                "index": choice_index,
                "delta": {"content": content},
                "finish_reason": None,
            }
        ]
    }
    return build_sse_data_chunk(payload)


def build_openai_chat_finish_sse_chunk(
    *,
    finish_reason: str = "content_filter",
    choice_index: int = 0,
) -> str:
    payload = {
        "choices": [
            {
                "index": choice_index,
                "delta": {},
                "finish_reason": finish_reason,
            }
        ]
    }
    return build_sse_data_chunk(payload)


def build_sse_done_chunk() -> str:
    return "data: [DONE]\n\n"


def build_sse_data_chunk(payload: dict[str, Any]) -> str:
    return f"data: {json.dumps(payload, separators=(',', ':'))}\n\n"
