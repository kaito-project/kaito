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

import pytest
from guardrail_core.streaming import openai as shared_openai
from guardrail_core.streaming import sse as shared_sse

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.streaming import openai  # noqa: E402
from ragengine.streaming.sse import SSEFramer, iter_sse_events  # noqa: E402


def test_openai_exports_are_shared_implementations():
    for name in (
        "OpenAIChatChunkParseStatus",
        "OpenAIChatChunkParseResult",
        "ParsedOpenAIChoice",
        "parse_openai_chat_sse_event",
        "build_openai_chat_delta_sse_chunk",
        "build_openai_chat_finish_reason_sse_chunk",
        "build_sse_data_chunk",
        "build_sse_done_chunk",
    ):
        assert getattr(openai, name) is getattr(shared_openai, name)


def test_text_adapter_delegates_to_shared_framer(monkeypatch):
    calls = []
    original = shared_sse.SSEFramer.feed

    def feed(self, chunk):
        calls.append(chunk)
        return original(self, chunk)

    monkeypatch.setattr(shared_sse.SSEFramer, "feed", feed)
    framer = SSEFramer()
    events = framer.feed("data: \u4f60\u597d\n\n") + framer.finish()
    assert events[0].data == "\u4f60\u597d"
    assert calls == ["data: \u4f60\u597d\n\n".encode()]


@pytest.mark.asyncio
@pytest.mark.parametrize("ending", ("\n\n", "\r\n\r\n", "\r\r", "\n\r", "\r\n\r"))
async def test_async_adapter_finishes_at_eof(ending):
    async def upstream():
        yield "data: safe" + ending

    events = [event async for event in iter_sse_events(upstream())]
    assert len(events) == 1
    assert events[0].raw_bytes == ("data: safe" + ending).encode()


@pytest.mark.asyncio
async def test_async_adapter_accepts_split_utf8_bytes():
    wire = "data: \u4f60\u597d\n\n".encode()

    async def upstream():
        for byte in wire:
            yield bytes([byte])

    events = [event async for event in iter_sse_events(upstream())]
    assert events[0].data == "\u4f60\u597d"


@pytest.mark.asyncio
@pytest.mark.parametrize("wire", (b"data: unfinished", b"data: unfinished\r"))
async def test_async_adapter_reports_eof_truncation(wire):
    async def upstream():
        yield wire

    with pytest.raises(shared_sse.SSEIncompleteEventError):
        _ = [event async for event in iter_sse_events(upstream())]


@pytest.mark.asyncio
async def test_async_adapter_propagates_decode_failure():
    async def upstream():
        yield b"data:\xff\n\n"

    with pytest.raises(shared_sse.SSEDecodeError):
        _ = [event async for event in iter_sse_events(upstream())]


@pytest.mark.asyncio
async def test_async_adapter_propagates_size_limit_failure():
    async def upstream():
        yield "data: too long\n\n"

    with pytest.raises(shared_sse.SSEEventTooLargeError):
        _ = [event async for event in iter_sse_events(upstream(), max_event_bytes=8)]
