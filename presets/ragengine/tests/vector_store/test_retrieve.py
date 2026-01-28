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

import os
from tempfile import TemporaryDirectory
from unittest.mock import patch

import pytest

from ragengine.config import LOCAL_EMBEDDING_MODEL_ID
from ragengine.embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from ragengine.models import Document
from ragengine.vector_store.faiss_store import FaissVectorStoreHandler


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
