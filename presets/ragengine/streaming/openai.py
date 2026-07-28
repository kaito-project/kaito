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
    INVALID_PAYLOAD = "invalid_payload"
    NO_DATA = "no_data"


@dataclass(frozen=True)
class ParsedOpenAIChoice:
    choice_index: int
    content: str | None = None
    finish_reason: str | None = None


@dataclass(frozen=True)
class OpenAIChatChunkParseResult:
    status: OpenAIChatChunkParseStatus
    payload: dict[str, Any] | None = None
    parsed_choices: tuple[ParsedOpenAIChoice, ...] = ()
    contents: tuple[str, ...] = ()
    finish_reasons: tuple[str, ...] = ()
    error: str | None = None


# Parses and validates an OpenAI SSE event, extracting content and finish reasons for each choice.
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
            status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
            error="OpenAI chat stream data must be a JSON object.",
        )

    parsed_choices: list[ParsedOpenAIChoice] = []
    choices = payload.get("choices", [])
    if not isinstance(choices, list):
        return OpenAIChatChunkParseResult(
            status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
            error="OpenAI chat stream choices must be a list.",
        )

    for choice in choices:
        if not isinstance(choice, dict):
            return OpenAIChatChunkParseResult(
                status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
                error="OpenAI chat stream choice must be a JSON object.",
            )

        choice_index = choice.get("index")
        if isinstance(choice_index, bool) or not isinstance(choice_index, int):
            return OpenAIChatChunkParseResult(
                status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
                error="OpenAI chat stream choice index must be an integer.",
            )

        delta = choice.get("delta", {})
        if delta is None:
            delta = {}
        if not isinstance(delta, dict):
            return OpenAIChatChunkParseResult(
                status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
                error="OpenAI chat stream choice delta must be a JSON object.",
            )

        content = delta.get("content")
        if content is not None and not isinstance(content, str):
            return OpenAIChatChunkParseResult(
                status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
                error="OpenAI chat stream delta content must be a string or null.",
            )

        finish_reason = choice.get("finish_reason")
        if finish_reason is not None and not isinstance(finish_reason, str):
            return OpenAIChatChunkParseResult(
                status=OpenAIChatChunkParseStatus.INVALID_PAYLOAD,
                error="OpenAI chat stream finish_reason must be a string or null.",
            )
        if content is not None or finish_reason is not None:
            parsed_choices.append(
                ParsedOpenAIChoice(
                    choice_index=choice_index,
                    content=content,
                    finish_reason=finish_reason,
                )
            )

    return OpenAIChatChunkParseResult(
        status=OpenAIChatChunkParseStatus.PARSED,
        payload=payload,
        parsed_choices=tuple(parsed_choices),
        contents=tuple(
            parsed_choice.content
            for parsed_choice in parsed_choices
            if parsed_choice.content is not None
        ),
        finish_reasons=tuple(
            parsed_choice.finish_reason
            for parsed_choice in parsed_choices
            if parsed_choice.finish_reason is not None
        ),
    )


# Builds an OpenAI SSE event containing incremental text content.
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


# Builds an OpenAI SSE event indicating that a choice has finished.
def build_openai_chat_finish_reason_sse_chunk(
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


# Builds the [DONE] event indicating that the entire stream has ended.
def build_sse_done_chunk() -> str:
    return "data: [DONE]\n\n"


# Serializes a payload and wraps it as a generic SSE data event.
def build_sse_data_chunk(payload: dict[str, Any]) -> str:
    return f"data: {json.dumps(payload, separators=(',', ':'))}\n\n"
