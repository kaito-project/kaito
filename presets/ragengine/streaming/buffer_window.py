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

"""Streaming buffer/window engine for incremental output scanning.

Safety contract:
- The unscanned tail is retained in ``pending_buffer`` and must not be emitted.
- Only scanner-confirmed safe prefixes may be emitted downstream.
- Feed scans include the holdback tail before emitting any prefix.
- Final flush scans the remaining buffer before emitting any remaining text.
"""

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class WindowScanResult:
    blocked: bool = False


class WindowScanner(Protocol):
    def scan(self, text: str) -> WindowScanResult: ...


@dataclass(frozen=True)
class WindowEmitResult:
    chunks: tuple[str, ...]
    blocked: bool = False


class StreamingBufferWindow:
    def __init__(
        self,
        scanner: WindowScanner,
        *,
        holdback_len: int,
    ) -> None:
        if holdback_len < 0:
            raise ValueError("holdback_len must be non-negative.")

        self._scanner = scanner
        self._holdback_len = holdback_len
        self._pending_buffer = ""
        self._blocked = False

    @property
    def blocked(self) -> bool:
        return self._blocked

    def feed(self, text: str) -> WindowEmitResult:
        if self._blocked:
            return WindowEmitResult(chunks=(), blocked=True)

        self._pending_buffer += text
        emit_len = self._calc_emit_len()
        if emit_len == 0:
            return WindowEmitResult(chunks=())

        return self._scan_and_emit(
            self._pending_buffer,
            emit_len=emit_len,
        )

    def flush(self) -> WindowEmitResult:
        if self._blocked:
            return WindowEmitResult(chunks=(), blocked=True)
        if not self._pending_buffer:
            return WindowEmitResult(chunks=())

        return self._scan_and_emit(
            self._pending_buffer,
            emit_len=len(self._pending_buffer),
        )

    def _calc_emit_len(self) -> int:
        if self._holdback_len == 0:
            return len(self._pending_buffer)
        return max(0, len(self._pending_buffer) - self._holdback_len)

    def _scan_and_emit(
        self,
        scan_text: str,
        *,
        emit_len: int,
    ) -> WindowEmitResult:
        scan_result = self._scanner.scan(scan_text)
        if scan_result.blocked:
            self._blocked = True
            self._pending_buffer = ""
            return WindowEmitResult(chunks=(), blocked=True)

        emit_text = self._pending_buffer[:emit_len]
        self._pending_buffer = self._pending_buffer[emit_len:]
        return WindowEmitResult(chunks=(emit_text,))
