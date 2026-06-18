# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.

from ragengine.streaming.buffering import (
    NoOpStreamScanner,
    StreamBuffer,
    StreamBufferConfig,
    StreamScanDecision,
)


class RecordingScanner:
    def __init__(self) -> None:
        self.scanned_texts: list[str] = []

    def inspect(self, text: str) -> StreamScanDecision:
        self.scanned_texts.append(text)
        return StreamScanDecision()


def test_stream_buffer_holds_back_suffix_until_flush():
    stream_buffer = StreamBuffer(
        config=StreamBufferConfig(holdback_chars=2),
        scanner=NoOpStreamScanner(),
    )

    assert stream_buffer.push_text("Hello") == ["Hel"]
    assert stream_buffer.pending_text == "lo"
    assert stream_buffer.flush() == ["lo"]
    assert stream_buffer.pending_text == ""


def test_stream_buffer_respects_max_emit_chars():
    stream_buffer = StreamBuffer(
        config=StreamBufferConfig(max_emit_chars=3),
        scanner=NoOpStreamScanner(),
    )

    assert stream_buffer.push_text("Hello") == ["Hel", "lo"]
    assert stream_buffer.pending_text == ""


def test_stream_buffer_scans_only_after_min_chars_and_uses_window_suffix():
    scanner = RecordingScanner()
    stream_buffer = StreamBuffer(
        config=StreamBufferConfig(min_scan_chars=4, max_window_chars=4),
        scanner=scanner,
    )

    assert stream_buffer.push_text("abc") == ["abc"]
    assert scanner.scanned_texts == []

    assert stream_buffer.push_text("def") == ["def"]
    assert scanner.scanned_texts == ["cdef"]


def test_stream_buffer_flush_emits_holdback_suffix():
    stream_buffer = StreamBuffer(
        config=StreamBufferConfig(holdback_chars=3),
        scanner=NoOpStreamScanner(),
    )

    assert stream_buffer.push_text("abcdef") == ["abc"]
    assert stream_buffer.flush() == ["def"]