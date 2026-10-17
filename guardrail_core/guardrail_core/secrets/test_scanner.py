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

import os
import subprocess
import sys
import tempfile
import threading
from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy

import pytest
from detect_secrets.core.secrets_collection import SecretsCollection
from detect_secrets.settings import default_settings, get_settings

import guardrail_core.secrets.detectors as detectors_module
from guardrail_core.secrets import NativeSecretsScanner
from guardrail_core.secrets.detectors import detect_builtin_secrets
from guardrail_core.secrets.providers import detect_kaito_provider_secrets

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
_HIGH_ENTROPY_SECRET = "Qm9" + "x7ZpL2vN8cR4tY6uI1oP5aS3dF0gH9jK"


def _detect_with_legacy_scan_file(text: str) -> set[str]:
    secrets = SecretsCollection()
    with tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        delete=False,
    ) as temp_file:
        temp_file.write(text)
        temp_path = temp_file.name

    try:
        with default_settings():
            secrets.scan_file(temp_path)
    finally:
        os.remove(temp_path)

    return {
        found_secret.secret_value
        for file_path in secrets.files
        for found_secret in secrets[file_path]
        if found_secret.secret_value
    }


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


@pytest.mark.parametrize(
    ("case", "text"),
    [
        ("positive", _AWS_KEY),
        ("clean", "ordinary model output"),
        ("allowlist", f"{_AWS_KEY}  # pragma: allowlist secret"),
        ("templated", 'password = "${DATABASE_PASSWORD}"'),
        ("indirect reference", "password = get_secret_key()"),
        ("high entropy", f'api_token = "{_HIGH_ENTROPY_SECRET}"'),
        ("private key", _PRIVATE_KEY),
        ("leading whitespace", f"    {_AWS_KEY}    "),
        ("multiline context", f"header\naws_access_key_id = {_AWS_KEY}\nfooter"),
    ],
)
def test_in_memory_detection_matches_legacy_scan_file(
    monkeypatch,
    case,
    text,
):
    del case
    legacy_builtin_values = _detect_with_legacy_scan_file(text)
    new_builtin_values = detect_builtin_secrets(text)

    assert new_builtin_values == legacy_builtin_values

    legacy_scanner = NativeSecretsScanner()
    monkeypatch.setattr(
        legacy_scanner,
        "_detect_secret_values",
        lambda output: legacy_builtin_values | detect_kaito_provider_secrets(output),
    )

    assert NativeSecretsScanner().scan("", text) == legacy_scanner.scan("", text)


@pytest.mark.parametrize(
    ("environment_variable", "limit"),
    [
        ("CHECKOV_MIN_LINE_LENGTH", "21"),
        ("CHECKOV_MAX_LINE_LENGTH", "19"),
    ],
)
def test_in_memory_detection_honors_line_length_overrides(
    environment_variable,
    limit,
):
    environment = {
        **os.environ,
        environment_variable: limit,
    }
    subprocess.run(
        [
            sys.executable,
            "-c",
            (
                "from guardrail_core.secrets.detectors import detect_builtin_secrets;"
                "secret = 'AKIA' + 'A' * 16;"
                "assert detect_builtin_secrets(secret) == set()"
            ),
        ],
        check=True,
        env=environment,
    )


def test_scan_does_not_write_model_output_to_disk(monkeypatch):
    def fail_if_called(*args, **kwargs):
        del args, kwargs
        raise AssertionError("scanner must not create temporary files")

    monkeypatch.setattr(tempfile, "NamedTemporaryFile", fail_if_called)

    assert NativeSecretsScanner().scan("", _AWS_KEY) == ("******", False, 1.0)


def test_scan_does_not_mutate_global_detect_secrets_settings():
    settings = get_settings()
    plugins_before = deepcopy(settings.plugins)
    filters_before = deepcopy(settings.filters)

    NativeSecretsScanner().scan("", _AWS_KEY)

    assert settings.plugins == plugins_before
    assert settings.filters == filters_before


def test_concurrent_scans_are_not_serialized(monkeypatch):
    barrier = threading.Barrier(2)
    instance_ids = set()
    instance_lock = threading.Lock()

    class ConcurrentPlugin:
        def __init__(self):
            with instance_lock:
                instance_ids.add(id(self))

        def analyze_line(self, **kwargs):
            del kwargs
            barrier.wait(timeout=2)
            return set()

    monkeypatch.setattr(
        detectors_module,
        "_BUILT_IN_PLUGIN_TYPES",
        (ConcurrentPlugin,),
    )

    scanner = NativeSecretsScanner()
    with ThreadPoolExecutor(max_workers=2) as executor:
        results = list(executor.map(lambda _: scanner.scan("", "plain text"), range(2)))

    assert results == [
        ("plain text", True, -1.0),
        ("plain text", True, -1.0),
    ]
    assert len(instance_ids) == 2
