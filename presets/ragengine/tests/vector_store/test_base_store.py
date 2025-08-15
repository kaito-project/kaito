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


import os
from abc import ABC, abstractmethod
from unittest.mock import patch

import httpx
import pytest
import respx

from ragengine.config import (
    DEFAULT_VECTOR_DB_PERSIST_DIR,
    LLM_INFERENCE_URL,
    LOCAL_EMBEDDING_MODEL_ID,
)
from ragengine.embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from ragengine.models import Document
from ragengine.vector_store.base import BaseVectorStore


class BaseVectorStoreTest(ABC):
    """Base class for vector store tests that defines the test structure."""

    @pytest.fixture(scope="session")
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
            Document(text=second_doc_text, metadata={"type": "text"}),
        ]

        doc_ids = await vector_store_manager.index_documents("test_index", documents)

        assert len(doc_ids) == 2
        assert set(doc_ids) == {
            BaseVectorStore.generate_doc_id(first_doc_text),
            BaseVectorStore.generate_doc_id(second_doc_text),
        }

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
        respx.post(LLM_INFERENCE_URL).mock(
            return_value=httpx.Response(200, json=mock_response)
        )

        documents = [
            Document(text="First document", metadata={"type": "text"}),
            Document(text="Second document", metadata={"type": "text"}),
        ]
        index_doc_resp = await vector_store_manager.index_documents(
            "test_index", documents
        )

        params = {"temperature": 0.7}
        query_result = await vector_store_manager.query(
            "test_index", "First", top_k=1, llm_params=params, rerank_params={}
        )

        assert query_result is not None
        assert (
            query_result["response"]
            == "{'result': 'This is the completion from the API'}"
        )
        assert query_result["source_nodes"][0]["text"] == "First document"
        assert query_result["source_nodes"][0]["score"] == pytest.approx(
            self.expected_query_score, rel=1e-6
        )
        assert query_result["source_nodes"][0]["doc_id"] == index_doc_resp[0]
        assert (
            respx.calls.call_count == 1
        )  # Ensure only one LLM inference request was made

    @pytest.mark.asyncio
    @respx.mock
    @patch("requests.get")
    async def test_chat_completions(self, mock_get, vector_store_manager, monkeypatch):
        import ragengine.config
        import ragengine.inference.inference

        monkeypatch.setattr(
            ragengine.config,
            "LLM_INFERENCE_URL",
            "http://localhost:5000/v1/chat/completions",
        )
        monkeypatch.setattr(
            ragengine.inference.inference,
            "LLM_INFERENCE_URL",
            "http://localhost:5000/v1/chat/completions",
        )
        mock_get.return_value.status_code = 200
        mock_get.return_value.json.return_value = {
            "data": [{"id": "mock-model", "max_model_len": 2048}]
        }

        mock_response = {
            "id": "chatcmpl-test123",
            "object": "chat.completion",
            "model": "mock-model",
            "choices": [
                {
                    "index": 0,
                    "message": {
                        "role": "assistant",
                        "content": "This is a helpful response about the test document.",
                    },
                    "finish_reason": "stop",
                }
            ],
        }
        respx.post("http://localhost:5000/v1/chat/completions").mock(
            return_value=httpx.Response(200, json=mock_response)
        )

        documents = [
            Document(text="First document", metadata={"type": "text"}),
            Document(text="Second document", metadata={"type": "text"}),
        ]
        index_doc_resp = await vector_store_manager.index_documents(
            "test_index", documents
        )

        chat_results = await vector_store_manager.chat_completion(
            {
                "index_name": "test_index",
                "model": "mock-model",
                "messages": [
                    {"role": "user", "content": "What is the first document?"}
                ],
                "top_k": 1,
                "temperature": 0.7,
                "max_tokens": 100,
            }
        )

        assert chat_results is not None
        assert chat_results.source_nodes is not None
        assert len(chat_results.source_nodes) == 1
        assert chat_results.source_nodes[0].text == "First document"
        assert chat_results.source_nodes[0].doc_id == index_doc_resp[0]
        assert chat_results.id is not None
        assert chat_results.model == "mock-model"
        assert chat_results.object == "chat.completion"
        assert chat_results.created is not None
        assert chat_results.choices is not None
        assert len(chat_results.choices) == 1
        assert chat_results.choices[0].finish_reason == "stop"
        assert chat_results.choices[0].index == 0
        assert chat_results.choices[0].message.role == "assistant"
        assert (
            chat_results.choices[0].message.content
            == "This is a helpful response about the test document."
        )

        assert (
            respx.calls.call_count == 1
        )  # Ensure only one LLM inference request was made

    @pytest.mark.asyncio
    async def test_add_document(self, vector_store_manager):
        documents = [Document(text="Third document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)

        new_document = [Document(text="Fourth document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", new_document)

        assert await vector_store_manager.document_exists(
            "test_index",
            new_document[0],
            BaseVectorStore.generate_doc_id("Fourth document"),
        )

    @pytest.mark.asyncio
    async def test_add_code_documents_with_code_splitting(self, vector_store_manager):
        sample_go_code = """package main
func main() {}"""
        documents = [
            Document(
                text=sample_go_code, metadata={"split_type": "code", "language": "go"}
            )
        ]
        result = await vector_store_manager.index_documents(
            "test_code_index", documents
        )

        assert await vector_store_manager.document_exists(
            "test_code_index", documents[0], result[0]
        )

        all_docs = await vector_store_manager.list_documents_in_index(
            "test_code_index", limit=10, offset=0
        )
        assert len(all_docs) == 1
        assert all_docs[0]["text"] == sample_go_code

        try:
            # Attempt to index a document with no language
            unsupported_doc = Document(
                text="Unsupported language code", metadata={"split_type": "code"}
            )
            await vector_store_manager.index_documents(
                "test_code_index", [unsupported_doc]
            )
            assert False, "Expected ValueError for unsupported language"
        except ValueError as e:
            assert str(e) == "Language not specified in node metadata."

        try:
            # Attempt to index a document with an invalid language
            unsupported_doc = Document(
                text="Unsupported language code",
                metadata={"split_type": "code", "language": "invalid"},
            )
            await vector_store_manager.index_documents(
                "test_code_index", [unsupported_doc]
            )
            assert False, "Expected ValueError for unsupported language"
        except LookupError:
            assert True

    @pytest.mark.asyncio
    @respx.mock
    @patch("requests.get")
    async def test_update_document(self, mock_get, vector_store_manager):
        mock_get.return_value.status_code = 200
        mock_get.return_value.json.return_value = {
            "data": [{"id": "mock-model", "max_model_len": 2048}]
        }

        mock_response = {"result": "This is the completion from the API"}
        respx.post(LLM_INFERENCE_URL).mock(
            return_value=httpx.Response(200, json=mock_response)
        )

        documents = [Document(text="Fifth document", metadata={"type": "text"})]
        ids = await vector_store_manager.index_documents("test_index", documents)

        result = await vector_store_manager.update_documents(
            "test_index",
            [
                Document(
                    doc_id=ids[0],
                    text="Updated Fifth document",
                    metadata={"type": "text"},
                )
            ],
        )
        assert result["updated_documents"][0].doc_id == ids[0]

        # Check preexisting unchanged document case
        result = await vector_store_manager.update_documents(
            "test_index",
            [
                Document(
                    doc_id=ids[0],
                    text="Updated Fifth document",
                    metadata={"type": "text"},
                )
            ],
        )
        assert result["unchanged_documents"][0].doc_id == ids[0]

        assert await vector_store_manager.document_exists(
            "test_index",
            Document(text="Updated Fifth document", metadata={"type": "text"}),
            ids[0],
        )

        # Check if the document was updated
        result = await vector_store_manager.query(
            "test_index",
            "Updated Fifth document",
            top_k=1,
            llm_params={},
            rerank_params={},
        )
        assert result["source_nodes"][0]["text"] == "Updated Fifth document"

        # Check documents not found case
        result = await vector_store_manager.update_documents(
            "test_index",
            [
                Document(
                    doc_id="baddocid",
                    text="Updated Fifth document",
                    metadata={"type": "text"},
                )
            ],
        )
        assert result["not_found_documents"][0].doc_id == "baddocid"

    @pytest.mark.asyncio
    @respx.mock
    @patch("requests.get")
    async def test_delete_document(self, mock_get, vector_store_manager):
        mock_get.return_value.status_code = 200
        mock_get.return_value.json.return_value = {
            "data": [{"id": "mock-model", "max_model_len": 2048}]
        }

        mock_response = {"result": "This is the completion from the API"}
        respx.post(LLM_INFERENCE_URL).mock(
            return_value=httpx.Response(200, json=mock_response)
        )

        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"}) for i in range(10)
        ]
        ids = await vector_store_manager.index_documents("test_index", documents)

        result = await vector_store_manager.delete_documents("test_index", ids)
        assert all(doc_id in result["deleted_doc_ids"] for doc_id in ids)

        # Check the not found case
        result = await vector_store_manager.delete_documents("test_index", ["baddocid"])
        assert result["not_found_doc_ids"] == ["baddocid"]

    @pytest.mark.asyncio
    async def test_add_document_on_existing_index(self, vector_store_manager):
        # Create index with single doc
        await vector_store_manager.index_documents(
            "test_add_index", [Document(text="Initial Doc", metadata={"type": "text"})]
        )

        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"}) for i in range(10)
        ]
        # load documents into the same index
        ids = await vector_store_manager.index_documents("test_add_index", documents)

        # Check if the documents were indexed correctly
        all_docs = await vector_store_manager.list_documents_in_index(
            "test_add_index", limit=10, offset=1
        )
        print(all_docs)

        # Validate id's from index_documents match the expected ids
        assert all(doc["doc_id"] == ids[idx] for idx, doc in enumerate(all_docs))

    @pytest.mark.asyncio
    async def test_persist_index(self, vector_store_manager):
        documents = [Document(text="Test document", metadata={"type": "text"})]
        await vector_store_manager.index_documents("test_index", documents)
        await vector_store_manager.persist("test_index", DEFAULT_VECTOR_DB_PERSIST_DIR)
        assert os.path.exists(DEFAULT_VECTOR_DB_PERSIST_DIR)

    @pytest.mark.asyncio
    async def test_delete_index(self, vector_store_manager):
        one_mb = b"\x00" * 1024
        documents = [
            Document(text=one_mb.decode(), metadata={"type": "text"}) for _ in range(10)
        ]
        await vector_store_manager.index_documents("test_index", documents)

        # Ensure index exists before deletion
        indexes = vector_store_manager.list_indexes()
        assert "test_index" in indexes

        await vector_store_manager.delete_index("test_index")

        # Ensure index is deleted
        indexes = vector_store_manager.list_indexes()
        assert "test_index" not in indexes

    @pytest.mark.asyncio
    async def test_list_documents_in_index(self, vector_store_manager):
        """Test various list document scenarios with different limit and offset values."""
        index_name = "test_index"
        # Create multiple documents
        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"}) for i in range(10)
        ]

        await vector_store_manager.index_documents(index_name, documents)

        # 1. Offset 0, Limit 5 (Basic Case)
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=5, offset=0
        )
        assert len(result) == 5

        # 2. Offset 5, Limit 5 (Next Batch)
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=5, offset=5
        )
        assert len(result) == 5

        # 3. Offset at max (Empty Case)
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=5, offset=10
        )
        assert index_name not in result or len(result) == 0

        # 4. Limit larger than available docs
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=15, offset=0
        )
        assert len(result) == 10  # Should return only available docs

        # 5. Limit exactly matches available docs
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=10, offset=0
        )
        assert len(result) == 10

        # 6. Limit of 1 (Single-Doc Retrieval)
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=1, offset=0
        )
        assert len(result) == 1

        # 7. max_text_length truncation check
        truncated_result = await vector_store_manager.list_documents_in_index(
            index_name, limit=1, offset=0, max_text_length=5
        )
        assert len(next(iter(truncated_result))["text"]) == 5  # Ensure truncation

        # 8. max_text_length is None (Full text should return)
        full_text_result = await vector_store_manager.list_documents_in_index(
            index_name, limit=1, offset=0, max_text_length=None
        )
        assert (
            "Document" in next(iter(full_text_result))["text"]
        )  # Ensure no truncation

    @pytest.mark.asyncio
    async def test_list_documents_with_filter_index(self, vector_store_manager):
        """Test various list document scenarios with different limit and offset values."""
        index_name = "test_index"
        # Create multiple documents
        documents = [
            Document(
                text=f"Document {i}",
                metadata={"type": "text", "filename": f"file_{i}", "branch": "main"},
            )
            for i in range(10)
        ]

        await vector_store_manager.index_documents(index_name, documents)

        # single filter
        result = await vector_store_manager.list_documents_in_index(
            index_name, limit=5, offset=0, metadata_filter={"filename": "file_1"}
        )
        assert len(result) == 1
        assert result[0]["metadata"]["filename"] == "file_1"

        first_five_results = await vector_store_manager.list_documents_in_index(
            index_name, limit=5, offset=0, metadata_filter={"branch": "main"}
        )
        assert len(first_five_results) == 5
        assert all(doc["metadata"]["branch"] == "main" for doc in first_five_results)

        second_five_results = await vector_store_manager.list_documents_in_index(
            index_name, limit=5, offset=5, metadata_filter={"branch": "main"}
        )
        assert len(second_five_results) == 5
        assert all(doc["metadata"]["branch"] == "main" for doc in second_five_results)
        assert first_five_results != second_five_results

        # multiple filters
        result = await vector_store_manager.list_documents_in_index(
            index_name,
            limit=5,
            offset=0,
            metadata_filter={"filename": "file_5", "branch": "main"},
        )
        assert len(result) == 1
        assert result[0]["metadata"]["filename"] == "file_5"
        assert result[0]["metadata"]["branch"] == "main"

        # no results
        result = await vector_store_manager.list_documents_in_index(
            index_name,
            limit=5,
            offset=0,
            metadata_filter={"filename": "file_15", "branch": "main"},
        )
        assert len(result) == 0

    @pytest.mark.asyncio
    async def test_persist_and_load_as_seperate_index(self, vector_store_manager):
        index_name, second_index_name = "test_index", "second_test_index"
        # Create multiple documents
        documents = [
            Document(
                text=f"Document {i}",
                metadata={"type": "text", "filename": f"file_{i}", "branch": "main"},
            )
            for i in range(10)
        ]

        await vector_store_manager.index_documents(index_name, documents)

        await vector_store_manager.persist(index_name, DEFAULT_VECTOR_DB_PERSIST_DIR)
        await vector_store_manager.load(
            second_index_name, DEFAULT_VECTOR_DB_PERSIST_DIR, overwrite=True
        )

        result = await vector_store_manager.list_documents_in_index(
            second_index_name, limit=5, offset=0
        )
        assert len(result) == 5

        # validate loaded index doesnt change the original index
        await vector_store_manager.delete_documents(
            second_index_name, [result[0]["doc_id"]]
        )

        first_index_result = await vector_store_manager.list_documents_in_index(
            index_name, limit=10, offset=0
        )
        second_index_result = await vector_store_manager.list_documents_in_index(
            second_index_name, limit=10, offset=0
        )
        assert len(first_index_result) == 10
        assert len(second_index_result) == 9

        second_index_result[0]["text"] = "Modified text"
        second_update_result = await vector_store_manager.update_documents(
            second_index_name,
            [
                Document(
                    doc_id=second_index_result[0]["doc_id"],
                    text="Modified text",
                    metadata=second_index_result[0]["metadata"],
                )
            ],
        )
        assert len(second_update_result["updated_documents"]) == 1
        assert second_update_result["updated_documents"][0].text == "Modified text"

        second_delete_result = await vector_store_manager.delete_documents(
            second_index_name, [second_index_result[0]["doc_id"]]
        )
        assert len(second_delete_result["deleted_doc_ids"]) == 1
        assert (
            second_delete_result["deleted_doc_ids"][0]
            == second_index_result[0]["doc_id"]
        )
