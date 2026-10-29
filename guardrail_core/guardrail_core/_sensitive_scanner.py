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

"""Internal pattern-based sensitive-data scanner."""

import ipaddress
import re
from dataclasses import dataclass

from guardrail_core.scanning import ScanResult

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


class _PatternPIIScanner:
    def __init__(self, *, detectors: list[str], redact: bool) -> None:
        self._detectors = list(detectors)
        self._redact = redact

    def scan(self, prompt: str, output: str) -> ScanResult:
        del prompt
        if output.strip() == "":
            return ScanResult(output, True, -1.0)
        matches = _collect_sensitive_matches(output, self._detectors)
        if not matches:
            return ScanResult(output, True, -1.0)
        sanitized_output = output
        if self._redact:
            sanitized_output = _redact_sensitive_matches(output, matches)
        return ScanResult(sanitized_output, False, 1.0)


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
                if ip.version == 4:
                    matches.append(
                        _SensitiveMatch(match.start(), match.end(), detector)
                    )
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
    for index, digit_char in enumerate(value[::-1]):
        digit = int(digit_char)
        if index % 2 == 1:
            digit *= 2
            if digit > 9:
                digit -= 9
        total += digit
    return total % 10 == 0
