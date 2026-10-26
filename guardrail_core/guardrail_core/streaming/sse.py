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

import re
from dataclasses import dataclass

_LINE_ENDING = re.compile(r"\r\n|\r|\n")
DEFAULT_MAX_EVENT_BYTES = 1024 * 1024


@dataclass(frozen=True)
class SSEEvent:
    raw: str
    lines: tuple[str, ...]
    data: str | None
    raw_bytes: bytes

    @classmethod
    def from_raw(cls, raw_event: str, *, raw_bytes: bytes | None = None) -> "SSEEvent":
        """Parse an event body; the framer also supplies its exact wire bytes."""
        lines = tuple(_LINE_ENDING.split(raw_event)) if raw_event else ()
        data_lines: list[str] = []
        for line in lines:
            field, _, value = line.partition(":")
            if field == "data":
                data_lines.append(value.removeprefix(" "))
        return cls(
            raw=raw_event,
            lines=lines,
            data="\n".join(data_lines) if data_lines else None,
            raw_bytes=raw_event.encode("utf-8") if raw_bytes is None else raw_bytes,
        )


class SSEFramingError(ValueError):
    """Terminal framing failure, including any events completed by this call."""

    def __init__(self, message: str, raw_bytes: bytes) -> None:
        super().__init__(message)
        self.raw_bytes = raw_bytes
        self.completed_events: tuple[SSEEvent, ...] = ()


class SSEDecodeError(SSEFramingError):
    """A complete event contains invalid UTF-8."""


class SSEIncompleteEventError(SSEFramingError):
    """EOF left an event without a terminating blank line."""


class SSEEventTooLargeError(SSEFramingError):
    """An event exceeded the configured wire-byte limit."""


class SSEFramer:
    """Incrementally frame bytes in linear time, decoding only complete events."""

    def __init__(self, *, max_event_bytes: int = DEFAULT_MAX_EVENT_BYTES) -> None:
        if isinstance(max_event_bytes, bool) or not isinstance(max_event_bytes, int):
            raise TypeError("max_event_bytes must be an integer.")
        if max_event_bytes <= 0:
            raise ValueError("max_event_bytes must be positive.")
        self.max_event_bytes = max_event_bytes
        self._buffer = bytearray()
        self._line_size = 0
        self._event_end = 0
        self._pending_cr = False
        self._at_stream_start = True
        self._closed = False

    def feed(self, chunk: bytes) -> list[SSEEvent]:
        self._ensure_open()
        if not isinstance(chunk, bytes):
            raise TypeError("SSEFramer.feed requires bytes.")
        events: list[SSEEvent] = []
        try:
            for byte in chunk:
                if self._pending_cr:
                    self._pending_cr = False
                    if byte == 10:
                        self._append(byte)
                        self._end_line(2, events)
                        continue
                    self._end_line(1, events)
                self._append(byte)
                if byte == 13:
                    self._pending_cr = True
                elif byte == 10:
                    self._end_line(1, events)
                else:
                    self._line_size += 1
        except SSEFramingError as error:
            self._closed = True
            error.completed_events = tuple(events)
            raise
        return events

    def finish(self) -> list[SSEEvent]:
        """Resolve a trailing CR, then reject any unterminated event."""
        self._ensure_open()
        self._closed = True
        events: list[SSEEvent] = []
        try:
            if self._pending_cr:
                self._pending_cr = False
                self._end_line(1, events)
            if self._buffer:
                raise SSEIncompleteEventError(
                    "EOF before the SSE event's terminating blank line.",
                    bytes(self._buffer),
                )
        except SSEFramingError as error:
            error.completed_events = tuple(events)
            raise
        return events

    def _ensure_open(self) -> None:
        if self._closed:
            raise ValueError("SSEFramer is closed after finish() or a framing error.")

    def _append(self, byte: int) -> None:
        if len(self._buffer) >= self.max_event_bytes:
            raise SSEEventTooLargeError(
                f"SSE event exceeds max_event_bytes={self.max_event_bytes}.",
                bytes(self._buffer),
            )
        self._buffer.append(byte)

    def _end_line(self, ending_size: int, events: list[SSEEvent]) -> None:
        if self._line_size:
            self._event_end = len(self._buffer) - ending_size
            self._line_size = 0
            return
        raw_bytes = bytes(self._buffer)
        parse_bytes = raw_bytes
        event_end = self._event_end
        if self._at_stream_start:
            self._at_stream_start = False
            if raw_bytes.startswith(b"\xef\xbb\xbf"):
                parse_bytes = raw_bytes[3:]
                event_end -= 3
        try:
            raw = parse_bytes[:event_end].decode("utf-8", errors="strict")
        except UnicodeDecodeError as error:
            raise SSEDecodeError(
                "Invalid UTF-8 in complete SSE event.", raw_bytes
            ) from error
        events.append(SSEEvent.from_raw(raw, raw_bytes=raw_bytes))
        self._buffer.clear()
        self._event_end = 0
