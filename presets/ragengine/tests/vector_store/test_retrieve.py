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


"""
Unit tests for the retrieve method.
"""

import math
import os
import re
import zlib
from tempfile import TemporaryDirectory
from unittest.mock import patch

import pytest
from llama_index.core import VectorStoreIndex
from llama_index.core.schema import TextNode

from ragengine.config import LOCAL_EMBEDDING_MODEL_ID
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from ragengine.models import Document
from ragengine.vector_store.faiss_store import FaissVectorStoreHandler
from ragengine.vector_store.retriever.hybrid_retriever import HybridRetriever

RANKING_DOCS = [
    "The cat sat on the mat.",
    "A kitten is resting on a rug.",
    "Quarterly revenue grew by twelve percent.",
]


class BagOfWordsEmbedding(BaseEmbeddingModel):
    """Deterministic embedding: normalized word counts hashed into buckets."""

    def get_embedding_dimension(self) -> int:
        return 64

    def _get_text_embedding(self, text: str) -> list[float]:
        vector = [0.0] * self.get_embedding_dimension()
        for word in re.findall(r"\w+", text.lower()):
            vector[zlib.crc32(word.encode()) % len(vector)] += 1.0
        norm = math.sqrt(sum(v * v for v in vector)) or 1.0
        return [v / norm for v in vector]

    def _get_query_embedding(self, query: str) -> list[float]:
        return self._get_text_embedding(query)


@pytest.fixture(scope="session")
def init_embed_manager():
    return LocalHuggingFaceEmbedding(LOCAL_EMBEDDING_MODEL_ID)


@pytest.fixture
def vector_store_with_docs(init_embed_manager):
    with TemporaryDirectory() as temp_dir:
        os.environ["PERSIST_DIR"] = temp_dir
        yield FaissVectorStoreHandler(init_embed_manager)


@pytest.fixture(autouse=True)
def mock_llm_model_info():
    """Mock LLM model info to avoid initialization overhead."""
    with patch(
        "ragengine.inference.inference.Inference._get_default_model_info"
    ) as mock_model_info:
        mock_model_info.return_value = ("mock-model", 4096)
        yield


@pytest.mark.asyncio
async def test_retrieve_basic(vector_store_with_docs):
    """Test basic retrieve functionality."""
    documents = [
        Document(
            text="Python is a programming language", metadata={"category": "tech"}
        ),
        Document(
            text="JavaScript is used for web development", metadata={"category": "tech"}
        ),
        Document(text="The sky is blue", metadata={"category": "nature"}),
    ]
    await vector_store_with_docs.index_documents("test_index", documents)

    result = await vector_store_with_docs.retrieve(
        index_name="test_index", query="What is Python?", max_node_count=3
    )

    assert result is not None
    assert "query" in result
    assert "results" in result
    assert "count" in result
    assert result["query"] == "What is Python?"
    assert result["count"] <= 3


@pytest.mark.asyncio
async def test_retrieve_max_node_count(vector_store_with_docs):
    """Test that max_node_count parameter limits results."""
    documents = [
        Document(text=f"Document {i}", metadata={"index": i}) for i in range(10)
    ]
    await vector_store_with_docs.index_documents("test_index", documents)

    result = await vector_store_with_docs.retrieve(
        index_name="test_index", query="document", max_node_count=2
    )

    assert result["count"] <= 2


@pytest.mark.asyncio
async def test_retrieve_default_max_node_count(vector_store_with_docs):
    """Test retrieve with default max_node_count (5)."""
    documents = [
        Document(text=f"Technology document {i}", metadata={}) for i in range(10)
    ]
    await vector_store_with_docs.index_documents("test_index", documents)

    result = await vector_store_with_docs.retrieve(
        index_name="test_index", query="technology"
    )

    assert result["count"] <= 5


@pytest.mark.asyncio
async def test_retrieve_nonexistent_index(vector_store_with_docs):
    """Test retrieve with a non-existent index."""
    from fastapi import HTTPException

    with pytest.raises(HTTPException) as exc_info:
        await vector_store_with_docs.retrieve(
            index_name="nonexistent_index", query="test query"
        )

    assert exc_info.value.status_code == 404


@pytest.mark.asyncio
async def test_retrieve_result_structure(vector_store_with_docs):
    """Test that retrieve results have the correct structure."""
    documents = [
        Document(text="Python is great", metadata={"lang": "python"}),
    ]
    await vector_store_with_docs.index_documents("test_index", documents)

    result = await vector_store_with_docs.retrieve(
        index_name="test_index", query="Python programming", max_node_count=3
    )

    assert isinstance(result, dict)
    assert "query" in result
    assert "results" in result
    assert "count" in result

    if result["count"] > 0:
        first_result = result["results"][0]
        assert "doc_id" in first_result
        assert "node_id" in first_result
        assert "text" in first_result
        assert "score" in first_result
        assert "metadata" in first_result


@pytest.mark.asyncio
async def test_retrieve_ranks_exact_match_first_with_faiss():
    """FAISS returns L2 distances; the closest match must still rank first."""
    with TemporaryDirectory() as temp_dir:
        os.environ["PERSIST_DIR"] = temp_dir
        store = FaissVectorStoreHandler(BagOfWordsEmbedding())
        await store.index_documents(
            "rank_index", [Document(text=text) for text in RANKING_DOCS]
        )

        result = await store.retrieve(
            index_name="rank_index", query=RANKING_DOCS[2], max_node_count=3
        )

    scores = [r["score"] for r in result["results"]]
    assert result["results"][0]["text"] == RANKING_DOCS[2]
    assert scores[0] > scores[1]


@pytest.mark.asyncio
async def test_hybrid_retriever_keeps_similarity_scores():
    """Stores that return similarities are fused without conversion."""
    index = VectorStoreIndex(
        nodes=[TextNode(text=text) for text in RANKING_DOCS],
        embed_model=BagOfWordsEmbedding(),
    )

    results = await HybridRetriever(index, max_results=3).aretrieve(RANKING_DOCS[2])

    assert results[0].node.get_content() == RANKING_DOCS[2]


@pytest.mark.asyncio
async def test_retrieve_without_bm25_scores_faiss_as_similarities():
    """Vector-only results use the same higher-is-better scores as fused ones."""
    with TemporaryDirectory() as temp_dir:
        os.environ["PERSIST_DIR"] = temp_dir
        store = FaissVectorStoreHandler(BagOfWordsEmbedding())
        await store.index_documents(
            "rank_index", [Document(text=text) for text in RANKING_DOCS]
        )

        with patch.object(HybridRetriever, "_build_bm25_retriever", return_value=None):
            result = await store.retrieve(
                index_name="rank_index", query=RANKING_DOCS[2], max_node_count=3
            )
            sync_results = HybridRetriever(
                store.index_map["rank_index"],
                max_results=3,
                vector_scores_are_distances=True,
            ).retrieve(RANKING_DOCS[2])

    scores = [r["score"] for r in result["results"]]
    assert result["results"][0]["text"] == RANKING_DOCS[2]
    assert scores[0] == pytest.approx(1.0)
    assert scores == sorted(scores, reverse=True)
    assert sync_results[0].node.get_content() == RANKING_DOCS[2]
    assert sync_results[0].score == pytest.approx(1.0)
