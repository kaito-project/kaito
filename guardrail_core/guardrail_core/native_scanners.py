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

"""KAITO-owned native guardrails scanners.

All scanners implement the KAITO output guardrail scanner interface:
    scan(prompt: str, output: str) -> tuple[str, bool, float]
"""

import hashlib
import os
import re
import tempfile
import threading
from enum import StrEnum

from detect_secrets.core.secrets_collection import SecretsCollection
from detect_secrets.settings import default_settings

# Compatibility patterns for token formats not fully detected by the currently
# pinned detect-secrets defaults.
_DETECT_SECRETS_COMPATIBILITY_PATTERNS = (
    re.compile(r"(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36}"),
    re.compile(r"github_pat_[0-9A-Za-z_]{82}"),
    re.compile(r"(?i)\bAIza[0-9A-Za-z_-]{35}(?=['|\"\n\r\s\x60;]|$)"),
)
# detect-secrets settings are process-global. Serialize scans to prevent
# concurrent settings mutation from causing missed detections.
_DETECT_SECRETS_SETTINGS_LOCK = threading.Lock()


class BanSubstringsMatchType(StrEnum):
    """Match types for BanSubstrings scanner."""

    WORD = "word"
    STR = "str"


class RegexMatchType(StrEnum):
    """Match types for Regex scanner."""

    SEARCH = "search"
    FULL_MATCH = "fullmatch"
    ALL = "all"


class NativeBanSubstringsScanner:
    """KAITO-owned BanSubstrings scanner using substring matching."""

    def __init__(
        self,
        substrings: list[str],
        match_type: BanSubstringsMatchType = BanSubstringsMatchType.WORD,
        case_sensitive: bool = False,
        contains_all: bool = False,
        redact: bool = False,
    ) -> None:
        self.substrings = substrings
        self.match_type = match_type
        self.case_sensitive = case_sensitive
        self.contains_all = contains_all
        self.redact = redact

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for banned substrings.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original or redacted output text.
                valid: True if output passes the scanner (no banned substrings found).
                score: Conformant to KAITO scanner contract: -1.0 when valid,
                    1.0 when invalid (substring found in block-list), or 0.0 in
                    contains_all mode when not all required substrings are found.
        """
        del prompt
        found_substrings = []
        search_text = output if self.case_sensitive else output.lower()

        for substring in self.substrings:
            search_str = substring if self.case_sensitive else substring.lower()

            if self.match_type == BanSubstringsMatchType.WORD:
                pattern = r"\b" + re.escape(search_str) + r"\b"
                if re.search(pattern, search_text):
                    found_substrings.append(substring)
            elif self.match_type == BanSubstringsMatchType.STR:
                if search_str in search_text:
                    found_substrings.append(substring)

        if self.contains_all and len(found_substrings) < len(self.substrings):
            return output, True, 0.0

        if found_substrings:
            if self.redact:
                # Collect all match positions based on original output
                all_matches = []
                for substring in found_substrings:
                    search_str = substring if self.case_sensitive else substring.lower()
                    start = 0
                    while True:
                        if self.case_sensitive:
                            pos = output.find(substring, start)
                        else:
                            pos = search_text.find(search_str, start)
                        if pos == -1:
                            break
                        all_matches.append((pos, pos + len(substring)))
                        start = pos + 1

                # Apply all replacements from right to left to avoid offset issues
                sanitized = output
                for match_start, match_end in sorted(all_matches, reverse=True):
                    sanitized = (
                        sanitized[:match_start] + "[REDACTED]" + sanitized[match_end:]
                    )

                return sanitized, False, 1.0
            else:
                return output, False, 1.0

        return output, True, -1.0


class NativeRegexScanner:
    """KAITO-owned Regex scanner for pattern matching."""

    def __init__(
        self,
        patterns: list[str],
        is_blocked: bool = True,
        match_type: RegexMatchType = RegexMatchType.SEARCH,
        redact: bool = False,
    ) -> None:
        self.patterns = patterns  # Store original pattern strings for compatibility
        self._compiled_patterns = [re.compile(p) for p in patterns]
        self.is_blocked = is_blocked
        self.match_type = match_type
        self.redact = redact

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for regex pattern matches.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original or redacted output text.
                valid: In block-list mode: True if no patterns matched (valid).
                    In allow-list mode: True if at least one pattern matched (valid).
                score: Conformant to KAITO scanner contract: -1.0 when valid,
                    1.0 when invalid (pattern matched in block-list or no match in allow-list).
        """
        del prompt
        for pattern in self._compiled_patterns:
            matches = []
            if self.match_type == RegexMatchType.SEARCH:
                match = pattern.search(output)
                matches = [match] if match else []
            elif self.match_type == RegexMatchType.FULL_MATCH:
                match = pattern.fullmatch(output)
                matches = [match] if match else []
            else:  # ALL
                matches = list(pattern.finditer(output))

            if not matches:
                # This pattern didn't match, try next one
                continue

            # First matching pattern found - handle and return
            if not self.is_blocked:
                # Allow-list: match found = valid, score -1.0
                return output, True, -1.0

            # Block-list: pattern matched = invalid
            if self.redact:
                sanitized = output
                # Sort by position descending to avoid offset issues
                for match in sorted(matches, key=lambda m: m.start(), reverse=True):
                    sanitized = (
                        sanitized[: match.start()]
                        + "[REDACTED]"
                        + sanitized[match.end() :]
                    )
                return sanitized, False, 1.0
            else:
                return output, False, 1.0

        # All patterns checked, none matched
        if self.is_blocked:
            # Block-list: no block patterns matched = valid
            return output, True, -1.0
        else:
            # Allow-list: no allowed patterns matched = invalid, score 1.0
            return output, False, 1.0


