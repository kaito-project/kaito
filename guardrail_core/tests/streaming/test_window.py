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

import asyncio
import subprocess
import sys

import pytest
from guardrail_core.streaming.window import (
    StreamingBufferWindow,
    WindowScanError,
    WindowScanResult,
)


class AllowScanner:
    def scan(
        self, text: str, *, flush: bool = False, preceding_char: str = ""
    ) -> WindowScanResult:
        del flush, preceding_char
        return WindowScanResult()


class BadSubstringScanner:
    def __init__(self, substring: str = "bad") -> None:
        self.substring = substring
        self.scanned_texts: list[str] = []
        self.flush_values: list[bool] = []
        self.preceding_chars: list[str] = []

    def scan(
        self, text: str, *, flush: bool = False, preceding_char: str = ""
    ) -> WindowScanResult:
        self.scanned_texts.append(text)
        self.flush_values.append(flush)
        self.preceding_chars.append(preceding_char)
        if self.substring in text:
            return WindowScanResult(blocked=True)
        return WindowScanResult()


class InvisibleTextRedactScanner:
    def scan(
        self, text: str, *, flush: bool = False, preceding_char: str = ""
    ) -> WindowScanResult:
        del flush, preceding_char
        sanitized_text = text.replace("\u200b", "").replace("\u200c", "")
        if sanitized_text == text:
            return WindowScanResult()
        return WindowScanResult(sanitized_text=sanitized_text)


def test_safe_text_emits_as_single_confirmed_chunk():
    window = StreamingBufferWindow(AllowScanner(), holdback_len=0)

    result = window.feed("abcdefgh")
    flush_result = window.flush()

    assert result.chunks == ("abcdefgh",)
    assert result.blocked is False
    assert flush_result.chunks == ()


def test_redaction_replaces_pending_text_before_emission():
    window = StreamingBufferWindow(InvisibleTextRedactScanner(), holdback_len=0)

    result = window.feed("hello\u200bworld")

    assert result.chunks == ("helloworld",)


def test_redaction_recalculates_emit_length_after_text_shrinks():
    window = StreamingBufferWindow(InvisibleTextRedactScanner(), holdback_len=5)

    result = window.feed("abc\u200b\u200cdef")
    flush_result = window.flush()

    assert result.chunks == ("a",)
    assert flush_result.chunks == ("bcdef",)


def test_redaction_can_remove_all_text_during_flush():
    window = StreamingBufferWindow(InvisibleTextRedactScanner(), holdback_len=5)

    result = window.feed("\u200b\u200c")
    flush_result = window.flush()

    assert result.chunks == ()
    assert flush_result.chunks == ()


@pytest.mark.parametrize(
    ("kwargs", "message"),
    (({"holdback_len": -1}, "holdback"),),
)
def test_constructor_rejects_invalid_window_settings(kwargs, message):
    with pytest.raises(ValueError, match=message):
        StreamingBufferWindow(AllowScanner(), **kwargs)


@pytest.mark.parametrize("holdback_len", [0.0, 1.5, -1.5, "3", None, True, False])
def test_constructor_rejects_non_integer_holdback(holdback_len):
    with pytest.raises(TypeError, match="holdback_len must be an integer"):
        StreamingBufferWindow(AllowScanner(), holdback_len=holdback_len)


def test_holdback_tail_is_retained_and_not_emitted():
    scanner = BadSubstringScanner()
    window = StreamingBufferWindow(scanner, holdback_len=3)

    result = window.feed("abcdef")
    flush_result = window.flush()

    assert result.chunks == ("abc",)
    assert flush_result.chunks == ("def",)
    assert scanner.scanned_texts == ["abcdef", "def"]
    assert scanner.flush_values == [False, True]
    assert scanner.preceding_chars == ["", "c"]


def test_blocked_substring_crossing_holdback_boundary_is_detected():
    window = StreamingBufferWindow(BadSubstringScanner(), holdback_len=1)

    result = window.feed("safe bad")

    assert result.blocked is True
    assert result.chunks == ()
    assert window.blocked is True


def test_split_bad_substring_is_detected_before_tail_emits():
    window = StreamingBufferWindow(BadSubstringScanner(), holdback_len=2)

    first_result = window.feed("safe b")
    second_result = window.feed("ad text")

    assert first_result.chunks == ("safe",)
    assert window.blocked is True
    assert second_result.blocked is True
    assert second_result.chunks == ()


def test_final_flush_scans_and_emits_remaining_text():
    scanner = BadSubstringScanner()
    window = StreamingBufferWindow(scanner, holdback_len=5)

    result = window.feed("tail")
    flush_result = window.flush()

    assert result.chunks == ()
    assert scanner.scanned_texts == ["tail"]
    assert flush_result.chunks == ("tail",)
    assert window.flush().chunks == ()


def test_blocked_decision_stops_downstream_emission():
    window = StreamingBufferWindow(BadSubstringScanner(), holdback_len=0)

    blocked_result = window.feed("bad content")
    later_result = window.feed(" safe content")
    flush_result = window.flush()

    assert blocked_result.blocked is True
    assert blocked_result.chunks == ()
    assert later_result.blocked is True
    assert later_result.chunks == ()
    assert flush_result.blocked is True
    assert flush_result.chunks == ()


def test_blocked_decision_clears_pending_buffer():
    window = StreamingBufferWindow(BadSubstringScanner(), holdback_len=2)

    result = window.feed("safe bad content")

    assert result.blocked is True
    assert result.chunks == ()
    assert window._pending_buffer == ""


