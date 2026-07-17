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

import os
import sys

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.streaming.openai import (
    OpenAIChatChunkParseStatus,
    ParsedOpenAIChoice,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import SSEFramer


def test_sse_framer_handles_fragmented_event():
    framer = SSEFramer()

    assert framer.feed('data: {"choices":[{"index":0,"delta":{"content":"hel') == []
    events = framer.feed('lo"}}]}\n\n')

    assert len(events) == 1
    result = parse_openai_chat_sse_event(events[0])
    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            content="hello",
        ),
    )


def test_sse_framer_handles_multiple_events_in_one_chunk():
    framer = SSEFramer()

    events = framer.feed(
        'data: {"choices":[{"index":0,"delta":{"content":"first"}}]}\n\n'
        'data: {"choices":[{"index":0,"delta":{"content":"second"}}]}\n\n'
    )

    assert [parse_openai_chat_sse_event(event).parsed_choices for event in events] == [
        (
            ParsedOpenAIChoice(
                choice_index=0,
                content="first",
            ),
        ),
        (
            ParsedOpenAIChoice(
                choice_index=0,
                content="second",
            ),
        ),
    ]


def test_openai_parser_detects_done_event():
    events = SSEFramer().feed("data: [DONE]\n\n")

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.DONE
    assert result.payload is None


def test_sse_framer_handles_crlf_separator():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"crlf"}}]}\r\n\r\n'
    )

    result = parse_openai_chat_sse_event(events[0])
    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            content="crlf",
        ),
    )


def test_openai_parser_returns_explicit_status_for_malformed_json():
    events = SSEFramer().feed('data: {"choices": [}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error is not None
    assert result.error.startswith("Malformed JSON: ")


def test_openai_parser_rejects_non_object_payload():
    events = SSEFramer().feed('data: ["not", "an", "object"]\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream data must be a JSON object."


def test_openai_parser_rejects_non_list_choices():
    events = SSEFramer().feed('data: {"choices":{"index":0}}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream choices must be a list."


def test_openai_parser_allows_empty_choices():
    events = SSEFramer().feed('data: {"choices":[]}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == ()


def test_openai_parser_allows_empty_choices_with_prompt_filter_results():
    events = SSEFramer().feed(
        'data: {"choices":[],"prompt_filter_results":[{"prompt_index":0}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == ()


def test_openai_parser_rejects_non_object_choice():
    events = SSEFramer().feed('data: {"choices":["not-an-object"]}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream choice must be a JSON object."


def test_openai_parser_rejects_missing_choice_index():
    events = SSEFramer().feed('data: {"choices":[{"delta":{"content":"text"}}]}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream choice index must be an integer."


def test_openai_parser_rejects_boolean_choice_index():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":true,"delta":{"content":"text"}}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream choice index must be an integer."


def test_openai_parser_parses_content_mixed_with_passthrough_fields():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"text","tool_calls":[]}}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            content="text",
        ),
    )


def test_openai_parser_ignores_null_non_content_delta_fields():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"text",'
        '"reasoning_content":null,"refusal":null}}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            content="text",
        ),
    )


def test_openai_parser_rejects_non_object_delta():
    events = SSEFramer().feed('data: {"choices":[{"index":0,"delta":"bad"}]}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream choice delta must be a JSON object."


def test_openai_parser_rejects_non_string_content():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":{"text":"bad"}}}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == "OpenAI chat stream delta content must be a string or null."


def test_openai_parser_rejects_invalid_finish_reason_type():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":["stop"]}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID
    assert result.error == (
        "OpenAI chat stream finish_reason must be a string or null."
    )


def test_openai_parser_tolerates_chunk_without_delta_content():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":"stop"}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])
    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            finish_reason="stop",
        ),
    )


def test_openai_parser_classifies_tool_call_delta_as_passthrough():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":2,"delta":{"tool_calls":[{"id":"call-1"}]},"finish_reason":null}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == ()


def test_openai_parser_classifies_tool_call_finish_reason():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            finish_reason="tool_calls",
        ),
    )


def test_openai_parser_preserves_multi_choice_content_indexes():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"zero"},"finish_reason":null},'
        '{"index":1,"delta":{"content":"one"},"finish_reason":null}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            content="zero",
        ),
        ParsedOpenAIChoice(
            choice_index=1,
            content="one",
        ),
    )


def test_openai_builder_builds_delta_content_chunk():
    chunk = build_openai_chat_delta_sse_chunk("safe text")
    result = parse_openai_chat_sse_event(SSEFramer().feed(chunk)[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            content="safe text",
        ),
    )


def test_openai_builder_builds_content_filter_finish_chunk():
    chunk = build_openai_chat_finish_reason_sse_chunk(finish_reason="content_filter")
    result = parse_openai_chat_sse_event(SSEFramer().feed(chunk)[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=0,
            finish_reason="content_filter",
        ),
    )


def test_openai_builder_builds_done_chunk():
    chunk = build_sse_done_chunk()
    result = parse_openai_chat_sse_event(SSEFramer().feed(chunk)[0])

    assert result.status == OpenAIChatChunkParseStatus.DONE
