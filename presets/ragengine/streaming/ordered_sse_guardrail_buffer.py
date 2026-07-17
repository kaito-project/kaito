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

from collections import deque
from dataclasses import dataclass
from enum import StrEnum
from typing import Protocol

from ragengine.streaming.buffer_window import StreamingBufferWindow, WindowScanResult
from ragengine.streaming.openai import OpenAIChatChunkParseResult
from ragengine.streaming.sse import SSEEvent

STREAMING_GUARDRAILS_HOLDBACK_LEN = 256
STREAMING_GUARDRAILS_MAX_PENDING_EVENTS = 1024
STREAMING_GUARDRAILS_MAX_PENDING_BYTES = 1024 * 1024


class WindowScanner(Protocol):
    def scan(self, text: str) -> WindowScanResult: ...


@dataclass
class ChoiceState:
    window: StreamingBufferWindow
    received_chars: int = 0
    released_chars: int = 0
    finished: bool = False


@dataclass
class PendingEvent:
    raw: str
    required_offsets: dict[int, int]


class ProcessStatus(StrEnum):
    OK = "ok"
    BLOCKED = "blocked"
    OVERFLOW = "overflow"
    INTERNAL_ERROR = "internal_error"


@dataclass(frozen=True)
class ProcessResult:
    ready_events: list[str]
    status: ProcessStatus = ProcessStatus.OK
    blocked_choice_index: int | None = None
    reason: str | None = None


class OrderedGuardrailBuffer:
    def __init__(
        self,
        scanner: WindowScanner,
        *,
        holdback_len: int = STREAMING_GUARDRAILS_HOLDBACK_LEN,
        max_pending_events: int = STREAMING_GUARDRAILS_MAX_PENDING_EVENTS,
        max_pending_bytes: int = STREAMING_GUARDRAILS_MAX_PENDING_BYTES,
    ) -> None:
        self._scanner = scanner
        self._holdback_len = holdback_len
        self._max_pending_events = max_pending_events
        self._max_pending_bytes = max_pending_bytes
        self._choice_states: dict[int, ChoiceState] = {}
        self._pending_events: deque[PendingEvent] = deque()
        self._pending_bytes = 0

    def push(
        self,
        event: SSEEvent,
        parse_result: OpenAIChatChunkParseResult,
    ) -> ProcessResult:
        required_offsets: dict[int, int] = {}
        for parsed_choice in parse_result.parsed_choices:
            if not parsed_choice.content:
                continue
            state = self._choice_state(parsed_choice.choice_index)
            if state.finished:
                return ProcessResult(
                    status=ProcessStatus.INTERNAL_ERROR,
                    ready_events=self._drain_ready_events(),
                    reason="content_after_finished_choice",
                )
            state.received_chars += len(parsed_choice.content)
            required_offsets[parsed_choice.choice_index] = state.received_chars

        self._append_pending_event(event, required_offsets)
        if self._exceeds_pending_limits():
            return ProcessResult(
                status=ProcessStatus.OVERFLOW,
                ready_events=self._drain_ready_events(),
            )

        for parsed_choice in parse_result.parsed_choices:
            if parsed_choice.content:
                state = self._choice_state(parsed_choice.choice_index)
                if state.finished:
                    return ProcessResult(
                        status=ProcessStatus.INTERNAL_ERROR,
                        ready_events=self._drain_ready_events(),
                        reason="content_after_finished_choice",
                    )
                emit_result = state.window.feed(parsed_choice.content)
                if emit_result.blocked:
                    return ProcessResult(
                        status=ProcessStatus.BLOCKED,
                        ready_events=self._drain_ready_events(),
                        blocked_choice_index=parsed_choice.choice_index,
                    )
                state.released_chars += sum(len(chunk) for chunk in emit_result.chunks)
                if state.released_chars > state.received_chars:
                    return ProcessResult(
                        status=ProcessStatus.INTERNAL_ERROR,
                        ready_events=self._drain_ready_events(),
                        reason="released_offset_exceeded_received_offset",
                    )

            if parsed_choice.finish_reason is not None:
                flush_status = self._flush_choice(parsed_choice.choice_index)
                if flush_status != ProcessStatus.OK:
                    return ProcessResult(
                        status=flush_status,
                        ready_events=self._drain_ready_events(),
                        blocked_choice_index=(
                            parsed_choice.choice_index
                            if flush_status == ProcessStatus.BLOCKED
                            else None
                        ),
                        reason=(
                            "choice_flush_failed"
                            if flush_status == ProcessStatus.INTERNAL_ERROR
                            else None
                        ),
                    )

        return ProcessResult(ready_events=self._drain_ready_events())

    def finish_stream(self) -> ProcessResult:
        for choice_index in self._choice_states:
            flush_status = self._flush_choice(choice_index)
            if flush_status != ProcessStatus.OK:
                return ProcessResult(
                    status=flush_status,
                    ready_events=self._drain_ready_events(),
                    blocked_choice_index=(
                        choice_index if flush_status == ProcessStatus.BLOCKED else None
                    ),
                    reason=(
                        "choice_flush_failed"
                        if flush_status == ProcessStatus.INTERNAL_ERROR
                        else None
                    ),
                )
        ready_events = self._drain_ready_events()
        if self._pending_events:
            return ProcessResult(
                status=ProcessStatus.INTERNAL_ERROR,
                ready_events=ready_events,
                reason="pending_events_not_drained",
            )
        self._choice_states.clear()
        return ProcessResult(ready_events=ready_events)

    def _choice_state(self, choice_index: int) -> ChoiceState:
        if choice_index not in self._choice_states:
            self._choice_states[choice_index] = ChoiceState(
                window=StreamingBufferWindow(
                    self._scanner,
                    holdback_len=self._holdback_len,
                )
            )
        return self._choice_states[choice_index]

    def _append_pending_event(
        self,
        event: SSEEvent,
        required_offsets: dict[int, int],
    ) -> None:
        pending_event = PendingEvent(
            raw=_raw_sse_chunk(event),
            required_offsets=required_offsets,
        )
        self._pending_events.append(pending_event)
        self._pending_bytes += len(pending_event.raw.encode("utf-8"))

    def _exceeds_pending_limits(self) -> bool:
        return (
            len(self._pending_events) > self._max_pending_events
            or self._pending_bytes > self._max_pending_bytes
        )

    def _drain_ready_events(self) -> list[str]:
        chunks = []
        while self._pending_events and self._event_ready(self._pending_events[0]):
            raw_chunk = self._pending_events.popleft().raw
            self._pending_bytes -= len(raw_chunk.encode("utf-8"))
            chunks.append(raw_chunk)
        return chunks

    def _event_ready(self, event: PendingEvent) -> bool:
        return all(
            self._choice_states[choice_index].released_chars >= required_offset
            for choice_index, required_offset in event.required_offsets.items()
        )

    def _flush_choice(self, choice_index: int) -> ProcessStatus:
        state = self._choice_states.get(choice_index)
        if state is None or state.finished:
            return ProcessStatus.OK
        flush_result = state.window.flush()
        if flush_result.blocked:
            return ProcessStatus.BLOCKED
        state.released_chars += sum(len(chunk) for chunk in flush_result.chunks)
        if state.released_chars != state.received_chars:
            return ProcessStatus.INTERNAL_ERROR
        state.finished = True
        return ProcessStatus.OK


def _raw_sse_chunk(event: SSEEvent) -> str:
    # SSEFramer normalizes event separators; passthrough preserves event fields/order,
    # not byte-for-byte original line endings.
    return f"{event.raw}\n\n"