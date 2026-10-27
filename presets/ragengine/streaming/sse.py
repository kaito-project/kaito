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

from guardrail_core.streaming.sse import DEFAULT_MAX_EVENT_BYTES
from guardrail_core.streaming.sse import SSEEvent as SSEEvent
from guardrail_core.streaming.sse import SSEFramer as ByteSSEFramer


class SSEFramer:
    """Adapt RAGEngine text chunks without replacing the shared byte parser."""

    def __init__(self, *, max_event_bytes: int = DEFAULT_MAX_EVENT_BYTES) -> None:
        self._framer = ByteSSEFramer(max_event_bytes=max_event_bytes)

    def feed(self, text_chunk: str | bytes) -> list[SSEEvent]:
        chunk = (
            text_chunk.encode("utf-8") if isinstance(text_chunk, str) else text_chunk
        )
        return self._framer.feed(chunk)

    def finish(self) -> list[SSEEvent]:
        return self._framer.finish()


async def iter_sse_events(
    text_chunks: AsyncIterable[str | bytes],
    *,
    max_event_bytes: int = DEFAULT_MAX_EVENT_BYTES,
) -> AsyncIterator[SSEEvent]:
    framer = SSEFramer(max_event_bytes=max_event_bytes)
    async for text_chunk in text_chunks:
        for event in framer.feed(text_chunk):
            yield event
    for event in framer.finish():
        yield event
