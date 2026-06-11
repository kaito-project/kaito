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
                for scanner in self._scanners:
                    decision = scanner.on_chunk(self._accumulator.content)
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
                        async for chunk_bytes in self._finalize_stream():
                            yield chunk_bytes
                        return

            async for chunk in self._finalize_stream():
                yield chunk
        except Exception:
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
            logger.info(
                "output_guardrails_streaming_triggered action=%s response_id=%s scanners=%s policy_hash=%s",
                result.action,
                self._accumulator.response_id,
                list(result.triggered_scanners),
                self._guardrails.policy_hash,
            )

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
            decision = scanner.finalize(prompt, sanitized_content)
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
