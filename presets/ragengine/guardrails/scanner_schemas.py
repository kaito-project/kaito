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

"""Scanner config schemas for output guardrails.

Each schema describes the YAML shape of one llm_guard scanner AND knows how
to build the corresponding scanner instance. To add a new scanner:
  1. Define a dataclass with `from_dict()` and `build()`
  2. Register it in SCANNER_REGISTRY below

TODO: Once this scanner config surface stabilizes, move schema validation
to the admission webhook so that invalid policies are rejected at
apply-time instead of being silently skipped at runtime.

TODO: Many llm_guard scanners (e.g. Toxicity, Bias, Language) do not
support redaction; pairing them with action=redact would be a no-op.
When adding such scanners, declare a per-schema `supports_redact` flag
and reject the (action=redact + non-redact scanner) combination at parse
time, instead of trying to fix it at runtime.
"""

import re
from dataclasses import dataclass
from typing import Any

import llm_guard.output_scanners as llm_guard_output_scanners
from llm_guard.input_scanners.ban_substrings import (
    MatchType as BanSubstringsMatchType,
)
from llm_guard.input_scanners.regex import MatchType as RegexMatchType

# Allowed match_type values, mirrored from llm_guard's enum *values* (not names).
# Keeping these here lets us reject invalid policies at parse time instead of
# letting the error surface only when the scanner is built.
_BAN_SUBSTRINGS_MATCH_TYPES = frozenset(m.value for m in BanSubstringsMatchType)
_REGEX_MATCH_TYPES = frozenset(m.value for m in RegexMatchType)


@dataclass
class BanSubstringsConfig:
    substrings: list[str]
    match_type: str = "word"
    case_sensitive: bool = False
    contains_all: bool = False

    @classmethod
    def from_dict(cls, raw: dict) -> "BanSubstringsConfig":
        substrings = _coerce_string_list(raw.get("substrings"))
        if not substrings:
            raise ValueError(
                "ban_substrings requires 'substrings' to be a non-empty list of strings"
            )
        match_type = str(raw.get("match_type", "word")).lower()
        if match_type not in _BAN_SUBSTRINGS_MATCH_TYPES:
            raise ValueError(
                f"ban_substrings 'match_type' must be one of "
                f"{sorted(_BAN_SUBSTRINGS_MATCH_TYPES)}, got {match_type!r}"
            )
        return cls(
            substrings=substrings,
            match_type=match_type,
            case_sensitive=bool(raw.get("case_sensitive", False)),
            contains_all=bool(raw.get("contains_all", False)),
        )

    def build(self, action_on_hit: str) -> Any:
        return llm_guard_output_scanners.BanSubstrings(
            substrings=list(self.substrings),
            match_type=BanSubstringsMatchType(self.match_type),
            case_sensitive=self.case_sensitive,
            contains_all=self.contains_all,
            redact=(action_on_hit == "redact"),
        )


@dataclass
class RegexConfig:
    patterns: list[str]
    is_blocked: bool = True
    match_type: str = "search"

    @classmethod
    def from_dict(cls, raw: dict) -> "RegexConfig":
        patterns = _coerce_string_list(raw.get("patterns"))
        if not patterns:
            raise ValueError(
                "regex requires 'patterns' to be a non-empty list of strings"
            )
        for pattern in patterns:
            try:
                re.compile(pattern)
            except re.error as exc:
                raise ValueError(
                    f"regex pattern {pattern!r} is not a valid regular expression: {exc}"
                ) from exc
        match_type = str(raw.get("match_type", "search")).lower()
        if match_type not in _REGEX_MATCH_TYPES:
            raise ValueError(
                f"regex 'match_type' must be one of "
                f"{sorted(_REGEX_MATCH_TYPES)}, got {match_type!r}"
            )
        return cls(
            patterns=patterns,
            is_blocked=bool(raw.get("is_blocked", True)),
            match_type=match_type,
        )

    def build(self, action_on_hit: str) -> Any:
        return llm_guard_output_scanners.Regex(
            patterns=list(self.patterns),
            is_blocked=self.is_blocked,
            match_type=RegexMatchType(self.match_type),
            redact=(action_on_hit == "redact"),
        )


SCANNER_REGISTRY: dict[str, type] = {
    "ban_substrings": BanSubstringsConfig,
    "regex": RegexConfig,
}


@dataclass
class ParsedScannerConfig:
    """A scanner config that has already passed schema validation."""

    type: str
    config: Any


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]
