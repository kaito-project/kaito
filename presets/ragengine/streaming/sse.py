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
from guardrail_core.streaming.sse import SSEFramer as SSEFramer


async def iter_sse_events(
    chunks: AsyncIterable[bytes],
    *,
    max_event_bytes: int = DEFAULT_MAX_EVENT_BYTES,
) -> AsyncIterator[SSEEvent]:
    framer = SSEFramer(max_event_bytes=max_event_bytes)
    async for chunk in chunks:
        for event in framer.feed(chunk):
            yield event
    for event in framer.finish():
        yield event
