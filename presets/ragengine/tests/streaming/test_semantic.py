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
    build_openai_chat_finish_sse_chunk,
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
    assert result.parsed_choices == (ParsedOpenAIChoice(choice_index=0, content="hello"),)
    assert result.contents == ("hello",)


def test_sse_framer_handles_multiple_events_in_one_chunk():
    framer = SSEFramer()

    events = framer.feed(
        'data: {"choices":[{"index":0,"delta":{"content":"first"}}]}\n\n'
        'data: {"choices":[{"index":0,"delta":{"content":"second"}}]}\n\n'
    )

    assert [parse_openai_chat_sse_event(event).contents for event in events] == [
        ("first",),
        ("second",),
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
    assert result.contents == ("crlf",)


def test_openai_parser_returns_explicit_status_for_malformed_json():
    events = SSEFramer().feed('data: {"choices": [}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.MALFORMED_JSON
    assert result.error


def test_openai_parser_tolerates_chunk_without_delta_content():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":2,"delta":{"role":"assistant"},'
        '"finish_reason":"stop"}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])
    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(choice_index=2, finish_reason="stop"),
    )
    assert result.contents == ()
    assert result.finish_reasons == ("stop",)


def test_openai_parser_extracts_choice_index_content_and_finish_reason():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":3,"delta":{"content":"safe"},'
        '"finish_reason":"stop"}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == (
        ParsedOpenAIChoice(
            choice_index=3,
            content="safe",
            finish_reason="stop",
        ),
    )
    assert result.contents == ("safe",)
    assert result.finish_reasons == ("stop",)


def test_openai_parser_supports_empty_choices():
    events = SSEFramer().feed('data: {"choices":[]}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.parsed_choices == ()
    assert result.contents == ()
    assert result.finish_reasons == ()


def test_openai_parser_rejects_non_object_payload():
    events = SSEFramer().feed('data: []\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID_PAYLOAD
    assert result.error == "OpenAI chat stream data must be a JSON object."


def test_openai_parser_rejects_non_list_choices():
    events = SSEFramer().feed('data: {"choices":{"index":0}}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID_PAYLOAD
    assert result.error == "OpenAI chat stream choices must be a list."


def test_openai_parser_rejects_invalid_choice_shape():
    events = SSEFramer().feed('data: {"choices":[null]}\n\n')

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID_PAYLOAD
    assert result.error == "OpenAI chat stream choice must be a JSON object."


def test_openai_parser_rejects_invalid_choice_index():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":true,"delta":{"content":"safe"}}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID_PAYLOAD
    assert result.error == "OpenAI chat stream choice index must be an integer."


def test_openai_parser_rejects_invalid_delta_content():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":42}}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID_PAYLOAD
    assert result.error == "OpenAI chat stream delta content must be a string or null."


def test_openai_parser_rejects_invalid_finish_reason():
    events = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":42}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])

    assert result.status == OpenAIChatChunkParseStatus.INVALID_PAYLOAD
    assert result.error == "OpenAI chat stream finish_reason must be a string or null."


def test_openai_builder_builds_delta_content_chunk():
    chunk = build_openai_chat_delta_sse_chunk("safe text")
    result = parse_openai_chat_sse_event(SSEFramer().feed(chunk)[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.contents == ("safe text",)


def test_openai_builder_builds_content_filter_finish_chunk():
    chunk = build_openai_chat_finish_sse_chunk(finish_reason="content_filter")
    result = parse_openai_chat_sse_event(SSEFramer().feed(chunk)[0])

    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.finish_reasons == ("content_filter",)


def test_openai_builder_builds_done_chunk():
    chunk = build_sse_done_chunk()
    result = parse_openai_chat_sse_event(SSEFramer().feed(chunk)[0])

    assert result.status == OpenAIChatChunkParseStatus.DONE
