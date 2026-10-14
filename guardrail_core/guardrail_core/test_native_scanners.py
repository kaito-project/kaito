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

import pytest

from guardrail_core.native_scanners import (
    BanSubstringsMatchType,
    NativeBanSubstringsScanner,
    NativeInvisibleTextScanner,
    NativeJSONScanner,
    NativeReadingTimeScanner,
    NativeRegexScanner,
    NativeTokenLimitScanner,
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

    def test_contains_all_empty_output(self):
        """Test contains_all with empty output returns score=0.0."""
        scanner = NativeBanSubstringsScanner(
            substrings=["secret", "key"], contains_all=True
        )
        output, valid, score = scanner.scan("", "")
        # All substrings missing (contains_all requires all) → score 0.0
        assert valid
        assert score == 0.0


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

    def test_empty_output_block_list(self):
        """Test empty output in block-list mode."""
        scanner = NativeRegexScanner(patterns=[r"\d+"], is_blocked=True)
        output, valid, score = scanner.scan("", "")
        assert valid  # Block-list: no patterns matched = valid
        assert score == -1.0

    def test_empty_output_allow_list(self):
        """Test empty output in allow-list mode."""
        scanner = NativeRegexScanner(patterns=[r"\d+"], is_blocked=False)
        output, valid, score = scanner.scan("", "")
        assert not valid  # Allow-list: no patterns matched = invalid
        assert score == 1.0


class TestNativeInvisibleTextScanner:
    def test_normal_text_unchanged(self):
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "Hello World")
        assert valid
        assert score == -1.0
        assert output == "Hello World"

    @pytest.mark.parametrize("text", ["你好", "café"])
    def test_visible_unicode_uses_neutral_score(self, text):
        scanner = NativeInvisibleTextScanner()

        assert scanner.scan("", text) == (text, True, 0.0)

    def test_invisible_unicode_removed(self):
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "Hello\u200bWorld")
        assert not valid
        assert score == 1.0
        assert "​" not in output

    def test_newline_tab_preserved(self):
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "Hello\nWorld")
        assert valid
        assert "\n" in output
        output, valid, score = scanner.scan("", "Hello\tWorld")
        assert valid
        assert "\t" in output


class TestNativeJSONScanner:
    @pytest.mark.parametrize(
        ("output", "required_elements", "expected_valid"),
        [
            ("", 0, True),
            ('Here is the result:\n{"a": 1}', 1, True),
            ('first {"a": 1} second {"b": 2}', 2, True),
            ('{"a": 1, "b": 2}', 2, False),
            ("plain text", 0, True),
            ("plain text", 1, False),
            ('good {"a": 1} bad {"b": }', 1, False),
        ],
    )
    def test_matches_legacy_candidate_semantics(
        self, output, required_elements, expected_valid
    ):
        scanner = NativeJSONScanner(
            required_elements=required_elements,
            repair=False,
        )

        assert scanner.scan("", output) == (
            output,
            expected_valid,
            -1.0 if expected_valid else 1.0,
        )

    def test_repairs_embedded_json_candidate(self):
        scanner = NativeJSONScanner(required_elements=1, repair=True)

        assert scanner.scan("", 'result {"a": 1,}') == (
            'result {"a": 1}',
            True,
            -1.0,
        )


class TestNativeReadingTimeScanner:
    def test_within_limit(self):
        scanner = NativeReadingTimeScanner(max_time=1.0)
        text = " ".join(["word"] * 60)
        output, valid, score = scanner.scan("", text)
        assert valid
        assert score == -1.0

    def test_exceed_limit_no_truncate(self):
        scanner = NativeReadingTimeScanner(max_time=0.5)
        text = " ".join(["word"] * 300)
        output, valid, score = scanner.scan("", text)
        assert not valid
        assert score == 1.0

    def test_exceed_limit_with_truncate(self):
        scanner = NativeReadingTimeScanner(max_time=0.5, truncate=True)
        text = " ".join(["word"] * 300)
        output, valid, score = scanner.scan("", text)
        assert not valid
        assert score == 1.0
        assert len(output.split()) <= 110  # Allow margin


class TestNativeTokenLimitScanner:
    def test_within_limit(self):
        scanner = NativeTokenLimitScanner(limit=100)
        output, valid, score = scanner.scan("", "Hello world")
        assert valid
        assert score == -1.0

    def test_exceed_limit_with_truncate(self):
        scanner = NativeTokenLimitScanner(limit=5)
        text = " ".join(["word"] * 50)
        output, valid, score = scanner.scan("", text)
        assert not valid
        assert score == 1.0
        assert len(output.split()) < len(text.split())

    def test_equal_to_limit_is_invalid(self):
        scanner = NativeTokenLimitScanner(limit=2)

        assert scanner.scan("", "hello world") == ("hello world", False, 1.0)

    def test_model_name_precedence(self):
        scanner = NativeTokenLimitScanner(
            limit=100, encoding_name="cl100k_base", model_name="gpt-3.5-turbo"
        )
        output, valid, score = scanner.scan("", "Hello world")
        assert valid
        assert score == -1.0

    def test_empty_output(self):
        scanner = NativeTokenLimitScanner(limit=100)
        output, valid, score = scanner.scan("", "")
        assert valid
        assert score == -1.0
