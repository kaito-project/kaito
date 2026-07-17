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
from typing import Any

from fastapi import HTTPException
from llm_guard import scan_output

from ragengine.guardrails import OutputGuardrails
from ragengine.streaming.buffer_window import WindowScanResult
from ragengine.streaming.openai import (
    OpenAIChatChunkParseStatus,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.ordered_sse_guardrail_buffer import (
    OrderedGuardrailBuffer,
    ProcessResult,
    ProcessStatus,
)
from ragengine.streaming.sse import iter_sse_events

STREAMING_GUARDRAILS_SUPPORTED_SCANNERS = frozenset({"ban_substrings"})

logger = logging.getLogger(__name__)


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
        buffer = OrderedGuardrailBuffer(scanner)
        saw_done = False

        async for event in iter_sse_events(upstream_chunks):
            parse_result = parse_openai_chat_sse_event(event)
            if parse_result.status == OpenAIChatChunkParseStatus.DONE:
                saw_done = True
                break

            if parse_result.status == OpenAIChatChunkParseStatus.INVALID:
                logger.warning(
                    "streaming_guardrails_fail_closed status=%s reason=%s",
                    parse_result.status,
                    parse_result.error,
                )
                for chunk in _build_refusal_chunks(guardrails, record_action=False):
                    yield chunk
                return

            result = buffer.push(event, parse_result)
            for ready_event in result.ready_events:
                yield ready_event
            if result.status != ProcessStatus.OK:
                _log_fail_closed(result)
                for chunk in _build_refusal_chunks(
                    guardrails,
                    choice_index=result.blocked_choice_index or 0,
                    record_action=result.status == ProcessStatus.BLOCKED,
                ):
                    yield chunk
                return

        result = buffer.finish_stream()
        for ready_event in result.ready_events:
            yield ready_event
        if result.status != ProcessStatus.OK:
            _log_fail_closed(result)
            for chunk in _build_refusal_chunks(
                guardrails,
                choice_index=result.blocked_choice_index or 0,
                record_action=result.status == ProcessStatus.BLOCKED,
            ):
                yield chunk
            return
        if saw_done:
            yield build_sse_done_chunk()
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


def _build_refusal_chunks(
    guardrails: OutputGuardrails,
    *,
    choice_index: int = 0,
    record_action: bool = True,
) -> tuple[str, str, str]:
    if record_action:
        guardrails._record_response_action("block")
    return (
        build_openai_chat_delta_sse_chunk(
            guardrails.block_message,
            choice_index=choice_index,
        ),
        build_openai_chat_finish_reason_sse_chunk(
            finish_reason="content_filter",
            choice_index=choice_index,
        ),
        build_sse_done_chunk(),
    )


def _log_fail_closed(result: ProcessResult) -> None:
    if result.status == ProcessStatus.BLOCKED:
        return
    logger.warning(
        "streaming_guardrails_fail_closed status=%s reason=%s",
        result.status,
        result.reason,
    )


async def _aclose(upstream_chunks: AsyncIterator[str]) -> None:
    aclose = getattr(upstream_chunks, "aclose", None)
    if aclose is not None:
        await aclose()


def raise_if_streaming_guardrails_unsupported(guardrails: OutputGuardrails) -> None:
    for scanner_config in guardrails.scanner_configs:
        scanner_action = scanner_config.action_on_hit or guardrails.action_on_hit
        if scanner_action != "block":
            raise HTTPException(
                status_code=400,
                detail=(
                    "stream=true with output guardrails only supports "
                    "action=block. Unsupported action: "
                    f"{scanner_action}."
                ),
            )
        if scanner_config.type not in STREAMING_GUARDRAILS_SUPPORTED_SCANNERS:
            raise HTTPException(
                status_code=400,
                detail=(
                    "stream=true with output guardrails only supports "
                    "ban_substrings scanners. Unsupported scanner: "
                    f"{scanner_config.type}."
                ),
            )
