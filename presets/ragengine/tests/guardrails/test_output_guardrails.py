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

import builtins
import os
import sys
import textwrap
import time

import pytest
from llm_guard import scan_output

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

import ragengine.guardrails.output_guardrails as output_guardrails_module
import ragengine.guardrails.scanner_schemas as scanner_schemas_module
from ragengine import config
from ragengine.guardrails.output_guardrails import (
    DEFAULT_BLOCK_MESSAGE,
    OutputGuardrails,
)
from ragengine.guardrails.scanner_schemas import (
    BanSubstringsConfig,
    ParsedScannerConfig,
    RegexConfig,
    SecretsConfig,
    SensitiveConfig,
)
from ragengine.metrics.prometheus_metrics import (
    guardrails_response_actions_total,
    guardrails_response_scanner_hits_total,
    output_guardrails_policy_load_total,
    output_guardrails_scanner_build_total,
)
from ragengine.models import ChatCompletionResponse

# ---------------------------------------------------------------------------
# Test helpers / fixtures
# ---------------------------------------------------------------------------


def _write_policy(tmp_path, monkeypatch, yaml_content: str, *, enabled: bool = True):
    """Write a guardrails YAML policy and point env vars at it."""
    policy_path = tmp_path / "guardrails.yaml"
    policy_path.write_text(textwrap.dedent(yaml_content).strip(), encoding="utf-8")
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", enabled)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", str(policy_path))
    return policy_path


def _regex_cfg(patterns=("a",), action_on_hit="redact", **kw) -> ParsedScannerConfig:
    return ParsedScannerConfig(
        type="regex",
        action_on_hit=action_on_hit,
        config=RegexConfig(patterns=list(patterns), **kw),
    )


def _ban_subs_cfg(
    substrings=("secret",), action_on_hit="redact", **kw
) -> ParsedScannerConfig:
    return ParsedScannerConfig(
        type="ban_substrings",
        action_on_hit=action_on_hit,
        config=BanSubstringsConfig(substrings=list(substrings), **kw),
    )


def _secrets_cfg(action_on_hit="redact", **kw) -> ParsedScannerConfig:
    return ParsedScannerConfig(
        type="secrets",
        action_on_hit=action_on_hit,
        config=SecretsConfig(**kw),
    )


def _sensitive_cfg(
    detectors=("email", "phone", "credit_card", "ip_address"),
    action_on_hit="redact",
    **kw,
) -> ParsedScannerConfig:
    return ParsedScannerConfig(
        type="sensitive",
        action_on_hit=action_on_hit,
        config=SensitiveConfig(detectors=list(detectors), **kw),
    )


def _make_response(content: str = "hello") -> ChatCompletionResponse:
    return ChatCompletionResponse(
        id="chatcmpl-test",
        object="chat.completion",
        created=int(time.time()),
        model="mock-model",
        choices=[
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": "stop",
            }
        ],
    )


def _make_tool_call_response() -> ChatCompletionResponse:
    """Assistant response carrying tool_calls but no string content."""
    return ChatCompletionResponse(
        id="chatcmpl-test",
        object="chat.completion",
        created=int(time.time()),
        model="mock-model",
        choices=[
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": None,
                    "tool_calls": [
                        {
                            "id": "call_1",
                            "type": "function",
                            "function": {"name": "f", "arguments": "{}"},
                        }
                    ],
                },
                "finish_reason": "tool_calls",
            }
        ],
    )


def _patch_scan_output(monkeypatch, fn):
    monkeypatch.setattr(output_guardrails_module, "scan_output", fn)


def _counter_value(metric, **labels) -> float:
    return metric.labels(**labels)._value.get()


