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
import os
import sys

import pytest
from fastapi import HTTPException

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.guardrails import OutputGuardrails  # noqa: E402
from ragengine.guardrails.scanner_schemas import (  # noqa: E402
    BanSubstringsConfig,
    JSONConfig,
    ParsedScannerConfig,
)
from ragengine.streaming.guardrails import (  # noqa: E402
    apply_streaming_guardrails,
    raise_if_streaming_guardrails_unsupported,
)
from ragengine.streaming.openai import parse_openai_chat_sse_event  # noqa: E402
from ragengine.streaming.ordered_sse_guardrail_buffer import (  # noqa: E402
    ChoiceState,
    OrderedGuardrailBuffer,
    ProcessStatus,
)
from ragengine.streaming.sse import SSEFramer  # noqa: E402


class AllowScanner:
    def scan(self, text):
        from ragengine.streaming.buffer_window import WindowScanResult

        return WindowScanResult()


class MismatchedWindow:
    def feed(self, text):
        from ragengine.streaming.buffer_window import WindowEmitResult

        return WindowEmitResult(chunks=("different",))

    def flush(self):
        from ragengine.streaming.buffer_window import WindowEmitResult

        return WindowEmitResult(chunks=())


class CountingWindow:
    def __init__(self) -> None:
        self.flush_count = 0

    def feed(self, text):
        from ragengine.streaming.buffer_window import WindowEmitResult

        return WindowEmitResult(chunks=(text,))

    def flush(self):
        from ragengine.streaming.buffer_window import WindowEmitResult

        self.flush_count += 1
        return WindowEmitResult(chunks=())


class DroppingWindow:
    def feed(self, text):
        from ragengine.streaming.buffer_window import WindowEmitResult

        return WindowEmitResult(chunks=())

    def flush(self):
        from ragengine.streaming.buffer_window import WindowEmitResult

        return WindowEmitResult(chunks=())


def _guardrails() -> OutputGuardrails:
    return OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="ban_substrings",
                action_on_hit="block",
                config=BanSubstringsConfig(substrings=["unsafe"], match_type="str"),
            ),
        ),
    )


def test_validate_streaming_guardrails_accepts_block_ban_substrings_policy():
    raise_if_streaming_guardrails_unsupported(
        OutputGuardrails(
            enabled=True,
            action_on_hit="block",
            scanner_configs=(
                ParsedScannerConfig(
                    type="ban_substrings",
                    action_on_hit="block",
                    config=BanSubstringsConfig(substrings=["unsafe"], match_type="str"),
                ),
            ),
        )
    )


def test_validate_streaming_guardrails_rejects_scanner_action_override():
    with pytest.raises(HTTPException) as exc_info:
        raise_if_streaming_guardrails_unsupported(
            OutputGuardrails(
                enabled=True,
                action_on_hit="block",
                scanner_configs=(
                    ParsedScannerConfig(
                        type="ban_substrings",
                        action_on_hit="mask",
                        config=BanSubstringsConfig(
                            substrings=["unsafe"], match_type="str"
                        ),
                    ),
                ),
            )
        )

    assert exc_info.value.status_code == 400
    assert exc_info.value.detail == (
        "stream=true with output guardrails only supports action=block. "
        "Unsupported action: mask."
    )


def test_validate_streaming_guardrails_rejects_streaming_unsafe_scanner():
    with pytest.raises(HTTPException) as exc_info:
        raise_if_streaming_guardrails_unsupported(
            OutputGuardrails(
                enabled=True,
                action_on_hit="block",
                scanner_configs=(
                    ParsedScannerConfig(
                        type="json",
                        action_on_hit="block",
                        config=JSONConfig(),
                    ),
                ),
            )
        )

    assert exc_info.value.status_code == 400
    assert exc_info.value.detail == (
        "stream=true with output guardrails only supports ban_substrings scanners. "
        "Unsupported scanner: json."
    )


def test_ordered_guardrail_buffer_reports_pending_overflow():
    event = SSEFramer().feed('data: {"choices":[]}\n\n')[0]
    result = parse_openai_chat_sse_event(event)
    buffer = OrderedGuardrailBuffer(AllowScanner(), max_pending_events=0)

    process_result = buffer.push(event, result)

    assert process_result.status == ProcessStatus.OVERFLOW
    assert process_result.blocked_choice_index is None


