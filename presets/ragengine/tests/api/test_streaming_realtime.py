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
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

import httpx
import pytest
import uvicorn
from fastapi import FastAPI, Request
from starlette.responses import StreamingResponse

from ragengine.guardrails import OutputGuardrails
from ragengine.guardrails.scanner_schemas import (
    BanSubstringsConfig,
    ParsedScannerConfig,
    RegexConfig,
)
from ragengine.main import app as ragengine_app


@asynccontextmanager
async def run_uvicorn_app(app: FastAPI, port: int) -> AsyncIterator[None]:
    config = uvicorn.Config(
        app,
        host="127.0.0.1",
        port=port,
        log_level="warning",
        lifespan="off",
    )
    server = uvicorn.Server(config)
    task = asyncio.create_task(server.serve())

    try:
        await asyncio.wait_for(_wait_for_startup(server), timeout=5)
        yield
    finally:
        server.should_exit = True
        await asyncio.wait_for(task, timeout=5)


async def _wait_for_startup(server: uvicorn.Server) -> None:
    while not server.started:
        await asyncio.sleep(0.01)


async def _next_non_empty_line(lines: AsyncIterator[str]) -> str:
    async for line in lines:
        if line:
            return line
    raise AssertionError("stream ended before a non-empty SSE line was received")


def _block_guardrails(
    scanner_config: ParsedScannerConfig,
    *,
    block_message: str = "blocked-by-policy",
) -> OutputGuardrails:
    return OutputGuardrails(
        enabled=True,
        fail_open=True,
        action_on_hit="block",
        block_message=block_message,
        scanner_configs=(scanner_config,),
    )


async def _read_sse_lines(response: httpx.Response) -> list[str]:
    lines = []
    async for line in response.aiter_lines():
        if line:
            lines.append(line)
    return lines


@pytest.mark.asyncio
async def test_streaming_passthrough_flushes_first_chunk_before_upstream_finishes(
    monkeypatch, unused_tcp_port_factory
):
    upstream_port = unused_tcp_port_factory()
    ragengine_port = unused_tcp_port_factory()
    release_second_chunk = asyncio.Event()
    upstream_requests: list[dict] = []

    upstream_app = FastAPI()

    @upstream_app.post("/v1/chat/completions")
    async def chat_completions(request: Request):
        upstream_requests.append(await request.json())

        async def events():
            yield 'data: {"choices":[{"delta":{"content":"first"}}]}\n\n'
            await release_second_chunk.wait()
            yield 'data: {"choices":[{"delta":{"content":"second"}}]}\n\n'
            yield "data: [DONE]\n\n"

        return StreamingResponse(events(), media_type="text/event-stream")

    import ragengine.inference.inference
    import ragengine.main

    monkeypatch.setattr(
        ragengine.inference.inference,
        "LLM_INFERENCE_URL",
        f"http://127.0.0.1:{upstream_port}/v1/chat/completions",
    )
    monkeypatch.setattr(
        ragengine.main.guardrails_reloader,
        "_current",
        OutputGuardrails(enabled=False),
    )

    async with (
        run_uvicorn_app(upstream_app, upstream_port),
        run_uvicorn_app(ragengine_app, ragengine_port),
        httpx.AsyncClient(timeout=5) as client,
        client.stream(
            "POST",
            f"http://127.0.0.1:{ragengine_port}/v1/chat/completions",
            json={
                "model": "mock-model",
                "messages": [{"role": "user", "content": "Hello"}],
                "stream": True,
            },
        ) as response,
    ):
        assert response.status_code == 200
        assert response.headers["content-type"].startswith("text/event-stream")

        lines = response.aiter_lines()
        first_line = await asyncio.wait_for(_next_non_empty_line(lines), timeout=1)
        assert first_line == 'data: {"choices":[{"delta":{"content":"first"}}]}'

        release_second_chunk.set()
        second_line = await _next_non_empty_line(lines)
        done_line = await _next_non_empty_line(lines)
        async for _ in lines:
            pass

    assert second_line == 'data: {"choices":[{"delta":{"content":"second"}}]}'
    assert done_line == "data: [DONE]"
    assert upstream_requests[0]["stream"] is True


