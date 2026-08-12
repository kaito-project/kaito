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

import re
from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Any

from fastapi import HTTPException
from llm_guard import scan_output

from ragengine.guardrails import OutputGuardrails
from ragengine.streaming.buffer_window import StreamingBufferWindow, WindowScanResult
from ragengine.streaming.openai import (
    OpenAIChatChunkParseStatus,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import iter_sse_events

DEFAULT_STREAMING_GUARDRAILS_HOLDBACK_LEN = 256
STREAMING_GUARDRAILS_CAPABILITIES = {
    "ban_substrings": frozenset({"block", "redact"}),
    "invisible_text": frozenset({"block", "redact"}),
    "secrets": frozenset({"block", "redact"}),
    "sensitive": frozenset({"block", "redact"}),
}
STREAMING_GUARDRAILS_SUPPORTED_SCANNERS = frozenset(STREAMING_GUARDRAILS_CAPABILITIES)


@dataclass(frozen=True)
class StreamingGuardrailsSupport:
    supported: bool
    detail: str | None = None


def validate_streaming_guardrails(
    guardrails: OutputGuardrails,
) -> StreamingGuardrailsSupport:
    for scanner_config in guardrails.scanner_configs:
        scanner_action = scanner_config.action_on_hit or guardrails.action_on_hit
        supported_actions = STREAMING_GUARDRAILS_CAPABILITIES.get(scanner_config.type)
        if supported_actions is None:
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with output guardrails only supports "
                    f"{sorted(STREAMING_GUARDRAILS_SUPPORTED_SCANNERS)} scanners. "
                    f"Unsupported scanner: {scanner_config.type}."
                ),
            )
        if scanner_action not in supported_actions:
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    f"stream=true does not support action={scanner_action} for "
                    f"scanner={scanner_config.type}. Supported actions: "
                    f"{sorted(supported_actions)}."
                ),
            )
        if (
            scanner_config.type == "ban_substrings"
            and scanner_config.config.contains_all
        ):
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true does not support contains_all=true for "
                    "scanner=ban_substrings because the current windowed "
                    "implementation cannot track matches across the complete response."
                ),
            )
        if (
            scanner_config.type == "secrets"
            and scanner_action == "redact"
            and scanner_config.config.redact_mode != "all"
        ):
            return StreamingGuardrailsSupport(
                supported=False,
                detail=(
                    "stream=true with action=redact for scanner=secrets only "
                    "supports redact_mode=all."
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
        scanner = _LLMGuardWindowScanner(
            prompt=prompt,
            built_scanners=built_scanners,
            default_action_on_hit=guardrails.action_on_hit,
        )
        window = StreamingBufferWindow(
            scanner,
            holdback_len=_get_streaming_guardrails_holdback_len(guardrails),
        )

        async for event in iter_sse_events(upstream_chunks):
            parse_result = parse_openai_chat_sse_event(event)
            if parse_result.status == OpenAIChatChunkParseStatus.DONE:
                async for chunk in _flush_window_or_block(window, guardrails):
                    yield chunk
                if window.blocked:
                    return
                _record_successful_redaction(window, guardrails)
                yield build_sse_done_chunk()
                return

            if parse_result.status != OpenAIChatChunkParseStatus.PARSED:
                async for chunk in _emit_refusal(guardrails):
                    yield chunk
                return

            if len(parse_result.parsed_choices) > 1 or any(
                choice.choice_index != 0 for choice in parse_result.parsed_choices
            ):
                async for chunk in _emit_refusal(guardrails):
                    yield chunk
                return

            for content in parse_result.contents:
                emit_result = window.feed(content)
                if emit_result.blocked:
                    async for chunk in _emit_refusal(guardrails):
                        yield chunk
                    return
                for safe_chunk in emit_result.chunks:
                    yield build_openai_chat_delta_sse_chunk(safe_chunk)

            if parse_result.finish_reasons:
                async for chunk in _flush_window_or_block(window, guardrails):
                    yield chunk
                if window.blocked:
                    return
                yield _raw_sse_chunk(event.raw)

        async for chunk in _flush_window_or_block(window, guardrails):
            yield chunk
        if not window.blocked:
            _record_successful_redaction(window, guardrails)
    finally:
        await _aclose(upstream_chunks)


def _get_streaming_guardrails_holdback_len(guardrails: OutputGuardrails) -> int:
    required_holdback = max(
        (
            len(substring)
            if scanner_config.config.match_type == "word"
            else len(substring) - 1
            for scanner_config in guardrails.scanner_configs
            if scanner_config.type == "ban_substrings"
            for substring in scanner_config.config.substrings
        ),
        default=0,
    )
    return max(DEFAULT_STREAMING_GUARDRAILS_HOLDBACK_LEN, required_holdback)


class _LLMGuardWindowScanner:
    def __init__(
        self,
        *,
        prompt: str,
        built_scanners: list[tuple[Any, Any]],
        default_action_on_hit: str,
    ) -> None:
        self._prompt = prompt
        self._built_scanners = built_scanners
        self._default_action_on_hit = default_action_on_hit

    def scan(
        self,
        text: str,
        *,
        flush: bool = False,
        preceding_char: str = "",
    ) -> WindowScanResult:
        sanitized_text = text
        for scanner_config, scanner in self._built_scanners:
            scanner_action = scanner_config.action_on_hit or self._default_action_on_hit
            if scanner_action != "redact":
                continue

            if scanner_config.type == "secrets":
                redacted_text = _redact_secrets_and_verify(
                    scanner,
                    self._prompt,
                    sanitized_text,
                )
                if redacted_text is None:
                    return WindowScanResult(blocked=True)
                sanitized_text = redacted_text
                continue

            if _is_word_match_scanner(scanner_config):
                match_spans = _find_word_match_spans(
                    sanitized_text,
                    scanner_config.config.substrings,
                    case_sensitive=scanner_config.config.case_sensitive,
                    preceding_char=preceding_char,
                    flush=flush,
                )
                if match_spans:
                    sanitized_text = _redact_match_spans(
                        sanitized_text,
                        match_spans,
                    )
                continue

            scan_result = self._scan_output(
                scanner,
                sanitized_text,
            )
            if scan_result is None:
                return WindowScanResult(blocked=True)
            scanner_output, results_valid = scan_result
            if not all(results_valid.values()):
                if (
                    not isinstance(scanner_output, str)
                    or scanner_output == sanitized_text
                ):
                    return WindowScanResult(blocked=True)
                sanitized_text = scanner_output

        for scanner_config, scanner in self._built_scanners:
            scanner_action = scanner_config.action_on_hit or self._default_action_on_hit
            if scanner_action != "block":
                continue

            if _is_word_match_scanner(scanner_config):
                if _find_word_match_spans(
                    sanitized_text,
                    scanner_config.config.substrings,
                    case_sensitive=scanner_config.config.case_sensitive,
                    preceding_char=preceding_char,
                    flush=flush,
                ):
                    return WindowScanResult(blocked=True)
                continue

            scan_result = self._scan_output(
                scanner,
                sanitized_text,
            )
            if scan_result is None:
                return WindowScanResult(blocked=True)
            _, results_valid = scan_result
            if not all(results_valid.values()):
                return WindowScanResult(blocked=True)

        if sanitized_text == text:
            return WindowScanResult()
        return WindowScanResult(sanitized_text=sanitized_text)

    def _scan_output(
        self,
        scanner: Any,
        text: str,
    ) -> tuple[str, dict[str, bool]] | None:
        scanner_output, results_valid, _ = scan_output(
            [scanner],
            self._prompt,
            text,
            fail_fast=False,
        )
        if not isinstance(scanner_output, str):
            return None

        return scanner_output, results_valid


def _is_word_match_scanner(scanner_config: Any) -> bool:
    return (
        scanner_config.type == "ban_substrings"
        and scanner_config.config.match_type == "word"
    )


def _find_word_match_spans(
    text: str,
    substrings: list[str],
    *,
    case_sensitive: bool,
    preceding_char: str,
    flush: bool,
) -> tuple[tuple[int, int], ...]:
    flags = 0 if case_sensitive else re.IGNORECASE
    spans: list[tuple[int, int]] = []

    for substring in substrings:
        for match in re.finditer(re.escape(substring), text, flags):
            start, end = match.span()
            left_char = text[start - 1] if start > 0 else preceding_char
            if _is_word_char(left_char) == _is_word_char(text[start]):
                continue
            if end == len(text) and not flush:
                continue
            right_char = text[end] if end < len(text) else ""
            if _is_word_char(text[end - 1]) == _is_word_char(right_char):
                continue
            spans.append((start, end))

    return _merge_match_spans(spans)


def _is_word_char(value: str) -> bool:
    return bool(value and re.fullmatch(r"\w", value))


def _merge_match_spans(
    spans: list[tuple[int, int]],
) -> tuple[tuple[int, int], ...]:
    merged: list[tuple[int, int]] = []
    for start, end in sorted(spans):
        if merged and start < merged[-1][1]:
            merged[-1] = (merged[-1][0], max(merged[-1][1], end))
        else:
            merged.append((start, end))
    return tuple(merged)


def _redact_match_spans(text: str, spans: tuple[tuple[int, int], ...]) -> str:
    redacted_parts: list[str] = []
    cursor = 0
    for start, end in spans:
        redacted_parts.extend((text[cursor:start], "[REDACTED]"))
        cursor = end
    redacted_parts.append(text[cursor:])
    return "".join(redacted_parts)


def _redact_secrets_and_verify(scanner: Any, prompt: str, text: str) -> str | None:
    sanitized, results_valid, _ = scan_output([scanner], prompt, text, fail_fast=False)
    if not isinstance(sanitized, str):
        return None
    if all(results_valid.values()):
        return text if sanitized == text else None
    if sanitized == text or len(sanitized) > len(text):
        return None

    verified, verified_valid, _ = scan_output(
        [scanner], prompt, sanitized, fail_fast=False
    )
    if (
        not isinstance(verified, str)
        or verified != sanitized
        or not all(verified_valid.values())
    ):
        return None

    return sanitized


async def _flush_window_or_block(
    window: StreamingBufferWindow,
    guardrails: OutputGuardrails,
) -> AsyncIterator[str]:
    flush_result = window.flush()
    if flush_result.blocked:
        async for chunk in _emit_refusal(guardrails):
            yield chunk
        return

    for safe_chunk in flush_result.chunks:
        yield build_openai_chat_delta_sse_chunk(safe_chunk)


async def _emit_refusal(guardrails: OutputGuardrails) -> AsyncIterator[str]:
    guardrails._record_response_action("block")
    yield build_openai_chat_delta_sse_chunk(guardrails.block_message)
    yield build_openai_chat_finish_reason_sse_chunk(finish_reason="content_filter")
    yield build_sse_done_chunk()


def _record_successful_redaction(
    window: StreamingBufferWindow,
    guardrails: OutputGuardrails,
) -> None:
    if window.redacted:
        guardrails._record_response_action("redact")


async def _aclose(upstream_chunks: AsyncIterator[str]) -> None:
    aclose = getattr(upstream_chunks, "aclose", None)
    if aclose is not None:
        await aclose()


def _raw_sse_chunk(raw_event: str) -> str:
    return f"{raw_event}\n\n"


def raise_if_streaming_guardrails_unsupported(guardrails: OutputGuardrails) -> None:
    support = validate_streaming_guardrails(guardrails)
    if not support.supported:
        raise HTTPException(status_code=400, detail=support.detail)
