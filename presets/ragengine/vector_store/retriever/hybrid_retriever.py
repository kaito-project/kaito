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

"""Hybrid retriever combining vector (semantic) and BM25 (keyword) search.

Uses weighted score fusion:
  finalScore = vector_weight * vectorScore + text_weight * textScore

where textScore is derived from BM25 rank via: 1 / (1 + rank).
Default weights: vector=0.7, text=0.3 (normalized to sum to 1.0).
"""

import logging

from llama_index.core import QueryBundle, VectorStoreIndex
from llama_index.core.retrievers import BaseRetriever
from llama_index.core.schema import NodeWithScore
from llama_index.core.vector_stores.types import (
    FilterCondition,
    FilterOperator,
    MetadataFilter,
    MetadataFilters,
)

try:
    from llama_index.retrievers.bm25 import BM25Retriever

    HAS_BM25 = True
except ImportError:
    HAS_BM25 = False

logger = logging.getLogger(__name__)


def _build_metadata_filters(
    metadata_filter: dict | None,
) -> MetadataFilters | None:
    """Convert a flat {key: value} dict to LlamaIndex MetadataFilters."""
    if not metadata_filter:
        return None
    return MetadataFilters(
        filters=[
            MetadataFilter(key=k, value=v, operator=FilterOperator.EQ)
            for k, v in metadata_filter.items()
        ],
        condition=FilterCondition.AND,
    )


