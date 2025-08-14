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

from typing import List, Optional

from llama_index.core.bridge.pydantic import Field, PrivateAttr
from llama_index.core.llms.llm import LLM
from llama_index.core.postprocessor.types import BaseNodePostprocessor
from llama_index.core.schema import NodeWithScore, QueryBundle
from llama_index.core.settings import Settings

ADDITION_PROMPT_TOKENS = 150 # Accounts for the addition prompt added by llamaindex after node processing
DEFAULT_RESPONSE_TOKEN_BUFFER = 1000  # Default buffer for response tokens to avoid exceeding context window

class ContextSelectionProcessor(BaseNodePostprocessor):
    """
    Context selection processor.
    This processor selects nodes based on their relevance to the query and the available context window.
    """
    llm: LLM = Field(description="The llm to get metadata from.")

    _max_tokens: int = PrivateAttr()
    _response_token_buffer: int = PrivateAttr()
    _similarity_threshold: Optional[float] = PrivateAttr()

    def __init__(
        self,
        llm: Optional[LLM] = None,
        max_tokens: Optional[int] = None,
        response_token_buffer: Optional[int] = None,
        similarity_threshold: Optional[float] = None,
    ) -> None:
        llm = llm or Settings.llm

        super().__init__(
            llm=llm,
        )

        # Use the passed in max_tokens or the context window size if not provided
        self._max_tokens = (
            max_tokens or llm.metadata.context_window
        )

        self._response_token_buffer = (
            response_token_buffer or DEFAULT_RESPONSE_TOKEN_BUFFER
        )

        self._similarity_threshold = (
            similarity_threshold or None
        )

    @classmethod
    def class_name(cls) -> str:
        return "ContextSelectionProcessor"

    def _postprocess_nodes(
        self,
        nodes: List[NodeWithScore],
        query_bundle: Optional[QueryBundle] = None,
    ) -> List[NodeWithScore]:
        if query_bundle is None:
            raise ValueError("Query bundle must be provided.")
        if len(nodes) == 0:
            return []

        query_token_aproximation = int(len(query_bundle.query_str) / 3) if query_bundle.query_str else 0
        # Total tokens available for context after accounting only for query
        available_tokens_for_context = self.llm.metadata.context_window - query_token_aproximation - ADDITION_PROMPT_TOKENS

        # max_tokens can be equal to the context window size, so we need to account for the response token buffer.
        # we handle updating the max_tokens value we send to the LLM in our inference code.
        available_tokens_for_context -= min(self._max_tokens, self._response_token_buffer)

        if available_tokens_for_context <= 0:
            return []

        # the scores from faiss are distances and we want to rerank based on relevance
        ranked_nodes = sorted(nodes, key=lambda x: x.score or 0.0)

        result: List[NodeWithScore] = []
        for idx in range(len(ranked_nodes)):
            node = ranked_nodes[idx]
            if self._similarity_threshold is not None and node.score > self._similarity_threshold:
                continue

            node_token_approximation = int(len(node.node.get_text()) / 3) if node.node.get_text() else 0
            if node_token_approximation > available_tokens_for_context:
                continue

            available_tokens_for_context -= node_token_approximation
            result.append(node)

        return result