@pytest.fixture
def fake_llm_guard_scanners(monkeypatch):
    """Replace llm_guard's Regex / BanSubstrings with simple recording stubs.

    Returns ``(FakeRegex, FakeBanSubstrings)`` so individual tests can assert on
    isinstance / captured kwargs.
    """

    class FakeRegex:
        def __init__(self, patterns, *, is_blocked=True, match_type=None, redact=False):
            self.patterns = patterns
            self.is_blocked = is_blocked
            self.match_type = match_type
            self.redact = redact

    class FakeBanSubstrings:
        def __init__(
            self,
            substrings,
            *,
            match_type=None,
            case_sensitive=False,
            contains_all=False,
            redact=False,
        ):
            self.substrings = substrings
            self.match_type = match_type
            self.case_sensitive = case_sensitive
            self.contains_all = contains_all
            self.redact = redact

    monkeypatch.setattr(
        scanner_schemas_module.llm_guard_output_scanners,
        "Regex",
        FakeRegex,
        raising=False,
    )
    monkeypatch.setattr(
        scanner_schemas_module.llm_guard_output_scanners,
        "BanSubstrings",
        FakeBanSubstrings,
        raising=False,
    )
    return FakeRegex, FakeBanSubstrings


# ---------------------------------------------------------------------------
# Policy loading via from_config
# ---------------------------------------------------------------------------


def test_from_config_loads_yaml_policy(tmp_path, monkeypatch):
    _write_policy(
        tmp_path,
        monkeypatch,
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
        """,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "block"
    assert guardrails.block_message == "blocked-by-policy"
    assert guardrails.policy_hash
    assert guardrails.policy_path.endswith("guardrails.yaml")
    assert guardrails.scanner_configs == (
        _regex_cfg(patterns=[r"https?://\S+"], action_on_hit="block"),
        _ban_subs_cfg(substrings=["secret"], action_on_hit="block"),
    )


def test_from_config_records_policy_load_metrics(tmp_path, monkeypatch):
    _write_policy(
        tmp_path,
        monkeypatch,
        """
        scanners:
          - type: regex
            patterns:
              - a
        """,
    )

    before = _counter_value(
        output_guardrails_policy_load_total,
        policy_status="success",
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.policy_hash
    assert (
        _counter_value(
            output_guardrails_policy_load_total,
            policy_status="success",
        )
        == before + 1
    )


def test_from_config_loads_yaml_policy_with_secrets_and_sensitive(
    tmp_path, monkeypatch
):
    _write_policy(
        tmp_path,
        monkeypatch,
        """
        action: redact
        scanners:
          - type: secrets
            redactMode: partial
          - type: sensitive
            detectors:
              - email
              - ip_address
        """,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.scanner_configs == (
        _secrets_cfg(redact_mode="partial"),
        _sensitive_cfg(detectors=["email", "ip_address"]),
    )


def test_from_config_keeps_empty_scanners_when_policy_path_missing(monkeypatch):
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(
        config, "OUTPUT_GUARDRAILS_POLICY_PATH", "/tmp/missing-guardrails.yaml"
    )
    before = _counter_value(
        output_guardrails_policy_load_total,
        policy_status="missing",
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.action_on_hit == "redact"
    assert guardrails.block_message == DEFAULT_BLOCK_MESSAGE
    assert guardrails.scanner_configs == ()
    assert (
        _counter_value(
            output_guardrails_policy_load_total,
            policy_status="missing",
        )
        == before + 1
    )


def test_from_config_records_load_failed_metric_on_yaml_error(tmp_path, monkeypatch):
    _write_policy(
        tmp_path,
        monkeypatch,
        "scanners:\n  - type: regex\n    patterns:\n      - a\n",
    )

    before = _counter_value(
        output_guardrails_policy_load_total,
        policy_status="load_failed",
    )

    def raising_safe_load(*args, **kwargs):
        raise ValueError("boom")

    monkeypatch.setattr(output_guardrails_module.yaml, "safe_load", raising_safe_load)

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.scanner_configs == ()
    assert (
        _counter_value(
            output_guardrails_policy_load_total,
            policy_status="load_failed",
        )
        == before + 1
    )


def test_from_config_records_invalid_metric_for_non_dict_policy(tmp_path, monkeypatch):
    _write_policy(
        tmp_path,
        monkeypatch,
        """
        - type: regex
          patterns:
            - a
        """,
    )

    before = _counter_value(
        output_guardrails_policy_load_total,
        policy_status="invalid",
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.scanner_configs == ()
    assert (
        _counter_value(
            output_guardrails_policy_load_total,
            policy_status="invalid",
        )
        == before + 1
    )


def test_from_config_replaces_scanners_with_policy_values(tmp_path, monkeypatch):
    _write_policy(
        tmp_path,
        monkeypatch,
        """
        action: block
        scanners:
          - type: ban_substrings
            substrings:
              - yaml-only
        """,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "block"
    assert guardrails.scanner_configs == (
        _ban_subs_cfg(substrings=["yaml-only"], action_on_hit="block"),
    )


def test_from_config_invalid_action_falls_back_to_default(tmp_path, monkeypatch):
    _write_policy(
        tmp_path,
        monkeypatch,
        """
        action: passthrough
        scanners:
          - type: regex
            patterns:
              - https?://\\S+
        """,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "redact"
    assert guardrails.scanner_configs == (_regex_cfg(patterns=[r"https?://\S+"]),)


def test_from_config_returns_empty_scanners_when_policy_scanners_is_not_a_list(
    tmp_path, monkeypatch
):
    _write_policy(
        tmp_path,
        monkeypatch,
        """
        action: block
        scanners:
          type: regex
        """,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.action_on_hit == "block"
    assert guardrails.scanner_configs == ()


def test_from_config_skips_invalid_scanners_and_filters_non_string_values(
    tmp_path, monkeypatch, fake_llm_guard_scanners
):
    _write_policy(
        tmp_path,
        monkeypatch,
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
        """,
    )

    guardrails = OutputGuardrails.from_config()

    assert guardrails.scanner_configs == (
        _regex_cfg(patterns=[r"https?://\S+"]),
        _ban_subs_cfg(substrings=["secret"]),
    )

    scanners = guardrails._build_scanners()

    assert len(scanners) == 2
    assert scanners[0].patterns == [r"https?://\S+"]
    assert scanners[0].redact is True
    assert scanners[1].substrings == ["secret"]
    assert scanners[1].redact is True