def test_ordered_guardrail_buffer_reports_released_text_mismatch():
    event = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'
    )[0]
    result = parse_openai_chat_sse_event(event)
    buffer = OrderedGuardrailBuffer(AllowScanner())
    buffer._choice_states[0] = ChoiceState(window=MismatchedWindow())

    process_result = buffer.push(event, result)

    assert process_result.status == ProcessStatus.INTERNAL_ERROR
    assert process_result.blocked_choice_index is None


def test_ordered_guardrail_buffer_reports_missing_released_text_on_finish():
    event = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},'
        '"finish_reason":"stop"}]}\n\n'
    )[0]
    result = parse_openai_chat_sse_event(event)
    buffer = OrderedGuardrailBuffer(AllowScanner())
    buffer._choice_states[0] = ChoiceState(window=DroppingWindow())

    process_result = buffer.push(event, result)

    assert process_result.status == ProcessStatus.INTERNAL_ERROR
    assert process_result.blocked_choice_index is None


def test_ordered_guardrail_buffer_does_not_flush_finished_choice_twice():
    event = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},'
        '"finish_reason":"stop"}]}\n\n'
    )[0]
    result = parse_openai_chat_sse_event(event)
    window = CountingWindow()
    buffer = OrderedGuardrailBuffer(AllowScanner())
    buffer._choice_states[0] = ChoiceState(window=window)

    process_result = buffer.push(event, result)
    finish_result = buffer.finish_stream()

    assert process_result.status == ProcessStatus.OK
    assert finish_result.status == ProcessStatus.OK
    assert window.flush_count == 1


def test_ordered_guardrail_buffer_reports_content_after_finished_choice():
    first_event = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},'
        '"finish_reason":"stop"}]}\n\n'
    )[0]
    second_event = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"again"}}]}\n\n'
    )[0]
    buffer = OrderedGuardrailBuffer(AllowScanner())

    first_result = buffer.push(first_event, parse_openai_chat_sse_event(first_event))
    second_result = buffer.push(second_event, parse_openai_chat_sse_event(second_event))

    assert first_result.status == ProcessStatus.OK
    assert second_result.status == ProcessStatus.INTERNAL_ERROR
    assert second_result.reason == "content_after_finished_choice"


