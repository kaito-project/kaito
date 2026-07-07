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

import os
import sys

import pytest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.guardrails import OutputGuardrails  # noqa: E402
from ragengine.guardrails.scanner_schemas import (  # noqa: E402
    BanSubstringsConfig,
    JSONConfig,
    ParsedScannerConfig,
)
from ragengine.streaming.guardrails import (  # noqa: E402
    apply_streaming_guardrails,
    validate_streaming_guardrails,
)


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
    support = validate_streaming_guardrails(
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

    assert support.supported is True
    assert support.detail is None


def test_validate_streaming_guardrails_rejects_scanner_action_override():
    support = validate_streaming_guardrails(
        OutputGuardrails(
            enabled=True,
            action_on_hit="block",
            scanner_configs=(
                ParsedScannerConfig(
                    type="ban_substrings",
                    action_on_hit="mask",
                    config=BanSubstringsConfig(substrings=["unsafe"], match_type="str"),
                ),
            ),
        )
    )

    assert support.supported is False
    assert support.detail == (
        "stream=true with output guardrails only supports action=block. "
        "Unsupported action: mask."
    )


def test_validate_streaming_guardrails_rejects_streaming_unsafe_scanner():
    support = validate_streaming_guardrails(
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

    assert support.supported is False
    assert support.detail == (
        "stream=true with output guardrails only supports ban_substrings scanners. "
        "Unsupported scanner: json."
    )


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
async def test_apply_streaming_guardrails_sanitizes_content_from_passthrough_event():
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
        'data: {"choices":[{"index":2,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":2,"delta":{"role":"assistant"},"finish_reason":"stop"}]}\n\n',
    ]


@pytest.mark.parametrize(
    ("upstream_delta", "expected_passthrough_delta"),
    (
        ('"content":"safe","role":"assistant"', '"role":"assistant"'),
        (
            '"content":"safe","tool_calls":[{"id":"call-1"}]',
            '"tool_calls":[{"id":"call-1"}]',
        ),
        ('"content":"safe","vendor_field":"value"', '"vendor_field":"value"'),
    ),
)
@pytest.mark.asyncio
async def test_apply_streaming_guardrails_strips_content_from_mixed_delta(
    upstream_delta: str,
    expected_passthrough_delta: str,
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
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        f'data: {{"choices":[{{"index":0,"delta":{{{expected_passthrough_delta}}}}}]}}\n\n',
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
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":1,"delta":{"role":"assistant"}}]}\n\n',
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
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n',
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
        'data: {"choices":[{"index":0,"delta":{"content":"un"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":1,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_emits_refusal_with_blocked_choice_index():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":1,"delta":{"content":"unsafe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), _guardrails(), {"messages": []}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":1,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":1,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]


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
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
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
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_preserves_no_data_sse_separator():
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
        ": keep-alive\r\nretry: 1000\r\n\r\n",
        'data: {"choices":[{"index":0,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
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
        'data: {"choices":[{"index":2,"delta":{"content":"safe"},"finish_reason":null}]}\n\n',
        "data: [DONE]\n\n",
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
        'data: {"choices":[{"index":0,"delta":{"content":"first-zero"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{"role":"assistant"}}]}\n\n',
        'data: {"choices":[{"index":1,"delta":{"content":"one-tail"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{"content":"zero-tail"},"finish_reason":null}]}\n\n',
        "data: [DONE]\n\n",
    ]
