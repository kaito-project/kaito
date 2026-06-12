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

from __future__ import annotations

import json
import logging
import time
from collections.abc import AsyncIterator
from dataclasses import dataclass, field
from typing import Any, Literal

from llm_guard import scan_output

from ragengine.guardrails.output_guardrails import (
    OutputGuardrails,
    OutputGuardrailsError,
)
from ragengine.guardrails.scanner_schemas import ParsedScannerConfig
from ragengine.metrics.prometheus_metrics import (
    output_guardrails_actions_total,
    stream_blocks_total,
    stream_buffer_bytes,
    stream_chunks_scanned_total,
    stream_decision_latency_ms,
    stream_finalize_actions_total,
    stream_redactions_total,
    stream_scanner_errors_total,
)

logger = logging.getLogger(__name__)

STREAMING_DECISION_BUFFER = "buffer"
STREAMING_DECISION_EMIT = "emit"
STREAMING_DECISION_BLOCK = "block"
STREAMING_DECISION_ERROR = "error"
SUPPORTED_STREAMING_SCANNERS = frozenset({"regex", "ban_substrings"})


@dataclass(frozen=True)
class StreamingDecision:
    state: Literal["buffer", "emit", "block", "error"]
    content: str = ""
    scores: dict[str, Any] = field(default_factory=dict)


class StreamingScanner:
    # Streaming redact remains finalize-only until incremental redaction
    # semantics are explicitly defined.
    supports_early_block = False

    def on_chunk(self, text: str) -> StreamingDecision:
        raise NotImplementedError

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        raise NotImplementedError


@dataclass
class DeterministicStreamingScanner(StreamingScanner):
    parsed: ParsedScannerConfig
    scanner: Any
    action_on_hit: str

    def on_chunk(self, text: str) -> StreamingDecision:
        del text
        return StreamingDecision(state=STREAMING_DECISION_BUFFER)

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        sanitized_output, results_valid, results_score = scan_output(
            [self.scanner], prompt, content, fail_fast=False
        )
        if all(results_valid.values()):
            return StreamingDecision(state=STREAMING_DECISION_EMIT, content=content)
        if self.action_on_hit == "block":
            return StreamingDecision(
                state=STREAMING_DECISION_BLOCK,
                content=sanitized_output,
                scores=results_score,
            )
        return StreamingDecision(
            state=STREAMING_DECISION_EMIT,
            content=sanitized_output,
            scores=results_score,
        )


@dataclass
class ChunkAccumulator:
    response_id: str = ""
    created: int = 0
    model: str = ""
    role: str = "assistant"
    finish_reason: str | None = None
    content_parts: list[str] = field(default_factory=list)

    def add_chunk(self, chunk: dict[str, Any]) -> None:
        self.response_id = self.response_id or str(chunk.get("id") or "")
        self.created = self.created or int(chunk.get("created") or 0)
        self.model = self.model or str(chunk.get("model") or "")

        choices = chunk.get("choices") or []
        if not choices:
            return

        choice = choices[0]
        delta = choice.get("delta") or {}
        if delta.get("tool_calls") or delta.get("function_call"):
            raise OutputGuardrailsError(
                "Streaming guardrails currently support assistant text deltas only."
            )

        self.role = delta.get("role") or self.role
        content = delta.get("content")
        if isinstance(content, str) and content:
            self.content_parts.append(content)
        if choice.get("finish_reason") is not None:
            self.finish_reason = choice.get("finish_reason")

    @property
    def content(self) -> str:
        return "".join(self.content_parts)


@dataclass(frozen=True)
class StreamingFinalizeResult:
    content: str
    action: str | None
    triggered_scanners: tuple[dict[str, Any], ...] = ()


