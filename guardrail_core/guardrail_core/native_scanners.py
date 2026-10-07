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

import json
import re
import unicodedata
from enum import StrEnum


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


class NativeInvisibleTextScanner:
    """KAITO-owned scanner to detect and remove invisible Unicode characters.

    Detects non-printable, zero-width, and control characters that can hide
    steganographic content or malicious instructions in model output.
    """

    def __init__(self) -> None:
        pass

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for invisible or non-printable Unicode characters.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original or sanitized output text with invisible chars removed.
                valid: True if no invisible characters found, False otherwise.
                score: Conformant to KAITO scanner contract: -1.0 when valid,
                    1.0 when invalid (invisible characters found).
        """
        del prompt

        invisible_chars = []
        for i, char in enumerate(output):
            category = unicodedata.category(char)
            # Detect format chars (Cf), private use (Co), and unassigned (Cn).
            # This matches llm-guard behavior: skips normal control chars like \n, \t, \r
            if (
                category in ("Cf", "Co", "Cn")
                or char
                in (
                    "​",  # Zero-width space
                    "‌",  # Zero-width non-joiner
                    "‍",  # Zero-width joiner
                    "﻿",  # Zero-width no-break space
                )
            ):
                invisible_chars.append(i)

        if not invisible_chars:
            return output, True, -1.0

        # Remove invisible characters
        sanitized = "".join(
            char for i, char in enumerate(output) if i not in invisible_chars
        )
        return sanitized, False, 1.0


class NativeJSONScanner:
    """KAITO-owned JSON validation and repair scanner.

    Validates that output is valid JSON. Can optionally attempt to repair
    malformed JSON using a simple heuristic repair algorithm.
    """

    def __init__(
        self,
        required_elements: int = 0,
        repair: bool = True,
    ) -> None:
        self.required_elements = required_elements
        self.repair = repair

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for valid JSON and optionally repair it.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original output, or repaired JSON if repair=True and fixable.
                valid: True if output is valid JSON and meets required_elements.
                score: Conformant to KAITO scanner contract: -1.0 when valid,
                    1.0 when invalid.
        """
        del prompt

        if output.strip() == "":
            return output, False, 1.0

        # Try to parse as JSON
        try:
            parsed = json.loads(output)
            # Check if it meets minimum element requirement
            if self.required_elements > 0 and (
                (isinstance(parsed, dict) and len(parsed) < self.required_elements)
                or (isinstance(parsed, list) and len(parsed) < self.required_elements)
            ):
                return output, False, 1.0
            return output, True, -1.0
        except (json.JSONDecodeError, ValueError):
            if not self.repair:
                return output, False, 1.0

            # Attempt simple repair strategies
            repaired = self._attempt_repair(output)
            if repaired is not None:
                try:
                    parsed = json.loads(repaired)
                    if self.required_elements > 0 and (
                        (
                            isinstance(parsed, dict)
                            and len(parsed) < self.required_elements
                        )
                        or (
                            isinstance(parsed, list)
                            and len(parsed) < self.required_elements
                        )
                    ):
                        return output, False, 1.0
                    return repaired, True, -1.0
                except (json.JSONDecodeError, ValueError):
                    return output, False, 1.0

            return output, False, 1.0

    def _attempt_repair(self, output: str) -> str | None:
        """Attempt to repair malformed JSON using simple heuristics.

        Returns:
            Repaired JSON string if repair was successful, None otherwise.
        """
        text = output.strip()

        # Try adding closing braces/brackets if missing
        if text.startswith("{"):
            braces_to_add = text.count("{") - text.count("}")
            text_with_close = text + "}" * braces_to_add
        elif text.startswith("["):
            brackets_to_add = text.count("[") - text.count("]")
            text_with_close = text + "]" * brackets_to_add
        else:
            return None

        try:
            json.loads(text_with_close)
            return text_with_close
        except (json.JSONDecodeError, ValueError):
            return None


class NativeReadingTimeScanner:
    """KAITO-owned scanner to enforce reading time limits on output.

    Uses a simple heuristic: assumes average reading speed of ~200 words per minute.
    Can either mark output invalid or truncate it to fit within the time limit.
    """

    # Average reading speed in words per minute
    _DEFAULT_READING_SPEED = 200

    def __init__(
        self,
        max_time: float,
        truncate: bool = False,
    ) -> None:
        self.max_time = max_time
        self.truncate = truncate

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for reading time and optionally truncate.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original or truncated output.
                valid: True if reading time <= max_time, False if exceeds or truncated.
                score: Conformant to KAITO scanner contract: -1.0 when valid,
                    1.0 when invalid/truncated.
        """
        del prompt

        if output.strip() == "":
            return output, True, -1.0

        # Estimate reading time
        words = output.split()
        word_count = len(words)
        reading_time = word_count / self._DEFAULT_READING_SPEED

        if reading_time <= self.max_time:
            return output, True, -1.0

        if not self.truncate:
            return output, False, 1.0

        # Calculate maximum words to keep
        max_words = int(self.max_time * self._DEFAULT_READING_SPEED)
        truncated = " ".join(words[:max_words])

        # Return False to indicate output was modified
        return truncated, False, 1.0


class NativeTokenLimitScanner:
    """KAITO-owned scanner to enforce token count limits on output.

    Uses tiktoken for accurate token counting with configurable tokenizer.
    When output exceeds the limit, optionally truncates it to fit.
    """

    def __init__(
        self,
        limit: int,
        encoding_name: str = "cl100k_base",
        model_name: str | None = None,
    ) -> None:
        self.limit = limit
        self.encoding_name = encoding_name
        self.model_name = model_name
        self._tokenizer = None

    def _get_tokenizer(self):
        """Lazily load tokenizer to avoid import overhead."""
        if self._tokenizer is None:
            try:
                import tiktoken

                if self.model_name:
                    self._tokenizer = tiktoken.encoding_for_model(self.model_name)
                else:
                    self._tokenizer = tiktoken.get_encoding(self.encoding_name)
            except ImportError:
                raise ImportError(
                    "tiktoken is required for NativeTokenLimitScanner. "
                    "Install it with: pip install tiktoken"
                )
        return self._tokenizer

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        """Scan output for token count limit.

        Args:
            prompt: The input prompt (unused, kept for interface compatibility).
            output: The text to scan.

        Returns:
            (output, valid, score):
                output: Original output, or truncated to fit within limit.
                valid: True if token count <= limit, False if truncated.
                score: Conformant to KAITO scanner contract: -1.0 when valid,
                    1.0 when invalid.
        """
        del prompt

        if output.strip() == "":
            return output, True, -1.0

        tokenizer = self._get_tokenizer()
        tokens = tokenizer.encode(output)
        token_count = len(tokens)

        if token_count <= self.limit:
            return output, True, -1.0

        # Truncate to fit within limit
        truncated_tokens = tokens[: self.limit]
        truncated_output = tokenizer.decode(truncated_tokens)

        return truncated_output, False, 1.0
