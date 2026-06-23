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

import logging
from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Any

from fastapi import HTTPException
from llm_guard import scan_output

from ragengine.guardrails import OutputGuardrails
from ragengine.metrics.prometheus_metrics import (
    guardrails_stream_actions_total,
    guardrails_stream_parse_events_total,
    guardrails_stream_scanner_hits_total,
)
from ragengine.streaming.openai import (
    OpenAIChatChunkParseStatus,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import iter_sse_events

logger = logging.getLogger(__name__)

STREAMING_GUARDRAILS_HOLDBACK_CHARS = 256
STREAMING_GUARDRAILS_MIN_SCAN_CHARS = 1
STREAMING_GUARDRAILS_MAX_EMIT_CHARS = 4096
STREAMING_GUARDRAILS_SUPPORTED_SCANNERS = frozenset({"ban_substrings", "regex"})
STREAMING_GUARDRAILS_SUPPORTED_ACTIONS = frozenset({"block", "redact"})


@dataclass(frozen=True)
class StreamingGuardrailsSupport:
    supported: bool
    detail: str | None = None


@dataclass(frozen=True)
class _StreamingScanResult:
    output: str
    action: str = "allow"
    blocked: bool = False


@dataclass(frozen=True)
class _StreamingEmitResult:
    chunks: tuple[str, ...]
    action: str = "allow"
    blocked: bool = False


def validate_streaming_guardrails(
    guardrails: OutputGuardrails,
) -> StreamingGuardrailsSupport:
    for scanner_config in guardrails.scanner_configs:
        scanner_action = scanner_config.action_on_hit or guardrails.action_on_hit
        if scanner_action not in STREAMING_GUARDRAILS_SUPPORTED_ACTIONS:
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with output guardrails only supports "
                    "action=block or action=redact. Unsupported action: "
                    f"{scanner_action}."
                ),
            )
        if scanner_config.type not in STREAMING_GUARDRAILS_SUPPORTED_SCANNERS:
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with output guardrails only supports "
                    "ban_substrings and regex scanners. Policies requiring "
                    "full-output scanning are rejected for streaming. "
                    "Unsupported scanner: "
                    f"{scanner_config.type}."
                ),
            )

    return StreamingGuardrailsSupport(supported=True)


async def apply_streaming_guardrails(
    upstream_chunks: AsyncIterator[str],
    guardrails: OutputGuardrails,
    request: dict[str, Any],
) -> AsyncIterator[str]:
    built_scanners = guardrails._build_scanners_with_configs()
    if not built_scanners:
        async for chunk in upstream_chunks:
            yield chunk
        return

    prompt = guardrails._extract_prompt(request)
    scanner = _LLMGuardStreamingScanner(
        prompt=prompt,
        guardrails=guardrails,
        built_scanners=built_scanners,
    )
    window = _StreamingGuardrailsWindow(
        scanner,
        holdback_chars=STREAMING_GUARDRAILS_HOLDBACK_CHARS,
        min_scan_chars=STREAMING_GUARDRAILS_MIN_SCAN_CHARS,
        max_emit_chars=STREAMING_GUARDRAILS_MAX_EMIT_CHARS,
    )
    stream_action = "allow"

    async for event in iter_sse_events(upstream_chunks):
        parse_result = parse_openai_chat_sse_event(event)
        _record_parse_status(guardrails, parse_result.status.value)
        if parse_result.status == OpenAIChatChunkParseStatus.DONE:
            async for chunk in _flush_window_or_block(
                window, guardrails, upstream_chunks
            ):
                yield chunk
            if window.blocked:
                return
            stream_action = _combine_actions(stream_action, window.last_action)
            _record_stream_action(guardrails, stream_action)
            yield build_sse_done_chunk()
            return

        if parse_result.status != OpenAIChatChunkParseStatus.PARSED:
            logger.warning(
                "streaming_guardrails_parse_status status=%s policy_hash=%s",
                parse_result.status.value,
                guardrails.policy_hash,
            )
            yield _raw_sse_chunk(event.raw)
            continue

        for content in parse_result.contents:
            emit_result = window.feed(content)
            stream_action = _combine_actions(stream_action, emit_result.action)
            if emit_result.blocked:
                async for chunk in _emit_block_and_close(guardrails, upstream_chunks):
                    yield chunk
                return
            for safe_chunk in emit_result.chunks:
                yield build_openai_chat_delta_sse_chunk(safe_chunk)

        if parse_result.finish_reasons:
            async for chunk in _flush_window_or_block(
                window, guardrails, upstream_chunks
            ):
                yield chunk
            if window.blocked:
                return
            stream_action = _combine_actions(stream_action, window.last_action)
            yield _raw_sse_chunk(event.raw)

    async for chunk in _flush_window_or_block(window, guardrails, upstream_chunks):
        yield chunk
    if not window.blocked:
        stream_action = _combine_actions(stream_action, window.last_action)
        _record_stream_action(guardrails, stream_action)