class NativeSecretsScanner:
    """KAITO-owned Secrets scanner using detect-secrets directly.

    Detects and redacts sensitive information like API keys, passwords,
    and other secrets using the detect-secrets library.

    Redaction behavior:
        - all:     "****** (6 asterisks, fixed-length to avoid leaking secret length)"
        - partial: "XX..YY (first and last 2 chars)"
        - hash:    "MD5 hash of the secret value"
    """

    def __init__(self, redact_mode: str = "all") -> None:
        """Initialize the secrets scanner.

        Args:
            redact_mode: How to redact detected secrets. One of:
                - "all": Replace entire secret with fixed-length mask
                - "partial": Show first 2 and last 2 characters
                - "hash": Replace with MD5 hash
        """
        self._redact_mode = redact_mode

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for secrets and redact if found.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original or redacted output text.
                valid: True if no secrets found, False if secrets detected.
                score: -1.0 if no secrets (valid), 1.0 if secrets found (invalid).
        """
        del prompt
        if output.strip() == "":
            return output, True, -1.0

        secret_values = self._detect_secret_values(output)
        if not secret_values:
            return output, True, -1.0

        sanitized_output = output
        for secret_value in sorted(
            secret_values,
            key=lambda value: (-len(value), value),
        ):
            replacement = self._redact_value(secret_value, self._redact_mode)
            sanitized_output = sanitized_output.replace(secret_value, replacement)

        return sanitized_output, False, 1.0

    @staticmethod
    def _redact_value(value: str, redact_mode: str) -> str:
        """Redact secret using the specified mode.

        Args:
            value: The secret value to redact.
            redact_mode: One of "all", "partial", or "hash".

        Returns:
            Redacted representation of the secret.
        """
        if redact_mode == "all":
            return "******"
        if redact_mode == "partial":
            return f"{value[:2]}..{value[-2:]}"
        if redact_mode == "hash":
            return hashlib.md5(value.encode()).hexdigest()
        raise ValueError(f"redact mode wasn't recognized {redact_mode}")

    def _detect_secret_values(self, text: str) -> set[str]:
        """Detect secrets using detect-secrets library directly.

        Uses detect-secrets defaults plus compatibility patterns for known gaps.

        Args:
            text: The text to scan for secrets.

        Returns:
            A set of detected secret values.
        """
        secrets = SecretsCollection()

        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            delete=False,
        ) as temp_file:
            temp_file.write(text)
            temp_path = temp_file.name

        try:
            with _DETECT_SECRETS_SETTINGS_LOCK, default_settings():
                secrets.scan_file(temp_path)
        finally:
            os.remove(temp_path)

        secret_values = {
            found_secret.secret_value
            for file_path in secrets.files
            for found_secret in secrets[file_path]
            if found_secret.secret_value
        }
        secret_values.update(
            match.group(0)
            for pattern in _DETECT_SECRETS_COMPATIBILITY_PATTERNS
            for match in pattern.finditer(text)
        )
        return secret_values
