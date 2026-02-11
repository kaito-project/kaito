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

from collections.abc import Sequence
from typing import Any

from llama_index.core.bridge.pydantic import Field
from llama_index.core.llms import (
    ChatMessage,
    ChatResponse,
    CompletionResponse,
    CompletionResponseGen,
    CustomLLM,
    LLMMetadata,
)
from llama_index.core.llms.callbacks import llm_chat_callback, llm_completion_callback

from ragengine.config import (
    LLM_CONTEXT_WINDOW,
)


# Custom LLM that is used to capture llm calls and return empty responses
# This is used to intercept calls for the /retrieve api
class RetrieveLLM(CustomLLM):
    messages_list: list[ChatMessage] = Field()
    nodes_list: list[Any] = Field()
    original_llm: CustomLLM = Field()

    def __init__(self, messages_list, nodes_list, original_llm):
        super().__init__(
            messages_list=messages_list,
            nodes_list=nodes_list,
            original_llm=original_llm,
        )
        self.messages_list = messages_list
        self.nodes_list = nodes_list
        self.original_llm = original_llm

    @llm_completion_callback()
    def stream_complete(self, prompt: str, **kwargs: Any) -> CompletionResponseGen:
        pass

    @llm_completion_callback()
    def complete(
        self, prompt: str, formatted: bool = False, **kwargs: Any
    ) -> CompletionResponse:
        return CompletionResponse(text="")

    @llm_completion_callback()
    async def acomplete(
        self, prompt: str, formatted: bool = False, **kwargs: Any
    ) -> CompletionResponse:
        return CompletionResponse(text="")

    @llm_chat_callback()
    def chat(
        self,
        messages: Sequence[ChatMessage],
        **kwargs: Any,
    ) -> ChatResponse:
        self.messages_list.clear()
        self.messages_list.extend(messages)
        # Return dummy response
        return ChatResponse(message=ChatMessage(content=""))

    @llm_chat_callback()
    async def achat(
        self,
        messages: Sequence[ChatMessage],
        **kwargs: Any,
    ) -> ChatResponse:
        self.messages_list.clear()
        self.messages_list.extend(messages)
        # Return dummy response
        return ChatResponse(message=ChatMessage(content=""))

    @property
    def metadata(self) -> LLMMetadata:
        return LLMMetadata(
            is_chat_model=False,
            context_window=LLM_CONTEXT_WINDOW,
        )