def test_from_config_with_empty_policy_path_keeps_defaults(monkeypatch):
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_ENABLED", True)
    monkeypatch.setattr(config, "OUTPUT_GUARDRAILS_POLICY_PATH", "")

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.scanner_configs == ()
    assert guardrails.action_on_hit == "redact"
    assert guardrails.block_message == DEFAULT_BLOCK_MESSAGE


def test_from_config_skips_policy_io_when_disabled(tmp_path, monkeypatch):
    """When the feature flag is off, the policy file must not be opened
    even if the path is set (and even if the file is malformed)."""
    policy_path = _write_policy(
        tmp_path,
        monkeypatch,
        "scanners: [unclosed\n",
        enabled=False,
    )

    open_calls: list[str] = []
    real_open = builtins.open

    def tracking_open(path, *args, **kwargs):
        open_calls.append(str(path))
        return real_open(path, *args, **kwargs)

    monkeypatch.setattr(builtins, "open", tracking_open)

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is False
    assert guardrails.scanner_configs == ()
    assert str(policy_path) not in open_calls


# ---------------------------------------------------------------------------
# Policy file load edge cases
# ---------------------------------------------------------------------------


def test_apply_policy_file_handles_yaml_parse_error(tmp_path, monkeypatch):
    _write_policy(tmp_path, monkeypatch, "scanners: [unclosed\n")

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.scanner_configs == ()
    assert guardrails.action_on_hit == "redact"
    assert guardrails.block_message == DEFAULT_BLOCK_MESSAGE


def test_apply_policy_file_rejects_non_dict_top_level(tmp_path, monkeypatch):
    _write_policy(tmp_path, monkeypatch, "- just\n- a list\n")

    guardrails = OutputGuardrails.from_config()

    assert guardrails.enabled is True
    assert guardrails.scanner_configs == ()


# ---------------------------------------------------------------------------
# _parse_policy_scanner_configs edge cases
# ---------------------------------------------------------------------------


