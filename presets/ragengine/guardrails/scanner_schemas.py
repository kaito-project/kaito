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

NOTE: Validation here is the runtime safety net for cases the
admission webhook cannot cover (failurePolicy=Ignore, pre-existing
CRs, ConfigMap-mounted policies, version skew). Bad configs are
logged and skipped so the rest of the chain still runs.
"""

import ipaddress
import re
from dataclasses import dataclass
from typing import Any, ClassVar

import llm_guard.input_scanners as llm_guard_input_scanners
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
_SECRETS_REDACT_MODES = frozenset({"all", "partial", "hash"})
_DEFAULT_SENSITIVE_DETECTORS = ("email", "phone", "credit_card", "ip_address")
_SENSITIVE_DETECTORS = frozenset(_DEFAULT_SENSITIVE_DETECTORS)
_EMAIL_PATTERN = re.compile(r"\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[A-Za-z]{2,}\b")
_PHONE_PATTERN = re.compile(r"(?<!\w)(?:\+?\d[\d().\- ]{8,}\d)(?!\w)")
_CREDIT_CARD_PATTERN = re.compile(r"(?<!\d)(?:\d[ -]?){12,18}\d(?!\d)")
_IPV4_PATTERN = re.compile(r"\b(?:\d{1,3}\.){3}\d{1,3}\b")


@dataclass(frozen=True)
class _SensitiveMatch:
    start: int
    end: int
    detector: str


class _OutputSecretsScanner:
    def __init__(self, scanner: Any) -> None:
        self._scanner = scanner

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        del prompt
        return self._scanner.scan(output)


class _PatternPIIScanner:
    def __init__(self, *, detectors: list[str], redact: bool) -> None:
        self._detectors = list(detectors)
        self._redact = redact

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        del prompt
        if output.strip() == "":
            return output, True, -1.0

        matches = _collect_sensitive_matches(output, self._detectors)
        if not matches:
            return output, True, -1.0

        sanitized_output = output
        if self._redact:
            sanitized_output = _redact_sensitive_matches(output, matches)

        return sanitized_output, False, 1.0


@dataclass
class SecretsConfig:
    supports_redact: ClassVar[bool] = True
    redact_mode: str = "all"

    @classmethod
    def from_dict(cls, raw: dict) -> "SecretsConfig":
        redact_mode = str(raw.get("redact_mode", "all")).lower()
        if redact_mode not in _SECRETS_REDACT_MODES:
            raise ValueError(
                f"secrets 'redact_mode' must be one of "
                f"{sorted(_SECRETS_REDACT_MODES)}, got {redact_mode!r}"
            )
        return cls(redact_mode=redact_mode)

    def build(self, action_on_hit: str) -> Any:
        return _OutputSecretsScanner(
            llm_guard_input_scanners.Secrets(redact_mode=self.redact_mode)
        )


@dataclass
class SensitiveConfig:
    supports_redact: ClassVar[bool] = True
    detectors: list[str]

    @classmethod
    def from_dict(cls, raw: dict) -> "SensitiveConfig":
        detectors_value = raw.get("detectors")
        if detectors_value is None:
            return cls(detectors=list(_DEFAULT_SENSITIVE_DETECTORS))

        detectors = [item.lower() for item in _coerce_string_list(detectors_value)]
        if not detectors:
            raise ValueError(
                "sensitive requires 'detectors' to be a non-empty list of strings"
            )

        invalid_detectors = [
            detector for detector in detectors if detector not in _SENSITIVE_DETECTORS
        ]
        if invalid_detectors:
            raise ValueError(
                f"sensitive 'detectors' must be drawn from "
                f"{sorted(_SENSITIVE_DETECTORS)}, got {invalid_detectors!r}"
            )

        return cls(detectors=_dedupe_strings(detectors))

    def build(self, action_on_hit: str) -> Any:
        return _PatternPIIScanner(
            detectors=self.detectors,
            redact=(action_on_hit == "redact"),
        )


@dataclass
class BanSubstringsConfig:
    supports_redact: ClassVar[bool] = True
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
            case_sensitive=_coerce_bool(
                raw.get("case_sensitive"), False, field="case_sensitive"
            ),
            contains_all=_coerce_bool(
                raw.get("contains_all"), False, field="contains_all"
            ),
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
    supports_redact: ClassVar[bool] = True
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
            is_blocked=_coerce_bool(raw.get("is_blocked"), True, field="is_blocked"),
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
    "secrets": SecretsConfig,
    "sensitive": SensitiveConfig,
}


@dataclass
class ParsedScannerConfig:
    """A scanner config that has already passed schema validation."""

    type: str
    config: Any
    action_on_hit: str | None = None


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]


def _coerce_bool(value: Any, fallback: bool, *, field: str) -> bool:
    # bool("false") is True, so we reject non-bool inputs explicitly instead of
    # silently inverting user intent. YAML native true/false parses to Python bool.
    if value is None:
        return fallback
    if isinstance(value, bool):
        return value
    raise ValueError(f"{field!r} must be a boolean (true/false), got {value!r}")


def _dedupe_strings(values: list[str]) -> list[str]:
    deduped: list[str] = []
    for value in values:
        if value not in deduped:
            deduped.append(value)
    return deduped


def _collect_sensitive_matches(
    text: str, detectors: list[str]
) -> list[_SensitiveMatch]:
    matches: list[_SensitiveMatch] = []
    for detector in detectors:
        if detector == "email":
            matches.extend(
                _SensitiveMatch(match.start(), match.end(), detector)
                for match in _EMAIL_PATTERN.finditer(text)
            )
        elif detector == "phone":
            for match in _PHONE_PATTERN.finditer(text):
                candidate = match.group(0)
                digits = re.sub(r"\D", "", candidate)
                if len(digits) < 10 or len(digits) > 15:
                    continue
                if not any(separator in candidate for separator in " +-.()"):
                    continue
                matches.append(_SensitiveMatch(match.start(), match.end(), detector))
        elif detector == "credit_card":
            for match in _CREDIT_CARD_PATTERN.finditer(text):
                digits = re.sub(r"\D", "", match.group(0))
                if len(digits) < 13 or len(digits) > 19:
                    continue
                if not _passes_luhn_check(digits):
                    continue
                matches.append(_SensitiveMatch(match.start(), match.end(), detector))
        elif detector == "ip_address":
            for match in _IPV4_PATTERN.finditer(text):
                try:
                    ip = ipaddress.ip_address(match.group(0))
                except ValueError:
                    continue
                if ip.version != 4:
                    continue
                matches.append(_SensitiveMatch(match.start(), match.end(), detector))

    return _dedupe_matches(matches)


def _dedupe_matches(matches: list[_SensitiveMatch]) -> list[_SensitiveMatch]:
    deduped: list[_SensitiveMatch] = []
    for match in sorted(
        matches, key=lambda item: (item.start, -(item.end - item.start))
    ):
        if any(
            match.start < existing.end and existing.start < match.end
            for existing in deduped
        ):
            continue
        deduped.append(match)
    return deduped


def _redact_sensitive_matches(text: str, matches: list[_SensitiveMatch]) -> str:
    redacted = text
    for match in sorted(matches, key=lambda item: item.start, reverse=True):
        replacement = f"<{match.detector.upper()}>"
        redacted = redacted[: match.start] + replacement + redacted[match.end :]
    return redacted


def _passes_luhn_check(value: str) -> bool:
    total = 0
    reverse_digits = value[::-1]
    for index, digit_char in enumerate(reverse_digits):
        digit = int(digit_char)
        if index % 2 == 1:
            digit *= 2
            if digit > 9:
                digit -= 9
        total += digit
    return total % 10 == 0
