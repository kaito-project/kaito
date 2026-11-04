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
from guardrail_core.streaming.scanners import ScannerWindowAdapter
from guardrail_core.streaming.sse import SSEFramingError
from guardrail_core.streaming.window import (
    StreamingBufferWindow,
    WindowScanError,
)

from ragengine.guardrails import OutputGuardrails
from ragengine.streaming.openai import (
    OpenAIChatChunkParseStatus,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)
from ragengine.streaming.sse import iter_sse_events

STREAMING_GUARDRAILS_CAPABILITIES = {
    "ban_substrings": frozenset({"block", "redact"}),
    "invisible_text": frozenset({"block", "redact"}),
    "secrets": frozenset({"block", "redact"}),
    "sensitive": frozenset({"block", "redact"}),
}
STREAMING_GUARDRAILS_SUPPORTED_SCANNERS = frozenset(STREAMING_GUARDRAILS_CAPABILITIES)
logger = logging.getLogger(__name__)


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
    upstream_chunks: AsyncIterator[bytes],
    guardrails: OutputGuardrails,
    request: dict[str, Any],
) -> AsyncIterator[str | bytes]:
    try:
        if not guardrails.scanner_configs:
            async for chunk in upstream_chunks:
                yield chunk
            return

        prompt = guardrails._extract_prompt(request)
        scanner = ScannerWindowAdapter(
            guardrails.scanner_configs,
            prompt=prompt,
            default_action=guardrails.action_on_hit,
        )
        window = StreamingBufferWindow(
            scanner,
            holdback_len=scanner.holdback_len,
        )

        async for event in iter_sse_events(upstream_chunks):
            parse_result = parse_openai_chat_sse_event(event)
            if parse_result.status == OpenAIChatChunkParseStatus.NO_DATA:
                continue
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
                yield event.raw_bytes

        async for chunk in _flush_window_or_block(window, guardrails):
            yield chunk
        if not window.blocked:
            _record_successful_redaction(window, guardrails)
    except WindowScanError as error:
        logger.error(
            "streaming_guardrails_scan_failed error_type=%s",
            type(error.__cause__).__name__,
        )
        async for chunk in _emit_refusal(guardrails):
            yield chunk
    except SSEFramingError as error:
        logger.warning("Rejecting invalid upstream SSE stream: %s", error)
        async for chunk in _emit_refusal(guardrails):
            yield chunk
    finally:
        await _aclose(upstream_chunks)


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


async def _aclose(upstream_chunks: AsyncIterator[bytes]) -> None:
    aclose = getattr(upstream_chunks, "aclose", None)
    if aclose is not None:
        await aclose()


def raise_if_streaming_guardrails_unsupported(guardrails: OutputGuardrails) -> None:
    support = validate_streaming_guardrails(guardrails)
    if not support.supported:
        raise HTTPException(status_code=400, detail=support.detail)