class StreamingGuardrailsProcessor:
    def __init__(
        self,
        guardrails: OutputGuardrails,
        request: dict[str, Any],
        scanners: list[StreamingScanner] | None = None,
    ) -> None:
        self._guardrails = guardrails
        self._request = request
        self._accumulator = ChunkAccumulator()
        self._scanners = scanners or self._build_streaming_scanners()
        self._blocked_during_stream = False
        self._chunk_index = 0
        self._error_scanner_type = "stream"
        self._error_phase = "chunk"

    def _stream_id(self) -> str:
        request_stream_id = str(self._request.get("stream_id") or "")
        return request_stream_id or self._accumulator.response_id or "chatcmpl-stream"

    def _buffered_bytes(self) -> int:
        return len(self._accumulator.content.encode("utf-8"))

    @staticmethod
    def _content_bytes(content: str) -> int:
        return len(content.encode("utf-8"))

    def _observe_decision_latency(
        self, scanner_type: str, phase: str, action: str, started_at: float
    ) -> None:
        stream_decision_latency_ms.labels(
            scanner_type=scanner_type,
            phase=phase,
            action=action,
        ).observe((time.perf_counter() - started_at) * 1000)

    def _log_chunk_scanned(self) -> None:
        logger.info(
            "output_guardrails_stream_chunk_scanned response_id=%s stream_id=%s chunk_index=%s buffered_bytes=%s fail_open=%s policy_hash=%s",
            self._accumulator.response_id,
            self._stream_id(),
            self._chunk_index,
            self._buffered_bytes(),
            self._guardrails.fail_open,
            self._guardrails.policy_hash,
        )

    def _log_partial_decision(self, scanner_type: str, partial_action: str) -> None:
        logger.info(
            "output_guardrails_stream_partial_decision response_id=%s stream_id=%s chunk_index=%s scanner_type=%s partial_action=%s buffered_bytes=%s emitted_bytes=%s fail_open=%s policy_hash=%s",
            self._accumulator.response_id,
            self._stream_id(),
            self._chunk_index,
            scanner_type,
            partial_action,
            self._buffered_bytes(),
            0,
            self._guardrails.fail_open,
            self._guardrails.policy_hash,
        )

    def _log_scanner_error(self, *, scanner_type: str, phase: str) -> None:
        logger.error(
            "output_guardrails_stream_scanner_error response_id=%s stream_id=%s chunk_index=%s scanner_type=%s fail_open=%s policy_hash=%s phase=%s buffered_bytes=%s",
            self._accumulator.response_id,
            self._stream_id(),
            self._chunk_index,
            scanner_type,
            self._guardrails.fail_open,
            self._guardrails.policy_hash,
            phase,
            self._buffered_bytes(),
        )

    def _log_finalize(self, *, result: StreamingFinalizeResult) -> None:
        scanner_types = ",".join(item["type"] for item in result.triggered_scanners)
        logger.info(
            "output_guardrails_stream_finalize response_id=%s stream_id=%s scanner_type=%s final_action=%s buffered_bytes=%s emitted_bytes=%s fail_open=%s policy_hash=%s",
            self._accumulator.response_id,
            self._stream_id(),
            scanner_types,
            result.action or "allow",
            self._buffered_bytes(),
            self._content_bytes(result.content),
            self._guardrails.fail_open,
            self._guardrails.policy_hash,
        )

    def _build_streaming_scanners(self) -> list[DeterministicStreamingScanner]:
        scanners: list[DeterministicStreamingScanner] = []
        for parsed, scanner in self._guardrails._build_scanners_with_configs():
            if parsed.type not in SUPPORTED_STREAMING_SCANNERS:
                continue
            scanners.append(
                DeterministicStreamingScanner(
                    parsed=parsed,
                    scanner=scanner,
                    action_on_hit=parsed.action_on_hit
                    or self._guardrails.action_on_hit,
                )
            )
        return scanners

    async def wrap(self, upstream_lines: AsyncIterator[str]) -> AsyncIterator[bytes]:
        try:
            async for line in upstream_lines:
                stripped = line.strip()
                if not stripped:
                    continue
                if not stripped.startswith("data:"):
                    continue
                payload = stripped[len("data:") :].strip()
                if payload == "[DONE]":
                    break

                chunk = json.loads(payload)
                self._accumulator.add_chunk(chunk)
                self._chunk_index += 1
                stream_chunks_scanned_total.inc()
                stream_buffer_bytes.set(self._buffered_bytes())
                self._log_chunk_scanned()
                for scanner in self._scanners:
                    self._error_scanner_type = scanner.parsed.type
                    self._error_phase = "chunk"
                    decision_started_at = time.perf_counter()
                    decision = scanner.on_chunk(self._accumulator.content)
                    self._observe_decision_latency(
                        scanner.parsed.type,
                        "chunk",
                        decision.state,
                        decision_started_at,
                    )
                    if decision.state == STREAMING_DECISION_ERROR:
                        raise OutputGuardrailsError(
                            "Output guardrails failed while scanning the streamed model response."
                        )
                    if (
                        decision.state == STREAMING_DECISION_EMIT
                        and decision.content != self._accumulator.content
                    ):
                        raise OutputGuardrailsError(
                            "Streaming guardrails only support finalize-time redaction."
                        )
                    if decision.state == STREAMING_DECISION_BLOCK:
                        self._blocked_during_stream = True
                        self._accumulator.finish_reason = "content_filter"
                        self._log_partial_decision(scanner.parsed.type, "block")
                        async for chunk_bytes in self._finalize_stream():
                            yield chunk_bytes
                        return

            self._error_phase = "finalize"
            async for chunk in self._finalize_stream():
                yield chunk
        except Exception:
            stream_scanner_errors_total.labels(
                scanner_type=self._error_scanner_type,
                phase=self._error_phase,
                fail_open=str(self._guardrails.fail_open).lower(),
            ).inc()
            self._log_scanner_error(
                scanner_type=self._error_scanner_type,
                phase=self._error_phase,
            )
            logger.exception(
                "output_guardrails_streaming_failed fail_open=%s",
                self._guardrails.fail_open,
            )
            if self._guardrails.fail_open:
                async for chunk_bytes in self._emit_passthrough_stream():
                    yield chunk_bytes
                return

            output_guardrails_actions_total.labels(action="fail_closed").inc()
            error = {
                "error": {
                    "message": "Output guardrails failed while scanning the streamed model response.",
                    "type": "server_error",
                }
            }
            yield self._format_sse(error)
            yield b"data: [DONE]\n\n"
            return

    async def _finalize_stream(self) -> AsyncIterator[bytes]:
        prompt = self._guardrails._extract_prompt(self._request)
        result = self._finalize_content(prompt)

        if result.action is not None:
            output_guardrails_actions_total.labels(action=result.action).inc()
            if result.action == "block":
                if result.triggered_scanners:
                    for scanner_info in result.triggered_scanners:
                        stream_blocks_total.labels(
                            scanner_type=scanner_info["type"]
                        ).inc()
                else:
                    stream_blocks_total.labels(scanner_type="stream").inc()
            elif result.action == "redact":
                for scanner_info in result.triggered_scanners:
                    stream_redactions_total.labels(
                        scanner_type=scanner_info["type"]
                    ).inc()

        stream_finalize_actions_total.labels(
            final_action=result.action or "allow"
        ).inc()
        self._log_finalize(result=result)

        finish_reason = self._accumulator.finish_reason
        if result.action == "block":
            finish_reason = "content_filter"
        async for chunk_bytes in self._emit_terminal_stream(
            result.content,
            finish_reason=finish_reason or "stop",
        ):
            yield chunk_bytes

    @staticmethod
    def _format_sse(payload: dict[str, Any]) -> bytes:
        return f"data: {json.dumps(payload)}\n\n".encode()

    async def _emit_passthrough_stream(self) -> AsyncIterator[bytes]:
        async for chunk_bytes in self._emit_terminal_stream(
            self._accumulator.content,
            finish_reason=self._accumulator.finish_reason or "stop",
        ):
            yield chunk_bytes

    async def _emit_terminal_stream(
        self, content: str, *, finish_reason: str
    ) -> AsyncIterator[bytes]:
        if content:
            yield self._format_sse(
                {
                    "id": self._accumulator.response_id or "chatcmpl-stream",
                    "object": "chat.completion.chunk",
                    "created": self._accumulator.created or int(time.time()),
                    "model": self._accumulator.model,
                    "choices": [
                        {
                            "index": 0,
                            "delta": {
                                "role": self._accumulator.role,
                                "content": content,
                            },
                            "finish_reason": None,
                        }
                    ],
                }
            )

        yield self._format_sse(
            {
                "id": self._accumulator.response_id or "chatcmpl-stream",
                "object": "chat.completion.chunk",
                "created": self._accumulator.created or int(time.time()),
                "model": self._accumulator.model,
                "choices": [
                    {
                        "index": 0,
                        "delta": {},
                        "finish_reason": finish_reason,
                    }
                ],
            }
        )
        yield b"data: [DONE]\n\n"

    def _finalize_content(self, prompt: str) -> StreamingFinalizeResult:
        if self._blocked_during_stream:
            return StreamingFinalizeResult(
                content=self._guardrails.block_message,
                action="block",
            )

        sanitized_content = self._accumulator.content
        triggered_scanners: list[dict[str, Any]] = []
        final_action: str | None = None

        for scanner in self._scanners:
            self._error_scanner_type = scanner.parsed.type
            self._error_phase = "finalize"
            decision_started_at = time.perf_counter()
            decision = scanner.finalize(prompt, sanitized_content)
            self._observe_decision_latency(
                scanner.parsed.type,
                "finalize",
                decision.state,
                decision_started_at,
            )
            if decision.state == STREAMING_DECISION_ERROR:
                raise OutputGuardrailsError(
                    "Output guardrails failed while scanning the streamed model response."
                )
            if (
                decision.state == STREAMING_DECISION_EMIT
                and decision.content == sanitized_content
            ):
                continue

            triggered_scanners.append(
                {
                    "type": scanner.parsed.type,
                    "action": scanner.action_on_hit,
                    "scores": decision.scores,
                }
            )
            if decision.state == STREAMING_DECISION_BLOCK:
                return StreamingFinalizeResult(
                    content=self._guardrails.block_message,
                    action="block",
                    triggered_scanners=tuple(triggered_scanners),
                )

            sanitized_content = decision.content
            final_action = "redact"

        return StreamingFinalizeResult(
            content=sanitized_content,
            action=final_action,
            triggered_scanners=tuple(triggered_scanners),
        )