def test_holdback_larger_than_text_defers_scanning_until_flush():
    scanner = BadSubstringScanner()
    window = StreamingBufferWindow(scanner, holdback_len=100)

    assert window.feed("short").chunks == ()
    assert window.feed("").chunks == ()
    assert scanner.scanned_texts == []
    assert window.flush().chunks == ("short",)
    assert scanner.scanned_texts == ["short"]
    assert scanner.flush_values == [True]


def test_match_across_three_feeds_is_not_released():
    scanner = BadSubstringScanner("unsafe")
    window = StreamingBufferWindow(scanner, holdback_len=5)

    first = window.feed("safe un")
    second = window.feed("sa")
    third = window.feed("fe tail")

    assert "".join(first.chunks + second.chunks) == "safe"
    assert third.blocked
    assert third.chunks == ()
    assert window.flush().chunks == ()


@pytest.mark.parametrize("replacement", ["[REDACTED]", ""])
def test_redaction_crosses_release_boundary_and_recalculates_lengths(replacement):
    class RedactScanner:
        def scan(
            self, text: str, *, flush: bool = False, preceding_char: str = ""
        ) -> WindowScanResult:
            sanitized = text.replace("bad", replacement)
            return WindowScanResult(
                sanitized_text=sanitized if sanitized != text else None
            )

    window = StreamingBufferWindow(RedactScanner(), holdback_len=2)
    emitted = []
    for text in ["safe b", "a", "d end"]:
        emitted.extend(window.feed(text).chunks)
    emitted.extend(window.flush().chunks)

    assert "".join(emitted) == f"safe {replacement} end"
    assert window.redacted
    assert not window.blocked


def test_flush_detects_match_in_unreleased_tail():
    scanner = BadSubstringScanner()
    window = StreamingBufferWindow(scanner, holdback_len=100)

    assert window.feed("bad").chunks == ()
    assert scanner.scanned_texts == []
    result = window.flush()

    assert result.blocked
    assert result.chunks == ()
    assert scanner.flush_values == [True]
    assert window.feed("safe").chunks == ()
    assert window.flush().blocked
    assert scanner.scanned_texts == ["bad"]


def test_flush_supplies_right_boundary_and_last_released_character():
    calls = []

    class WordScanner:
        def scan(
            self, text: str, *, flush: bool = False, preceding_char: str = ""
        ) -> WindowScanResult:
            calls.append((text, flush, preceding_char))
            return WindowScanResult(
                blocked=flush and preceding_char == " " and text == "bad"
            )

    window = StreamingBufferWindow(WordScanner(), holdback_len=3)

    assert window.feed("safe bad").chunks == ("safe ",)
    assert window.flush().blocked
    assert calls == [("safe bad", False, ""), ("bad", True, " ")]


def test_holdback_counts_chinese_characters_not_utf8_bytes():
    window = StreamingBufferWindow(BadSubstringScanner("\u79d8\u5bc6"), holdback_len=1)

    result = window.feed("\u4f60\u597d\u79d8")
    assert result.chunks == ("\u4f60\u597d",)
    result = window.feed("\u5bc6")
    assert result.blocked
    assert result.chunks == ()


def test_preceding_char_is_one_unicode_character_after_each_release():
    scanner = BadSubstringScanner()
    window = StreamingBufferWindow(scanner, holdback_len=2)

    assert window.feed("\u4f60\u597dx").chunks == ("\u4f60",)
    assert window.feed("y").chunks == ("\u597d",)
    assert window.flush().chunks == ("xy",)
    assert scanner.preceding_chars == ["", "\u4f60", "\u597d"]


def test_holdback_uses_code_points_not_graphemes():
    window = StreamingBufferWindow(AllowScanner(), holdback_len=1)

    assert window.feed("e\u0301\U0001f642").chunks == ("e\u0301",)
    assert window.flush().chunks == ("\U0001f642",)


@pytest.mark.parametrize("scan_on_flush", [False, True])
@pytest.mark.parametrize("error_type", [RuntimeError, ValueError, OSError])
def test_scanner_failure_is_reported_and_permanently_discards_pending_text(
    scan_on_flush, error_type
):
    original = error_type("scanner failure")
    calls = []

    class FailingScanner:
        def scan(
            self, text: str, *, flush: bool = False, preceding_char: str = ""
        ) -> WindowScanResult:
            calls.append((text, flush, preceding_char))
            raise original

    window = StreamingBufferWindow(
        FailingScanner(), holdback_len=100 if scan_on_flush else 0
    )
    with pytest.raises(WindowScanError) as caught:
        if scan_on_flush:
            assert window.feed("unscanned").chunks == ()
            window.flush()
        else:
            window.feed("unscanned")

    assert caught.value.__cause__ is original
    assert calls == [("unscanned", scan_on_flush, "")]
    assert window.blocked
    assert not window.redacted
    assert window._pending_buffer == ""
    assert window.feed("later").blocked
    assert window.feed("later").chunks == ()
    assert window.flush().blocked
    assert window.flush().chunks == ()
    assert len(calls) == 1


def test_scanner_cancellation_is_not_converted_into_a_scan_error():
    class CancelledScanner:
        def scan(
            self, text: str, *, flush: bool = False, preceding_char: str = ""
        ) -> WindowScanResult:
            raise asyncio.CancelledError()

    window = StreamingBufferWindow(CancelledScanner(), holdback_len=0)

    with pytest.raises(asyncio.CancelledError):
        window.feed("text")


def test_shared_window_import_does_not_load_scanner_or_framework_dependencies():
    subprocess.run(
        [
            sys.executable,
            "-I",
            "-c",
            "import guardrail_core.streaming.window; import sys; "
            "assert not {'fastapi', 'ragengine', 'envoy', 'detect_secrets', "
            "'tiktoken', 'regex', 'json_repair'} & sys.modules.keys()",
        ],
        check=True,
        capture_output=True,
        text=True,
    )
