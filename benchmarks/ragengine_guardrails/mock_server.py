"""Deterministic OpenAI-compatible mock server for guardrails benchmarking.

Controls: response length, SSE chunk size, chunk interval, upstream TTFT,
finish_reason, prohibited value position, and secret split position.
"""

import asyncio
import json
import time
from dataclasses import dataclass

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse


@dataclass
class MockConfig:
    """Configuration for mock server response behavior."""

    response_text: str = "This is a safe response from the mock model."
    chunk_size: int = 20  # characters per SSE delta
    chunk_interval_ms: float = 0  # ms between chunks (0 = no delay)
    upstream_ttft_ms: float = 0  # simulated time-to-first-token delay
    finish_reason: str = "stop"
    model: str = "mock-model"


# Global mutable config — updated by benchmark driver before each run.
_config = MockConfig()


def get_config() -> MockConfig:
    return _config


def set_config(config: MockConfig) -> None:
    global _config
    _config = config


app = FastAPI(title="Mock OpenAI for Guardrails Benchmark")


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    body = await request.json()
    stream = body.get("stream", False)
    cfg = get_config()

    if stream:
        return StreamingResponse(
            _stream_response(cfg),
            media_type="text/event-stream",
            headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
        )

    return JSONResponse(_build_non_streaming_response(cfg))


def _build_non_streaming_response(cfg: MockConfig) -> dict:
    return {
        "id": "chatcmpl-bench",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": cfg.model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": cfg.response_text},
                "finish_reason": cfg.finish_reason,
            }
        ],
        "usage": {
            "prompt_tokens": 10,
            "completion_tokens": max(1, len(cfg.response_text) // 4),
            "total_tokens": 10 + max(1, len(cfg.response_text) // 4),
        },
    }


async def _stream_response(cfg: MockConfig):
    """Yield SSE chunks with deterministic timing."""
    created = int(time.time())

    # Simulate upstream TTFT delay
    if cfg.upstream_ttft_ms > 0:
        await asyncio.sleep(cfg.upstream_ttft_ms / 1000.0)

    # Split response_text into fixed-size chunks
    text = cfg.response_text
    chunks = []
    for i in range(0, len(text), cfg.chunk_size):
        chunks.append(text[i : i + cfg.chunk_size])
    if not chunks:
        chunks = [""]

    for chunk_text in chunks:
        payload = {
            "id": "chatcmpl-bench",
            "object": "chat.completion.chunk",
            "created": created,
            "model": cfg.model,
            "choices": [
                {
                    "index": 0,
                    "delta": {"content": chunk_text},
                    "finish_reason": None,
                }
            ],
        }
        yield f"data: {json.dumps(payload, separators=(',', ':'))}\n\n"

        if cfg.chunk_interval_ms > 0:
            await asyncio.sleep(cfg.chunk_interval_ms / 1000.0)

    # Finish reason chunk
    finish_payload = {
        "id": "chatcmpl-bench",
        "object": "chat.completion.chunk",
        "created": created,
        "model": cfg.model,
        "choices": [
            {
                "index": 0,
                "delta": {},
                "finish_reason": cfg.finish_reason,
            }
        ],
    }
    yield f"data: {json.dumps(finish_payload, separators=(',', ':'))}\n\n"
    yield "data: [DONE]\n\n"


def run_mock_server(host: str = "127.0.0.1", port: int = 9100):
    """Run mock server standalone (for debugging)."""
    import uvicorn

    uvicorn.run(app, host=host, port=port, log_level="warning")


if __name__ == "__main__":
    run_mock_server()
