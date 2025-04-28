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
                              LLM_ACCESS_SECRET, DEFAULT_VECTOR_DB_PERSIST_DIR)
import httpx
import respx

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
    @respx.mock
    @patch("requests.get")
    async def test_query_documents(self, mock_get, vector_store_manager):
        mock_get.return_value.status_code = 200
        mock_get.return_value.json.return_value = {
            "data": [{"id": "mock-model", "max_model_len": 2048}]
        }

        mock_response = {"result": "This is the completion from the API"}
        respx.post(LLM_INFERENCE_URL).mock(return_value=httpx.Response(200, json=mock_response))

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
        assert respx.calls.call_count == 1  # Ensure only one LLM inference request was made

    @pytest.mark.asyncio
    async def test_add_document(self, vector_store_manager):
        documents = [Document(text="Third document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)

        new_document = [Document(text="Fourth document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", new_document)

        assert await vector_store_manager.document_exists("test_index", new_document[0],
                                                    BaseVectorStore.generate_doc_id("Fourth document"))

    @pytest.mark.asyncio
    async def test_add_document_on_existing_index(self, vector_store_manager):
        # Create index with single doc
        await vector_store_manager.index_documents("test_add_index", [Document(text=f"Initial Doc", metadata={"type": "text"})])

        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"})
            for i in range(10)
        ]
        # load documents into the same index
        ids = await vector_store_manager.index_documents("test_add_index", documents)

        # Check if the documents were indexed correctly
        all_docs = await vector_store_manager.list_documents_in_index("test_add_index", limit=10, offset=1)
        print(all_docs)

        for idx, doc_id in enumerate(ids):
            # list_documents_in_index returns documents with doc_id's that are not the same as the id's returned from index_documents
            response_doc = [doc for doc in all_docs if doc['doc_id'] == vector_store_manager.index_map['test_add_index'].docstore.get_ref_doc_info(doc_id).node_ids[0]][0] or None
            assert response_doc is not None
            assert documents[idx].text == response_doc['text']

    @pytest.mark.asyncio
    async def test_persist_index(self, vector_store_manager):
        documents = [Document(text="Test document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)
        await vector_store_manager.persist("test_index", DEFAULT_VECTOR_DB_PERSIST_DIR)
        assert os.path.exists(DEFAULT_VECTOR_DB_PERSIST_DIR)

    @pytest.mark.asyncio
    async def test_list_documents_in_index(self, vector_store_manager):
        """Test various list document scenarios with different limit and offset values."""
        index_name = "test_index"
        # Create multiple documents
        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"})
            for i in range(10)
        ]
        
        await vector_store_manager.index_documents(index_name, documents)

        # 1. Offset 0, Limit 5 (Basic Case)
        result = await vector_store_manager.list_documents_in_index(index_name, limit=5, offset=0)
        assert len(result) == 5

        # 2. Offset 5, Limit 5 (Next Batch)
        result = await vector_store_manager.list_documents_in_index(index_name, limit=5, offset=5)
        assert len(result) == 5

        # 3. Offset at max (Empty Case)
        result = await vector_store_manager.list_documents_in_index(index_name, limit=5, offset=10)
        assert index_name not in result or len(result) == 0

        # 4. Limit larger than available docs
        result = await vector_store_manager.list_documents_in_index(index_name, limit=15, offset=0)
        assert len(result) == 10  # Should return only available docs

        # 5. Limit exactly matches available docs
        result = await vector_store_manager.list_documents_in_index(index_name, limit=10, offset=0)
        assert len(result) == 10

        # 6. Limit of 1 (Single-Doc Retrieval)
        result = await vector_store_manager.list_documents_in_index(index_name, limit=1, offset=0)
        assert len(result) == 1

        # 7. max_text_length truncation check
        truncated_result = await vector_store_manager.list_documents_in_index(index_name, limit=1, offset=0, max_text_length=5)
        assert len(next(iter(truncated_result))['text']) == 5  # Ensure truncation

        # 8. max_text_length is None (Full text should return)
        full_text_result = await vector_store_manager.list_documents_in_index(index_name, limit=1, offset=0, max_text_length=None)
        assert "Document" in next(iter(full_text_result))['text']  # Ensure no truncation
