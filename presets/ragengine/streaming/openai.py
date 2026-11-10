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

"""Compatibility exports for the shared OpenAI streaming implementation."""

from guardrail_core.streaming.openai import (
    OpenAIChatChunkParseResult,
    OpenAIChatChunkParseStatus,
    ParsedOpenAIChoice,
    build_openai_chat_delta_sse_chunk,
    build_openai_chat_finish_reason_sse_chunk,
    build_sse_data_chunk,
    build_sse_done_chunk,
    parse_openai_chat_sse_event,
)

__all__ = [
    "OpenAIChatChunkParseResult",
    "OpenAIChatChunkParseStatus",
    "ParsedOpenAIChoice",
    "build_openai_chat_delta_sse_chunk",
    "build_openai_chat_finish_reason_sse_chunk",
    "build_sse_data_chunk",
    "build_sse_done_chunk",
    "parse_openai_chat_sse_event",
]
