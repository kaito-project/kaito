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
import os
import sys
from types import SimpleNamespace

import pytest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.guardrails.output_guardrails import OutputGuardrails
from ragengine.guardrails.streaming import (
    STREAMING_DECISION_BLOCK,
    STREAMING_DECISION_BUFFER,
    STREAMING_DECISION_EMIT,
    ChunkAccumulator,
    StreamingDecision,
    StreamingGuardrailsProcessor,
    StreamingScanner,
)
from ragengine.metrics.prometheus_metrics import (
    stream_blocks_total,
    stream_buffer_bytes,
    stream_chunks_scanned_total,
    stream_decision_latency_ms,
    stream_finalize_actions_total,
    stream_redactions_total,
    stream_scanner_errors_total,
)


class _LineStream:
    def __init__(self, *lines: str) -> None:
        self._lines = list(lines)
        self.consumed = 0

    def __aiter__(self):
        return self

    async def __anext__(self) -> str:
        if self.consumed >= len(self._lines):
            raise StopAsyncIteration
        line = self._lines[self.consumed]
        self.consumed += 1
        return line


class _FakeScannerBase(StreamingScanner):
    def __init__(
        self, *, scanner_type: str = "test", action_on_hit: str = "block"
    ) -> None:
        self.parsed = SimpleNamespace(type=scanner_type)
        self.action_on_hit = action_on_hit


class _EarlyBlockScanner(_FakeScannerBase):
    supports_early_block = True

    def __init__(self, needle: str) -> None:
        super().__init__(scanner_type="early_block", action_on_hit="block")
        self._needle = needle

    def on_chunk(self, text: str) -> StreamingDecision:
        if self._needle in text:
            return StreamingDecision(state=STREAMING_DECISION_BLOCK)
        return StreamingDecision(state=STREAMING_DECISION_BUFFER)

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        del prompt, content
        return StreamingDecision(state=STREAMING_DECISION_EMIT)


class _FinalizeBlockScanner(_FakeScannerBase):
    def __init__(self, needle: str) -> None:
        super().__init__(scanner_type="finalize_block", action_on_hit="block")
        self._needle = needle

    def on_chunk(self, text: str) -> StreamingDecision:
        del text
        return StreamingDecision(state=STREAMING_DECISION_BUFFER)

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        del prompt
        if self._needle in content:
            return StreamingDecision(state=STREAMING_DECISION_BLOCK, content=content)
        return StreamingDecision(state=STREAMING_DECISION_EMIT, content=content)


class _FinalizeRedactScanner(_FakeScannerBase):
    def __init__(self, needle: str, replacement: str) -> None:
        super().__init__(scanner_type="finalize_redact", action_on_hit="redact")
        self._needle = needle
        self._replacement = replacement

    def on_chunk(self, text: str) -> StreamingDecision:
        del text
        return StreamingDecision(state=STREAMING_DECISION_BUFFER)

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        del prompt
        if self._needle not in content:
            return StreamingDecision(state=STREAMING_DECISION_EMIT, content=content)
        return StreamingDecision(
            state=STREAMING_DECISION_EMIT,
            content=content.replace(self._needle, self._replacement),
        )


class _ExplodingScanner(_FakeScannerBase):
    def __init__(self, *, explode_on: str) -> None:
        super().__init__(scanner_type="exploding", action_on_hit="block")
        self._explode_on = explode_on

    def on_chunk(self, text: str) -> StreamingDecision:
        del text
        if self._explode_on == "chunk":
            raise RuntimeError("scanner exploded")
        return StreamingDecision(state=STREAMING_DECISION_BUFFER)

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        del prompt, content
        if self._explode_on == "finalize":
            raise RuntimeError("scanner exploded")
        return StreamingDecision(state=STREAMING_DECISION_EMIT)


def _make_line(content: str, *, finish_reason: str | None = None) -> str:
    payload = {
        "id": "chatcmpl-stream",
        "object": "chat.completion.chunk",
        "created": 1,
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "delta": {"role": "assistant", "content": content},
                "finish_reason": finish_reason,
            }
        ],
    }
    return f"data: {json.dumps(payload)}"


async def _collect_stream(processor: StreamingGuardrailsProcessor, upstream) -> str:
    chunks: list[str] = []
    async for chunk in processor.wrap(upstream):
        chunks.append(chunk.decode())
    return "".join(chunks)


def _counter_value(metric, **labels) -> float:
    return metric.labels(**labels)._value.get()


def _histogram_count(metric, **labels) -> float:
    for sample in metric.collect()[0].samples:
        if sample.name.endswith("_count") and sample.labels == labels:
            return sample.value
    return 0.0


