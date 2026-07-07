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
from typing import Any

from fastapi import HTTPException
from llm_guard import scan_output

from ragengine.guardrails import OutputGuardrails
from ragengine.streaming.buffer_window import StreamingBufferWindow, WindowScanResult
from ragengine.streaming.openai import (
    OpenAIChatChunkParseStatus,
    ParsedOpenAIChoiceKind,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_data_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import iter_sse_events

STREAMING_GUARDRAILS_HOLDBACK_LEN = 256
STREAMING_GUARDRAILS_SUPPORTED_SCANNERS = frozenset({"ban_substrings"})


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
                    "ban_substrings scanners. Unsupported scanner: "
                    f"{scanner_config.type}."
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
        window = StreamingBufferWindow(
            scanner,
            holdback_len=STREAMING_GUARDRAILS_HOLDBACK_LEN,
        )
        pending_content_choice_index = 0

        async for event in iter_sse_events(upstream_chunks):
            parse_result = parse_openai_chat_sse_event(event)
            if parse_result.status == OpenAIChatChunkParseStatus.DONE:
                async for chunk in _flush_window_or_block(
                    window,
                    guardrails,
                    choice_index=pending_content_choice_index,
                ):
                    yield chunk
                if window.blocked:
                    return
                yield build_sse_done_chunk()
                return

            if parse_result.status != OpenAIChatChunkParseStatus.PARSED:
                async for chunk in _emit_refusal(guardrails):
                    yield chunk
                return

            has_passthrough_choice = False
            for parsed_choice in parse_result.parsed_choices:
                if parsed_choice.kind == ParsedOpenAIChoiceKind.CONTENT:
                    pending_content_choice_index = parsed_choice.choice_index
                    emit_result = window.feed(parsed_choice.content or "")
                    if emit_result.blocked:
                        async for chunk in _emit_refusal(guardrails):
                            yield chunk
                        return
                    for safe_chunk in emit_result.chunks:
                        yield build_openai_chat_delta_sse_chunk(
                            safe_chunk,
                            choice_index=parsed_choice.choice_index,
                        )
                    continue

                has_passthrough_choice = True

            if has_passthrough_choice:
                async for chunk in _flush_window_or_block(
                    window,
                    guardrails,
                    choice_index=pending_content_choice_index,
                ):
                    yield chunk
                if window.blocked:
                    return
                passthrough_payload = _build_passthrough_payload(parse_result.payload)
                if passthrough_payload is not None:
                    yield build_sse_data_chunk(passthrough_payload)

        async for chunk in _flush_window_or_block(
            window,
            guardrails,
            choice_index=pending_content_choice_index,
        ):
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
    choice_index: int = 0,
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


def _build_passthrough_payload(payload: dict[str, Any] | None) -> dict[str, Any] | None:
    if payload is None:
        return None

    passthrough_choices = []
    for choice in payload.get("choices", []):
        delta = choice.get("delta") or {}
        passthrough_delta = {
            key: value for key, value in delta.items() if key != "content"
        }
        has_finish_reason = choice.get("finish_reason") is not None
        if not passthrough_delta and not has_finish_reason:
            continue

        passthrough_choice = dict(choice)
        passthrough_choice["delta"] = passthrough_delta
        passthrough_choices.append(passthrough_choice)

    if not passthrough_choices:
        return None

    passthrough_payload = dict(payload)
    passthrough_payload["choices"] = passthrough_choices
    return passthrough_payload


async def _emit_refusal(guardrails: OutputGuardrails) -> AsyncIterator[str]:
    guardrails._record_response_action("block")
    yield build_openai_chat_delta_sse_chunk(guardrails.block_message)
    yield build_openai_chat_finish_reason_sse_chunk(finish_reason="content_filter")
    yield build_sse_done_chunk()


async def _aclose(upstream_chunks: AsyncIterator[str]) -> None:
    aclose = getattr(upstream_chunks, "aclose", None)
    if aclose is not None:
        await aclose()


def raise_if_streaming_guardrails_unsupported(guardrails: OutputGuardrails) -> None:
    support = validate_streaming_guardrails(guardrails)
    if not support.supported:
        raise HTTPException(status_code=400, detail=support.detail)
