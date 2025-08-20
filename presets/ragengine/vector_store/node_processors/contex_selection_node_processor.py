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

import logging

from llama_index.core.bridge.pydantic import Field, PrivateAttr
from llama_index.core.llms.llm import LLM
from llama_index.core.postprocessor.types import BaseNodePostprocessor
from llama_index.core.schema import NodeWithScore, QueryBundle
from llama_index.core.settings import Settings

ADDITION_PROMPT_TOKENS = (
    150  # Accounts for the addition prompt added by llamaindex after node processing
)

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class ContextSelectionProcessor(BaseNodePostprocessor):
    """
    Context selection processor.
    This processor selects nodes based on their relevance to the query and the available context window.
    """

    llm: LLM = Field(description="The llm to get metadata from.")

    _max_tokens: int = PrivateAttr()
    _rag_context_token_fill_ratio: float = PrivateAttr()
    _similarity_threshold: float | None = PrivateAttr()

    def __init__(
        self,
        rag_context_token_fill_ratio: float,
        llm: LLM | None = None,
        max_tokens: int | None = None,
        similarity_threshold: float | None = None,
    ) -> None:
        llm = llm or Settings.llm

        super().__init__(
            llm=llm,
        )

        self._rag_context_token_fill_ratio = rag_context_token_fill_ratio

        # Use the passed in max_tokens or the context window size if not provided
        self._max_tokens = max_tokens or llm.metadata.context_window

        self._similarity_threshold = similarity_threshold or None

    @classmethod
    def class_name(cls) -> str:
        return "ContextSelectionProcessor"

    def _postprocess_nodes(
        self,
        nodes: list[NodeWithScore],
        query_bundle: QueryBundle | None = None,
    ) -> list[NodeWithScore]:
        if query_bundle is None:
            raise ValueError("Query bundle must be provided.")
        if len(nodes) == 0:
            return []

        query_token_aproximation = self.llm.count_tokens(query_bundle.query_str)
        # Total tokens available for context after accounting only for query
        available_tokens_for_context = (
            self.llm.metadata.context_window
            - query_token_aproximation
            - ADDITION_PROMPT_TOKENS
        )
        # Take the lesser of the max_tokens param and available context
        available_tokens_for_context = min(
            self._max_tokens, available_tokens_for_context
        )
        # Apply the RAG context fill ratio
        available_tokens_for_context = int(
            available_tokens_for_context * self._rag_context_token_fill_ratio
        )

        if available_tokens_for_context <= 0:
            logger.warning(
                "No available tokens for context after accounting for query and addition prompt."
            )
            return []

        # the scores from faiss are distances and we want to rerank based on relevance
        ranked_nodes = sorted(nodes, key=lambda x: x.score or 0.0)

        result: list[NodeWithScore] = []
        for idx in range(len(ranked_nodes)):
            node = ranked_nodes[idx]
            if (
                self._similarity_threshold is not None
                and node.score > self._similarity_threshold
            ):
                continue

            node_token_approximation = self.llm.count_tokens(node.text)
            if node_token_approximation > available_tokens_for_context:
                continue

            available_tokens_for_context -= node_token_approximation
            result.append(node)

        logger.info(
            f"Selected {len(result)} nodes out of {len(nodes)} based on context window and similarity threshold."
        )
        return result
