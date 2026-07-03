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

    guardrails = OutputGuardrails(
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
