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

import json
import os
import sys

import pytest

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
    _redact_secrets_and_verify,
    apply_streaming_guardrails,
    validate_streaming_guardrails,
)


def _invisible_text_scanner(action="redact"):
    return ParsedScannerConfig(
        type="invisible_text",
        action_on_hit=action,
        config=InvisibleTextConfig(),
    )


def _secrets_scanner(redact_mode="all"):
    return ParsedScannerConfig(
        type="secrets",
        action_on_hit="redact",
        config=SecretsConfig(redact_mode=redact_mode),
    )


def _sensitive_scanner(detectors):
    return ParsedScannerConfig(
        type="sensitive",
        action_on_hit="redact",
        config=SensitiveConfig(detectors=list(detectors)),
    )


def _ban_substrings_scanner(
    substring="unsafe",
    action="block",
    *,
    match_type="str",
    case_sensitive=False,
    contains_all=False,
):
    substrings = [substring] if isinstance(substring, str) else list(substring)
    return ParsedScannerConfig(
        type="ban_substrings",
        action_on_hit=action,
        config=BanSubstringsConfig(
            substrings=substrings,
            match_type=match_type,
            case_sensitive=case_sensitive,
            contains_all=contains_all,
        ),
    )


def _streaming_guardrails(*scanner_configs):
    return OutputGuardrails(
        enabled=True,
        fail_open=False,
        action_on_hit="block",
        block_message="blocked-by-policy",
        scanner_configs=scanner_configs,
    )


async def _apply_text(text, guardrails, *, finish_reason=None):
    return await _apply_content_chunks([text], guardrails, finish_reason=finish_reason)


async def _apply_content_chunks(contents, guardrails, *, finish_reason=None):
    payloads = [
        {"choices": [{"index": 0, "delta": {"content": content}}]}
        for content in contents
    ]

    async def upstream_chunks():
        for payload in payloads:
            yield f"data: {json.dumps(payload, separators=(',', ':'))}\n\n"
        if finish_reason is not None:
            yield (
                'data: {"choices":[{"index":0,"delta":{},'
                f'"finish_reason":"{finish_reason}"}}]}}\n\n'
            )
        yield "data: [DONE]\n\n"

    return [
        chunk
        async for chunk in apply_streaming_guardrails(
            upstream_chunks(), guardrails, {"messages": []}
        )
    ]


def _emitted_text(chunks):
    text = ""
    for chunk in chunks:
        if not chunk.startswith("data: {"):
            continue
        payload = json.loads(chunk.removeprefix("data: ").strip())
        text += payload["choices"][0].get("delta", {}).get("content", "")
    return text


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


def test_validate_streaming_guardrails_accepts_redact_ban_substrings_policy():
    support = validate_streaming_guardrails(
        _streaming_guardrails(_ban_substrings_scanner(action="redact"))
    )

    assert support.supported is True
    assert support.detail is None


@pytest.mark.parametrize("action", ["block", "redact"])
def test_validate_streaming_guardrails_rejects_ban_substrings_contains_all(action):
    scanner = _ban_substrings_scanner(
        ["unsafe", "prohibited"],
        action=action,
        contains_all=True,
    )

    support = validate_streaming_guardrails(_streaming_guardrails(scanner))

    assert support.supported is False
    assert support.detail == (
        "stream=true does not support contains_all=true for "
        "scanner=ban_substrings because the current windowed "
        "implementation cannot track matches across the complete response."
    )


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
        "Supported actions: ['block', 'redact']."
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


