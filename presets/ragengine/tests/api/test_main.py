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
import re
import time
from unittest.mock import patch

import httpx
import pytest
import respx

import ragengine
from ragengine.config import DEFAULT_VECTOR_DB_PERSIST_DIR

AUTO_GEN_DOC_ID_LEN = 64


@pytest.mark.asyncio
async def test_index_documents_success(async_client):
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert doc1["text"] == "This is a test document"
    assert len(doc1["doc_id"]) == AUTO_GEN_DOC_ID_LEN
    assert not doc1["metadata"]

    assert doc2["text"] == "Another test document"
    assert len(doc2["doc_id"]) == AUTO_GEN_DOC_ID_LEN
    assert not doc2["metadata"]

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_index_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
async def test_document_update_success(mock_get, async_client, monkeypatch):
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
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {
        "id": "chatcmpl-test123",
        "object": "chat.completion",
        "created": int(time.time()),
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
        "usage": {"prompt_tokens": 25, "completion_tokens": 12, "total_tokens": 37},
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index Request
    request_data = {
        "index_name": "test_update_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert doc2["doc_id"] != ""

    doc2["text"] = "This is an updated test document"
    not_existing_doc = {
        "doc_id": "nonexistingdoc",
        "text": "This is a new test document",
    }
    update_request_data = {
        "documents": [doc2, not_existing_doc, doc1],
    }
    response = await async_client.post(
        "/indexes/test_update_index/documents", json=update_request_data
    )
    assert response.status_code == 200
    assert (
        response.json()["updated_documents"][0]["text"]
        == "This is an updated test document"
    )
    assert (
        response.json()["not_found_documents"][0]["doc_id"]
        == not_existing_doc["doc_id"]
    )
    assert response.json()["unchanged_documents"][0]["text"] == doc1["text"]

    # Test chat completion request
    chat_request = {
        "index_name": "test_update_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "updates test query"}],
        "temperature": 0.7,
        "max_tokens": 50,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    print(response.json())
    assert response.status_code == 200

    response_data = response.json()
    assert "source_nodes" in response_data
    assert len(response_data["source_nodes"]) == 2
    assert (
        response.json()["source_nodes"][0]["text"] == "This is an updated test document"
    )
    assert response.json()["source_nodes"][0]["score"] == pytest.approx(
        0.48061275482177734, rel=1e-6
    )
    assert response.json()["source_nodes"][0]["metadata"] == {}
    assert respx.calls.call_count == 1

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_index_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_chat_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_indexes_update_document_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_document_delete_success(async_client):
    # Index Request
    request_data = {
        "index_name": "test_delete_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert doc2["doc_id"] != ""

    delete_request_data = {
        "doc_ids": [doc2["doc_id"], "nonexistingdoc"],
    }
    response = await async_client.post(
        "/indexes/test_delete_index/documents/delete", json=delete_request_data
    )
    assert response.status_code == 200
    assert response.json()["deleted_doc_ids"] == [doc2["doc_id"]]
    assert response.json()["not_found_doc_ids"] == ["nonexistingdoc"]

    response = await async_client.get("/indexes/test_delete_index/documents")
    assert response.status_code == 200
    assert response.json()["count"] == 1
    assert len(response.json()["documents"]) == 1
    assert response.json()["documents"][0]["text"] == "This is a test document"

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_index_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_indexes_delete_document_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_indexes_document_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_list_documents_in_index_success(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {"detail": "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()

    # Retrieve documents for the specific index
    response = await async_client.get(f"/indexes/{index_name}/documents")
    assert response.status_code == 200
    response_json = response.json()

    # Ensure documents exist correctly in the specific index
    assert response_json["count"] == 2
    assert response_json["total_items"] == 2
    assert len(response_json["documents"]) == 2
    assert all(
        (
            (item["doc_id"] == doc1["doc_id"] and item["text"] == doc1["text"])
            or (item["doc_id"] == doc2["doc_id"] and item["text"] == doc2["text"])
        )
        for item in response_json["documents"]
    )

    assert {item["text"] for item in response_json["documents"]} == {
        item["text"] for item in request_data["documents"]
    }


@pytest.mark.asyncio
async def test_list_documents_with_metadata_filter_success(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {"detail": "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {
                "text": "This is a test document",
                "metadata": {"filename": "test.txt", "branch": "main"},
            },
            {
                "text": "Another test document",
                "metadata": {"filename": "main.py", "branch": "main"},
            },
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Retrieve documents for the specific index
    filters = {
        "filename": "test.txt",
    }
    response = await async_client.get(
        f"/indexes/{index_name}/documents?metadata_filter={json.dumps(filters)}"
    )
    assert response.status_code == 200
    response_json = response.json()

    # Ensure documents exist correctly in the specific index
    assert response_json["count"] == 1
    assert len(response_json["documents"]) == 1
    assert response_json["documents"][0]["text"] == "This is a test document"


@pytest.mark.asyncio
async def test_list_documents_with_metadata_filter_failure(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {"detail": "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {
                "text": "This is a test document",
                "metadata": {"filename": "test.txt", "branch": "main"},
            },
            {
                "text": "Another test document",
                "metadata": {"filename": "main.py", "branch": "main"},
            },
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    response = await async_client.get(
        f"/indexes/{index_name}/documents?metadata_filter=invalidjsonstring"
    )
    assert response.status_code == 400


@pytest.mark.asyncio
async def test_persist_documents(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {"detail": "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Persist documents for the specific index
    response = await async_client.post(f"/persist/{index_name}")
    assert response.status_code == 200
    response_json = response.json()
    assert response_json == {
        "message": f"Successfully persisted index {index_name} to {DEFAULT_VECTOR_DB_PERSIST_DIR}/{index_name}."
    }
    assert os.path.exists(os.path.join(DEFAULT_VECTOR_DB_PERSIST_DIR, index_name))

    # Persist documents for the specific index at a custom path
    custom_path = "./custom_test_path"
    response = await async_client.post(f"/persist/{index_name}?path={custom_path}")
    assert response.status_code == 200
    response_json = response.json()
    assert response_json == {
        "message": f"Successfully persisted index {index_name} to {custom_path}."
    }
    assert os.path.exists(custom_path)

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_index_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_indexes_document_requests_total{status="failure"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_persist_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_load_documents(async_client):
    index_name = "test_index"
    response = await async_client.post(
        f"/load/{index_name}?path={DEFAULT_VECTOR_DB_PERSIST_DIR}/{index_name}"
    )

    assert response.status_code == 200
    assert response.json() == {
        "message": "Successfully loaded index test_index from storage/test_index."
    }

    response = await async_client.get("/indexes")
    assert response.status_code == 200
    assert response.json() == [index_name]

    response = await async_client.get("/indexes/test_index/documents")
    assert response.status_code == 200
    response_data = response.json()

    assert response_data["count"] == 2
    assert len(response_data["documents"]) == 2
    assert response_data["documents"][0]["text"] == "This is a test document"
    assert response_data["documents"][1]["text"] == "Another test document"

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_load_requests_total{status="success"} ([1-9]\d*).0', response.text
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_indexes_document_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_indexes_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_delete_index(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {"detail": "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Delete the index
    response = await async_client.delete(f"/indexes/{index_name}")
    assert response.status_code == 200
    response_json = response.json()
    assert response_json == {"message": f"Successfully deleted index {index_name}."}

    # Ensure index deleted
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {"detail": "No such index: 'test_index' exists."}

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_indexes_document_requests_total{status="failure"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_delete_index_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )
    assert (
        len(
            re.findall(
                r'rag_index_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_retrieve_success(async_client):
    """Test successful retrieval of documents from an index."""
    index_name = "test_retrieve_index"

    # Create index with documents (need 20+ docs to support hybrid retriever's 3x multiplier)
    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "Python is a programming language", "metadata": {"category": "tech"}},
            {"text": "JavaScript is used for web development", "metadata": {"category": "tech"}},
            {"text": "Java is an object-oriented language", "metadata": {"category": "tech"}},
            {"text": "C++ is a systems programming language", "metadata": {"category": "tech"}},
            {"text": "Ruby is a dynamic language", "metadata": {"category": "tech"}},
            {"text": "Go is a compiled language", "metadata": {"category": "tech"}},
            {"text": "Rust is a memory-safe language", "metadata": {"category": "tech"}},
            {"text": "Swift is used for iOS development", "metadata": {"category": "tech"}},
            {"text": "Kotlin is used for Android development", "metadata": {"category": "tech"}},
            {"text": "TypeScript is JavaScript with types", "metadata": {"category": "tech"}},
            {"text": "PHP is a server-side language", "metadata": {"category": "tech"}},
            {"text": "Scala is a functional language", "metadata": {"category": "tech"}},
            {"text": "Haskell is a pure functional language", "metadata": {"category": "tech"}},
            {"text": "Erlang is used for distributed systems", "metadata": {"category": "tech"}},
            {"text": "Elixir is built on the Erlang VM", "metadata": {"category": "tech"}},
            {"text": "Clojure is a Lisp dialect", "metadata": {"category": "tech"}},
            {"text": "F# is a functional-first language", "metadata": {"category": "tech"}},
            {"text": "OCaml is a functional language", "metadata": {"category": "tech"}},
            {"text": "Dart is used for Flutter development", "metadata": {"category": "tech"}},
            {"text": "Lua is a lightweight scripting language", "metadata": {"category": "tech"}},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Test retrieve with default parameters
    retrieve_request = {
        "index_name": index_name,
        "query": "What is Python?",
    }

    response = await async_client.post("/retrieve", json=retrieve_request)
    assert response.status_code == 200
    response_data = response.json()

    # Verify response structure
    assert "query" in response_data
    assert "results" in response_data
    assert "count" in response_data
    assert response_data["query"] == "What is Python?"
    assert response_data["count"] <= 5  # Default max_node_count is 5
    assert len(response_data["results"]) == response_data["count"]

    # Verify result structure
    if response_data["count"] > 0:
        first_result = response_data["results"][0]
        assert "doc_id" in first_result
        assert "node_id" in first_result
        assert "text" in first_result
        assert "score" in first_result
        assert "metadata" in first_result

    # Verify metrics
    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_indexes_retrieve_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_retrieve_with_custom_max_node_count(async_client):
    """Test retrieve with custom max_node_count parameter."""
    index_name = "test_retrieve_max_count_index"

    # Create index with documents
    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": f"Document {i} about technology", "metadata": {"index": i}}
            for i in range(30)
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Test retrieve with custom max_node_count
    retrieve_request = {
        "index_name": index_name,
        "query": "technology",
        "max_node_count": 3,
    }

    response = await async_client.post("/retrieve", json=retrieve_request)
    assert response.status_code == 200
    response_data = response.json()

    assert response_data["count"] <= 3
    assert len(response_data["results"]) <= 3


@pytest.mark.asyncio
async def test_retrieve_with_metadata_filter(async_client):
    """Test retrieve with metadata filtering."""
    index_name = "test_retrieve_filter_index"

    # Create index with documents with different categories
    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": f"Tech document {i}", "metadata": {"category": "tech", "index": i}}
            for i in range(15)
        ]
        + [
            {"text": f"Science document {i}", "metadata": {"category": "science", "index": i}}
            for i in range(15)
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Test retrieve with metadata filter
    retrieve_request = {
        "index_name": index_name,
        "query": "document",
        "max_node_count": 5,
        "metadata_filter": {"category": "tech"},
    }

    response = await async_client.post("/retrieve", json=retrieve_request)
    assert response.status_code == 200
    response_data = response.json()

    assert response_data["count"] <= 5
    # Verify all results have the correct category
    for result in response_data["results"]:
        assert result["metadata"]["category"] == "tech"


@pytest.mark.asyncio
async def test_retrieve_nonexistent_index(async_client):
    """Test retrieve from non-existent index."""
    retrieve_request = {
        "index_name": "nonexistent_index",
        "query": "test query",
    }

    response = await async_client.post("/retrieve", json=retrieve_request)
    assert response.status_code == 404
    assert "No such index" in response.json()["detail"]


@pytest.mark.asyncio
async def test_retrieve_empty_query(async_client):
    """Test retrieve with empty query string."""
    index_name = "test_empty_query_index"

    # Create a simple index
    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "Test document", "metadata": {}},
        ] * 15,
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Test retrieve with empty query
    retrieve_request = {
        "index_name": index_name,
        "query": "",
    }

    response = await async_client.post("/retrieve", json=retrieve_request)
    # The base exception handler wraps HTTPException as 500
    assert response.status_code == 500
    assert "empty" in response.json()["detail"].lower()