@pytest.mark.asyncio
async def test_streaming_guardrails_safe_stream_passes(
    monkeypatch, unused_tcp_port_factory
):
    upstream_port = unused_tcp_port_factory()
    ragengine_port = unused_tcp_port_factory()

    upstream_app = FastAPI()

    @upstream_app.post("/v1/chat/completions")
    async def chat_completions(request: Request):
        await request.json()

        async def events():
            yield 'data: {"choices":[{"delta":{"content":"safe "}}]}\n\n'
            yield 'data: {"choices":[{"delta":{"content":"text"}}]}\n\n'
            yield "data: [DONE]\n\n"

        return StreamingResponse(events(), media_type="text/event-stream")

    import ragengine.inference.inference
    import ragengine.main

    monkeypatch.setattr(
        ragengine.inference.inference,
        "LLM_INFERENCE_URL",
        f"http://127.0.0.1:{upstream_port}/v1/chat/completions",
    )
    monkeypatch.setattr(
        ragengine.main.guardrails_reloader,
        "_current",
        _block_guardrails(
            ParsedScannerConfig(
                type="ban_substrings",
                action_on_hit="block",
                config=BanSubstringsConfig(
                    substrings=["blocked"],
                    match_type="str",
                ),
            )
        ),
    )

    async with (
        run_uvicorn_app(upstream_app, upstream_port),
        run_uvicorn_app(ragengine_app, ragengine_port),
        httpx.AsyncClient(timeout=5) as client,
        client.stream(
            "POST",
            f"http://127.0.0.1:{ragengine_port}/v1/chat/completions",
            json={
                "model": "mock-model",
                "messages": [{"role": "user", "content": "Hello"}],
                "stream": True,
            },
        ) as response,
    ):
        assert response.status_code == 200
        lines = await _read_sse_lines(response)

    assert lines == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe text"},"finish_reason":null}]}',
        "data: [DONE]",
    ]


@pytest.mark.asyncio
async def test_streaming_guardrails_blocked_stream_emits_block_message(
    monkeypatch, unused_tcp_port_factory
):
    upstream_port = unused_tcp_port_factory()
    ragengine_port = unused_tcp_port_factory()

    upstream_app = FastAPI()

    @upstream_app.post("/v1/chat/completions")
    async def chat_completions(request: Request):
        await request.json()

        async def events():
            yield 'data: {"choices":[{"delta":{"content":"safe "}}]}\n\n'
            yield 'data: {"choices":[{"delta":{"content":"http://evil.example"}}]}\n\n'
            yield 'data: {"choices":[{"delta":{"content":" after"}}]}\n\n'
            yield "data: [DONE]\n\n"

        return StreamingResponse(events(), media_type="text/event-stream")

    import ragengine.inference.inference
    import ragengine.main

    monkeypatch.setattr(
        ragengine.inference.inference,
        "LLM_INFERENCE_URL",
        f"http://127.0.0.1:{upstream_port}/v1/chat/completions",
    )
    monkeypatch.setattr(
        ragengine.main.guardrails_reloader,
        "_current",
        _block_guardrails(
            ParsedScannerConfig(
                type="regex",
                action_on_hit="block",
                config=RegexConfig(patterns=[r"https?://\S+"]),
            ),
            block_message="blocked-by-policy",
        ),
    )

    async with (
        run_uvicorn_app(upstream_app, upstream_port),
        run_uvicorn_app(ragengine_app, ragengine_port),
        httpx.AsyncClient(timeout=5) as client,
        client.stream(
            "POST",
            f"http://127.0.0.1:{ragengine_port}/v1/chat/completions",
            json={
                "model": "mock-model",
                "messages": [{"role": "user", "content": "Share a link"}],
                "stream": True,
            },
        ) as response,
    ):
        assert response.status_code == 200
        lines = await _read_sse_lines(response)

    assert lines == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}',
        "data: [DONE]",
    ]


@pytest.mark.asyncio
async def test_streaming_guardrails_blocks_split_violation_across_chunks(
    monkeypatch, unused_tcp_port_factory
):
    upstream_port = unused_tcp_port_factory()
    ragengine_port = unused_tcp_port_factory()

    upstream_app = FastAPI()

    @upstream_app.post("/v1/chat/completions")
    async def chat_completions(request: Request):
        await request.json()

        async def events():
            yield 'data: {"choices":[{"delta":{"content":"safe ba"}}]}\n\n'
            yield 'data: {"choices":[{"delta":{"content":"d text"}}]}\n\n'
            yield "data: [DONE]\n\n"

        return StreamingResponse(events(), media_type="text/event-stream")

    import ragengine.inference.inference
    import ragengine.main

    monkeypatch.setattr(
        ragengine.inference.inference,
        "LLM_INFERENCE_URL",
        f"http://127.0.0.1:{upstream_port}/v1/chat/completions",
    )
    monkeypatch.setattr(
        ragengine.main.guardrails_reloader,
        "_current",
        _block_guardrails(
            ParsedScannerConfig(
                type="ban_substrings",
                action_on_hit="block",
                config=BanSubstringsConfig(
                    substrings=["bad"],
                    match_type="str",
                ),
            )
        ),
    )

    async with (
        run_uvicorn_app(upstream_app, upstream_port),
        run_uvicorn_app(ragengine_app, ragengine_port),
        httpx.AsyncClient(timeout=5) as client,
        client.stream(
            "POST",
            f"http://127.0.0.1:{ragengine_port}/v1/chat/completions",
            json={
                "model": "mock-model",
                "messages": [{"role": "user", "content": "Hello"}],
                "stream": True,
            },
        ) as response,
    ):
        assert response.status_code == 200
        lines = await _read_sse_lines(response)

    assert lines == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}',
        "data: [DONE]",
    ]
