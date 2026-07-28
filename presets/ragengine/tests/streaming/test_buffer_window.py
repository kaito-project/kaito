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

import os
import sys

import pytest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.streaming.buffer_window import (
    StreamingBufferWindow,
    WindowScanResult,
)  # noqa: E402


class AllowScanner:
    def scan(self, text: str) -> WindowScanResult:
        return WindowScanResult()


class BadSubstringScanner:
    def __init__(self, substring: str = "bad") -> None:
        self.substring = substring
        self.scanned_texts: list[str] = []

    def scan(self, text: str) -> WindowScanResult:
        self.scanned_texts.append(text)
        if self.substring in text:
            return WindowScanResult(blocked=True)
        return WindowScanResult()


def test_safe_text_emits_as_single_confirmed_chunk():
    window = StreamingBufferWindow(AllowScanner(), holdback_len=0)

    result = window.feed("abcdefgh")
    flush_result = window.flush()

    assert result.chunks == ("abcdefgh",)
    assert result.blocked is False
    assert flush_result.chunks == ()


@pytest.mark.parametrize(
    ("kwargs", "message"),
    (({"holdback_len": -1}, "holdback"),),
)
def test_constructor_rejects_invalid_window_settings(kwargs, message):
    with pytest.raises(ValueError, match=message):
        StreamingBufferWindow(AllowScanner(), **kwargs)


def test_holdback_tail_is_retained_and_not_emitted():
    scanner = BadSubstringScanner()
    window = StreamingBufferWindow(scanner, holdback_len=3)

    result = window.feed("abcdef")
    flush_result = window.flush()

    assert result.chunks == ("abc",)
    assert flush_result.chunks == ("def",)
    assert scanner.scanned_texts == ["abcdef", "def"]


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
