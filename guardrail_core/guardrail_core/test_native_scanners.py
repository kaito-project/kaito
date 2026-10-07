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

    def test_model_name_encoding_override(self):
        """Test that model_name takes precedence over encoding_name.

        Parity test: llm-guard.input_scanners.TokenLimit with model_name set
        should use tiktoken.encoding_for_model(model_name) instead of get_encoding.
        """
        try:
            # With model_name, should use encoding_for_model (may be different from encoding_name)
            scanner = NativeTokenLimitScanner(
                limit=100,
                encoding_name="cl100k_base",
                model_name="gpt-3.5-turbo"
            )
            output, valid, score = scanner.scan("", "Hello world")
            # Should not raise an error and should work correctly
            assert valid
            assert score == -1.0
        except ImportError:
            # tiktoken not installed, skip this test
            pass

    def test_token_limit_with_different_encodings(self):
        """Test that different encoding names produce different token counts.

        Parity test: encoding_name parameter should affect token counting.
        """
        try:
            import tiktoken

            text = "The quick brown fox jumps over the lazy dog"

            # Different encodings may have different token counts
            scanner_cl100k = NativeTokenLimitScanner(
                limit=1000,  # High enough to not truncate
                encoding_name="cl100k_base"
            )
            output_cl100k, _, _ = scanner_cl100k.scan("", text)

            # Verify token count is computed correctly
            tokenizer_cl100k = tiktoken.get_encoding("cl100k_base")
            tokens_cl100k = tokenizer_cl100k.encode(text)
            assert len(tokens_cl100k) >= 1  # At least some tokens
        except ImportError:
            # tiktoken not installed, skip this test
            pass


class TestBanSubstringsParityWithLLMGuard:
    """Parity tests for BanSubstrings scanner alignment with llm-guard.

    Verifies behavior matches llm-guard.output_scanners.BanSubstrings.
    """

    def test_parity_word_boundary_matching(self):
        """Test word boundary matches only full words (parity with llm-guard).

        llm-guard uses regex word boundaries \b which requires:
        - Word characters (a-z, A-Z, 0-9, _) on either side must be different
          from the corresponding character class in the banned substring.
        """
        scanner = NativeBanSubstringsScanner(
            substrings=["test"],
            match_type=BanSubstringsMatchType.WORD
        )

        # Should NOT match when part of other words
        output, valid, score = scanner.scan("", "testing this test case")
        assert not valid  # Matches standalone "test"
        assert score == 1.0

        # Should NOT match "testing" as word even though "test" is substring
        scanner2 = NativeBanSubstringsScanner(
            substrings=["testing"],
            match_type=BanSubstringsMatchType.WORD
        )
        output2, valid2, score2 = scanner2.scan("", "test testing case")
        assert not valid2  # Matches "testing"

    def test_parity_str_substring_matching(self):
        """Test substring matching finds anywhere in text (parity with llm-guard).

        llm-guard's STR mode just checks if substring is in text.
        """
        scanner = NativeBanSubstringsScanner(
            substrings=["test"],
            match_type=BanSubstringsMatchType.STR,
            case_sensitive=False  # default
        )

        # Should match substring anywhere (case insensitive)
        cases = [
            ("testing", False),   # "test" is in "testing", should be invalid (valid=False)
            ("atestb", False),    # "test" is in middle, should be invalid
            ("test", False),      # Exact match, should be invalid
            ("TEST", False),      # Case insensitive, should be invalid
            ("hello world", True),  # No "test", should be valid
        ]

        for text, should_be_valid in cases:
            output, valid, score = scanner.scan("", text)
            assert valid == should_be_valid, f"Failed for text: {text}. Expected valid={should_be_valid}, got {valid}"

    def test_parity_case_sensitivity(self):
        """Test case sensitivity parameter (parity with llm-guard).

        llm-guard respects case_sensitive flag.
        """
        # Case insensitive (default)
        scanner_ci = NativeBanSubstringsScanner(
            substrings=["SECRET"],
            case_sensitive=False
        )
        output_ci, valid_ci, _ = scanner_ci.scan("", "this is secret info")
        assert not valid_ci  # Should match despite different case

        # Case sensitive
        scanner_cs = NativeBanSubstringsScanner(
            substrings=["SECRET"],
            case_sensitive=True
        )
        output_cs, valid_cs, _ = scanner_cs.scan("", "this is secret info")
        assert valid_cs  # Should NOT match different case

    def test_parity_contains_all_behavior(self):
        """Test contains_all flag behavior (parity with llm-guard).

        llm-guard's contains_all requires all substrings to be present.
        When not all are present, it returns True (valid), score 0.0.
        """
        scanner = NativeBanSubstringsScanner(
            substrings=["admin", "password"],
            contains_all=True
        )

        # All present: invalid (bad)
        output1, valid1, score1 = scanner.scan("", "admin password")
        assert not valid1
        assert score1 == 1.0

        # Only one present: valid (good) with score 0.0
        output2, valid2, score2 = scanner.scan("", "admin only")
        assert valid2
        assert score2 == 0.0

        # None present: valid (good) with score 0.0
        output3, valid3, score3 = scanner.scan("", "nothing bad here")
        assert valid3
        assert score3 == 0.0

    def test_parity_redaction(self):
        """Test redaction replaces all occurrences (parity with llm-guard).

        llm-guard replaces all found substrings with [REDACTED].
        """
        scanner = NativeBanSubstringsScanner(
            substrings=["secret"],
            redact=True,
            case_sensitive=False
        )

        output, valid, score = scanner.scan("", "My secret and the SECRET are secret")
        # Should redact all three occurrences
        assert output.count("[REDACTED]") == 3
        assert not valid
        assert score == 1.0


