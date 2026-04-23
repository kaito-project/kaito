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
from pathlib import Path

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../..")))

from ragengine import config
from ragengine.guardrails.output_guardrails import OutputGuardrails


def test_output_guardrails_from_config_loads_yaml_policy(tmp_path, monkeypatch):
    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(
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
""".strip(),
        encoding="utf-8",
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_FILE", str(policy_path))
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact")
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_REGEX_PATTERNS", ())
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS", ())
    monkeypatch.setattr(
        config,
        "OUTPUT_GUARDRAILS_BLOCK_MESSAGE",
        "The model output was blocked by output guardrails.",
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "block"
    assert guardrails.regex_patterns == [r"https?://\S+"]
    assert guardrails.banned_substrings == ["secret"]
    assert guardrails.block_message == "blocked-by-policy"


def test_output_guardrails_from_config_loads_bundled_default_policy(monkeypatch):
    default_policy = (
        Path(__file__).resolve().parents[2]
        / "guardrails"
        / "default_guardrails.yaml"
    )

    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_FILE", str(default_policy))

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "redact"
    assert r"https?://\S+" in guardrails.regex_patterns
    assert "secret" in guardrails.banned_substrings