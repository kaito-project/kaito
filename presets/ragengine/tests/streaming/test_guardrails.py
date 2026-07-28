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
from fastapi import HTTPException

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.guardrails import OutputGuardrails  # noqa: E402
from ragengine.guardrails.scanner_schemas import (  # noqa: E402
    BanSubstringsConfig,
    InvisibleTextConfig,
    JSONConfig,
    ParsedScannerConfig,
    SecretsConfig,
    SensitiveConfig,
)
from ragengine.streaming.guardrails import (  # noqa: E402
    STREAMING_GUARDRAILS_SUPPORTED_SCANNERS,
    apply_streaming_guardrails,
    raise_if_streaming_guardrails_unsupported,
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
        "stream=true does not support action=mask for scanner=ban_substrings. "
        "Supported actions: ['block']."
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
        "stream=true with output guardrails only supports "
        f"{sorted(STREAMING_GUARDRAILS_SUPPORTED_SCANNERS)} scanners. "
        "Unsupported scanner: json."
    )


@pytest.mark.parametrize(
    "scanner_type,config",
    [
        ("invisible_text", InvisibleTextConfig()),
        ("secrets", SecretsConfig()),
        ("sensitive", SensitiveConfig(detectors=["email"])),
    ],
)
def test_validate_streaming_guardrails_accepts_newly_supported_scanners(
    scanner_type, config
):
    support = validate_streaming_guardrails(
        OutputGuardrails(
            enabled=True,
            action_on_hit="block",
            scanner_configs=(
                ParsedScannerConfig(
                    type=scanner_type,
                    action_on_hit="block",
                    config=config,
                ),
            ),
        )
    )

    assert support.supported is True
    assert support.detail is None


def test_validate_streaming_guardrails_accepts_invisible_text_redaction():
    support = validate_streaming_guardrails(
        OutputGuardrails(
            enabled=True,
            action_on_hit="block",
            scanner_configs=(
                ParsedScannerConfig(
                    type="invisible_text",
                    action_on_hit="redact",
                    config=InvisibleTextConfig(),
                ),
            ),
        )
    )

    assert support.supported is True
    assert support.detail is None


def test_sensitive_redaction_is_rejected_with_http_400():
    guardrails = OutputGuardrails(
        enabled=True,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="sensitive",
                action_on_hit="redact",
                config=SensitiveConfig(detectors=["email"]),
            ),
        ),
    )

    with pytest.raises(HTTPException) as exc_info:
        raise_if_streaming_guardrails_unsupported(guardrails)

    assert exc_info.value.status_code == 400


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


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "event",
    [
        (
            'data: {"choices":['
            '{"index":0,"delta":{"content":"first"}},'
            '{"index":1,"delta":{"content":"second"}}]}\n\n'
        ),
        'data: {"choices":[{"index":1,"delta":{"content":"second"}}]}\n\n',
    ],
)
async def test_apply_streaming_guardrails_rejects_non_single_choice_event(event):
    async def upstream_chunks():
        yield event
        yield "data: [DONE]\n\n"

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
            upstream_chunks(), guardrails, {"messages": [], "n": 1}
        )
    ]

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_blocks_secrets_scanner_hit():
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":0,"delta":{"content":'
            '"Contact me at AKIA1234567890ABCDEF for access."}}]}\n\n'
        )
        yield 'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="secrets",
                action_on_hit="block",
                config=SecretsConfig(),
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


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_blocks_sensitive_scanner_hit():
    async def upstream_chunks():
        yield (
            'data: {"choices":[{"index":0,"delta":{"content":'
            '"Reach me at leaked@example.com anytime."}}]}\n\n'
        )
        yield 'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="sensitive",
                action_on_hit="block",
                config=SensitiveConfig(detectors=["email"]),
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


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_blocks_invisible_text_scanner_hit():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"hello\\u200bworld"}}]}\n\n'
        yield 'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="block",
                config=InvisibleTextConfig(),
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


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("unsafe_text", "safe_text"),
    [
        ("hello\\u200bworld", "helloworld"),
        ("hello\\u200b\\u200cworld", "helloworld"),
    ],
)
async def test_apply_streaming_guardrails_redacts_invisible_text_during_flush(
    unsafe_text, safe_text
):
    async def upstream_chunks():
        yield f'data: {{"choices":[{{"index":0,"delta":{{"content":"{unsafe_text}"}}}}]}}\n\n'
        yield 'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
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
        f'data: {{"choices":[{{"index":0,"delta":{{"content":"{safe_text}"}},'
        '"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert "\u200b" not in "".join(chunks)
    assert "\\u200b" not in "".join(chunks)
    assert "\u200c" not in "".join(chunks)


