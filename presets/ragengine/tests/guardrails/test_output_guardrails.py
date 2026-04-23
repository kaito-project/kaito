import textwrap
import os
import sys

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine import config
from ragengine.guardrails.output_guardrails import DEFAULT_BLOCK_MESSAGE, OutputGuardrails


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
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_REGEX_PATTERNS", tuple())
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS", tuple())
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_BLOCK_MESSAGE", DEFAULT_BLOCK_MESSAGE)

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "block"
    assert guardrails.regex_patterns == [r"https?://\S+"]
    assert guardrails.banned_substrings == ["secret"]
    assert guardrails.block_message == "blocked-by-policy"


def test_from_config_falls_back_to_env_values_when_policy_path_missing(monkeypatch):
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", "/tmp/missing-guardrails.yaml")
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact")
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_REGEX_PATTERNS", (r"secret",))
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS", ("token",))
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_BLOCK_MESSAGE", DEFAULT_BLOCK_MESSAGE)

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "redact"
    assert guardrails.regex_patterns == [r"secret"]
    assert guardrails.banned_substrings == ["token"]
    assert guardrails.block_message == DEFAULT_BLOCK_MESSAGE