@pytest.mark.asyncio
async def test_streaming_early_block_stops_after_middle_chunk(caplog) -> None:
    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        block_message="blocked",
        policy_hash="policy-123",
    )
    before_chunks = stream_chunks_scanned_total._value.get()
    before_finalize = _counter_value(
        stream_finalize_actions_total,
        final_action="block",
    )
    before_blocks = _counter_value(stream_blocks_total, scanner_type="stream")
    before_latency = _histogram_count(
        stream_decision_latency_ms,
        scanner_type="early_block",
        phase="chunk",
        action=STREAMING_DECISION_BLOCK,
    )
    upstream = _LineStream(
        _make_line("bad"),
        _make_line("word"),
        _make_line(" tail", finish_reason="stop"),
        "data: [DONE]",
    )
    processor = StreamingGuardrailsProcessor(
        guardrails,
        {"messages": []},
        scanners=[_EarlyBlockScanner("badword")],
    )

    with caplog.at_level("INFO"):
        text = await _collect_stream(processor, upstream)

    assert "blocked" in text
    assert "tail" not in text
    assert '"finish_reason": "content_filter"' in text
    assert upstream.consumed == 2
    assert stream_chunks_scanned_total._value.get() == before_chunks + 2
    assert (
        _counter_value(stream_finalize_actions_total, final_action="block")
        == before_finalize + 1
    )
    assert (
        _counter_value(stream_blocks_total, scanner_type="stream") == before_blocks + 1
    )
    assert (
        _histogram_count(
            stream_decision_latency_ms,
            scanner_type="early_block",
            phase="chunk",
            action=STREAMING_DECISION_BLOCK,
        )
        == before_latency + 1
    )
    assert stream_buffer_bytes._value.get() == len(b"badword")
    assert "stream_id=chatcmpl-stream" in caplog.text
    assert "chunk_index=2" in caplog.text
    assert "scanner_type=early_block" in caplog.text
    assert "partial_action=block" in caplog.text
    assert "final_action=block" in caplog.text
    assert "buffered_bytes=7" in caplog.text
    assert "emitted_bytes=7" in caplog.text
    assert "fail_open=False" in caplog.text
    assert "policy_hash=policy-123" in caplog.text


@pytest.mark.asyncio
async def test_streaming_finalize_violation_blocks_after_buffering() -> None:
    guardrails = OutputGuardrails(
        enabled=True, fail_open=False, block_message="blocked"
    )
    upstream = _LineStream(
        _make_line("bad"),
        _make_line("word"),
        _make_line(" tail", finish_reason="stop"),
        "data: [DONE]",
    )
    processor = StreamingGuardrailsProcessor(
        guardrails,
        {"messages": []},
        scanners=[_FinalizeBlockScanner("badword")],
    )

    text = await _collect_stream(processor, upstream)

    assert "blocked" in text
    assert '"finish_reason": "content_filter"' in text
    assert upstream.consumed == 4


@pytest.mark.asyncio
async def test_streaming_finalize_redaction_handles_cross_chunk_content() -> None:
    guardrails = OutputGuardrails(enabled=True, fail_open=False)
    before_redactions = _counter_value(
        stream_redactions_total,
        scanner_type="finalize_redact",
    )
    upstream = _LineStream(
        _make_line("bad"),
        _make_line("word"),
        _make_line(" tail", finish_reason="stop"),
        "data: [DONE]",
    )
    processor = StreamingGuardrailsProcessor(
        guardrails,
        {"messages": []},
        scanners=[_FinalizeRedactScanner("badword", "[REDACTED]")],
    )

    text = await _collect_stream(processor, upstream)

    assert '"content": "[REDACTED] tail"' in text
    assert text.rstrip().endswith("data: [DONE]")
    assert (
        _counter_value(stream_redactions_total, scanner_type="finalize_redact")
        == before_redactions + 1
    )


@pytest.mark.asyncio
async def test_streaming_fail_open_emits_original_content_on_scanner_exception() -> (
    None
):
    guardrails = OutputGuardrails(enabled=True, fail_open=True)
    before_errors = _counter_value(
        stream_scanner_errors_total,
        scanner_type="exploding",
        phase="finalize",
        fail_open="true",
    )
    upstream = _LineStream(
        _make_line("hello "),
        _make_line("world", finish_reason="stop"),
        "data: [DONE]",
    )
    processor = StreamingGuardrailsProcessor(
        guardrails,
        {"messages": []},
        scanners=[_ExplodingScanner(explode_on="finalize")],
    )

    text = await _collect_stream(processor, upstream)

    assert '"content": "hello world"' in text
    assert '"type": "server_error"' not in text
    assert '"finish_reason": "stop"' in text
    assert (
        _counter_value(
            stream_scanner_errors_total,
            scanner_type="exploding",
            phase="finalize",
            fail_open="true",
        )
        == before_errors + 1
    )


@pytest.mark.asyncio
async def test_streaming_fail_closed_emits_error_on_scanner_exception() -> None:
    guardrails = OutputGuardrails(enabled=True, fail_open=False)
    upstream = _LineStream(
        _make_line("hello "),
        _make_line("world", finish_reason="stop"),
        "data: [DONE]",
    )
    processor = StreamingGuardrailsProcessor(
        guardrails,
        {"messages": []},
        scanners=[_ExplodingScanner(explode_on="finalize")],
    )

    text = await _collect_stream(processor, upstream)

    assert '"type": "server_error"' in text
    assert '"content": "hello world"' not in text


def test_chunk_accumulator_handles_empty_small_and_large_chunks() -> None:
    accumulator = ChunkAccumulator()
    accumulator.add_chunk(
        {
            "id": "chatcmpl-stream",
            "created": 1,
            "model": "mock-model",
            "choices": [
                {
                    "index": 0,
                    "delta": {"role": "assistant", "content": ""},
                    "finish_reason": None,
                }
            ],
        }
    )
    accumulator.add_chunk(
        {
            "id": "chatcmpl-stream",
            "created": 1,
            "model": "mock-model",
            "choices": [{"index": 0, "delta": {"content": "a"}, "finish_reason": None}],
        }
    )
    accumulator.add_chunk(
        {
            "id": "chatcmpl-stream",
            "created": 1,
            "model": "mock-model",
            "choices": [
                {
                    "index": 0,
                    "delta": {"content": "b" * 100_000},
                    "finish_reason": "stop",
                }
            ],
        }
    )

    assert accumulator.role == "assistant"
    assert accumulator.finish_reason == "stop"
    assert accumulator.content.startswith("a")
    assert len(accumulator.content) == 100_001
