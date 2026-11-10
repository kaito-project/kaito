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

import asyncio
import os
import sys

import httpx
import pytest
from openai.types.chat import CompletionCreateParams

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.guardrails import OutputGuardrails  # noqa: E402
from ragengine.guardrails.scanner_schemas import (  # noqa: E402
    BanSubstringsConfig,
    ParsedScannerConfig,
)
from ragengine.inference.inference import Inference  # noqa: E402
from ragengine.streaming.guardrails import apply_streaming_guardrails  # noqa: E402
from ragengine.streaming.openai import (  # noqa: E402
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_done_chunk,
)

REQUEST: CompletionCreateParams = {
    "model": "example",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": True,
}


class UpstreamStream(httpx.AsyncByteStream):
    def __init__(self, chunks, *, wait_at_end=False):
        self.chunks = chunks
        self.wait_at_end = wait_at_end
        self.waiting = asyncio.Event()
        self.closed = False

    async def __aiter__(self):
        for chunk in self.chunks:
            yield chunk
        if self.wait_at_end:
            self.waiting.set()
            await asyncio.Event().wait()

    async def aclose(self):
        self.closed = True


@pytest.fixture
def guardrails():
    return OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="ban_substrings",
                action_on_hit="block",
                config=BanSubstringsConfig(substrings=["unsafe"], match_type="str"),
            ),
        ),
    )


@pytest.fixture(autouse=True)
def inference_url(monkeypatch):
    monkeypatch.setattr(
        "ragengine.inference.inference.LLM_INFERENCE_URL",
        "http://upstream.test/v1/chat/completions",
    )


def client_for(stream):
    return httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(
                200, stream=stream, headers={"content-type": "text/event-stream"}
            )
        )
    )


@pytest.mark.asyncio
async def test_passthrough_preserves_bytes_including_split_utf8():
    raw = build_openai_chat_delta_sse_chunk("safe").encode() + b"data: \xe4\xbd\xa0\n\n"
    stream = UpstreamStream([bytes([byte]) for byte in raw])
    async with client_for(stream) as client:
        inference = Inference()
        inference._async_http_client = client
        upstream = await inference.chat_completions_stream_passthrough(REQUEST)
        chunks = [chunk async for chunk in upstream]
    assert all(isinstance(chunk, bytes) for chunk in chunks)
    assert b"".join(chunks) == raw
    assert stream.closed


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "invalid", (b"data:\xff\n\n", b"data:\xe4\xbd", b"data: truncated")
)
async def test_real_http_stream_fails_closed_without_replacement_or_tail_release(
    invalid, guardrails
):
    stream = UpstreamStream([build_openai_chat_delta_sse_chunk("un").encode(), invalid])
    async with client_for(stream) as client:
        inference = Inference()
        inference._async_http_client = client
        upstream = await inference.chat_completions_stream_passthrough(REQUEST)
        chunks = [
            chunk
            async for chunk in apply_streaming_guardrails(upstream, guardrails, REQUEST)
        ]
    assert chunks == [
        build_openai_chat_delta_sse_chunk("blocked-by-policy"),
        build_openai_chat_finish_reason_sse_chunk(),
        build_sse_done_chunk(),
    ]
    assert stream.closed


@pytest.mark.asyncio
async def test_real_http_stream_cancellation_closes_response(guardrails):
    stream = UpstreamStream(
        [build_openai_chat_delta_sse_chunk("un").encode()], wait_at_end=True
    )
    async with client_for(stream) as client:
        inference = Inference()
        inference._async_http_client = client
        upstream = await inference.chat_completions_stream_passthrough(REQUEST)
        output = apply_streaming_guardrails(upstream, guardrails, REQUEST)
        task = asyncio.create_task(anext(output))
        try:
            await asyncio.wait_for(stream.waiting.wait(), timeout=1)
        finally:
            task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert [chunk async for chunk in output] == []
    assert stream.closed


@pytest.mark.asyncio
async def test_real_http_stream_done_closes_response_early(guardrails):
    stream = UpstreamStream(
        [
            build_openai_chat_delta_sse_chunk("safe").encode(),
            build_sse_done_chunk().encode(),
        ],
        wait_at_end=True,
    )
    async with client_for(stream) as client:
        inference = Inference()
        inference._async_http_client = client
        upstream = await inference.chat_completions_stream_passthrough(REQUEST)
        chunks = [
            chunk
            async for chunk in apply_streaming_guardrails(upstream, guardrails, REQUEST)
        ]
    assert chunks == [
        build_openai_chat_delta_sse_chunk("safe"),
        build_sse_done_chunk(),
    ]
    assert not stream.waiting.is_set()
    assert stream.closed