class _LLMGuardStreamingScanner:
    def __init__(
        self,
        *,
        prompt: str,
        guardrails: OutputGuardrails,
        built_scanners: list[tuple[Any, Any]],
    ) -> None:
        self._prompt = prompt
        self._guardrails = guardrails
        self._built_scanners = built_scanners

    def scan(self, text: str) -> _StreamingScanResult:
        output = text
        final_action = "allow"
        for scanner_config, scanner in self._built_scanners:
            scanner_action = (
                scanner_config.action_on_hit or self._guardrails.action_on_hit
            )
            sanitized_output, results_valid, results_score = scan_output(
                [scanner], self._prompt, output, fail_fast=False
            )
            output = sanitized_output
            if all(results_valid.values()):
                continue

            _record_scanner_hit(self._guardrails, scanner_config.type, scanner_action)
            logger.info(
                "streaming_guardrails_triggered action=%s scanner=%s scores=%s policy_hash=%s",
                scanner_action,
                scanner_config.type,
                results_score,
                self._guardrails.policy_hash,
            )
            if scanner_action == "block":
                return _StreamingScanResult(output="", action="block", blocked=True)

            final_action = "redact"

        return _StreamingScanResult(output=output, action=final_action)


class _StreamingGuardrailsWindow:
    def __init__(
        self,
        scanner: _LLMGuardStreamingScanner,
        *,
        holdback_chars: int,
        min_scan_chars: int,
        max_emit_chars: int,
    ) -> None:
        self._scanner = scanner
        self._holdback_chars = holdback_chars
        self._min_scan_chars = min_scan_chars
        self._max_emit_chars = max_emit_chars
        self._pending_buffer = ""
        self._blocked = False
        self._last_action = "allow"

    @property
    def blocked(self) -> bool:
        return self._blocked

    @property
    def last_action(self) -> str:
        return self._last_action

    def feed(self, text: str) -> _StreamingEmitResult:
        if self._blocked:
            return _StreamingEmitResult(chunks=(), action="block", blocked=True)

        self._pending_buffer += text
        scan_text = self._scan_window()
        if len(scan_text) < self._min_scan_chars:
            return _StreamingEmitResult(chunks=())

        return self._scan_and_emit(scan_text)

    def flush(self) -> _StreamingEmitResult:
        if self._blocked:
            return _StreamingEmitResult(chunks=(), action="block", blocked=True)
        if not self._pending_buffer:
            return _StreamingEmitResult(chunks=())

        return self._scan_and_emit(self._pending_buffer)

    def _scan_window(self) -> str:
        if self._holdback_chars == 0:
            return self._pending_buffer
        return self._pending_buffer[: -self._holdback_chars]

    def _scan_and_emit(self, scan_text: str) -> _StreamingEmitResult:
        scan_result = self._scanner.scan(scan_text)
        self._last_action = _combine_actions(self._last_action, scan_result.action)
        if scan_result.blocked:
            self._blocked = True
            return _StreamingEmitResult(chunks=(), action="block", blocked=True)

        self._pending_buffer = self._pending_buffer[len(scan_text) :]
        return _StreamingEmitResult(
            chunks=self._chunk_text(scan_result.output),
            action=scan_result.action,
        )

    def _chunk_text(self, text: str) -> tuple[str, ...]:
        if not text:
            return ()
        return tuple(
            text[index : index + self._max_emit_chars]
            for index in range(0, len(text), self._max_emit_chars)
        )


async def _flush_window_or_block(
    window: _StreamingGuardrailsWindow,
    guardrails: OutputGuardrails,
    upstream_chunks: AsyncIterator[str],
) -> AsyncIterator[str]:
    flush_result = window.flush()
    if flush_result.blocked:
        async for chunk in _emit_block_and_close(guardrails, upstream_chunks):
            yield chunk
        return

    for safe_chunk in flush_result.chunks:
        yield build_openai_chat_delta_sse_chunk(safe_chunk)


async def _emit_block_and_close(
    guardrails: OutputGuardrails,
    upstream_chunks: AsyncIterator[str],
) -> AsyncIterator[str]:
    _record_stream_action(guardrails, "block")
    yield build_openai_chat_delta_sse_chunk(guardrails.block_message)
    yield build_openai_chat_finish_sse_chunk(finish_reason="content_filter")
    yield build_sse_done_chunk()
    await _aclose(upstream_chunks)


async def _aclose(upstream_chunks: AsyncIterator[str]) -> None:
    aclose = getattr(upstream_chunks, "aclose", None)
    if aclose is not None:
        await aclose()


def _raw_sse_chunk(raw_event: str) -> str:
    return f"{raw_event}\n\n"


def _combine_actions(current: str, next_action: str) -> str:
    if current == "block" or next_action == "block":
        return "block"
    if current == "redact" or next_action == "redact":
        return "redact"
    return "allow"


def _record_scanner_hit(
    guardrails: OutputGuardrails,
    scanner_type: str,
    action: str,
) -> None:
    guardrails_stream_scanner_hits_total.labels(
        scanner_type=scanner_type,
        action=action,
        policy_hash=guardrails.policy_hash,
    ).inc()


def _record_stream_action(guardrails: OutputGuardrails, final_action: str) -> None:
    guardrails_stream_actions_total.labels(
        final_action=final_action,
        policy_hash=guardrails.policy_hash,
    ).inc()


def _record_parse_status(guardrails: OutputGuardrails, parse_status: str) -> None:
    guardrails_stream_parse_events_total.labels(
        parse_status=parse_status,
        policy_hash=guardrails.policy_hash,
    ).inc()


def raise_if_streaming_guardrails_unsupported(guardrails: OutputGuardrails) -> None:
    support = validate_streaming_guardrails(guardrails)
    if not support.supported:
        raise HTTPException(status_code=400, detail=support.detail)
