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


import json
import os
import time
from tempfile import TemporaryDirectory
from unittest.mock import patch

import httpx
import pytest
import respx

from ragengine.config import DEFAULT_VECTOR_DB_PERSIST_DIR
from ragengine.models import Document
from ragengine.tests.vector_store.test_base_store import BaseVectorStoreTest
from ragengine.vector_store.qdrant_store import QdrantVectorStoreHandler


class TestQdrantVectorStore(BaseVectorStoreTest):
    """Test implementation for Qdrant vector store (in-memory mode)."""

    @pytest.fixture
    def vector_store_manager(self, init_embed_manager):
        with TemporaryDirectory() as temp_dir:
            print(f"Saving temporary test storage at: {temp_dir}")
            os.environ["PERSIST_DIR"] = temp_dir
            # Uses in-memory Qdrant by default (qdrant_url=None)
            yield QdrantVectorStoreHandler(init_embed_manager)

    @pytest.mark.asyncio
    async def check_indexed_documents(self, vector_store_manager):
        expected_output_1 = [
            Document(
                doc_id="",
                text="First document in index1",
                metadata={"type": "text"},
                hash_value="1e64a170be48c45efeaa8667ab35919106da0489ec99a11d0029f2842db133aa",
                is_truncated=False,
            )
        ]
        expected_output_2 = [
            Document(
                doc_id="",
                text="First document in index2",
                metadata={"type": "text"},
                hash_value="a222f875b83ce8b6eb72b3cae278b620de9bcc7c6b73222424d3ce979d1a463b",
                is_truncated=False,
            )
        ]

        for index, expected_output in zip(
            ["index1", "index2"], [expected_output_1, expected_output_2], strict=False
        ):
            resp = await vector_store_manager.list_documents_in_index(
                index, limit=10, offset=0, max_text_length=1000
            )

            assert all(
                resp_doc.text == expected_doc.text
                and resp_doc.hash_value == expected_doc.hash_value
                and resp_doc.metadata == expected_doc.metadata
                for resp_doc, expected_doc in zip(resp.documents, expected_output)
            )

    @property
    def expected_query_score(self):
        """Qdrant uses cosine similarity by default, scores differ from FAISS L2.

        Qdrant cosine similarity returns values in [0, 1] range where higher is
        more similar, unlike FAISS L2 where lower distance means more similar.
        Set to None to skip exact score comparison.
        """
        return None

    # ── Qdrant-specific test overrides ────────────────────────────

    @pytest.mark.asyncio
    @respx.mock
    @patch("requests.get")
    async def test_chat_completions(self, mock_get, vector_store_manager, monkeypatch):
        """Override: Qdrant hybrid search may return source_nodes in different order.

        Instead of asserting source_nodes[0].text == "First document", we verify
        both documents are present in the results regardless of order.
        """
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
                "temperature": 0.7,
                "max_tokens": 100,
            }
        )

        assert chat_results is not None
        assert chat_results.source_nodes is not None
        assert len(chat_results.source_nodes) == 2
        # Qdrant hybrid search may return results in different order than FAISS.
        # Verify both documents are present regardless of order.
        source_texts = {node.text for node in chat_results.source_nodes}
        source_doc_ids = {node.doc_id for node in chat_results.source_nodes}
        assert source_texts == {"First document", "Second document"}
        assert source_doc_ids == set(index_doc_resp)
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

        assert respx.calls.call_count == 1

        llm_req = respx.calls[0].request
        json_request = json.loads(llm_req.content)
        assert json_request["model"] == "mock-model"
        assert json_request["temperature"] == 0.7
        assert len(json_request["messages"]) == 2
        assert json_request["messages"][0]["role"] == "system"
        assert (
            "Use the context information below to assist the user."
            in json_request["messages"][0]["content"]
        )
        assert json_request["messages"][1]["role"] == "user"
        assert json_request["messages"][1]["content"] == "What is the first document?"

    @pytest.mark.asyncio
    @respx.mock
    @patch("requests.get")
    async def test_chat_completions_with_no_context(
        self, mock_get, vector_store_manager, monkeypatch
    ):
        """Override: Qdrant hybrid search may return low-relevance results.

        Unlike FAISS where unrelated queries return empty results (leading to
        source_nodes=None via passthrough), Qdrant hybrid search (dense + sparse)
        may still return results for unrelated queries. We verify the chat
        completion succeeds rather than asserting source_nodes is None.
        """
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
            "created": int(time.time()),
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
            Document(text="Cats and dogs are animals", metadata={"type": "text"}),
        ]
        await vector_store_manager.index_documents("test_index", documents)

        chat_results = await vector_store_manager.chat_completion(
            {
                "index_name": "test_index",
                "model": "mock-model",
                "messages": [{"role": "user", "content": "What is pasta made of?"}],
                "temperature": 0.7,
                "max_tokens": 100,
            }
        )

        assert chat_results is not None
        # Qdrant hybrid search may return low-relevance results instead of empty.
        # Accept both cases: source_nodes is None (passthrough) or not None (with context).
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

        assert respx.calls.call_count == 1

        llm_req = respx.calls[0].request
        json_request = json.loads(llm_req.content)
        assert json_request["model"] == "mock-model"
        assert json_request["temperature"] == 0.7
        if chat_results.source_nodes is None:
            # Passthrough case: no system message prepended
            assert len(json_request["messages"]) == 1
            assert json_request["messages"][0]["role"] == "user"
            assert json_request["messages"][0]["content"] == "What is pasta made of?"
        else:
            # Context case: system message with context prepended
            assert len(json_request["messages"]) == 2
            assert json_request["messages"][0]["role"] == "system"
            assert json_request["messages"][1]["role"] == "user"
            assert json_request["messages"][1]["content"] == "What is pasta made of?"

    @pytest.mark.asyncio
    async def test_add_document_on_existing_index(self, vector_store_manager):
        """Override: Qdrant docstore iteration order may differ from insertion order.

        Validates document IDs match as a set rather than asserting positional
        ordering, since Qdrant's docstore rebuild from scroll does not guarantee
        insertion-order iteration.
        """
        await vector_store_manager.index_documents(
            "test_add_index", [Document(text="Initial Doc", metadata={"type": "text"})]
        )

        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"}) for i in range(10)
        ]
        ids = await vector_store_manager.index_documents("test_add_index", documents)

        resp = await vector_store_manager.list_documents_in_index(
            "test_add_index", limit=10, offset=1
        )

        # Validate all IDs from index_documents are present (order may differ)
        resp_doc_ids = {doc.doc_id for doc in resp.documents}
        assert resp_doc_ids == set(ids)
        assert resp.total_items == 11

    @pytest.mark.asyncio
    async def test_persist_and_load_as_seperate_index(self, vector_store_manager):
        """Override: Qdrant in-memory mode persist/load has different vector storage.

        When persisting, only docstore/index_store go to disk — vectors stay in
        the original Qdrant collection. Loading creates a new collection without
        vectors, so delete_ref_doc may fail at the vector store level. The Qdrant
        handler gracefully falls back to docstore-only cleanup.
        """
        index_name, second_index_name = "test_index", "second_test_index"
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
        assert len(result.documents) == 5
        assert result.total_items == 10

        # Delete a document from the loaded index
        await vector_store_manager.delete_documents(
            second_index_name, [result.documents[0].doc_id]
        )

        first_index_result = await vector_store_manager.list_documents_in_index(
            index_name, limit=10, offset=0
        )
        second_index_result = await vector_store_manager.list_documents_in_index(
            second_index_name, limit=10, offset=0
        )
        # Original index is unchanged
        assert len(first_index_result.documents) == 10
        # Loaded index has one less document after delete
        assert len(second_index_result.documents) == 9

        # Update a document in the loaded index
        second_index_result.documents[0].text = "Modified text"
        second_update_result = await vector_store_manager.update_documents(
            second_index_name,
            [
                Document(
                    doc_id=second_index_result.documents[0].doc_id,
                    text="Modified text",
                    metadata=second_index_result.documents[0].metadata,
                )
            ],
        )
        assert len(second_update_result["updated_documents"]) == 1
        assert second_update_result["updated_documents"][0].text == "Modified text"

        # Delete another document
        second_delete_result = await vector_store_manager.delete_documents(
            second_index_name, [second_index_result.documents[0].doc_id]
        )
        assert len(second_delete_result["deleted_doc_ids"]) == 1
        assert (
            second_delete_result["deleted_doc_ids"][0]
            == second_index_result.documents[0].doc_id
        )