def test_validate_streaming_guardrails_accepts_sensitive_redaction():
    support = validate_streaming_guardrails(
        OutputGuardrails(
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
    )

    assert support.supported is True
    assert support.detail is None


def test_validate_streaming_guardrails_accepts_secrets_all_redaction():
    support = validate_streaming_guardrails(_streaming_guardrails(_secrets_scanner()))

    assert support.supported is True
    assert support.detail is None


@pytest.mark.parametrize("redact_mode", ["partial", "hash"])
def test_validate_streaming_guardrails_rejects_non_all_secrets_redaction(
    redact_mode,
):
    support = validate_streaming_guardrails(
        _streaming_guardrails(_secrets_scanner(redact_mode))
    )

    assert support.supported is False
    assert support.detail == (
        "stream=true with action=redact for scanner=secrets only supports "
        "redact_mode=all."
    )


@pytest.mark.parametrize(
    ("scanner_output", "results_valid"),
    [
        ("secret", {"secrets": False}),
        (object(), {"secrets": False}),
        ("secret unexpectedly expanded", {"secrets": False}),
        ("unexpected mutation", {"secrets": True}),
    ],
)
def test_redact_secrets_and_verify_fails_closed_on_invalid_result(
    monkeypatch, scanner_output, results_valid
):
    monkeypatch.setattr(
        "ragengine.streaming.guardrails.scan_output",
        lambda scanners, prompt, output, fail_fast: (
            scanner_output,
            results_valid,
            {"secrets": 1.0},
        ),
    )

    assert _redact_secrets_and_verify(object(), "prompt", "secret") is None


def test_redact_secrets_and_verify_scans_once_then_verifies_clean(monkeypatch):
    scan_results = iter(
        [
            ("******", {"secrets": False}, {"secrets": 1.0}),
            ("******", {"secrets": True}, {"secrets": -1.0}),
        ]
    )
    scan_count = 0

    def scan_once_then_verify(scanners, prompt, output, fail_fast):
        nonlocal scan_count
        scan_count += 1
        return next(scan_results)

    monkeypatch.setattr(
        "ragengine.streaming.guardrails.scan_output", scan_once_then_verify
    )

    assert _redact_secrets_and_verify(object(), "prompt", "secret") == "******"
    assert scan_count == 2


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
    ("input_text", "safe_text"),
    [
        ("hello\u200bworld", "helloworld"),
        ("hello\u200b\u200cworld", "helloworld"),
        ("hello world", "hello world"),
    ],
)
async def test_apply_streaming_guardrails_preserves_or_redacts_text_during_flush(
    input_text, safe_text
):
    chunks = await _apply_text(
        input_text,
        _streaming_guardrails(_invisible_text_scanner()),
        finish_reason="stop",
    )

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
@pytest.mark.parametrize(
    "scanner_configs",
    [
        (_invisible_text_scanner(), _ban_substrings_scanner()),
        (_ban_substrings_scanner(), _invisible_text_scanner()),
    ],
)
async def test_block_scanner_checks_final_redacted_text(scanner_configs):
    chunks = await _apply_text("un\u200bsafe", _streaming_guardrails(*scanner_configs))

    assert chunks == [
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},"finish_reason":null}]}\n\n',
        'data: {"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}\n\n',
        "data: [DONE]\n\n",
    ]
    assert "\u200b" not in "".join(chunks)
    assert "\\u200b" not in "".join(chunks)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("text", "scanner_configs", "expected_action"),
    [
        ("hello\u200bworld", (_invisible_text_scanner(),), "redact"),
        (
            "un\u200bsafe",
            (_invisible_text_scanner(), _ban_substrings_scanner()),
            "block",
        ),
    ],
)
async def test_records_only_final_response_action(
    monkeypatch, text, scanner_configs, expected_action
):
    recorded_actions = []
    monkeypatch.setattr(
        OutputGuardrails,
        "_record_response_action",
        lambda self, action: recorded_actions.append(action),
    )

    chunks = await _apply_text(text, _streaming_guardrails(*scanner_configs))

    assert chunks[-1] == "data: [DONE]\n\n"
    assert recorded_actions == [expected_action]


