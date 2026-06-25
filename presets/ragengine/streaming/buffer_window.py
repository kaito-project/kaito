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
- Final flush scans the remaining buffer before emitting any remaining text.
"""

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class WindowScanResult:
    safe_prefix_chars: int
    blocked: bool = False


class WindowScanner(Protocol):
    def scan(self, text: str) -> WindowScanResult:
        pass


@dataclass(frozen=True)
class WindowEmitResult:
    chunks: tuple[str, ...]
    blocked: bool = False


class StreamingBufferWindow:
    def __init__(
        self,
        scanner: WindowScanner,
        *,
        holdback_chars: int,
        min_scan_chars: int,
        max_emit_chars: int,
    ) -> None:
        if holdback_chars < 0:
            raise ValueError("holdback_chars must be non-negative.")
        if min_scan_chars < 0:
            raise ValueError("min_scan_chars must be non-negative.")
        if max_emit_chars <= 0:
            raise ValueError("max_emit_chars must be positive.")

        self._scanner = scanner
        self._holdback_chars = holdback_chars
        self._min_scan_chars = min_scan_chars
        self._max_emit_chars = max_emit_chars
        self._pending_buffer = ""
        self._blocked = False

    @property
    def blocked(self) -> bool:
        return self._blocked

    def feed(self, text: str) -> WindowEmitResult:
        if self._blocked:
            return WindowEmitResult(chunks=(), blocked=True)

        self._pending_buffer += text
        scan_text = self._scan_window()
        if len(scan_text) < self._min_scan_chars:
            return WindowEmitResult(chunks=())

        return self._scan_and_emit(scan_text)

    def flush(self) -> WindowEmitResult:
        if self._blocked:
            return WindowEmitResult(chunks=(), blocked=True)
        if not self._pending_buffer:
            return WindowEmitResult(chunks=())

        return self._scan_and_emit(self._pending_buffer)

    def _scan_window(self) -> str:
        if self._holdback_chars == 0:
            return self._pending_buffer
        return self._pending_buffer[: -self._holdback_chars]

    def _scan_and_emit(self, scan_text: str) -> WindowEmitResult:
        scan_result = self._scanner.scan(scan_text)
        if scan_result.blocked:
            self._blocked = True
            self._pending_buffer = ""
            return WindowEmitResult(chunks=(), blocked=True)

        safe_prefix_chars = max(0, min(scan_result.safe_prefix_chars, len(scan_text)))
        if safe_prefix_chars == 0:
            return WindowEmitResult(chunks=())

        safe_prefix = self._pending_buffer[:safe_prefix_chars]
        self._pending_buffer = self._pending_buffer[safe_prefix_chars:]
        return WindowEmitResult(chunks=self._chunk_safe_prefix(safe_prefix))

    def _chunk_safe_prefix(self, safe_prefix: str) -> tuple[str, ...]:
        return tuple(
            safe_prefix[index : index + self._max_emit_chars]
            for index in range(0, len(safe_prefix), self._max_emit_chars)
        )
