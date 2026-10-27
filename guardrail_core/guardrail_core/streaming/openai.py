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

from guardrail_core.streaming.sse import SSEEvent


class OpenAIChatChunkParseStatus(StrEnum):
    PARSED = "parsed"
    DONE = "done"
    MALFORMED_JSON = "malformed_json"
    INVALID_PAYLOAD = "invalid_payload"
    NO_DATA = "no_data"


@dataclass(frozen=True)
class ParsedOpenAIChoice:
    choice_index: int
    content: str | None = None
    finish_reason: str | None = None
    choice: dict[str, Any] | None = None


@dataclass(frozen=True)
class OpenAIChatChunkParseResult:
    status: OpenAIChatChunkParseStatus
    payload: dict[str, Any] | None = None
    parsed_choices: tuple[ParsedOpenAIChoice, ...] = ()
    contents: tuple[str, ...] = ()
    finish_reasons: tuple[str, ...] = ()
    error: str | None = None


def parse_openai_chat_sse_event(event: SSEEvent) -> OpenAIChatChunkParseResult:
    if event.data is None or not event.data.strip():
        return OpenAIChatChunkParseResult(status=OpenAIChatChunkParseStatus.NO_DATA)
    data = event.data.strip()
    if data == "[DONE]":
        return OpenAIChatChunkParseResult(status=OpenAIChatChunkParseStatus.DONE)
    try:
        payload = json.loads(data)
    except (ValueError, RecursionError) as error:
        return OpenAIChatChunkParseResult(
            status=OpenAIChatChunkParseStatus.MALFORMED_JSON, error=str(error)
        )
    if not isinstance(payload, dict):
        return _invalid("OpenAI chat stream data must be a JSON object.")
    choices = payload.get("choices", [])
    if not isinstance(choices, list):
        return _invalid("OpenAI chat stream choices must be a list.", payload)

    parsed_choices: list[ParsedOpenAIChoice] = []
    for choice in choices:
        if not isinstance(choice, dict):
            return _invalid("OpenAI chat stream choice must be a JSON object.", payload)
        choice_index = choice.get("index")
        if isinstance(choice_index, bool) or not isinstance(choice_index, int):
            return _invalid(
                "OpenAI chat stream choice index must be an integer.", payload
            )
        delta = choice.get("delta")
        if delta is None:
            delta = {}
        if not isinstance(delta, dict):
            return _invalid(
                "OpenAI chat stream choice delta must be a JSON object.", payload
            )
        content = delta.get("content")
        if content is not None and not isinstance(content, str):
            return _invalid(
                "OpenAI chat stream delta content must be a string or null.", payload
            )
        finish_reason = choice.get("finish_reason")
        if finish_reason is not None and not isinstance(finish_reason, str):
            return _invalid(
                "OpenAI chat stream finish_reason must be a string or null.", payload
            )
        parsed_choices.append(
            ParsedOpenAIChoice(
                choice_index=choice_index,
                content=content,
                finish_reason=finish_reason,
                choice=choice,
            )
        )
    return OpenAIChatChunkParseResult(
        status=OpenAIChatChunkParseStatus.PARSED,
        payload=payload,
        parsed_choices=tuple(parsed_choices),
        contents=tuple(
            choice.content for choice in parsed_choices if choice.content is not None
        ),
        finish_reasons=tuple(
            choice.finish_reason
            for choice in parsed_choices
            if choice.finish_reason is not None
        ),
    )


def _invalid(
    error: str, payload: dict[str, Any] | None = None
) -> OpenAIChatChunkParseResult:
    return OpenAIChatChunkParseResult(
        status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD, payload=payload, error=error
    )


def build_openai_chat_delta_sse_chunk(content: str, *, choice_index: int = 0) -> str:
    return build_sse_data_chunk(
        {
            "choices": [
                {
                    "index": choice_index,
                    "delta": {"content": content},
                    "finish_reason": None,
                }
            ]
        }
    )


def build_openai_chat_finish_reason_sse_chunk(
    *, finish_reason: str = "content_filter", choice_index: int = 0
) -> str:
    return build_sse_data_chunk(
        {
            "choices": [
                {"index": choice_index, "delta": {}, "finish_reason": finish_reason}
            ]
        }
    )


def build_sse_done_chunk() -> str:
    return "data: [DONE]\n\n"


def build_sse_data_chunk(payload: dict[str, Any]) -> str:
    return f"data: {json.dumps(payload, separators=(',', ':'))}\n\n"