@pytest.mark.asyncio
async def test_invisible_redaction_emits_sanitized_text_before_stream_finishes():
    unsafe_text = "a" * 300 + "\u200b" + "tail"
    sanitized_text = "a" * 300 + "tail"

    chunks = await _apply_text(
        unsafe_text, _streaming_guardrails(_invisible_text_scanner())
    )
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
async def test_invisible_redaction_without_modified_text_fails_closed(monkeypatch):
    def scan_without_redaction(scanners, prompt, output, fail_fast):
        return output, {"invisible_text": False}, {"invisible_text": 1.0}

    monkeypatch.setattr(
        "ragengine.streaming.guardrails.scan_output", scan_without_redaction
    )

    chunks = await _apply_text(
        "hello\u200bworld", _streaming_guardrails(_invisible_text_scanner())
    )

    assert chunks[0] == (
        'data: {"choices":[{"index":0,"delta":{"content":"blocked-by-policy"},'
        '"finish_reason":null}]}\n\n'
    )


@pytest.mark.asyncio
async def test_ban_substrings_redacts_long_value_split_after_default_holdback():
    substring = "x" * 300

    chunks = await _apply_content_chunks(
        [substring[:270], substring[270:]],
        _streaming_guardrails(_ban_substrings_scanner(substring, action="redact")),
    )

    assert _emitted_text(chunks) == "[REDACTED]"
    assert substring not in "".join(chunks)


@pytest.mark.asyncio
async def test_ban_substrings_redacts_value_split_across_content_chunks():
    chunks = await _apply_content_chunks(
        ["prefix un", "safe suffix"],
        _streaming_guardrails(_ban_substrings_scanner(action="redact")),
    )

    assert _emitted_text(chunks) == "prefix [REDACTED] suffix"


@pytest.mark.asyncio
async def test_ban_substrings_redaction_is_case_insensitive_when_configured():
    chunks = await _apply_text(
        "UNSAFE unsafe",
        _streaming_guardrails(
            _ban_substrings_scanner(
                action="redact",
                case_sensitive=False,
            )
        ),
    )

    assert _emitted_text(chunks) == "[REDACTED] [REDACTED]"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("match_type", "expected"),
    [
        ("word", "unsafeish"),
        ("str", "[REDACTED]ish"),
    ],
)
async def test_ban_substrings_redaction_respects_match_type(match_type, expected):
    chunks = await _apply_text(
        "unsafeish",
        _streaming_guardrails(
            _ban_substrings_scanner(
                action="redact",
                match_type=match_type,
            )
        ),
    )

    assert _emitted_text(chunks) == expected


@pytest.mark.asyncio
async def test_word_match_does_not_redact_chunk_end_before_lookahead():
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " unsafe", "ish"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe",
                action="redact",
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " unsafeish"


@pytest.mark.asyncio
async def test_word_match_redacts_after_boundary_arrives():
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " unsafe", " suffix"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe",
                action="redact",
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " [REDACTED] suffix"


@pytest.mark.asyncio
async def test_word_match_redacts_at_final_flush():
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " unsafe"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe",
                action="redact",
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " [REDACTED]"


@pytest.mark.asyncio
async def test_word_block_does_not_reject_chunk_end_before_lookahead():
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " unsafe", "ish"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe",
                action="block",
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " unsafeish"


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["redact", "block"])
async def test_word_match_preserves_left_boundary_after_window_slides(action):
    prefix = "p" * 300
    expected = prefix + "xunsafe " + "q" * 300

    chunks = await _apply_content_chunks(
        [prefix + "xunsafe "] + ["q"] * 300,
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe",
                action=action,
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == expected


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["block", "redact"])
async def test_word_match_with_nonword_suffix_waits_for_real_boundary(action):
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " unsafe-", " suffix"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe-",
                action=action,
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " unsafe- suffix"


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["block", "redact"])
async def test_word_match_with_nonword_suffix_matches_at_real_boundary(action):
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " unsafe-", "x suffix"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                "unsafe-",
                action=action,
                match_type="word",
            )
        ),
    )

    emitted_text = _emitted_text(chunks)
    if action == "block":
        assert emitted_text.endswith("blocked-by-policy")
        assert "unsafe-" not in emitted_text
    else:
        assert emitted_text == prefix + " [REDACTED]x suffix"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("substring", "input_value"),
    [
        ("x", "x"),
        ("X", "x"),
    ],
)
async def test_single_character_word_redaction_does_not_modify_boundary_guard(
    substring, input_value
):
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + f" {input_value} tail"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                substring,
                action="redact",
                match_type="word",
                case_sensitive=False,
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " [REDACTED] tail"


