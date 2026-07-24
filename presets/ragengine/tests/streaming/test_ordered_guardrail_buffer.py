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

from ragengine.streaming import ordered_guardrail_buffer as buffer_module  # noqa: E402
from ragengine.streaming.buffer_window import (  # noqa: E402
    WindowEmitResult,
    WindowScanResult,
)
from ragengine.streaming.openai import ParsedOpenAIChoice  # noqa: E402
from ragengine.streaming.ordered_guardrail_buffer import (  # noqa: E402
    OrderedGuardrailBuffer,
    OrderedGuardrailBufferOverflowError,
)


class FakeScanner:
    def __init__(self, *, blocked_substring: str | None = None) -> None:
        self.blocked_substring = blocked_substring
        self.scanned_texts: list[str] = []

    def scan(self, text: str) -> WindowScanResult:
        self.scanned_texts.append(text)
        return WindowScanResult(
            blocked=self.blocked_substring is not None
            and self.blocked_substring in text
        )


class FakeScannerFactory:
    def __init__(self, *, blocked_substring: str | None = None) -> None:
        self.blocked_substring = blocked_substring
        self.scanners: dict[int, FakeScanner] = {}

    def __call__(self, choice_index: int) -> FakeScanner:
        scanner = FakeScanner(blocked_substring=self.blocked_substring)
        self.scanners[choice_index] = scanner
        return scanner


class OverReleasingWindow:
    def __init__(self, scanner: FakeScanner, *, holdback_len: int) -> None:
        self.scanner = scanner
        self.holdback_len = holdback_len

    def feed(self, text: str) -> WindowEmitResult:
        return WindowEmitResult(chunks=(text + "extra",))

    def flush(self) -> WindowEmitResult:
        return WindowEmitResult(chunks=())


class UnderReleasingFlushWindow:
    def __init__(self, scanner: FakeScanner, *, holdback_len: int) -> None:
        self.scanner = scanner
        self.holdback_len = holdback_len

    def feed(self, text: str) -> WindowEmitResult:
        return WindowEmitResult(chunks=())

    def flush(self) -> WindowEmitResult:
        return WindowEmitResult(chunks=())


def _choice(
    choice_index: int,
    *,
    content: str | None = None,
    finish_reason: str | None = None,
) -> ParsedOpenAIChoice:
    return ParsedOpenAIChoice(
        choice_index=choice_index,
        content=content,
        finish_reason=finish_reason,
    )


def test_releases_original_events_in_order_when_choice_offsets_are_safe():
    factory = FakeScannerFactory()
    buffer = OrderedGuardrailBuffer(
        factory,
        holdback_len=2,
        max_pending_events=10,
    )

    assert buffer.feed("choice-0-a", (_choice(0, content="abc"),)).ready_events == ()
    assert buffer.feed("choice-1-a", (_choice(1, content="XYZ"),)).ready_events == ()

    result = buffer.feed("choice-0-b", (_choice(0, content="de"),))

    assert result.ready_events == ("choice-0-a",)

    finish_result = buffer.feed("finish-1", (_choice(1, finish_reason="stop"),))

    assert finish_result.ready_events == ("choice-1-a",)

    assert buffer.feed(
        "finish-0", (_choice(0, finish_reason="stop"),)
    ).ready_events == (
        "choice-0-b",
        "finish-1",
        "finish-0",
    )


def test_scans_text_crossing_chunk_boundaries_per_choice():
    factory = FakeScannerFactory(blocked_substring="bad")
    buffer = OrderedGuardrailBuffer(
        factory,
        holdback_len=2,
        max_pending_events=10,
    )

    assert buffer.feed("first", (_choice(0, content="ba"),)).blocked is False

    result = buffer.feed("second", (_choice(0, content="d payload"),))

    assert result.blocked is True
    assert result.ready_events == ()
    assert factory.scanners[0].scanned_texts == ["bad payload"]


def test_does_not_scan_across_choice_boundaries():
    factory = FakeScannerFactory(blocked_substring="bad")
    buffer = OrderedGuardrailBuffer(
        factory,
        holdback_len=2,
        max_pending_events=10,
    )

    assert buffer.feed("choice-0", (_choice(0, content="ba"),)).blocked is False
    assert buffer.feed("choice-1", (_choice(1, content="d"),)).blocked is False

    result = buffer.finish_stream("done")

    assert result.ready_events == ("choice-0", "choice-1", "done")
    assert factory.scanners[0].scanned_texts == ["ba"]
    assert factory.scanners[1].scanned_texts == ["d"]


