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

"""Unit tests for native KAITO guardrails scanners."""

from .scanner_schemas import (
    BanSubstringsMatchType,
    NativeBanSubstringsScanner,
    NativeRegexScanner,
    RegexMatchType,
)


class TestNativeBanSubstringsScanner:
    """Test native BanSubstrings scanner."""

    def test_case_sensitive_exact(self):
        """Test exact case-sensitive matching."""
        scanner = NativeBanSubstringsScanner(substrings=["secret"], case_sensitive=True)
        output, valid, score = scanner.scan("", "This contains secret")
        assert not valid
        assert output == "This contains secret"

    def test_case_insensitive_detection(self):
        """Test case-insensitive detection."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret"], case_sensitive=False
        )
        output, valid, score = scanner.scan("", "This contains SECRET")
        assert not valid

    def test_case_insensitive_redaction(self):
        """Test case-insensitive redaction."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret"], case_sensitive=False, redact=True
        )
        output, valid, score = scanner.scan("", "This contains SECRET")
        assert output == "This contains [REDACTED]"
        assert not valid

    def test_word_match_type(self):
        """Test word boundary matching."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret"], match_type=BanSubstringsMatchType.WORD
        )
        output, valid, score = scanner.scan("", "This contains secrets")
        assert valid  # "secrets" is not "secret" as word

    def test_str_match_type(self):
        """Test substring matching."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret"], match_type=BanSubstringsMatchType.STR
        )
        output, valid, score = scanner.scan("", "This contains secrets")
        assert not valid  # "secret" is in "secrets"

    def test_multiple_substrings_any(self):
        """Test multiple substrings (any match fails)."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret", "password"], contains_all=False
        )
        output, valid, score = scanner.scan("", "This has secret")
        assert not valid

    def test_multiple_substrings_all(self):
        """Test multiple substrings (all required)."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret", "password"], contains_all=True
        )
        output, valid, score = scanner.scan("", "This has secret but not pass")
        assert valid  # Only one substring found, need all

    def test_empty_output(self):
        """Test empty output."""
        scanner = NativeBanSubstringsScanner(substrings=["secret"])
        output, valid, score = scanner.scan("", "")
        assert valid
        assert output == ""

    def test_redact_multiple_occurrences(self):
        """Test redacting multiple occurrences."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret"], redact=True, case_sensitive=False
        )
        output, valid, score = scanner.scan("", "secret and SECRET both secret")
        # All occurrences should be redacted
        assert output == "[REDACTED] and [REDACTED] both [REDACTED]"


class TestNativeRegexScanner:
    """Test native Regex scanner."""

    def test_simple_pattern_found(self):
        """Test simple pattern matching."""
        scanner = NativeRegexScanner(patterns=[r"\d{3}-\d{4}"])
        output, valid, score = scanner.scan("", "Call 123-4567 now")
        assert not valid
        assert score == 1.0

    def test_pattern_not_found(self):
        """Test pattern not found."""
        scanner = NativeRegexScanner(patterns=[r"\d{3}-\d{4}"])
        output, valid, score = scanner.scan("", "Call me tomorrow")
        assert valid
        assert score == -1.0

    def test_search_single_match(self):
        """Test SEARCH mode finds only first match."""
        scanner = NativeRegexScanner(
            patterns=[r"\d+"], match_type=RegexMatchType.SEARCH, redact=True
        )
        output, valid, score = scanner.scan("", "123 and 456 and 789")
        # SEARCH should only redact first match
        assert output == "[REDACTED] and 456 and 789"
        assert not valid

    def test_all_multiple_matches(self):
        """Test ALL mode finds all matches."""
        scanner = NativeRegexScanner(
            patterns=[r"\d+"], match_type=RegexMatchType.ALL, redact=True
        )
        output, valid, score = scanner.scan("", "123 and 456 and 789")
        # ALL should redact all matches
        assert output == "[REDACTED] and [REDACTED] and [REDACTED]"
        assert not valid

    def test_fullmatch_entire_string(self):
        """Test FULL_MATCH requires entire string match."""
        pattern = r"\d+"
        scanner = NativeRegexScanner(
            patterns=[pattern], match_type=RegexMatchType.FULL_MATCH
        )

        output, valid, score = scanner.scan("", "123")
        assert not valid  # entire string is "123"

        output, valid, score = scanner.scan("", "abc123def")
        assert valid  # entire string doesn't match \d+

    def test_is_blocked_false_allow_list_match(self):
        """Test is_blocked=False (allow-list) with match found."""
        scanner = NativeRegexScanner(patterns=[r"\d+"], is_blocked=False)
        output, valid, score = scanner.scan("", "has 123 number")
        assert valid  # Pattern matched in allow-list = valid
        assert score == -1.0  # Allow-list match score is -1.0

    def test_is_blocked_false_allow_list_no_match(self):
        """Test is_blocked=False (allow-list) with no match."""
        scanner = NativeRegexScanner(patterns=[r"\d+"], is_blocked=False)
        output, valid, score = scanner.scan("", "no numbers here")
        assert not valid  # No pattern match in allow-list = invalid
        assert score == 1.0  # Allow-list no-match score is 1.0

    def test_multiple_patterns_first_match_stops(self):
        """Test that first matching pattern stops iteration."""
        scanner = NativeRegexScanner(patterns=[r"\d+", r"secret"], redact=True)
        output, valid, score = scanner.scan("", "123 secret")
        # First pattern \d+ matches and triggers redaction, second never checked
        assert output == "[REDACTED] secret"
        assert not valid

    def test_empty_output(self):
        """Test empty output."""
        scanner = NativeRegexScanner(patterns=[r"\d+"])
        output, valid, score = scanner.scan("", "")
        assert valid
        assert score == -1.0