def test_parse_policy_scanner_configs_skips_unknown_and_invalid_schema():
    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            {"type": "unknown_scanner"},
            {"type": "regex"},  # missing required 'patterns'
            {"type": "ban_substrings"},  # missing required 'substrings'
            {"type": "secrets", "redact_mode": "bogus"},
            {"type": "sensitive", "detectors": ["email", "bogus"]},
            {"type": "regex", "patterns": ["a"]},
        ],
        "guardrails.yaml",
    )

    assert parsed == (_regex_cfg(patterns=["a"]),)


def test_parse_policy_scanner_configs_handles_none_and_blank_type():
    assert (
        output_guardrails_module._parse_policy_scanner_configs(None, "guardrails.yaml")
        == ()
    )

    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            {"type": ""},
            {"type": "   "},
            {},
            {"type": "regex", "patterns": ["ok"]},
        ],
        "guardrails.yaml",
    )
    assert parsed == (_regex_cfg(patterns=["ok"]),)


def test_parse_policy_scanner_configs_skips_invalid_match_type():
    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            {"type": "ban_substrings", "substrings": ["a"], "match_type": "bogus"},
            {"type": "regex", "patterns": ["a"], "match_type": "bogus"},
            {"type": "ban_substrings", "substrings": ["a"], "match_type": "WORD"},
            {"type": "regex", "patterns": ["a"], "match_type": "FullMatch"},
        ],
        "guardrails.yaml",
    )

    # Invalid match_type values are rejected at parse time; valid ones are
    # accepted case-insensitively and stored in normalized lowercase form.
    assert parsed == (
        _ban_subs_cfg(substrings=["a"], match_type="word"),
        _regex_cfg(patterns=["a"], match_type="fullmatch"),
    )


def test_parse_policy_scanner_configs_skips_uncompilable_regex_pattern():
    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            {"type": "regex", "patterns": ["[unclosed"]},
            {"type": "regex", "patterns": ["valid", "(?P<x>"]},
            {"type": "regex", "patterns": [r"\d+"]},
        ],
        "guardrails.yaml",
    )

    # Any pattern in the list that fails to compile rejects the whole scanner.
    assert parsed == (_regex_cfg(patterns=[r"\d+"]),)


def test_parse_policy_scanner_configs_rejects_non_bool_flags():
    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            # String "false" is truthy in Python; must be rejected, not silently
            # treated as True.
            {"type": "ban_substrings", "substrings": ["a"], "case_sensitive": "false"},
            {"type": "ban_substrings", "substrings": ["a"], "contains_all": 1},
            {"type": "regex", "patterns": ["a"], "is_blocked": "no"},
            # Native YAML booleans (already parsed to Python bool) are accepted.
            {"type": "ban_substrings", "substrings": ["a"], "case_sensitive": True},
        ],
        "guardrails.yaml",
    )

    assert parsed == (_ban_subs_cfg(substrings=["a"], case_sensitive=True),)


def test_parse_policy_scanner_configs_accepts_secrets_and_sensitive():
    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            {"type": "secrets", "redact_mode": "partial"},
            {"type": "sensitive", "detectors": ["email", "credit_card"]},
        ],
        "guardrails.yaml",
    )

    assert parsed == (
        _secrets_cfg(redact_mode="partial"),
        ParsedScannerConfig(
            type="sensitive",
            action_on_hit="redact",
            config=SensitiveConfig(detectors=["email", "credit_card"]),
        ),
    )


def test_parse_policy_scanner_configs_skips_redact_incompatible_scanners(
    monkeypatch,
):
    class NonRedactingScannerConfig:
        supports_redact = False

        @classmethod
        def from_dict(cls, raw):
            return cls()

        def build(self, action_on_hit):
            return object()

    monkeypatch.setitem(
        output_guardrails_module.SCANNER_REGISTRY,
        "non_redacting",
        NonRedactingScannerConfig,
    )

    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [
            {"type": "non_redacting"},
            {"type": "regex", "patterns": ["a"]},
        ],
        "guardrails.yaml",
        action_on_hit="redact",
    )

    assert parsed == (_regex_cfg(patterns=["a"]),)