@pytest.mark.asyncio
async def test_single_character_nonword_redaction_does_not_modify_boundary_guard():
    prefix = "p" * 300

    chunks = await _apply_content_chunks(
        [prefix + " a.b "],
        _streaming_guardrails(
            _ban_substrings_scanner(
                ".",
                action="redact",
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == prefix + " a[REDACTED]b "


@pytest.mark.asyncio
async def test_long_word_match_is_held_until_right_boundary_arrives():
    substring = "a" * 300

    chunks = await _apply_content_chunks(
        [substring, " suffix"],
        _streaming_guardrails(
            _ban_substrings_scanner(
                substring,
                action="redact",
                match_type="word",
            )
        ),
    )

    assert _emitted_text(chunks) == "[REDACTED] suffix"


@pytest.mark.asyncio
async def test_ban_substrings_redacts_multiple_configured_values():
    chunks = await _apply_text(
        "unsafe and prohibited",
        _streaming_guardrails(
            _ban_substrings_scanner(
                ["unsafe", "prohibited"],
                action="redact",
            )
        ),
    )

    assert _emitted_text(chunks) == "[REDACTED] and [REDACTED]"


@pytest.mark.asyncio
async def test_ban_substrings_redaction_runs_before_later_block_scanner():
    chunks = await _apply_text(
        "unsafe",
        _streaming_guardrails(
            _ban_substrings_scanner("unsafe", action="redact"),
            _ban_substrings_scanner("[REDACTED]", action="block"),
        ),
    )

    assert _emitted_text(chunks) == "blocked-by-policy"


@pytest.mark.asyncio
async def test_secrets_all_redacts_aws_access_key_split_across_content_chunks():
    secret = "AKIA1234567890ABCDEF"

    chunks = await _apply_content_chunks(
        ["AWS key: AKIA1234", "567890ABCDEF"],
        _streaming_guardrails(_secrets_scanner()),
    )

    assert _emitted_text(chunks) == "AWS key: ******"
    assert secret not in "".join(chunks)


@pytest.mark.asyncio
async def test_secrets_all_redacts_multiple_secrets():
    first_secret = "AKIA1234567890ABCDEF"
    second_secret = "AKIAZYXWVUTSRQPONMLK"
    first_prefix = "a" * 300
    second_prefix = "b" * 300

    chunks = await _apply_content_chunks(
        [
            f"{first_prefix} first {first_secret}",
            f"{second_prefix} second {second_secret}",
        ],
        _streaming_guardrails(_secrets_scanner()),
    )

    assert _emitted_text(chunks) == (
        f"{first_prefix} first ******{second_prefix} second ******"
    )
    assert first_secret not in "".join(chunks)
    assert second_secret not in "".join(chunks)


@pytest.mark.asyncio
async def test_secrets_all_redacts_multiple_secret_types():
    aws_key = "AKIA1234567890ABCDEF"
    github_token = "ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"
    gcp_key = "AIza" + "A" * 35

    chunks = await _apply_text(
        f"Keys: {aws_key}, {github_token}, {gcp_key}",
        _streaming_guardrails(_secrets_scanner()),
    )

    assert _emitted_text(chunks) == "Keys: ******, ******, ******"
    assert aws_key not in "".join(chunks)
    assert github_token not in "".join(chunks)
    assert gcp_key not in "".join(chunks)


@pytest.mark.asyncio
async def test_secrets_all_redacts_twenty_repeated_identical_secrets():
    secret = "AKIA1234567890ABCDEF"

    chunks = await _apply_text(
        "Keys: " + ", ".join([secret] * 20),
        _streaming_guardrails(_secrets_scanner()),
    )

    assert _emitted_text(chunks) == "Keys: " + ", ".join(["******"] * 20)
    assert secret not in "".join(chunks)


@pytest.mark.asyncio
async def test_secrets_all_redaction_is_stable_when_window_is_rescanned():
    secret = "AKIA1234567890ABCDEF"
    prefix = "a" * 300

    chunks = await _apply_content_chunks(
        [f"{prefix} token = {secret}", " tail"],
        _streaming_guardrails(_secrets_scanner()),
    )

    assert _emitted_text(chunks) == f"{prefix} token = ****** tail"
    assert secret not in "".join(chunks)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("detector", "pii", "replacement", "safe_text"),
    [
        ("email", "alice@example.com", "<EMAIL>", "alice at example dot com"),
        ("phone", "+1 (206) 555-0100", "<PHONE>", "extension 42"),
        (
            "credit_card",
            "4111 1111 1111 1111",
            "<CREDIT_CARD>",
            "order 123456789012",
        ),
        ("ip_address", "10.0.0.1", "<IP_ADDRESS>", "version 10.0.0"),
    ],
)
async def test_sensitive_redaction_detector_and_safe_cases(
    detector, pii, replacement, safe_text
):
    guardrails = _streaming_guardrails(_sensitive_scanner([detector]))

    redacted_chunks = await _apply_text(f"Value: {pii}", guardrails)
    safe_chunks = await _apply_text(f"Value: {safe_text}", guardrails)

    assert _emitted_text(redacted_chunks) == f"Value: {replacement}"
    assert pii not in "".join(redacted_chunks)
    assert _emitted_text(safe_chunks) == f"Value: {safe_text}"


@pytest.mark.asyncio
async def test_sensitive_redaction_detects_match_split_across_content_chunks():
    chunks = await _apply_content_chunks(
        ["Contact alice@", "example.com now"],
        _streaming_guardrails(_sensitive_scanner(["email"])),
    )

    assert _emitted_text(chunks) == "Contact <EMAIL> now"
    assert "alice@example.com" not in "".join(chunks)


@pytest.mark.asyncio
async def test_sensitive_redaction_holds_split_match_across_release_boundary():
    prefix = "a" * 250
    chunks = await _apply_content_chunks(
        [prefix + " alice@", "example.com now"],
        _streaming_guardrails(_sensitive_scanner(["email"])),
    )

    assert all("alice@" not in chunk for chunk in chunks)
    assert _emitted_text(chunks) == prefix + " <EMAIL> now"


@pytest.mark.asyncio
async def test_sensitive_redaction_redacts_multiple_pii_values():
    original = (
        "Email alice@example.com, call +1 (206) 555-0100, "
        "use 4111 1111 1111 1111 from 10.0.0.1"
    )
    chunks = await _apply_text(
        original,
        _streaming_guardrails(
            _sensitive_scanner(["email", "phone", "credit_card", "ip_address"])
        ),
    )

    assert _emitted_text(chunks) == (
        "Email <EMAIL>, call <PHONE>, use <CREDIT_CARD> from <IP_ADDRESS>"
    )
    assert all(
        pii not in "".join(chunks)
        for pii in (
            "alice@example.com",
            "+1 (206) 555-0100",
            "4111 1111 1111 1111",
            "10.0.0.1",
        )
    )


@pytest.mark.asyncio
async def test_sensitive_redaction_runs_before_block_scanner():
    chunks = await _apply_text(
        "Contact alice@example.com",
        _streaming_guardrails(
            _sensitive_scanner(["email"]),
            _ban_substrings_scanner("<EMAIL>"),
        ),
    )

    assert _emitted_text(chunks) == "blocked-by-policy"
    assert "alice@example.com" not in "".join(chunks)
