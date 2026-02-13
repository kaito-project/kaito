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

from llama_index.core import QueryBundle
from llama_index.core.retrievers import (
    BaseRetriever,
    VectorIndexRetriever,
)
from llama_index.core.schema import NodeWithScore
from llama_index.retrievers.bm25 import BM25Retriever


class HybridRetriever(BaseRetriever):
    """HybridRetriever that performs weighted fusion of semantic and keyword search.
    
    Follows the openclaw hybrid search pattern:
    1. Retrieve candidate pools (maxResults * candidateMultiplier) from both retrievers
    2. Convert keyword rank to score: textScore = 1 / (1 + max(0, rank))
    3. Compute weighted final score: finalScore = vectorWeight * vectorScore + textWeight * textScore
    4. Sort by final score and return top maxResults
    """

    def __init__(
        self,
        vector_retriever: VectorIndexRetriever,
        keyword_retriever: BM25Retriever,
        max_results: int = 10,
        candidate_multiplier: float = 3.0,
        vector_weight: float = 0.7,
        text_weight: float = 0.3,
    ) -> None:
        """Initialize hybrid retriever with weighted score fusion.
        
        Args:
            vector_retriever: Retriever for semantic/vector search
            keyword_retriever: Retriever for BM25 keyword search
            max_results: Number of final results to return
            candidate_multiplier: Multiplier for candidate pool size (>=1.0)
            vector_weight: Weight for vector scores (normalized with text_weight)
            text_weight: Weight for text/keyword scores (normalized with vector_weight)
        """
        # Normalize weights so they sum to 1.0
        total_weight = vector_weight + text_weight
        self._vector_weight = vector_weight / total_weight
        self._text_weight = text_weight / total_weight
        
        self._vector_retriever = vector_retriever
        self._keyword_retriever = keyword_retriever
        self._max_results = max_results
        self._candidate_multiplier = max(1.0, candidate_multiplier)
        self._candidate_pool_size = int(max_results * self._candidate_multiplier)
        
        super().__init__()

    def _retrieve(self, query_bundle: QueryBundle) -> list[NodeWithScore]:
        """Retrieve nodes using weighted hybrid search.
        
        Returns nodes sorted by weighted combination of vector and keyword scores.
        """
        # Get the actual number of documents available in the corpus
        available_docs = 0
        if hasattr(self._keyword_retriever, 'corpus') and self._keyword_retriever.corpus:
            available_docs = len(self._keyword_retriever.corpus)
        elif hasattr(self._keyword_retriever, 'bm25') and self._keyword_retriever.bm25 and hasattr(self._keyword_retriever.bm25, 'scores') and hasattr(self._keyword_retriever.bm25.scores, 'get'):
                available_docs = self._keyword_retriever.bm25.scores.get('num_docs', 0)
        
        # Cap candidate_pool_size to available documents to prevent retrieval errors
        effective_candidate_pool_size = min(self._candidate_pool_size, available_docs) if available_docs > 0 else self._candidate_pool_size
        
        # Retrieve candidate pools (more than final max_results)
        # Temporarily adjust retriever top_k values
        original_vector_top_k = getattr(self._vector_retriever, 'similarity_top_k', self._max_results)
        original_keyword_top_k = getattr(self._keyword_retriever, 'similarity_top_k', self._max_results)
        
        try:
            self._vector_retriever.similarity_top_k = effective_candidate_pool_size
            self._keyword_retriever.similarity_top_k = effective_candidate_pool_size
            
            vector_nodes = self._vector_retriever.retrieve(query_bundle)
            keyword_nodes = self._keyword_retriever.retrieve(query_bundle)
        finally:
            # Restore original values
            self._vector_retriever.similarity_top_k = original_vector_top_k
            self._keyword_retriever.similarity_top_k = original_keyword_top_k

        # Create dictionaries for scores and ranks
        vector_scores = {n.node.node_id: n.score if n.score is not None else 0.0 
                        for n in vector_nodes}
        
        # Use position in list as rank (0 = best rank)
        keyword_ranks = {n.node.node_id: idx for idx, n in enumerate(keyword_nodes)}
        
        # Get all unique node IDs
        all_node_ids = set(vector_scores.keys()).union(set(keyword_ranks.keys()))
        
        # Build a lookup for nodes themselves
        node_lookup = {}
        for n in vector_nodes:
            node_lookup[n.node.node_id] = n.node
        for n in keyword_nodes:
            node_lookup[n.node.node_id] = n.node
        
        # Compute weighted scores for each node
        scored_nodes = []
        for node_id in all_node_ids:
            # Vector score (typically 0..1 range for cosine similarity)
            vector_score = vector_scores.get(node_id, 0.0)
            
            # Convert keyword rank to score using openclaw formula
            # textScore = 1 / (1 + max(0, rank))
            # Rank 0 (best) -> score = 1.0
            # Rank 1 -> score = 0.5
            # Rank 2 -> score = 0.33, etc.
            keyword_rank = keyword_ranks.get(node_id)
            if keyword_rank is not None:
                text_score = 1.0 / (1.0 + max(0, keyword_rank))
            else:
                text_score = 0.0
            
            # Compute weighted final score
            final_score = (self._vector_weight * vector_score) + (self._text_weight * text_score)
            
            scored_nodes.append(NodeWithScore(
                node=node_lookup[node_id],
                score=final_score
            ))
        
        # Sort by final score (descending) and take top max_results
        scored_nodes.sort(key=lambda x: x.score if x.score is not None else 0.0, reverse=True)
        return scored_nodes[:self._max_results]