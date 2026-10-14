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

"""Tests for the KAITO-owned secrets scanner."""

from concurrent.futures import ThreadPoolExecutor

import pytest

import guardrail_core.secrets.scanner as scanner_module
from guardrail_core.secrets import NativeSecretsScanner

_AWS_KEY = "AKIA" + "A" * 16
_AWS_SECRET_ACCESS_KEY = "aB3/" * 10
_AZURE_KEY = "A" * 86 + "=="
_GITHUB_TOKEN = "ghp_" + "A" * 36
_GITHUB_FINE_GRAINED_TOKEN = "github_pat_" + "A" * 82
_GCP_KEY = "AIza" + "A" * 35
_OPENAI_KEY = "sk-" + "A" * 20 + "T3BlbkFJ" + "B" * 20
_OPENAI_PROJECT_KEY = "sk-proj-" + "A" * 80
_OPENAI_SERVICE_ACCOUNT_KEY = "sk-svcacct-" + "A" * 80
_SLACK_TOKEN = "-".join(
    ("xoxb", "1234567890", "1234567890", "abcdefghijklmnopqrstuvwx")
)
_SLACK_WEBHOOK = "https://" + "/".join(
    ("hooks.slack.com", "services", "T" + "A" * 8, "B" + "B" * 8, "C" * 24)
)
_JWT = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl"
_PRIVATE_KEY_BODY = "A" * 32
_PRIVATE_KEY = (
    f"-----BEGIN PRIVATE KEY-----\n{_PRIVATE_KEY_BODY}\n-----END PRIVATE KEY-----"
)
_BASIC_AUTH_PASSWORD = "password"
_BASIC_AUTH_URL = f"https://user:{_BASIC_AUTH_PASSWORD}@internal.test/path"


def test_native_scanners_keeps_compatibility_export():
    from guardrail_core.native_scanners import (
        NativeSecretsScanner as CompatibilitySecretsScanner,
    )

    assert CompatibilitySecretsScanner is NativeSecretsScanner


@pytest.mark.parametrize(
    ("provider", "text", "expected_secret"),
    [
        ("AWS", _AWS_KEY, _AWS_KEY),
        (
            "AWS secret access key",
            f'aws_secret_access_key = "{_AWS_SECRET_ACCESS_KEY}"',
            _AWS_SECRET_ACCESS_KEY,
        ),
        ("Azure", f"AccountKey={_AZURE_KEY}", _AZURE_KEY),
        ("GitHub", _GITHUB_TOKEN, _GITHUB_TOKEN),
        ("GitHub fine-grained", _GITHUB_FINE_GRAINED_TOKEN, _GITHUB_FINE_GRAINED_TOKEN),
        ("GCP", _GCP_KEY, _GCP_KEY),
        ("OpenAI", _OPENAI_KEY, _OPENAI_KEY),
        ("OpenAI project", _OPENAI_PROJECT_KEY, _OPENAI_PROJECT_KEY),
        (
            "OpenAI service account",
            _OPENAI_SERVICE_ACCOUNT_KEY,
            _OPENAI_SERVICE_ACCOUNT_KEY,
        ),
        ("Slack", _SLACK_TOKEN, _SLACK_TOKEN),
        ("Slack webhook", _SLACK_WEBHOOK, _SLACK_WEBHOOK),
        ("JWT", _JWT, _JWT),
        ("Private Key", _PRIVATE_KEY, _PRIVATE_KEY_BODY),
        ("Basic Auth", _BASIC_AUTH_URL, _BASIC_AUTH_PASSWORD),
    ],
)
def test_detects_supported_provider_secrets(provider, text, expected_secret):
    del provider
    scanner = NativeSecretsScanner()

    assert expected_secret in scanner._detect_secret_values(text)


