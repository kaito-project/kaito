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
    """Test native InvisibleText scanner."""

    def test_no_invisible_characters(self):
        """Test output with no invisible characters."""
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "Hello World")
        assert valid
        assert score == -1.0
        assert output == "Hello World"

    def test_zero_width_space_detected(self):
        """Test detection of zero-width space."""
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan(
            "", "Hello​World"
        )  # Contains zero-width space
        assert not valid
        assert score == 1.0
        assert "​" not in output  # Should be removed

    def test_zero_width_non_joiner(self):
        """Test detection of zero-width non-joiner."""
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "Hello‌World")  # Contains ZWNJ
        assert not valid
        assert "‌" not in output

    def test_control_characters(self):
        """Test detection of control characters."""
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "Hello\x00World")  # Null character
        assert not valid
        assert "\x00" not in output

    def test_empty_output(self):
        """Test empty output."""
        scanner = NativeInvisibleTextScanner()
        output, valid, score = scanner.scan("", "")
        assert valid
        assert score == -1.0


class TestNativeJSONScanner:
    """Test native JSON scanner."""

    def test_valid_json_object(self):
        """Test valid JSON object."""
        scanner = NativeJSONScanner()
        output, valid, score = scanner.scan("", '{"key": "value"}')
        assert valid
        assert score == -1.0

    def test_valid_json_array(self):
        """Test valid JSON array."""
        scanner = NativeJSONScanner()
        output, valid, score = scanner.scan("", "[1, 2, 3]")
        assert valid
        assert score == -1.0

    def test_invalid_json_no_repair(self):
        """Test invalid JSON without repair."""
        scanner = NativeJSONScanner(repair=False)
        output, valid, score = scanner.scan("", '{"key": "value"')
        assert not valid
        assert score == 1.0

    def test_invalid_json_with_repair(self):
        """Test invalid JSON with repair."""
        scanner = NativeJSONScanner(repair=True)
        output, valid, score = scanner.scan("", '{"key": "value"')
        assert valid  # Should be repaired
        assert score == -1.0

    def test_required_elements_met(self):
        """Test required_elements constraint is met."""
        scanner = NativeJSONScanner(required_elements=2)
        output, valid, score = scanner.scan("", '{"a": 1, "b": 2, "c": 3}')
        assert valid
        assert score == -1.0

    def test_required_elements_not_met(self):
        """Test required_elements constraint is not met."""
        scanner = NativeJSONScanner(required_elements=3)
        output, valid, score = scanner.scan("", '{"a": 1, "b": 2}')
        assert not valid
        assert score == 1.0

    def test_empty_output(self):
        """Test empty output."""
        scanner = NativeJSONScanner()
        output, valid, score = scanner.scan("", "")
        assert not valid
        assert score == 1.0


class TestNativeReadingTimeScanner:
    """Test native ReadingTime scanner."""

    def test_short_text_valid(self):
        """Test short text within time limit."""
        scanner = NativeReadingTimeScanner(max_time=1.0)
        # ~60 words = ~18 seconds = well under 1 minute
        text = " ".join(["word"] * 60)
        output, valid, score = scanner.scan("", text)
        assert valid
        assert score == -1.0

    def test_long_text_invalid(self):
        """Test long text exceeding time limit."""
        scanner = NativeReadingTimeScanner(max_time=0.5)  # 30 seconds max
        # ~300 words = ~1.5 minutes = exceeds 30 seconds
        text = " ".join(["word"] * 300)
        output, valid, score = scanner.scan("", text)
        assert not valid
        assert score == 1.0

    def test_long_text_with_truncation(self):
        """Test long text is truncated."""
        scanner = NativeReadingTimeScanner(max_time=0.5, truncate=True)
        # ~300 words
        text = " ".join(["word"] * 300)
        output, valid, score = scanner.scan("", text)
        assert not valid  # Should return False when truncated
        assert score == 1.0
        # Should be truncated to ~100 words (0.5 * 200 wpm)
        assert len(output.split()) <= 110  # Allow small margin

    def test_empty_output(self):
        """Test empty output."""
        scanner = NativeReadingTimeScanner(max_time=1.0)
        output, valid, score = scanner.scan("", "")
        assert valid
        assert score == -1.0


class TestNativeTokenLimitScanner:
    """Test native TokenLimit scanner."""

    def test_token_count_within_limit(self):
        """Test token count within limit."""
        try:
            scanner = NativeTokenLimitScanner(limit=100)
            output, valid, score = scanner.scan("", "Hello world")
            assert valid
            assert score == -1.0
        except ImportError:
            # tiktoken not installed, skip this test
            pass

    def test_token_count_exceeds_limit(self):
        """Test token count exceeds limit."""
        try:
            scanner = NativeTokenLimitScanner(limit=5)
            # Generate text with more tokens than limit
            text = " ".join(["word"] * 50)
            output, valid, score = scanner.scan("", text)
            assert not valid
            assert score == 1.0
            # Output should be truncated
            assert len(output.split()) < len(text.split())
        except ImportError:
            # tiktoken not installed, skip this test
            pass

    def test_truncation_preserves_validity(self):
        """Test that truncated output is within token limit."""
        try:
            scanner = NativeTokenLimitScanner(limit=10)
            # Generate text with more tokens than limit
            text = " ".join(["word"] * 100)
            output, valid, score = scanner.scan("", text)

            # The returned output should now be within the limit
            import tiktoken

            tokenizer = tiktoken.get_encoding("cl100k_base")
            tokens = tokenizer.encode(output)
            assert len(tokens) <= scanner.limit
        except ImportError:
            # tiktoken not installed, skip this test
            pass

    def test_empty_output(self):
        """Test empty output."""
        try:
            scanner = NativeTokenLimitScanner(limit=100)
            output, valid, score = scanner.scan("", "")
            assert valid
            assert score == -1.0
        except ImportError:
            # tiktoken not installed, skip this test
            pass
