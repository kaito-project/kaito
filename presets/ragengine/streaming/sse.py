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

from collections.abc import AsyncIterable, AsyncIterator
from dataclasses import dataclass

SSE_EVENT_SEPARATORS = ("\r\n\r\n", "\n\n")


@dataclass(frozen=True)
class SSEEvent:
    raw: str
    lines: tuple[str, ...]
    data: str | None

    @classmethod
    def from_raw(cls, raw_event: str) -> "SSEEvent":
        lines = tuple(raw_event.splitlines())
        data_lines: list[str] = []
        for line in lines:
            if not line.startswith("data:"):
                continue

            data_line = line[len("data:") :]
            if data_line.startswith(" "):
                data_line = data_line[1:]
            data_lines.append(data_line)

        return cls(
            raw=raw_event,
            lines=lines,
            data="\n".join(data_lines) if data_lines else None,
        )


class SSEFramer:
    def __init__(self) -> None:
        self._buffer = ""

    def feed(self, text_chunk: str) -> list[SSEEvent]:
        if not text_chunk:
            return []

        self._buffer += text_chunk
        events: list[SSEEvent] = []

        while separator := self._next_separator():
            separator_index, separator_text = separator
            raw_event = self._buffer[:separator_index]
            self._buffer = self._buffer[separator_index + len(separator_text) :]
            if raw_event:
                events.append(SSEEvent.from_raw(raw_event))

        return events

    def _next_separator(self) -> tuple[int, str] | None:
        candidates = [
            (separator_index, separator_text)
            for separator_text in SSE_EVENT_SEPARATORS
            if (separator_index := self._buffer.find(separator_text)) != -1
        ]
        if not candidates:
            return None

        return min(candidates, key=lambda candidate: candidate[0])


async def iter_sse_events(
    text_chunks: AsyncIterable[str],
) -> AsyncIterator[SSEEvent]:
    framer = SSEFramer()
    async for text_chunk in text_chunks:
        for event in framer.feed(text_chunk):
            yield event