@pytest.mark.parametrize(
    ("provider", "near_match"),
    [
        ("AWS", "AKIA" + "A" * 15),
        ("Azure", "azure " + "A" * 83 + "=="),
        ("GitHub", "ghp_" + "A" * 35),
        ("GitHub fine-grained", "github_pat_" + "A" * 81),
        ("GCP", "AIza" + "A" * 34),
        ("OpenAI", "sk-" + "A" * 19 + "T3BlbkFJ" + "B" * 20),
        ("OpenAI project", "sk-proj-" + "A" * 79),
        ("Slack", "xoxz-1234567890-abcdefghijklmnopqrstuvwx"),
        ("JWT", "eyJnot-json.not-json.signature"),
        ("Private Key", "-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----"),
        ("Basic Auth", "https://user:password@example.com/path"),
    ],
)
def test_ignores_provider_near_matches(provider, near_match):
    del provider
    scanner = NativeSecretsScanner()

    assert scanner._detect_secret_values(near_match) == set()


@pytest.mark.parametrize(
    "overlong_token",
    [
        "ghp_" + "A" * 37,
        "github_pat_" + "A" * 83,
    ],
)
def test_ignores_overlong_github_tokens(overlong_token):
    scanner = NativeSecretsScanner()

    assert scanner._detect_secret_values(overlong_token) == set()


@pytest.mark.parametrize(
    "token",
    [
        _GITHUB_TOKEN,
        _GITHUB_FINE_GRAINED_TOKEN,
    ],
)
def test_detects_github_tokens_before_punctuation(token):
    scanner = NativeSecretsScanner()

    assert token in scanner._detect_secret_values(f"{token},")


def test_uses_kaito_patterns_instead_of_builtin_github_finding(monkeypatch):
    monkeypatch.setattr(
        scanner_module,
        "detect_kaito_provider_secrets",
        lambda text: set(),
    )
    scanner = NativeSecretsScanner()

    assert scanner._detect_secret_values(_GITHUB_TOKEN) == set()


def test_allows_text_without_secrets():
    scanner = NativeSecretsScanner()

    assert scanner.scan("", "ordinary model output") == (
        "ordinary model output",
        True,
        -1.0,
    )


@pytest.mark.parametrize(
    ("secret", "redact_mode", "expected"),
    [
        ("secret-token", "all", "******"),
        ("secret-token", "partial", "se..en"),
        ("abc", "partial", "ab..bc"),
        ("secret-token", "hash", "3c28d89b80f70302b04fce2a1451f6ea"),
    ],
)
def test_matches_legacy_redaction(monkeypatch, secret, redact_mode, expected):
    scanner = NativeSecretsScanner(redact_mode=redact_mode)
    monkeypatch.setattr(scanner, "_detect_secret_values", lambda output: {secret})

    assert scanner.scan("", secret) == (expected, False, 1.0)


def test_rejects_unknown_redaction_mode(monkeypatch):
    scanner = NativeSecretsScanner(redact_mode="unknown")
    monkeypatch.setattr(scanner, "_detect_secret_values", lambda output: {"secret"})

    with pytest.raises(ValueError, match="redact mode wasn't recognized unknown"):
        scanner.scan("", "secret")


def test_redacts_multiple_provider_secrets():
    scanner = NativeSecretsScanner(redact_mode="all")

    assert scanner.scan("", f"{_AWS_KEY} {_GCP_KEY} {_OPENAI_KEY}") == (
        "****** ****** ******",
        False,
        1.0,
    )


def test_redacts_repeated_secrets():
    scanner = NativeSecretsScanner(redact_mode="all")

    assert scanner.scan("", f"{_GITHUB_TOKEN} and {_GITHUB_TOKEN}") == (
        "****** and ******",
        False,
        1.0,
    )


def test_concurrent_scans_preserve_detection():
    scanner = NativeSecretsScanner(redact_mode="all")
    samples = [
        (_AWS_KEY, ("******", False, 1.0)),
        (_GITHUB_TOKEN, ("******", False, 1.0)),
        ("plain text", ("plain text", True, -1.0)),
    ]

    def scan(index):
        output, expected = samples[index % len(samples)]
        return scanner.scan("", output), expected

    with ThreadPoolExecutor(max_workers=20) as executor:
        results = executor.map(scan, range(100))

    for actual, expected in results:
        assert actual == expected
