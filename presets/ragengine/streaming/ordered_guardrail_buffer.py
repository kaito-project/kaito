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

"""Ordered multi-choice streaming buffer for guardrail scanning.

This module deliberately stays below OutputGuardrails, HTTP/SSE framing,
llm-guard, and block-response policy. It coordinates choice-local
StreamingBufferWindow instances and releases the original input events only
after all content offsets referenced by earlier queued events have been
scanner-confirmed.
"""

from collections import deque
from collections.abc import Callable
from dataclasses import dataclass

from ragengine.streaming.buffer_window import (
    StreamingBufferWindow,
    WindowEmitResult,
    WindowScanner,
)
from ragengine.streaming.openai import ParsedOpenAIChoice

WindowScannerFactory = Callable[[int], WindowScanner]
DEFAULT_MAX_PENDING_EVENTS = 1024
DEFAULT_MAX_PENDING_BYTES = 1024 * 1024


class OrderedGuardrailBufferOverflowError(RuntimeError):
    """Raised when the pending event queue reaches its configured limit."""


@dataclass(frozen=True)
class OrderedGuardrailBufferResult:
    ready_events: tuple[str, ...] = ()
    blocked: bool = False


@dataclass
class _ChoiceState:
    """Scanning progress for one OpenAI choice."""

    window: StreamingBufferWindow
    received_offset: int = 0  # Total characters received.
    released_offset: int = 0  # Characters confirmed safe by the window.
    finished: bool = False  # Whether the window has been flushed.


@dataclass(frozen=True)
class _PendingEvent:
    """A raw event waiting for its referenced content to become safe."""

    raw: str
    # choice_index -> released_offset required before release.
    required_offsets: dict[int, int]
    size: int  # UTF-8 size used for queue limits.