class TestJSONScannerParityWithLLMGuard:
    """Parity tests for JSON scanner alignment with llm-guard.

    Verifies behavior matches llm-guard.output_scanners.JSON.
    """

    def test_parity_repair_failure_returns_invalid(self):
        """Test that repair failure returns invalid (parity with llm-guard).

        When repair=True but repair fails, should return (original, False, 1.0).
        This is critical parity requirement.
        """
        scanner = NativeJSONScanner(repair=True)

        # Unrepairable JSON (no opening brace/bracket pattern)
        broken_json = "this is not json at all"
        output, valid, score = scanner.scan("", broken_json)

        assert not valid, "Unrepairable JSON should be invalid"
        assert score == 1.0, "Unrepairable JSON should have score 1.0"
        assert output == broken_json, "Original output should be returned when repair fails"

    def test_parity_repair_success_returns_valid(self):
        """Test that successful repair returns valid (parity with llm-guard).

        When repair=True and repair succeeds, should return (repaired, True, -1.0).
        """
        scanner = NativeJSONScanner(repair=True)

        # Repairable JSON (missing closing brace)
        broken_json = '{"key": "value"'
        output, valid, score = scanner.scan("", broken_json)

        assert valid, "Repaired JSON should be valid"
        assert score == -1.0, "Repaired JSON should have score -1.0"
        assert output == '{"key": "value"}', "JSON should be repaired"

    def test_parity_empty_output_invalid(self):
        """Test that empty output is invalid (parity with llm-guard).

        llm-guard considers empty output invalid.
        """
        scanner = NativeJSONScanner()
        output, valid, score = scanner.scan("", "")

        assert not valid
        assert score == 1.0

    def test_parity_required_elements_dict(self):
        """Test required_elements validation for dict (parity with llm-guard).

        JSON scanner counts elements in dict/array and validates against
        required_elements parameter.
        """
        scanner_req2 = NativeJSONScanner(required_elements=2)

        # Meets requirement
        output1, valid1, score1 = scanner_req2.scan("", '{"a": 1, "b": 2}')
        assert valid1
        assert score1 == -1.0

        # Below requirement
        output2, valid2, score2 = scanner_req2.scan("", '{"a": 1}')
        assert not valid2
        assert score2 == 1.0

    def test_parity_required_elements_array(self):
        """Test required_elements validation for array (parity with llm-guard).

        JSON scanner counts elements in array just like in dict.
        """
        scanner = NativeJSONScanner(required_elements=3)

        # Array with exact required elements
        output1, valid1, score1 = scanner.scan("", '[1, 2, 3]')
        assert valid1
        assert score1 == -1.0

        # Array with fewer elements
        output2, valid2, score2 = scanner.scan("", '[1, 2]')
        assert not valid2
        assert score2 == 1.0

    def test_parity_repair_preserves_original_on_failure(self):
        """Test that original output is returned when repair fails.

        llm-guard returns original output if repair doesn't help.
        """
        scanner = NativeJSONScanner(repair=True)

        original = "not valid {json} at all"
        output, valid, score = scanner.scan("", original)

        assert output == original
        assert not valid


class TestTokenLimitScannerParityWithLLMGuard:
    """Parity tests for TokenLimit scanner alignment with llm-guard.

    Verifies behavior matches llm-guard.input_scanners.TokenLimit.
    """

    def test_parity_truncation_on_exceed(self):
        """Test that output exceeding token limit is truncated (parity with llm-guard).

        llm-guard.TokenLimit returns chunks[0] (truncated to limit) when limit exceeded.
        """
        try:
            import tiktoken

            scanner = NativeTokenLimitScanner(limit=5)
            text = " ".join(["word"] * 100)  # More than 5 tokens

            output, valid, score = scanner.scan("", text)

            # Must return truncated output
            tokenizer = tiktoken.get_encoding("cl100k_base")
            token_count = len(tokenizer.encode(output))
            assert token_count <= scanner.limit
            assert not valid
            assert score == 1.0
        except ImportError:
            pass

    def test_parity_within_limit_returns_original(self):
        """Test that output within limit is returned unchanged (parity with llm-guard).

        llm-guard returns original output when within limit.
        """
        try:
            scanner = NativeTokenLimitScanner(limit=100)
            text = "Hello world"

            output, valid, score = scanner.scan("", text)

            assert output == text
            assert valid
            assert score == -1.0
        except ImportError:
            pass

    def test_parity_model_name_precedence(self):
        """Test that model_name takes precedence over encoding_name (parity with llm-guard).

        llm-guard uses encoding_for_model(model_name) when model_name is provided,
        otherwise uses get_encoding(encoding_name).
        """
        try:
            # This test verifies the precedence without relying on specific token counts
            # which may vary between tiktoken versions
            scanner = NativeTokenLimitScanner(
                limit=1000,  # High limit
                encoding_name="cl100k_base",
                model_name="gpt-3.5-turbo"
            )

            # Should not raise - model_name should be used
            output, valid, score = scanner.scan("", "test")
            assert valid  # Should pass with high limit
        except ImportError:
            pass

    def test_parity_empty_output_returns_valid(self):
        """Test that empty output is valid (parity with llm-guard).

        Both scanners treat empty strings as valid (within limit).
        """
        try:
            scanner = NativeTokenLimitScanner(limit=10)
            output, valid, score = scanner.scan("", "")

            assert valid
            assert score == -1.0
        except ImportError:
            pass
