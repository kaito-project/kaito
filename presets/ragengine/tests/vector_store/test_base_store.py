# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import os
from unittest.mock import patch
import pytest
from abc import ABC, abstractmethod

from ragengine.vector_store.base import BaseVectorStore
from ragengine.models import Document
from ragengine.embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from ragengine.config import (LOCAL_EMBEDDING_MODEL_ID, LLM_INFERENCE_URL,
                              LLM_ACCESS_SECRET, VECTOR_DB_PERSIST_DIR)

class BaseVectorStoreTest(ABC):
    """Base class for vector store tests that defines the test structure."""
    
    @pytest.fixture(scope='session')
    def init_embed_manager(self):
        return LocalHuggingFaceEmbedding(LOCAL_EMBEDDING_MODEL_ID)

    @pytest.fixture
    @abstractmethod
    def vector_store_manager(self, init_embed_manager):
        """Each implementation must provide its own vector store manager."""
        pass
    
    @property
    @abstractmethod
    def expected_query_score(self):
        """Override this in implementation-specific test classes."""
        pass

    @pytest.mark.asyncio
    async def test_index_documents(self, vector_store_manager):
        first_doc_text, second_doc_text = "First document", "Second document"
        documents = [
            Document(text=first_doc_text, metadata={"type": "text"}),
            Document(text=second_doc_text, metadata={"type": "text"})
        ]
        
        doc_ids = await vector_store_manager.index_documents("test_index", documents)
        
        assert len(doc_ids) == 2
        assert set(doc_ids) == {BaseVectorStore.generate_doc_id(first_doc_text),
                                BaseVectorStore.generate_doc_id(second_doc_text)}

    @pytest.mark.asyncio
    async def test_index_documents_isolation(self, vector_store_manager):
        documents1 = [
            Document(text="First document in index1", metadata={"type": "text"}),
        ]
        documents2 = [
            Document(text="First document in index2", metadata={"type": "text"}),
        ]

        # Index documents in separate indices
        index_name_1, index_name_2 = "index1", "index2"
        await vector_store_manager.index_documents(index_name_1, documents1)
        await vector_store_manager.index_documents(index_name_2, documents2)

        # Call the backend-specific check method
        await self.check_indexed_documents(vector_store_manager)

    @abstractmethod
    def check_indexed_documents(self, vector_store_manager):
        """Abstract method to check indexed documents in backend-specific format."""
        pass

    @pytest.mark.asyncio
    @patch('requests.post')
    async def test_query_documents(self, mock_post, vector_store_manager):
        mock_response = {
            "result": "This is the completion from the API"
        }
        mock_post.return_value.json.return_value = mock_response

        documents = [
            Document(text="First document", metadata={"type": "text"}),
            Document(text="Second document", metadata={"type": "text"})
        ]
        await vector_store_manager.index_documents("test_index", documents)

        params = {"temperature": 0.7}
        query_result = await vector_store_manager.query("test_index", "First", top_k=1,
                                                  llm_params=params, rerank_params={})

        assert query_result is not None
        assert query_result["response"] == "{'result': 'This is the completion from the API'}"
        assert query_result["source_nodes"][0]["text"] == "First document"
        assert query_result["source_nodes"][0]["score"] == pytest.approx(self.expected_query_score, rel=1e-6)

        mock_post.assert_called_once_with(
            LLM_INFERENCE_URL,
            json={"prompt": "Context information is below.\n---------------------\ntype: text\n\nFirst document\n---------------------\nGiven the context information and not prior knowledge, answer the query.\nQuery: First\nAnswer: ", 'temperature': 0.7},
            headers={"Authorization": f"Bearer {LLM_ACCESS_SECRET}", 'Content-Type': 'application/json'}
        )

    @pytest.mark.asyncio
    async def test_add_document(self, vector_store_manager):
        documents = [Document(text="Third document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)

        new_document = [Document(text="Fourth document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", new_document)

        assert await vector_store_manager.document_exists("test_index", new_document[0],
                                                    BaseVectorStore.generate_doc_id("Fourth document"))

    @pytest.mark.asyncio
    async def test_persist_index_1(self, vector_store_manager):
        documents = [Document(text="Test document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)
        await vector_store_manager._persist("test_index")
        assert os.path.exists(VECTOR_DB_PERSIST_DIR)

    @pytest.mark.asyncio
    async def test_persist_index_2(self, vector_store_manager):
        documents = [Document(text="Test document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)

        documents = [Document(text="Another Test document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("another_test_index", documents)

        await vector_store_manager._persist_all()
        assert os.path.exists(VECTOR_DB_PERSIST_DIR)

    @pytest.mark.asyncio
    async def test_list_documents_pagination(self, vector_store_manager):
        """Test various pagination scenarios with different limit and offset values."""
        # Create multiple documents
        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"})
            for i in range(10)
        ]
        
        await vector_store_manager.index_documents("test_index", documents)

        # 1. Offset 0, Limit 5 (Basic Case)
        result = await vector_store_manager.list_documents_paginated(limit=5, offset=0)
        assert len(result["test_index"]) == 5

        # 2. Offset 5, Limit 5 (Next Batch)
        result = await vector_store_manager.list_documents_paginated(limit=5, offset=5)
        assert len(result["test_index"]) == 5

        # 3. Offset at max (Empty Case)
        result = await vector_store_manager.list_documents_paginated(limit=5, offset=10)
        assert "test_index" not in result or len(result["test_index"]) == 0

        # 4. Limit larger than available docs
        result = await vector_store_manager.list_documents_paginated(limit=15, offset=0)
        assert len(result["test_index"]) == 10  # Should return only available docs

        # 5. Limit exactly matches available docs
        result = await vector_store_manager.list_documents_paginated(limit=10, offset=0)
        assert len(result["test_index"]) == 10

        # 6. Limit of 1 (Single-Doc Retrieval)
        result = await vector_store_manager.list_documents_paginated(limit=1, offset=0)
        assert len(result["test_index"]) == 1

        # 7. max_text_length truncation check
        truncated_result = await vector_store_manager.list_documents_paginated(limit=1, offset=0, max_text_length=5)
        assert len(truncated_result["test_index"][0]["text"]) == 5  # Ensure truncation

        # 8. Limit + offset spanning multiple indexes
        await vector_store_manager.index_documents("another_index", documents)
        result = await vector_store_manager.list_documents_paginated(limit=6, offset=8)
        assert sum(len(docs) for docs in result.values()) == 6  # Ensure documents split across indexes

        # 9. max_text_length is None (Full text should return)
        full_text_result = await vector_store_manager.list_documents_paginated(limit=1, offset=0, max_text_length=None)
        assert "Document" in full_text_result["test_index"][0]["text"]  # Ensure no truncation