class HybridRetriever(BaseRetriever):
    """Retriever that fuses vector similarity and BM25 keyword scores.

    For each query:
      1. Retrieve `candidate_pool_size` candidates from both vector and BM25.
      2. Merge results by node_id.
      3. Compute weighted score: vector_weight * vec_score + text_weight * (1/(1+rank)).
      4. Return top `max_results` sorted by final score descending.

    The BM25 retriever is built fresh from the index docstore each call,
    avoiding stale-cache bugs when documents are added/deleted between queries.
    """

    def __init__(
        self,
        index: VectorStoreIndex,
        max_results: int = 10,
        candidate_multiplier: float = 3.0,
        vector_weight: float = 0.7,
        text_weight: float = 0.3,
        metadata_filter: dict | None = None,
    ) -> None:
        """
        Args:
            index: The VectorStoreIndex to retrieve from.
            max_results: Number of final results to return.
            candidate_multiplier: Over-fetch factor for candidate pool (>= 1.0).
            vector_weight: Weight for vector similarity scores.
            text_weight: Weight for BM25 keyword scores.
            metadata_filter: Optional {key: value} dict for metadata filtering.
        """
        total = vector_weight + text_weight
        self._vector_weight = vector_weight / total
        self._text_weight = text_weight / total

        self._index = index
        self._max_results = max_results
        self._candidate_multiplier = max(1.0, candidate_multiplier)
        self._candidate_pool_size = int(max_results * self._candidate_multiplier)
        self._metadata_filter = metadata_filter
        self._llama_filters = _build_metadata_filters(metadata_filter)

        super().__init__()

    def _build_bm25_retriever(self, top_k: int) -> "BM25Retriever | None":
        """Build a fresh BM25Retriever from the current index docstore.

        Returns None if:
          - BM25 package not installed
          - Docstore is empty (e.g. Qdrant stores docs externally)
          - BM25 indexing fails for any reason
        In these cases, retrieval falls back to vector-only.
        """
        if not HAS_BM25:
            logger.debug("BM25 retriever not available (package not installed)")
            return None
        try:
            # Check if docstore actually has documents
            all_docs = self._index.docstore.docs
            if not all_docs:
                logger.info("Docstore is empty — falling back to vector-only retrieval")
                return None
            return BM25Retriever.from_defaults(
                docstore=self._index.docstore,
                similarity_top_k=top_k,
            )
        except Exception as e:
            logger.warning(
                f"Failed to build BM25 retriever: {e}. Falling back to vector-only."
            )
            return None

    def _fuse(
        self,
        vector_nodes: list[NodeWithScore],
        keyword_nodes: list[NodeWithScore],
    ) -> list[NodeWithScore]:
        """Fuse vector and keyword results using weighted scoring."""
        # Collect vector scores by node_id
        vector_scores = {
            n.node.node_id: (n.score if n.score is not None else 0.0)
            for n in vector_nodes
        }

        # BM25 results: convert position rank to score = 1/(1+rank)
        keyword_ranks = {n.node.node_id: idx for idx, n in enumerate(keyword_nodes)}

        # Build node lookup (vector nodes take precedence for identical IDs)
        node_lookup = {}
        for n in keyword_nodes:
            node_lookup[n.node.node_id] = n.node
        for n in vector_nodes:
            node_lookup[n.node.node_id] = n.node

        # Compute weighted scores
        all_ids = set(vector_scores.keys()) | set(keyword_ranks.keys())
        scored = []
        for nid in all_ids:
            vec_score = vector_scores.get(nid, 0.0)
            kw_rank = keyword_ranks.get(nid)
            text_score = 1.0 / (1.0 + kw_rank) if kw_rank is not None else 0.0

            final = self._vector_weight * vec_score + self._text_weight * text_score
            scored.append(NodeWithScore(node=node_lookup[nid], score=final))

        scored.sort(key=lambda x: x.score if x.score is not None else 0.0, reverse=True)
        return scored[: self._max_results]

    def _retrieve(self, query_bundle: QueryBundle) -> list[NodeWithScore]:
        """Synchronous hybrid retrieval (falls back to vector-only if BM25 unavailable)."""
        pool_size = self._candidate_pool_size

        # Vector retriever
        vector_retriever = self._index.as_retriever(
            similarity_top_k=pool_size,
            filters=self._llama_filters,
        )
        vector_nodes = vector_retriever.retrieve(query_bundle)

        # BM25 retriever (fresh each time — fast, avoids stale cache)
        bm25_retriever = self._build_bm25_retriever(pool_size)
        if bm25_retriever is None:
            logger.info(f"Vector-only retrieve: {len(vector_nodes)} candidates")
            return vector_nodes[: self._max_results]

        keyword_nodes = bm25_retriever.retrieve(query_bundle)

        logger.info(
            f"Hybrid retrieve: {len(vector_nodes)} vector + "
            f"{len(keyword_nodes)} BM25 candidates"
        )

        # Apply metadata filter to keyword results (BM25 doesn't support native filtering)
        if self._metadata_filter:
            keyword_nodes = [
                n
                for n in keyword_nodes
                if all(
                    (n.node.metadata or {}).get(k) == v
                    for k, v in self._metadata_filter.items()
                )
            ]

        return self._fuse(vector_nodes, keyword_nodes)

    async def _aretrieve(self, query_bundle: QueryBundle) -> list[NodeWithScore]:
        """Async hybrid retrieval (falls back to vector-only if BM25 unavailable)."""
        pool_size = self._candidate_pool_size

        vector_retriever = self._index.as_retriever(
            similarity_top_k=pool_size,
            filters=self._llama_filters,
        )
        vector_nodes = await vector_retriever.aretrieve(query_bundle)

        bm25_retriever = self._build_bm25_retriever(pool_size)
        if bm25_retriever is None:
            logger.info(f"Vector-only retrieve (async): {len(vector_nodes)} candidates")
            return vector_nodes[: self._max_results]

        keyword_nodes = await bm25_retriever.aretrieve(query_bundle)

        logger.info(
            f"Hybrid retrieve (async): {len(vector_nodes)} vector + "
            f"{len(keyword_nodes)} BM25 candidates"
        )

        if self._metadata_filter:
            keyword_nodes = [
                n
                for n in keyword_nodes
                if all(
                    (n.node.metadata or {}).get(k) == v
                    for k, v in self._metadata_filter.items()
                )
            ]

        return self._fuse(vector_nodes, keyword_nodes)
