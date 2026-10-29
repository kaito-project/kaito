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

import importlib.util
from pathlib import Path

import pytest
from guardrail_core.config import (
    BanSubstringsConfig,
    ParsedScannerConfig,
    build_scanner,
    parse_scanner_config,
)

SCANNER_CASES = [
    {"type": "ban_substrings", "substrings": ["secret"], "match_type": "str"},
    {"type": "regex", "patterns": ["secret"]},
    {"type": "invisible_text"},
    {"type": "json"},
    {"type": "reading_time", "max_time": 1},
    {"type": "token_limit", "limit": 10},
    {"type": "secrets", "redact_mode": "partial"},
    {"type": "sensitive", "detectors": ["email"]},
]


@pytest.mark.parametrize("raw", SCANNER_CASES)
@pytest.mark.parametrize("action", ["block", "redact"])
def test_shared_configs_preserve_ragengine_scanner_behavior(raw, action):
    source = (
        Path(__file__).resolve().parents[2]
        / "presets/ragengine/guardrails/scanner_schemas.py"
    )
    if not source.exists():
        pytest.skip("RAGEngine comparison is only available in the source checkout")
    spec = importlib.util.spec_from_file_location("legacy_scanner_schemas", source)
    assert spec is not None and spec.loader is not None
    legacy = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(legacy)
    parsed = parse_scanner_config(raw, default_action=action)
    shared = build_scanner(parsed)
    old_config = legacy.SCANNER_REGISTRY[raw["type"]].from_dict(
        {key: value for key, value in raw.items() if key != "type"}
    )
    original = old_config.build(action)
    for text in ("safe output", "a secret", "contact alice@example.com", '{"ok": 1}'):
        assert shared.scan("", text) == original.scan("", text)


@pytest.mark.parametrize("raw", SCANNER_CASES)
def test_all_shared_scanners_build_without_ragengine(raw):
    scanner = build_scanner(parse_scanner_config(raw))
    assert callable(scanner.scan)


def test_normalized_fields_and_action_override():
    parsed = parse_scanner_config(
        {
            "type": "BanSubstrings",
            "substrings": ["secret"],
            "matchType": "str",
            "case-sensitive": True,
            "action": "redact",
        }
    )
    assert parsed.type == "ban_substrings"
    assert parsed.action_on_hit == "redact"
    assert build_scanner(parsed).scan("", "secret SECRET").output == "[REDACTED] SECRET"


@pytest.mark.parametrize(
    "raw",
    [
        {},
        {"type": 1},
        {"type": "unknown"},
        {"type": "regex", "patterns": ["["]},
        {"type": "regex", "patterns": ["ok", 1]},
        {"type": "regex", "patterns": [""]},
        {"type": "regex", "patterns": []},
        {"type": "regex", "patterns": ["ok"], "is_blocked": "false"},
        {"type": "regex", "patterns": ["ok"], "is_blocked": None},
        {"type": "regex", "patterns": ["ok"], "match_type": "unknown"},
        {"type": "regex", "patterns": ["ok"], "action": "ignore"},
        {"type": "regex", "patterns": ["ok"], "typo": True},
        {
            "type": "regex",
            "patterns": ["ok"],
            "matchType": "all",
            "match_type": "search",
        },
        {"type": "regex", "patterns": ["ok"], 1: True},
        {"type": "ban_substrings", "substrings": [1]},
        {"type": "invisible_text", "extra": True},
        {"type": "token_limit", "limit": True},
        {"type": "token_limit", "limit": -1},
        {"type": "json", "required_elements": False},
        {"type": "reading_time", "max_time": True},
        {"type": "reading_time", "max_time": 0},
        {"type": "secrets", "redact_mode": "unknown"},
        {"type": "sensitive", "detectors": ["unknown"]},
    ],
)
def test_invalid_config_raises_instead_of_skipping(raw):
    with pytest.raises(ValueError):
        parse_scanner_config(raw)


@pytest.mark.parametrize("action", ["ignore", "", None, 1])
def test_invalid_default_action_raises(action):
    with pytest.raises(ValueError, match="action"):
        parse_scanner_config({"type": "invisible_text"}, default_action=action)
    with pytest.raises(ValueError, match="action"):
        build_scanner(
            ParsedScannerConfig("ban_substrings", BanSubstringsConfig(["secret"])),
            default_action=action,
        )


def test_build_uses_default_action_when_not_set():
    config = ParsedScannerConfig("ban_substrings", BanSubstringsConfig(["secret"]))
    scanner = build_scanner(config, default_action="redact")
    assert scanner.scan("", "secret").output == "[REDACTED]"


def test_build_rejects_explicit_empty_action():
    config = ParsedScannerConfig(
        "ban_substrings", BanSubstringsConfig(["secret"]), action_on_hit=""
    )
    with pytest.raises(ValueError, match="action"):
        build_scanner(config)


def test_build_failure_propagates(monkeypatch):
    def fail(self, action_on_hit):
        raise RuntimeError("construction failed")

    monkeypatch.setattr(BanSubstringsConfig, "build", fail)
    with pytest.raises(RuntimeError, match="construction failed"):
        build_scanner(parse_scanner_config(SCANNER_CASES[0]))