def test_parse_policy_scanner_configs_allows_non_redact_scanners_for_block(
    monkeypatch,
):
    class NonRedactingScannerConfig:
        supports_redact = False

        @classmethod
        def from_dict(cls, raw):
            return cls()

        def build(self, action_on_hit):
            return object()

    monkeypatch.setitem(
        output_guardrails_module.SCANNER_REGISTRY,
        "non_redacting",
        NonRedactingScannerConfig,
    )

    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [{"type": "non_redacting"}],
        "guardrails.yaml",
        action_on_hit="block",
    )

    assert len(parsed) == 1
    assert parsed[0].type == "non_redacting"


# ---------------------------------------------------------------------------
# _build_scanners
# ---------------------------------------------------------------------------


def test_build_scanners_supports_normalized_ban_substrings_type(
    fake_llm_guard_scanners,
):
    _, FakeBanSubstrings = fake_llm_guard_scanners

    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [{"type": "ban-substrings", "substrings": ["secret"]}],
        "guardrails.yaml",
    )

    guardrails = OutputGuardrails(
        enabled=True,
        action_on_hit="redact",
        scanner_configs=parsed,
    )

    scanners = guardrails._build_scanners()

    assert parsed == (_ban_subs_cfg(substrings=["secret"]),)
    assert len(scanners) == 1
    assert isinstance(scanners[0], FakeBanSubstrings)
    assert scanners[0].substrings == ["secret"]
    assert scanners[0].redact is True


def test_build_scanners_uses_per_scanner_action(fake_llm_guard_scanners):
    parsed = (
        _regex_cfg(patterns=["a"], action_on_hit="redact"),
        _ban_subs_cfg(substrings=["secret"], action_on_hit="block"),
    )

    guardrails = OutputGuardrails(
        enabled=True,
        action_on_hit="redact",
        scanner_configs=parsed,
    )

    scanners = guardrails._build_scanners()

    assert scanners[0].redact is True
    assert scanners[1].redact is False


def test_build_scanners_supports_secrets_type(monkeypatch):
    class FakeSecrets:
        def __init__(self, *, redact_mode="all"):
            self.redact_mode = redact_mode

        def scan(self, output):
            return f"{self.redact_mode}:{output}", False, 1.0

    monkeypatch.setattr(
        scanner_schemas_module.llm_guard_input_scanners,
        "Secrets",
        FakeSecrets,
        raising=False,
    )

    parsed = output_guardrails_module._parse_policy_scanner_configs(
        [{"type": "secrets", "redactMode": "partial"}],
        "guardrails.yaml",
    )
    guardrails = OutputGuardrails(enabled=True, scanner_configs=parsed)

    scanners = guardrails._build_scanners()

    assert parsed == (_secrets_cfg(redact_mode="partial"),)
    assert scanners[0].scan("ignored", "secret-value") == (
        "partial:secret-value",
        False,
        1.0,
    )


def test_secrets_config_build_works_with_scan_output_end_to_end():
    scanner = SecretsConfig(redact_mode="partial").build("redact")
    original_output = "Contact me at AKIA1234567890ABCDEF for access."

    sanitized_output, results_valid, results_score = scan_output(
        [scanner],
        "ignored prompt",
        original_output,
        fail_fast=False,
    )

    assert sanitized_output != original_output
    assert any(valid is False for valid in results_valid.values())
    assert results_score


def test_sensitive_config_build_redacts_requested_detectors_only():
    scanner = SensitiveConfig(detectors=["email", "ip_address"]).build("redact")

    sanitized, is_valid, risk_score = scanner.scan(
        "",
        "Email alice@example.com from 10.0.0.1 but keep 4111 1111 1111 1111",
    )

    assert sanitized == "Email <EMAIL> from <IP_ADDRESS> but keep 4111 1111 1111 1111"
    assert is_valid is False
    assert risk_score == 1.0


def test_sensitive_config_detects_luhn_valid_credit_cards_only():
    scanner = SensitiveConfig(detectors=["credit_card"]).build("redact")

    sanitized, is_valid, risk_score = scanner.scan(
        "",
        "good 4111 1111 1111 1111 bad 4111 1111 1111 1112",
    )

    assert sanitized == "good <CREDIT_CARD> bad 4111 1111 1111 1112"
    assert is_valid is False
    assert risk_score == 1.0