def test_ordered_guardrail_buffer_rejects_duplicate_choice_content_after_finish():
    event = SSEFramer().feed(
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},'
        '"finish_reason":"stop"},{"index":0,"delta":{"content":"again"}}]}'
        "\n\n"
    )[0]
    buffer = OrderedGuardrailBuffer(AllowScanner())

    process_result = buffer.push(event, parse_openai_chat_sse_event(event))

    assert process_result.status == ProcessStatus.INTERNAL_ERROR


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_emits_refusal_for_malformed_sse_event():
    closed = False

    async def upstream_chunks():
        nonlocal closed
        try:
            yield 'data: {"choices": [}\n\n'
            yield 'data: {"choices":[{"delta":{"content":"unsafe after"}}]}\n\n'
        finally:
            closed = True

    guardrails = _guardrails()

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert closed is True


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_emits_refusal_for_invalid_payload(
    monkeypatch,
    caplog,
):
    async def upstream_chunks():
        yield 'data: {"choices":{"index":0}}\n\n'

    guardrails = _guardrails()
    recorded_actions = []
    caplog.set_level(logging.WARNING)
    monkeypatch.setattr(
        OutputGuardrails,
        "_record_response_action",
        lambda self, action: recorded_actions.append(action),
    )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert recorded_actions == []
    assert "streaming_guardrails_fail_closed" in caplog.text
    assert "OpenAI chat stream choices must be a list." in caplog.text


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_logs_pending_overflow(
    monkeypatch,
    caplog,
):
    import ragengine.streaming.guardrails as streaming_guardrails

    class TinyPendingBuffer(OrderedGuardrailBuffer):
        def __init__(self, scanner):
            super().__init__(scanner, max_pending_events=0)

    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'

    caplog.set_level(logging.WARNING)
    monkeypatch.setattr(streaming_guardrails, "OrderedGuardrailBuffer", TinyPendingBuffer)

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert "streaming_guardrails_fail_closed" in caplog.text
    assert "status=overflow" in caplog.text


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_logs_content_after_finished_choice(caplog):
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":0,"delta":{"content":"safe"},'
            '"finish_reason":"stop"}]}\n\n'
        )
        yield 'data: {"choices":[{"index":0,"delta":{"content":"again"}}]}\n\n'

    caplog.set_level(logging.WARNING)

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":"stop"}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert "streaming_guardrails_fail_closed" in caplog.text
    assert "content_after_finished_choice" in caplog.text


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_emits_content_then_finish_reason():
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":2,"delta":{"content":"safe",'
            '"role":"assistant"},"finish_reason":"stop"}]}\n\n'
        )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":2,"delta":{"content":"safe","role":"assistant"},"finish_reason":"stop"}]}\n\n',
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_forwards_tool_calls_without_flushing():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'
        yield (
            'data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call-1"}]}}]}\n\n'
        )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call-1"}]}}]}\n\n',
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_does_not_flush_for_null_reasoning_content():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"un","reasoning_content":null}}]}\n\n'
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe","reasoning_content":null}}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.parametrize(
    "upstream_delta",
    (
        '"content":"safe","role":"assistant"',
        '"content":"safe","tool_calls":[{"id":"call-1"}]',
        '"content":"safe","vendor_field":"value"',
    ),
)
@pytest.mark.asyncio
async def test_apply_streaming_guardrails_scans_content_from_mixed_delta(
    upstream_delta: str,
):
    async def upstream_chunks():
        yield f'data: {{"choices":[{{"index":0,"delta":{{{upstream_delta}}}}}]}}\n\n'

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        f'data: {{"choices":[{{"index":0,"delta":{{{upstream_delta}}}}}]}}\n\n',
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_handles_content_and_passthrough_choices():
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":0,"delta":{"content":"safe"}},'
            '{"index":1,"delta":{"role":"assistant"}}]}\n\n'
        )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}},{"index":1,"delta":{"role":"assistant"}}]}\n\n',
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_flushes_safe_content_before_finish_reason():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":"stop"}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_blocks_before_forwarding_finish_reason():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"unsafe"},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_uses_separate_windows_per_choice():
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":0,"delta":{"content":"un"}},'
            '{"index":1,"delta":{"content":"safe"}}]}\n\n'
        )
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"un"}},{"index":1,"delta":{"content":"safe"}}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_emits_refusal_with_blocked_choice_index(
    monkeypatch,
):
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":1,"delta":{"content":"unsafe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = _guardrails()
    recorded_actions = []
    monkeypatch.setattr(
        OutputGuardrails,
        "_record_response_action",
        lambda self, action: recorded_actions.append(action),
    )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":1,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":1,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert recorded_actions == ["block"]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_forwards_empty_choices_usage_chunk():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'
        yield (
            'data: {"choices":[],"usage":{"prompt_tokens":1,'
            '"completion_tokens":1,"total_tokens":2}}\n\n'
        )
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n',
        'data: {"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_forwards_no_data_sse_event():
    async def upstream_chunks():
        yield ": keep-alive\n\n"
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        ": keep-alive\n\n",
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_preserves_no_data_event_order():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'
        yield ": keep-alive\n\n"
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n',
        ": keep-alive\n\n",
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_forwards_no_data_sse_event_with_standard_separator():
    async def upstream_chunks():
        yield ": keep-alive\r\nretry: 1000\r\n\r\n"
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        ": keep-alive\r\nretry: 1000\n\n",
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_preserves_choice_index_on_done_flush():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":2,"delta":{"content":"safe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":2,"delta":{"content":"safe"}}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_flushes_safe_content_on_upstream_eof_without_done():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n'

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"safe"}}]}\n\n',
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_flushes_pending_choices_in_stream_order():
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":0,"delta":{"content":"first-zero",'
            '"role":"assistant"}}]}\n\n'
        )
        yield 'data: {"choices":[{"index":1,"delta":{"content":"one-tail"}}]}\n\n'
        yield 'data: {"choices":[{"index":0,"delta":{"content":"zero-tail"}}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"first-zero","role":"assistant"}}]}\n\n',
        'data: {"choices":[{"index":1,"delta":{"content":"one-tail"}}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{"content":"zero-tail"}}]}\n\n',
        "data: [DONE]\n\n",
    ]
