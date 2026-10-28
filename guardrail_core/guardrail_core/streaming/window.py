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

"""Hold back output tails so scanners can detect matches across text chunks.

Release only scanned prefixes; scan remaining text at normal stream completion.
"""

from dataclasses import dataclass
from typing import Protocol


class WindowScanError(RuntimeError):
    """A scanner failed; the original exception is available as __cause__."""


@dataclass(frozen=True)
class WindowScanResult:
    """Allow text unchanged, replace it with sanitized_text, or block release.

    Sanitization replaces the whole pending buffer and recalculates release
    length, marking the window redacted. Blocking takes precedence over
    sanitization.
    """

    sanitized_text: str | None = None
    blocked: bool = False


class WindowScanner(Protocol):
    """Scan pending text, using preceding_char as left-boundary context.

    preceding_char is the last released character, or "" before the first release.
    flush=True marks normal end-of-stream for right-boundary checks.
    """

    def scan(
        self,
        text: str,
        *,
        flush: bool = False,
        preceding_char: str = "",
    ) -> WindowScanResult: ...


@dataclass(frozen=True)
class WindowEmitResult:
    """Scanner-approved chunks to release; blocked means stop all further output."""

    chunks: tuple[str, ...]
    blocked: bool = False


class StreamingBufferWindow:
    """Retain an output tail so scanner matches can span input chunks.

    Scanner exceptions clear the buffer, permanently block release, and raise
    WindowScanError with the original cause chained. Cancellation and other
    BaseException signals propagate; callers must stop without flushing.
    Callers own policy, refusal messages, error handling, and upstream closure.
    The window logs no model text and chooses no fail-open policy.
    """

    def __init__(
        self,
        scanner: WindowScanner,
        *,
        holdback_len: int,
    ) -> None:
        """Use scanner with a non-negative holdback_len in Unicode code points.

        Units are not bytes or graphemes. Non-integers (including bool) raise
        TypeError; negative integers raise ValueError. Zero scans each non-empty
        feed; insufficient holdback cannot guarantee cross-chunk detection.
        """
        if isinstance(holdback_len, bool) or not isinstance(holdback_len, int):
            raise TypeError("holdback_len must be an integer.")
        if holdback_len < 0:
            raise ValueError("holdback_len must be non-negative.")

        self._scanner = scanner
        self._holdback_len = holdback_len
        self._pending_buffer = ""
        self._preceding_char = ""
        self._blocked = False
        self._redacted = False

    @property
    def blocked(self) -> bool:
        return self._blocked

    @property
    def redacted(self) -> bool:
        return self._redacted

    def feed(self, text: str) -> WindowEmitResult:
        """Buffer decoded str text and return WindowEmitResult(chunks, blocked).

        Once the buffer exceeds holdback, scan it including the retained tail,
        then release only the approved prefix. Otherwise return no chunks.
        Blocked windows return an empty, blocked result without scanning.
        """
        if self._blocked:
            return WindowEmitResult(chunks=(), blocked=True)

        self._pending_buffer += text
        emit_len = self._calc_emit_len()
        if emit_len == 0:
            return WindowEmitResult(chunks=())

        return self._scan_and_emit(
            self._pending_buffer,
            flush=False,
        )

    def flush(self) -> WindowEmitResult:
        """At normal EOF, scan remaining text with flush=True and return its result.

        WindowEmitResult contains all approved remaining chunks or blocked=True.
        Empty or blocked windows emit nothing; a normal flush does not close
        the window. After parse failures or cancellation, discard it without
        flushing.
        """
        if self._blocked:
            return WindowEmitResult(chunks=(), blocked=True)
        if not self._pending_buffer:
            return WindowEmitResult(chunks=())

        return self._scan_and_emit(
            self._pending_buffer,
            flush=True,
        )

    def _calc_emit_len(self) -> int:
        if self._holdback_len == 0:
            return len(self._pending_buffer)
        return max(0, len(self._pending_buffer) - self._holdback_len)

    def _scan_and_emit(
        self,
        scan_text: str,
        *,
        flush: bool,
    ) -> WindowEmitResult:
        try:
            scan_result = self._scanner.scan(
                scan_text,
                flush=flush,
                preceding_char=self._preceding_char,
            )
        except Exception as error:
            self._blocked = True
            self._pending_buffer = ""
            raise WindowScanError("Streaming window scanner failed.") from error
        if scan_result.blocked:
            self._blocked = True
            self._pending_buffer = ""
            return WindowEmitResult(chunks=(), blocked=True)

        if scan_result.sanitized_text is not None:
            self._pending_buffer = scan_result.sanitized_text
            self._redacted = True

        emit_len = len(self._pending_buffer) if flush else self._calc_emit_len()
        if emit_len == 0:
            return WindowEmitResult(chunks=())

        emit_text = self._pending_buffer[:emit_len]
        self._pending_buffer = self._pending_buffer[emit_len:]
        self._preceding_char = emit_text[-1:]
        return WindowEmitResult(chunks=(emit_text,))