def test_guard_response_redacts_sensitive_entities_end_to_end():
    guardrails = OutputGuardrails(
        enabled=True,
        scanner_configs=(_sensitive_cfg(detectors=["email", "phone", "ip_address"]),),
    )

    out = guardrails.guard_response(
        _make_response(
            "Email alice@example.com or call +1 (206) 555-0100 from 10.0.0.1"
        ),
        {"messages": []},
    )

    assert out.choices[0].message.content == (
        "Email <EMAIL> or call <PHONE> from <IP_ADDRESS>"
    )


def test_build_scanners_skips_configs_whose_build_raises(monkeypatch):
    sentinel = object()
    call_count = {"n": 0}

    def fake_regex(*args, **kwargs):
        call_count["n"] += 1
        if call_count["n"] == 1:
            raise RuntimeError("simulated build failure")
        return sentinel

    monkeypatch.setattr(
        scanner_schemas_module.llm_guard_output_scanners,
        "Regex",
        fake_regex,
        raising=False,
    )

    guardrails = OutputGuardrails(
        enabled=True,
        scanner_configs=[_regex_cfg(patterns=["a"]), _regex_cfg(patterns=["b"])],
    )

    success_before = _counter_value(
        output_guardrails_scanner_build_total,
        type="regex",
        status="success",
    )
    failure_before = _counter_value(
        output_guardrails_scanner_build_total,
        type="regex",
        status="failure",
    )

    # First config raised -> skipped; second was built successfully.
    assert guardrails._build_scanners() == [sentinel]
    assert (
        _counter_value(
            output_guardrails_scanner_build_total,
            type="regex",
            status="success",
        )
        == success_before + 1
    )
    assert (
        _counter_value(
            output_guardrails_scanner_build_total,
            type="regex",
            status="failure",
        )
        == failure_before + 1
    )


def test_regex_config_build_uses_value_lookup_for_fullmatch(
    fake_llm_guard_scanners,
):
    """Regression test: enum value 'fullmatch' must work end-to-end (the enum
    NAME is FULL_MATCH, so a name-based lookup would raise KeyError)."""
    cfg = RegexConfig.from_dict({"patterns": ["a"], "match_type": "fullmatch"})
    scanner = cfg.build("redact")

    assert scanner.match_type == scanner_schemas_module.RegexMatchType.FULL_MATCH


# ---------------------------------------------------------------------------
# guard_response runtime branches
# ---------------------------------------------------------------------------


def test_guard_response_short_circuits_when_disabled():
    response = _make_response("anything")
    assert (
        OutputGuardrails(enabled=False).guard_response(response, {"messages": []})
        is response
    )


def test_guard_response_short_circuits_when_no_scanners():
    response = _make_response("anything")
    guardrails = OutputGuardrails(enabled=True, scanner_configs=[])
    assert guardrails.guard_response(response, {"messages": []}) is response


def test_guard_response_skips_non_string_content(monkeypatch):
    """When the assistant message has no string content (e.g. tool_calls only),
    the scanner pipeline must be skipped instead of being fed a None."""

    def _fail_scan(*args, **kwargs):
        raise AssertionError("scan_output should not be invoked")

    _patch_scan_output(monkeypatch, _fail_scan)

    guardrails = OutputGuardrails(
        enabled=True,
        scanner_configs=[_regex_cfg(patterns=[r"\S+"])],
    )

    out = guardrails.guard_response(_make_tool_call_response(), {"messages": []})
    assert out.choices[0].message.content is None


def test_guard_response_passes_through_when_no_scanner_triggered(monkeypatch):
    before = _counter_value(
        guardrails_response_actions_total,
        final_action="allow",
    )

    _patch_scan_output(
        monkeypatch,
        lambda scanners, prompt, output, fail_fast: (
            output,
            {"regex": True},
            {"regex": 0.0},
        ),
    )

    guardrails = OutputGuardrails(
        enabled=True,
        scanner_configs=[_regex_cfg(patterns=[r"never-matches"])],
    )

    out = guardrails.guard_response(_make_response("clean output"), {"messages": []})
    assert out.choices[0].message.content == "clean output"
    assert (
        _counter_value(
            guardrails_response_actions_total,
            final_action="allow",
        )
        == before + 1
    )


