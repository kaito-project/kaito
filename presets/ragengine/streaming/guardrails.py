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

from collections.abc import AsyncIterator
from dataclasses import dataclass
from re import _parser as regex_parser
from typing import Any

from fastapi import HTTPException
from llm_guard import scan_output

from ragengine.guardrails import OutputGuardrails
from ragengine.guardrails.scanner_schemas import (
    BanSubstringsConfig,
    RegexConfig,
)
from ragengine.streaming.buffer_window import StreamingBufferWindow, WindowScanResult
from ragengine.streaming.openai import (
    OpenAIChatChoiceDelta,
    OpenAIChatChunkParseStatus,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import iter_sse_events

STREAMING_GUARDRAILS_SUPPORTED_SCANNERS = frozenset({"ban_substrings", "regex"})


@dataclass(frozen=True)
class StreamingGuardrailsSupport:
    supported: bool
    detail: str | None = None


def validate_streaming_guardrails(
    guardrails: OutputGuardrails,
) -> StreamingGuardrailsSupport:
    for scanner_config in guardrails.scanner_configs:
        scanner_action = scanner_config.action_on_hit or guardrails.action_on_hit
        if scanner_action != "block":
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with output guardrails only supports "
                    "action=block. Unsupported action: "
                    f"{scanner_action}."
                ),
            )
        if scanner_config.type not in STREAMING_GUARDRAILS_SUPPORTED_SCANNERS:
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with output guardrails only supports "
                    "ban_substrings and regex scanners. Unsupported scanner: "
                    f"{scanner_config.type}."
                ),
            )
        if _scanner_holdback_len(scanner_config.config) is None:
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with output guardrails only supports regex "
                    "patterns with bounded maximum width."
                ),
            )

    return StreamingGuardrailsSupport(supported=True)


async def apply_streaming_guardrails(
    upstream_chunks: AsyncIterator[str],
    guardrails: OutputGuardrails,
    request: dict[str, Any],
) -> AsyncIterator[str]:
    try:
        built_scanners = guardrails._build_scanners_with_configs()
        if not built_scanners:
            async for chunk in upstream_chunks:
                yield chunk
            return

        prompt = guardrails._extract_prompt(request)
        scanner = _LLMGuardWindowScanner(prompt=prompt, built_scanners=built_scanners)
        holdback_len = _calculate_streaming_holdback_len(guardrails)
        windows: dict[int, StreamingBufferWindow] = {}

        async for event in iter_sse_events(upstream_chunks):
            parse_result = parse_openai_chat_sse_event(event)
            if parse_result.status == OpenAIChatChunkParseStatus.DONE:
                async for chunk in _flush_windows_or_block(windows, guardrails):
                    yield chunk
                if _any_window_blocked(windows):
                    return
                yield build_sse_done_chunk()
                return

            if parse_result.status != OpenAIChatChunkParseStatus.PARSED:
                async for chunk in _emit_refusal(guardrails):
                    yield chunk
                return

            if _has_passthrough_delta(parse_result.choice_deltas):
                async for chunk in _flush_windows_or_block(windows, guardrails):
                    yield chunk
                if _any_window_blocked(windows):
                    return
                yield _raw_sse_chunk(event.raw)
                continue

            finish_deltas: list[OpenAIChatChoiceDelta] = []
            for delta in parse_result.choice_deltas:
                if delta.content is not None:
                    window = _window_for_choice(
                        windows,
                        delta.choice_index,
                        scanner=scanner,
                        holdback_len=holdback_len,
                    )
                    emit_result = window.feed(delta.content)
                    if emit_result.blocked:
                        async for chunk in _emit_refusal(guardrails):
                            yield chunk
                        return
                    for safe_chunk in emit_result.chunks:
                        yield build_openai_chat_delta_sse_chunk(
                            safe_chunk,
                            choice_index=delta.choice_index,
                        )
                if delta.finish_reason is not None:
                    finish_deltas.append(delta)

            for delta in finish_deltas:
                window = windows.get(delta.choice_index)
                if window is not None:
                    async for chunk in _flush_window_or_block(
                        window,
                        guardrails,
                        choice_index=delta.choice_index,
                    ):
                        yield chunk
                    if window.blocked:
                        return
                yield build_openai_chat_finish_sse_chunk(
                    finish_reason=delta.finish_reason,
                    choice_index=delta.choice_index,
                )

        async for chunk in _flush_windows_or_block(windows, guardrails):
            yield chunk
    finally:
        await _aclose(upstream_chunks)