class OrderedGuardrailBuffer:
    def __init__(
        self,
        scanner_factory: WindowScannerFactory,
        *,
        holdback_len: int,
        max_pending_events: int = DEFAULT_MAX_PENDING_EVENTS,
        max_pending_bytes: int = DEFAULT_MAX_PENDING_BYTES,
    ) -> None:
        """Initialize per-choice scanning and pending raw-event queue limits."""
        if holdback_len < 0:
            raise ValueError("holdback_len must be non-negative.")
        if max_pending_events <= 0:
            raise ValueError("max_pending_events must be positive.")
        if max_pending_bytes <= 0:
            raise ValueError("max_pending_bytes must be positive.")

        self._scanner_factory = scanner_factory
        self._holdback_len = holdback_len
        self._max_pending_events = max_pending_events
        self._max_pending_bytes = max_pending_bytes
        self._choices: dict[int, _ChoiceState] = {}
        self._pending_events: deque[_PendingEvent] = deque()
        self._pending_bytes = 0
        self._blocked = False
        self._stream_finished = False

    def feed(
        self,
        raw: str,
        choices: tuple[ParsedOpenAIChoice, ...],
    ) -> OrderedGuardrailBufferResult:
        """Public entry point for one parsed OpenAI event.

        Scans choice content, records this raw event's release requirements, and
        returns all queued raw events that are now safe to pass through.
        """
        if self._blocked:
            return OrderedGuardrailBufferResult(blocked=True)
        if self._stream_finished:
            raise RuntimeError("cannot feed events after the stream is finished.")

        raw_size = self._ensure_queue_capacity(raw)
        required_offsets: dict[int, int] = {}

        for choice in choices:
            if choice.content is not None and self._scan_content(
                choice.choice_index,
                choice.content,
                required_offsets,
            ):
                return OrderedGuardrailBufferResult(blocked=True)

            if choice.finish_reason is not None:
                state = self._get_choice_state(choice.choice_index)
                required_offsets[choice.choice_index] = state.received_offset

        # Enqueue the raw event before flushing finished choices so the finish
        # event is released together with all earlier content in its original order.
        self._enqueue(raw, required_offsets, raw_size)

        for choice in choices:
            if choice.finish_reason is None:
                continue
            if self._finish_choice(choice.choice_index):
                return OrderedGuardrailBufferResult(blocked=True)

        return self._release_ready_events()

    def finish_stream(self, raw: str | None = None) -> OrderedGuardrailBufferResult:
        """Public entry point for stream completion.

        Optionally queues a final raw event, flushes every choice window, and
        releases all safe raw events before marking the stream finished.
        """
        if self._blocked:
            return OrderedGuardrailBufferResult(blocked=True)
        if self._stream_finished:
            raise RuntimeError("stream is already finished.")

        if raw is not None:
            raw_size = self._ensure_queue_capacity(raw)
            required_offsets = {
                choice_index: state.received_offset
                for choice_index, state in self._choices.items()
            }
            self._enqueue(raw, required_offsets, raw_size)

        for choice_index in tuple(self._choices):
            if self._finish_choice(choice_index):
                return OrderedGuardrailBufferResult(blocked=True)
        self._stream_finished = True

        return self._release_ready_events()

    def _scan_content(
        self,
        choice_index: int,
        content: str,
        required_offsets: dict[int, int],
    ) -> bool:
        """Update one choice's scan state for content in the current raw event."""
        state = self._get_choice_state(choice_index)
        if state.finished:
            raise RuntimeError(f"choice {choice_index} is already finished.")

        state.received_offset += len(content)
        required_offsets[choice_index] = state.received_offset

        if not content:
            return False

        emit_result = state.window.feed(content)
        return self._record_window_result(state, emit_result)

    def _finish_choice(self, choice_index: int) -> bool:
        """Flush one choice window so its final raw event can be released."""
        state = self._get_choice_state(choice_index)
        if state.finished:
            return False

        emit_result = state.window.flush()
        if self._record_window_result(state, emit_result):
            return True

        if state.released_offset != state.received_offset:
            raise RuntimeError("choice offsets do not match after flush.")

        state.finished = True
        return False

    def _record_window_result(
        self,
        state: _ChoiceState,
        emit_result: WindowEmitResult,
    ) -> bool:
        """Record a window result into choice offsets and block state."""
        if emit_result.blocked:
            self._blocked = True
            self._pending_events.clear()
            self._pending_bytes = 0
            return True

        state.released_offset += sum(len(chunk) for chunk in emit_result.chunks)
        if state.released_offset > state.received_offset:
            raise RuntimeError("released offset exceeded received offset.")

        return False

    def _ensure_queue_capacity(self, raw: str) -> int:
        """Protect the pending raw-event queue before accepting another event."""
        raw_size = len(raw.encode("utf-8"))
        if len(self._pending_events) >= self._max_pending_events:
            raise OrderedGuardrailBufferOverflowError(
                "pending event queue reached max_pending_events."
            )
        if self._pending_bytes + raw_size > self._max_pending_bytes:
            raise OrderedGuardrailBufferOverflowError(
                "pending event queue reached max_pending_bytes."
            )
        return raw_size

    def _enqueue(
        self,
        raw: str,
        required_offsets: dict[int, int],
        raw_size: int,
    ) -> None:
        """Store a raw event until all of its choice offsets are safe."""
        self._pending_events.append(
            _PendingEvent(
                raw=raw,
                required_offsets=required_offsets,
                size=raw_size,
            )
        )
        self._pending_bytes += raw_size

    def _release_ready_events(self) -> OrderedGuardrailBufferResult:
        """Release queued raw events from the head while they are safe."""
        ready_events: list[str] = []
        while self._pending_events and self._event_is_ready(self._pending_events[0]):
            event = self._pending_events.popleft()
            self._pending_bytes -= event.size
            ready_events.append(event.raw)

        return OrderedGuardrailBufferResult(ready_events=tuple(ready_events))

    def _event_is_ready(self, event: _PendingEvent) -> bool:
        """Check whether one pending raw event has met every choice requirement."""
        for choice_index, required_offset in event.required_offsets.items():
            state = self._choices.get(choice_index)
            if state is None or state.released_offset < required_offset:
                return False
        return True

    def _get_choice_state(self, choice_index: int) -> _ChoiceState:
        """Return the scanning state and window dedicated to one choice."""
        self._validate_choice_index(choice_index)
        state = self._choices.get(choice_index)
        if state is None:
            state = _ChoiceState(
                window=StreamingBufferWindow(
                    self._scanner_factory(choice_index),
                    holdback_len=self._holdback_len,
                )
            )
            self._choices[choice_index] = state
        return state

    @staticmethod
    def _validate_choice_index(choice_index: int) -> None:
        """Validate OpenAI choice indexes before they create buffer state."""
        if (
            isinstance(choice_index, bool)
            or not isinstance(choice_index, int)
            or choice_index < 0
        ):
            raise ValueError("choice_index must be a non-negative integer.")