def test_multi_choice_event_waits_for_all_choice_offsets():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=2,
        max_pending_events=10,
    )

    assert (
        buffer.feed(
            "both",
            (_choice(0, content="abc"), _choice(1, content="x")),
        ).ready_events
        == ()
    )
    assert (
        buffer.feed("finish-0", (_choice(0, finish_reason="stop"),)).ready_events == ()
    )

    assert buffer.feed(
        "finish-1", (_choice(1, finish_reason="stop"),)
    ).ready_events == (
        "both",
        "finish-0",
        "finish-1",
    )


def test_dangerous_holdback_blocks_on_finish_choice():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(blocked_substring="bad"),
        holdback_len=3,
        max_pending_events=10,
    )

    assert buffer.feed("pending", (_choice(0, content="bad"),)).blocked is False

    result = buffer.feed("finish", (_choice(0, finish_reason="stop"),))

    assert result.blocked is True
    assert result.ready_events == ()


def test_dangerous_holdback_blocks_on_finish_stream():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(blocked_substring="bad"),
        holdback_len=3,
        max_pending_events=10,
    )

    assert buffer.feed("pending", (_choice(0, content="bad"),)).blocked is False

    result = buffer.finish_stream("done")

    assert result.blocked is True
    assert result.ready_events == ()


def test_blocked_buffer_remains_blocked():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(blocked_substring="bad"),
        holdback_len=3,
        max_pending_events=10,
    )

    assert buffer.feed("pending", (_choice(0, content="ok"),)).ready_events == ()

    blocked_result = buffer.feed("blocking", (_choice(0, content="bad"),))

    assert blocked_result.blocked is True
    assert blocked_result.ready_events == ()

    feed_result = buffer.feed("after-block", (_choice(0, content="safe"),))
    finish_result = buffer.finish_stream("done")

    assert feed_result.blocked is True
    assert feed_result.ready_events == ()
    assert finish_result.blocked is True
    assert finish_result.ready_events == ()


def test_metadata_event_waits_behind_unreleased_content_event():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=5,
        max_pending_events=10,
    )

    assert buffer.feed("content", (_choice(0, content="abc"),)).ready_events == ()
    assert buffer.feed("metadata-only", ()).ready_events == ()

    assert buffer.feed("finish", (_choice(0, finish_reason="stop"),)).ready_events == (
        "content",
        "metadata-only",
        "finish",
    )


def test_feed_can_finish_choice_and_release_the_original_finish_event():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=5,
        max_pending_events=10,
    )

    assert buffer.feed("content", (_choice(0, content="tail"),)).ready_events == ()

    result = buffer.feed("finish-choice", (_choice(0, finish_reason="stop"),))

    assert result.ready_events == ("content", "finish-choice")


def test_finish_stream_flushes_all_choices_before_releasing_stream_event():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=5,
        max_pending_events=10,
    )

    assert buffer.feed("choice-0", (_choice(0, content="abc"),)).ready_events == ()
    assert buffer.feed("choice-1", (_choice(1, content="xyz"),)).ready_events == ()

    result = buffer.finish_stream("done")

    assert result.ready_events == ("choice-0", "choice-1", "done")


def test_finish_stream_without_raw_flushes_pending_events_on_upstream_eof():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=5,
        max_pending_events=10,
    )

    assert buffer.feed("choice-0", (_choice(0, content="abc"),)).ready_events == ()
    assert buffer.feed("metadata-only", ()).ready_events == ()

    result = buffer.finish_stream()

    assert result.ready_events == ("choice-0", "metadata-only")


def test_finish_choice_is_idempotent(monkeypatch: pytest.MonkeyPatch):
    windows = []

    class CountingFlushWindow:
        def __init__(self, scanner: FakeScanner, *, holdback_len: int) -> None:
            self.scanner = scanner
            self.holdback_len = holdback_len
            self.flush_count = 0
            windows.append(self)

        def feed(self, text: str) -> WindowEmitResult:
            return WindowEmitResult(chunks=(text,))

        def flush(self) -> WindowEmitResult:
            self.flush_count += 1
            return WindowEmitResult(chunks=())

    monkeypatch.setattr(buffer_module, "StreamingBufferWindow", CountingFlushWindow)
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    assert buffer.feed(
        "finish-once",
        (_choice(0, content="done", finish_reason="stop"),),
    ).ready_events == ("finish-once",)
    assert len(windows) == 1
    assert windows[0].flush_count == 1

    assert buffer.feed(
        "finish-again", (_choice(0, finish_reason="stop"),)
    ).ready_events == ("finish-again",)
    assert windows[0].flush_count == 1


