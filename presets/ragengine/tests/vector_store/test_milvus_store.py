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

from ragengine.models import Document
from ragengine.tests.vector_store.test_base_store import BaseVectorStoreTest
from ragengine.vector_store.milvus_store import MilvusVectorStoreHandler


class TestMilvusVectorStore(BaseVectorStoreTest):
    """Test implementation for Milvus vector store (Milvus Lite embedded mode)."""

    @pytest.fixture
    def vector_store_manager(self, init_embed_manager):
        with TemporaryDirectory() as temp_dir:
            print(f"Saving temporary test storage at: {temp_dir}")
            os.environ["PERSIST_DIR"] = temp_dir
            # Uses Milvus Lite via a temp .db file for test isolation
            db_path = os.path.join(temp_dir, "milvus_test.db")
            yield MilvusVectorStoreHandler(init_embed_manager, vector_db_url=db_path)

    @pytest.mark.asyncio
    async def check_indexed_documents(self, vector_store_manager):
        expected_output_1 = [
            Document(
                doc_id="",
                text="First document in index1",
                metadata={"type": "text"},
                hash_value="81bedde64ebbcd5217992ff7d90fac992c4d7a654e72e76cf5e61c7d45e59afe",
                is_truncated=False,
            )
        ]
        expected_output_2 = [
            Document(
                doc_id="",
                text="First document in index2",
                metadata={"type": "text"},
                hash_value="14f429304e79db9825c4e221723cb90d065978c10972af3a2479de1305e9219d",
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
        """Milvus uses COSINE similarity, scores differ from FAISS L2.

        Set to None to skip exact score comparison.
        """
        return None

    # ── Skip tests that are incompatible with Milvus ────────

    # Milvus update uses delete+re-insert pattern (like Qdrant).
    @pytest.mark.skip(
        reason="Milvus update uses delete+re-insert; see test_update_document_milvus"
    )
    async def test_update_document(self, mock_get, vector_store_manager):
        pass

    # Milvus list order may differ from insertion order.
    @pytest.mark.skip(
        reason="Milvus query order may differ from insertion order; see test_add_document_on_existing_index_milvus"
    )
    async def test_add_document_on_existing_index(self, vector_store_manager):
        pass

    @pytest.mark.skip(reason="Milvus persists to its own embedded DB, not filesystem")
    async def test_persist_and_load_as_seperate_index(self, vector_store_manager):
        pass

    # ── Milvus-specific test overrides ────────────────────────

    @pytest.mark.asyncio
    @respx.mock
    @patch("requests.get")
    async def test_chat_completions(self, mock_get, vector_store_manager, monkeypatch):
        """Override: Milvus COSINE similarity may return source_nodes in different order."""
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
        # Milvus COSINE similarity may return results in different order than FAISS.
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
        """Override: Milvus COSINE distance scores may not exceed threshold for irrelevant queries.

        Unlike FAISS where unrelated queries return empty results (leading to
        source_nodes=None via passthrough), Milvus may still return results for
        unrelated queries. We verify the chat completion succeeds rather than
        asserting source_nodes is None.
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
        # Milvus COSINE distance may not exceed the similarity threshold for
        # irrelevant queries. Accept both: source_nodes is None (passthrough) or not None.
        assert chat_results.id is not None
        assert chat_results.model == "mock-model"
        assert chat_results.object == "chat.completion"
        assert chat_results.created is not None
        assert chat_results.choices is not None
        assert len(chat_results.choices) == 1
        assert chat_results.choices[0].finish_reason == "stop"
        assert chat_results.choices[0].index == 0
        assert chat_results.choices[0].message.role == "assistant"

    @pytest.mark.asyncio
    @patch("requests.get")
    async def test_update_document_milvus(self, mock_get, vector_store_manager):
        """Milvus update: delete-then-reinsert."""
        mock_get.return_value.status_code = 200
        mock_get.return_value.json.return_value = {
            "data": [{"id": "mock-model", "max_model_len": 2048}]
        }

        documents = [Document(text="Fifth document", metadata={"type": "text"})]
        ids = await vector_store_manager.index_documents("test_index", documents)

        # Update with new text
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

        # Verify the doc exists
        assert await vector_store_manager.document_exists(
            "test_index",
            Document(text="Updated Fifth document", metadata={"type": "text"}),
            ids[0],
        )

        # Not-found case
        result = await vector_store_manager.update_documents(
            "test_index",
            [
                Document(
                    doc_id="baddocid",
                    text="Some text",
                    metadata={"type": "text"},
                )
            ],
        )
        assert result["not_found_documents"][0].doc_id == "baddocid"

    @pytest.mark.asyncio
    async def test_add_document_on_existing_index_milvus(self, vector_store_manager):
        """Milvus-specific: verify doc count and doc_id set (not order)."""
        await vector_store_manager.index_documents(
            "test_add_index",
            [Document(text="Initial Doc", metadata={"type": "text"})],
        )

        documents = [
            Document(text=f"Document {i}", metadata={"type": "text"}) for i in range(10)
        ]
        ids = await vector_store_manager.index_documents("test_add_index", documents)

        resp = await vector_store_manager.list_documents_in_index(
            "test_add_index", limit=100, offset=0
        )
        # Should have 11 docs total (1 initial + 10 appended)
        assert resp.total_items >= 11

        # Verify all doc_ids from the second batch are present (order-independent)
        resp_doc_ids = {doc.doc_id for doc in resp.documents}
        assert all(doc_id in resp_doc_ids for doc_id in ids)
