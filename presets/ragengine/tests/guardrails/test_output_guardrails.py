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
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact")
    monkeypatch.setattr(
        config, "OUTPUT_GUARDRAILS_BLOCK_MESSAGE", DEFAULT_BLOCK_MESSAGE
    )

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
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact")
    monkeypatch.setattr(
        config, "OUTPUT_GUARDRAILS_BLOCK_MESSAGE", DEFAULT_BLOCK_MESSAGE
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
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact")
    monkeypatch.setattr(
        config, "OUTPUT_GUARDRAILS_BLOCK_MESSAGE", DEFAULT_BLOCK_MESSAGE
    )

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
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact")
    monkeypatch.setattr(
        config, "OUTPUT_GUARDRAILS_BLOCK_MESSAGE", DEFAULT_BLOCK_MESSAGE
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "redact"
    assert guardrails.scanner_configs == [
        {"type": "regex", "patterns": [r"https?://\S+"]},
    ]