def test_queue_limit_rejects_more_pending_events():
    factory = FakeScannerFactory()
    buffer = OrderedGuardrailBuffer(
        factory,
        holdback_len=5,
        max_pending_events=1,
    )

    assert buffer.feed("first", (_choice(0, content="abc"),)).ready_events == ()

    with pytest.raises(OrderedGuardrailBufferOverflowError, match="max_pending"):
        buffer.feed("second", (_choice(0, content="def"),))

    assert factory.scanners[0].scanned_texts == []


def test_queue_limit_rejects_more_pending_bytes():
    factory = FakeScannerFactory()
    buffer = OrderedGuardrailBuffer(
        factory,
        holdback_len=5,
        max_pending_events=10,
        max_pending_bytes=len(b"first"),
    )

    assert buffer.feed("first", (_choice(0, content="abc"),)).ready_events == ()

    with pytest.raises(OrderedGuardrailBufferOverflowError, match="max_pending"):
        buffer.feed("second", (_choice(0, content="def"),))

    assert factory.scanners[0].scanned_texts == []


def test_byte_limit_allows_more_events_after_pending_events_release():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
        max_pending_bytes=len(b"content"),
    )

    assert buffer.feed("content", (_choice(0, content="abc"),)).ready_events == (
        "content",
    )

    assert buffer.feed("next", ()).ready_events == ("next",)


def test_constructor_rejects_invalid_queue_limit():
    with pytest.raises(ValueError, match="holdback_len"):
        OrderedGuardrailBuffer(
            FakeScannerFactory(),
            holdback_len=-1,
        )

    with pytest.raises(ValueError, match="max_pending_events"):
        OrderedGuardrailBuffer(
            FakeScannerFactory(),
            holdback_len=0,
            max_pending_events=0,
        )

    with pytest.raises(ValueError, match="max_pending_bytes"):
        OrderedGuardrailBuffer(
            FakeScannerFactory(),
            holdback_len=0,
            max_pending_bytes=0,
        )


@pytest.mark.parametrize("choice_index", [True, -1, "0"])
def test_content_choice_rejects_invalid_choice_index(choice_index: object):
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    with pytest.raises(ValueError, match="non-negative integer"):
        buffer.feed(
            "content",
            (ParsedOpenAIChoice(choice_index=choice_index, content="content"),),
        )


@pytest.mark.parametrize("choice_index", [True, -1, "0"])
def test_finished_choice_rejects_invalid_choice_index(choice_index: object):
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    with pytest.raises(ValueError, match="non-negative integer"):
        buffer.feed(
            "finish",
            (ParsedOpenAIChoice(choice_index=choice_index, finish_reason="stop"),),
        )


def test_feed_after_choice_finish_raises():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    assert buffer.feed(
        "finish",
        (_choice(0, content="done", finish_reason="stop"),),
    ).ready_events

    with pytest.raises(RuntimeError, match="already finished"):
        buffer.feed("after-finish", (_choice(0, content="again"),))


def test_released_offset_cannot_exceed_received_offset(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setattr(buffer_module, "StreamingBufferWindow", OverReleasingWindow)
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    with pytest.raises(RuntimeError, match="released offset exceeded received offset"):
        buffer.feed("content", (_choice(0, content="abc"),))


def test_finish_choice_requires_all_received_offsets_released(
    monkeypatch: pytest.MonkeyPatch,
):
    monkeypatch.setattr(
        buffer_module, "StreamingBufferWindow", UnderReleasingFlushWindow
    )
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    assert buffer.feed("content", (_choice(0, content="abc"),)).ready_events == ()

    with pytest.raises(RuntimeError, match="choice offsets do not match after flush"):
        buffer.feed("finish", (_choice(0, finish_reason="stop"),))


def test_feed_after_stream_finish_raises():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    assert buffer.finish_stream("done").ready_events == ("done",)

    with pytest.raises(RuntimeError, match="stream is finished"):
        buffer.feed("after-stream", (_choice(0, content="again"),))


def test_finish_stream_after_stream_finish_raises():
    buffer = OrderedGuardrailBuffer(
        FakeScannerFactory(),
        holdback_len=0,
        max_pending_events=10,
    )

    assert buffer.finish_stream().ready_events == ()

    with pytest.raises(RuntimeError, match="stream is already finished"):
        buffer.finish_stream()