def test_guard_response_recovers_when_scan_output_raises(monkeypatch):
    def _boom(*args, **kwargs):
        raise RuntimeError("scanner exploded")

    _patch_scan_output(monkeypatch, _boom)

    response = _make_response("clean output")
    guardrails = OutputGuardrails(
        enabled=True,
        scanner_configs=[_regex_cfg(patterns=[r"\S+"])],
    )

    # Internal failure must degrade safely: return the original response object.
    assert guardrails.guard_response(response, {"messages": []}) is response


@pytest.mark.parametrize(
    "action,block_message,expected_content",
    [
        ("redact", DEFAULT_BLOCK_MESSAGE, "REDACTED-CONTENT"),
        ("block", "blocked!", "blocked!"),
    ],
)
def test_guard_response_applies_action(
    monkeypatch, action, block_message, expected_content
):
    response_before = _counter_value(
        guardrails_response_actions_total,
        final_action=action,
    )

    _patch_scan_output(
        monkeypatch,
        lambda scanners, prompt, output, fail_fast: (
            "REDACTED-CONTENT",
            {"regex": False},
            {"regex": 0.9},
        ),
    )

    guardrails = OutputGuardrails(
        enabled=True,
        action_on_hit=action,
        block_message=block_message,
        scanner_configs=[_regex_cfg(patterns=[r"\S+"], action_on_hit=action)],
    )

    out = guardrails.guard_response(_make_response("dirty"), {"messages": []})
    assert out.choices[0].message.content == expected_content
    assert (
        _counter_value(
            guardrails_response_actions_total,
            final_action=action,
        )
        == response_before + 1
    )


def test_guard_response_increments_hit_metric(monkeypatch):
    before = _counter_value(
        guardrails_response_scanner_hits_total,
        scanner_type="regex",
        action="redact",
    )

    _patch_scan_output(
        monkeypatch,
        lambda scanners, prompt, output, fail_fast: (
            "REDACTED-CONTENT",
            {"regex": False},
            {"regex": 0.9},
        ),
    )

    guardrails = OutputGuardrails(
        enabled=True,
        scanner_configs=(_regex_cfg(patterns=[r"\S+"], action_on_hit="redact"),),
    )

    guardrails.guard_response(_make_response("dirty"), {"messages": []})

    assert (
        _counter_value(
            guardrails_response_scanner_hits_total,
            scanner_type="regex",
            action="redact",
        )
        == before + 1
    )


def test_guard_response_increments_fail_closed_metric(monkeypatch):
    before = _counter_value(
        guardrails_response_actions_total,
        final_action="fail_closed",
    )

    def _boom(*args, **kwargs):
        raise RuntimeError("scanner exploded")

    _patch_scan_output(monkeypatch, _boom)

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=False,
        scanner_configs=(_regex_cfg(patterns=[r"\S+"]),),
    )

    with pytest.raises(output_guardrails_module.OutputGuardrailsError):
        guardrails.guard_response(_make_response("dirty"), {"messages": []})

    assert (
        _counter_value(
            guardrails_response_actions_total,
            final_action="fail_closed",
        )
        == before + 1
    )


# ---------------------------------------------------------------------------
# _extract_prompt defensive branches
# ---------------------------------------------------------------------------


def test_extract_prompt_handles_non_list_messages():
    guardrails = OutputGuardrails(enabled=True)
    assert guardrails._extract_prompt({"messages": "not-a-list"}) == ""
    assert guardrails._extract_prompt({}) == ""


def test_extract_prompt_skips_non_dict_message_entries():
    guardrails = OutputGuardrails(enabled=True)
    prompt = guardrails._extract_prompt(
        {
            "messages": [
                "garbage",
                None,
                {"role": "user", "content": "hello"},
                42,
                {"role": "user", "content": "world"},
            ]
        }
    )

    assert prompt == "hello\n\nworld"
