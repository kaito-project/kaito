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
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import SSEFramer


def test_sse_framer_handles_fragmented_event():
    framer = SSEFramer()

    assert framer.feed('data: {"choices":[{"delta":{"content":"hel') == []
    events = framer.feed('lo"}}]}\n\n')

    assert len(events) == 1
    result = parse_openai_chat_sse_event(events[0])
    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.contents == ("hello",)


def test_sse_framer_handles_multiple_events_in_one_chunk():
    framer = SSEFramer()

    events = framer.feed(
        'data: {"choices":[{"delta":{"content":"first"}}]}\n\n'
        'data: {"choices":[{"delta":{"content":"second"}}]}\n\n'
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
        'data: {"choices":[{"delta":{"content":"crlf"}}]}\r\n\r\n'
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
        'data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":"stop"}]}\n\n'
    )

    result = parse_openai_chat_sse_event(events[0])
    assert result.status == OpenAIChatChunkParseStatus.PARSED
    assert result.contents == ()
    assert result.finish_reasons == ("stop",)


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