@pytest.mark.asyncio
async def test_invisible_redaction_runs_before_ban_substrings_block():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"un\\u200bsafe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
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
    assert "\u200b" not in "".join(chunks)
    assert "\\u200b" not in "".join(chunks)


@pytest.mark.asyncio
async def test_ban_substrings_block_rechecks_text_after_invisible_redaction():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"un\\u200bsafe"}}]}\n\n'
        yield "data: [DONE]\n\n"

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
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
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
    assert "\u200b" not in "".join(chunks)


@pytest.mark.asyncio
async def test_successful_redaction_records_final_action_once(monkeypatch):
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"hello\\u200bworld"}}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
        ),
    )
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

    assert chunks[-1] == "data: [DONE]\n\n"
    assert recorded_actions == ["redact"]


@pytest.mark.asyncio
async def test_redaction_before_block_records_only_block_final_action(monkeypatch):
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"un\\u200bsafe"}}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
            ParsedScannerConfig(
                type="ban_substrings",
                action_on_hit="block",
                config=BanSubstringsConfig(substrings=["unsafe"], match_type="str"),
            ),
        ),
    )
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

    assert chunks[-1] == "data: [DONE]\n\n"
    assert recorded_actions == ["block"]


@pytest.mark.asyncio
async def test_invisible_redaction_emits_sanitized_text_before_stream_finishes():
    unsafe_text = "a" * 300 + "\u200b" + "tail"
    sanitized_text = "a" * 300 + "tail"

    async def upstream_chunks():
        yield f'data: {{"choices":[{{"index":0,"delta":{{"content":"{unsafe_text}"}}}}]}}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
        ),
    )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]
    emitted_text = "".join(
        chunk.split('"content":"', 1)[1].split('"', 1)[0]
        for chunk in chunks
        if '"content":"' in chunk
    )

    assert len(chunks[0].split('"content":"', 1)[1].split('"', 1)[0]) == 48
    assert emitted_text == sanitized_text
    assert "\u200b" not in "".join(chunks)
    assert "\\u200b" not in "".join(chunks)
    assert chunks[-1] == "data: [DONE]\n\n"


@pytest.mark.asyncio
async def test_invisible_redaction_handles_sse_event_split_across_network_chunks():
    event = (
        'data: {"choices":[{"index":0,"delta":{"content":"hello\\u200bworld"}}]}\n\n'
    )

    async def upstream_chunks():
        yield event[:17]
        yield event[17:43]
        yield event[43:]
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
        ),
    )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]

    assert '"content":"helloworld"' in "".join(chunks)
    assert "\u200b" not in "".join(chunks)
    assert "\\u200b" not in "".join(chunks)
    assert chunks[-1] == "data: [DONE]\n\n"


@pytest.mark.asyncio
async def test_invisible_redaction_without_modified_text_fails_closed(monkeypatch):
    def scan_without_redaction(scanners, prompt, output, fail_fast):
        return output, {"invisible_text": False}, {"invisible_text": 1.0}

    monkeypatch.setattr(
        "ragengine.streaming.guardrails.scan_output", scan_without_redaction
    )

    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"hello\\u200bworld"}}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
        ),
    )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]

    assert chunks[0] == (
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},'
        '"finish_reason":null}]}\n\n'
    )


@pytest.mark.asyncio
async def test_apply_streaming_guardrails_preserves_safe_invisible_text_content():
    async def upstream_chunks():
        yield 'data: {"choices":[{"index":0,"delta":{"content":"hello "}}]}\n\n'
        yield 'data: {"choices":[{"index":0,"delta":{"content":"world"}}]}\n\n'
        yield 'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n'
        yield "data: [DONE]\n\n"

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        scanner_configs=(
            ParsedScannerConfig(
                type="invisible_text",
                action_on_hit="redact",
                config=InvisibleTextConfig(),
            ),
        ),
    )

    chunks = [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]

    assert "".join(chunks) == (
        'data: {"choices":[{"index":0,"delta":{"content":"hello world"},'
        '"finish_reason":null}]}\n\n'
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\n'
        "data: [DONE]\n\n"
    )
