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
import textwrap

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

import ragengine.guardrails.output_guardrails as output_guardrails_module
from ragengine import config
from ragengine.guardrails.output_guardrails import (
    DEFAULT_BLOCK_MESSAGE,
    OutputGuardrails,
)


def test_from_config_loads_yaml_policy(tmp_path, monkeypatch):
    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(
        textwrap.dedent(
            """
            action: block
            blockMessage: blocked-by-policy
            scanners:
              - type: regex
                patterns:
                  - https?://\\S+
              - type: ban_substrings
                substrings:
                  - secret
            """
        ).strip(),
        encoding="utf-8",
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", str(policy_path))

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "block"
    assert guardrails.block_message == "blocked-by-policy"
    assert guardrails.scanner_configs == [
        {"type": "regex", "patterns": [r"https?://\S+"]},
        {"type": "ban_substrings", "substrings": ["secret"]},
    ]


def test_from_config_keeps_empty_scanners_when_policy_path_missing(monkeypatch):
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(
        config, "OUTPUT_GUARDRAILS_POLICY_PATH", "/tmp/missing-guardrails.yaml"
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "redact"
    assert guardrails.block_message == DEFAULT_BLOCK_MESSAGE
    assert guardrails.scanner_configs == []


def test_from_config_replaces_scanners_with_policy_values(tmp_path, monkeypatch):
    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(
        textwrap.dedent(
            """
            action: block
            scanners:
              - type: ban_substrings
                substrings:
                  - yaml-only
            """
        ).strip(),
        encoding="utf-8",
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", str(policy_path))

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "block"
    assert guardrails.scanner_configs == [
        {"type": "ban_substrings", "substrings": ["yaml-only"]},
    ]


def test_from_config_invalid_action_falls_back_to_env_value(tmp_path, monkeypatch):
    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(
        textwrap.dedent(
            """
            action: passthrough
            scanners:
              - type: regex
                patterns:
                  - https?://\\S+
            """
        ).strip(),
        encoding="utf-8",
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", str(policy_path))

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "redact"
    assert guardrails.scanner_configs == [
        {"type": "regex", "patterns": [r"https?://\S+"]},
    ]


def test_from_config_returns_empty_scanners_when_policy_scanners_is_not_a_list(
    tmp_path, monkeypatch
):
    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(
        textwrap.dedent(
            """
            action: block
            scanners:
              type: regex
            """
        ).strip(),
        encoding="utf-8",
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", str(policy_path))

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "block"
    assert guardrails.scanner_configs == []


def test_from_config_skips_invalid_scanners_and_filters_non_string_values(
    tmp_path, monkeypatch
):
    class FakeRegex:
        def __init__(self, patterns, redact=False):
            self.patterns = patterns
            self.redact = redact

    class FakeBanSubstrings:
        def __init__(self, substrings, redact=False):
            self.substrings = substrings
            self.redact = redact

    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(
        textwrap.dedent(
            """
            scanners:
              - not-a-dict
              - type: regex
                patterns:
                  - https?://\\S+
                  - ""
                  - 123
              - type: ban-substrings
                substrings:
                  - secret
                  - null
                  - ""
            """
        ).strip(),
        encoding="utf-8",
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", str(policy_path))
    monkeypatch.setattr(
        output_guardrails_module.llm_guard_output_scanners,
        "Regex",
        FakeRegex,
        raising=False,
    )
    monkeypatch.setattr(
        output_guardrails_module.llm_guard_output_scanners,
        "BanSubstrings",
        FakeBanSubstrings,
        raising=False,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.scanner_configs == [
        {"type": "regex", "patterns": [r"https?://\S+", "", 123]},
        {"type": "ban_substrings", "substrings": ["secret", None, ""]},
    ]

    scanners = guardrails._build_scanners()

    assert len(scanners) == 2
    assert scanners[0].patterns == [r"https?://\S+"]
    assert scanners[0].redact is True
    assert scanners[1].substrings == ["secret"]
    assert scanners[1].redact is True


def test_build_scanners_skips_unknown_and_missing_required_scanners(monkeypatch):
    class FakeRequiredScanner:
        def __init__(self, required_value):
            self.required_value = required_value

    class FakeOkScanner:
        def __init__(self, redact=False):
            self.redact = redact

    monkeypatch.setitem(
        output_guardrails_module.SUPPORTED_POLICY_SCANNERS,
        "fake_required",
        "FakeRequiredScanner",
    )
    monkeypatch.setitem(
        output_guardrails_module.SUPPORTED_POLICY_SCANNERS,
        "fake_ok",
        "FakeOkScanner",
    )
    monkeypatch.setattr(
        output_guardrails_module.llm_guard_output_scanners,
        "FakeRequiredScanner",
        FakeRequiredScanner,
        raising=False,
    )
    monkeypatch.setattr(
        output_guardrails_module.llm_guard_output_scanners,
        "FakeOkScanner",
        FakeOkScanner,
        raising=False,
    )

    guardrails = OutputGuardrails(
        enabled=True,
        action_on_hit="redact",
        scanner_configs=[
            {"type": "unknown_scanner"},
            {"type": "fake_required"},
            {"type": "fake_ok"},
        ],
    )

    scanners = guardrails._build_scanners()

    assert len(scanners) == 1
    assert isinstance(scanners[0], FakeOkScanner)
    assert scanners[0].redact is True


def test_build_scanners_supports_normalized_ban_substrings_type(monkeypatch):
    class FakeBanSubstrings:
        def __init__(self, substrings, redact=False):
            self.substrings = substrings
            self.redact = redact

    monkeypatch.setattr(
        output_guardrails_module.llm_guard_output_scanners,
        "BanSubstrings",
        FakeBanSubstrings,
        raising=False,
    )

    scanner_configs = output_guardrails_module._normalize_policy_scanner_configs(
        [{"type": "ban-substrings", "substrings": ["secret"]}],
        "guardrails.yaml",
    )

    guardrails = OutputGuardrails(
        enabled=True,
        action_on_hit="redact",
        scanner_configs=scanner_configs,
    )

    scanners = guardrails._build_scanners()

    assert guardrails.scanner_configs == [
        {"type": "ban_substrings", "substrings": ["secret"]},
    ]
    assert len(scanners) == 1
    assert isinstance(scanners[0], FakeBanSubstrings)
    assert scanners[0].substrings == ["secret"]
    assert scanners[0].redact is True