class _LLMGuardWindowScanner:
    def __init__(self, *, prompt: str, built_scanners: list[tuple[Any, Any]]) -> None:
        self._prompt = prompt
        self._built_scanners = built_scanners

    def scan(self, text: str) -> WindowScanResult:
        for _, scanner in self._built_scanners:
            _, results_valid, _ = scan_output(
                [scanner], self._prompt, text, fail_fast=False
            )
            if not all(results_valid.values()):
                return WindowScanResult(blocked=True)
        return WindowScanResult()


async def _flush_window_or_block(
    window: StreamingBufferWindow,
    guardrails: OutputGuardrails,
    *,
    choice_index: int,
) -> AsyncIterator[str]:
    flush_result = window.flush()
    if flush_result.blocked:
        async for chunk in _emit_refusal(guardrails):
            yield chunk
        return

    for safe_chunk in flush_result.chunks:
        yield build_openai_chat_delta_sse_chunk(
            safe_chunk,
            choice_index=choice_index,
        )


async def _flush_windows_or_block(
    windows: dict[int, StreamingBufferWindow],
    guardrails: OutputGuardrails,
) -> AsyncIterator[str]:
    for choice_index, window in sorted(windows.items()):
        async for chunk in _flush_window_or_block(
            window,
            guardrails,
            choice_index=choice_index,
        ):
            yield chunk
        if window.blocked:
            return


async def _emit_refusal(guardrails: OutputGuardrails) -> AsyncIterator[str]:
    guardrails._record_response_action("block")
    yield build_openai_chat_delta_sse_chunk(guardrails.block_message)
    yield build_openai_chat_finish_sse_chunk(finish_reason="content_filter")
    yield build_sse_done_chunk()


async def _aclose(upstream_chunks: AsyncIterator[str]) -> None:
    aclose = getattr(upstream_chunks, "aclose", None)
    if aclose is not None:
        await aclose()


def _raw_sse_chunk(raw_event: str) -> str:
    return f"{raw_event}\n\n"


def _window_for_choice(
    windows: dict[int, StreamingBufferWindow],
    choice_index: int,
    *,
    scanner: _LLMGuardWindowScanner,
    holdback_len: int,
) -> StreamingBufferWindow:
    window = windows.get(choice_index)
    if window is None:
        window = StreamingBufferWindow(
            scanner,
            holdback_len=holdback_len,
        )
        windows[choice_index] = window
    return window


def _has_passthrough_delta(deltas: tuple[OpenAIChatChoiceDelta, ...]) -> bool:
    return any(delta.passthrough for delta in deltas)


def _any_window_blocked(windows: dict[int, StreamingBufferWindow]) -> bool:
    return any(window.blocked for window in windows.values())


def _calculate_streaming_holdback_len(guardrails: OutputGuardrails) -> int:
    holdback_len = 0
    for scanner_config in guardrails.scanner_configs:
        scanner_holdback = _scanner_holdback_len(scanner_config.config)
        if scanner_holdback is None:
            raise ValueError("streaming scanner holdback length must be bounded.")
        holdback_len = max(holdback_len, scanner_holdback)
    return holdback_len


def _scanner_holdback_len(config: Any) -> int | None:
    if isinstance(config, BanSubstringsConfig):
        return max((len(substring) - 1 for substring in config.substrings), default=0)
    if isinstance(config, RegexConfig):
        max_pattern_width = 0
        for pattern in config.patterns:
            pattern_width = _regex_max_width(pattern)
            if pattern_width is None:
                return None
            max_pattern_width = max(max_pattern_width, pattern_width)
        return max(0, max_pattern_width - 1)
    return None


def _regex_max_width(pattern: str) -> int | None:
    _, max_width = regex_parser.parse(pattern, 0).getwidth()
    if max_width >= regex_parser.MAXWIDTH:
        return None
    return max_width


def raise_if_streaming_guardrails_unsupported(guardrails: OutputGuardrails) -> None:
    support = validate_streaming_guardrails(guardrails)
    if not support.supported:
        raise HTTPException(status_code=400, detail=support.detail)
