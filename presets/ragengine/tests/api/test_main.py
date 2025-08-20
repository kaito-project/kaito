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
from unittest.mock import patch

import httpx
import pytest
import respx

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
async def test_query_index_success(mock_get, async_client):
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "This is the completion from the API"}
    respx.post("http://localhost:5000/v1/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index Request
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"},
        ],
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Query Request
    request_data = {
        "index_name": "test_index",
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7},
    }

    response = await async_client.post("/query", json=request_data)
    assert response.status_code == 200
    assert (
        response.json()["response"]
        == "{'result': 'This is the completion from the API'}"
    )
    assert len(response.json()["source_nodes"]) == 1
    assert response.json()["source_nodes"][0]["text"] == "This is a test document"
    assert response.json()["source_nodes"][0]["score"] == pytest.approx(
        0.5354418754577637, rel=1e-6
    )
    assert response.json()["source_nodes"][0]["metadata"] == {}

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
                r'rag_query_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
async def test_document_update_success(mock_get, async_client):
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "This is the completion from the API"}
    respx.post("http://localhost:5000/v1/completions").mock(
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

    # Query Request
    request_data = {
        "index_name": "test_update_index",
        "query": "updates test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7},
    }

    response = await async_client.post("/query", json=request_data)
    assert response.status_code == 200
    assert (
        response.json()["response"]
        == "{'result': 'This is the completion from the API'}"
    )
    assert len(response.json()["source_nodes"]) == 1
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
                r'rag_query_requests_total{status="success"} ([1-9]\d*).0',
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
@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
async def test_reranker_and_query_with_index(mock_get, async_client):
    """
    Test reranker and query functionality with indexed documents.

    This test ensures the following:
    1. The custom reranker returns a relevance-sorted list of documents.
    2. The query response matches the expected format and contains the correct top results.

    Template for reranker input:
    A list of documents is shown below. Each document has a number next to it along with a summary of the document.
    A question is also provided. Respond with the numbers of the documents you should consult to answer the question,
    in order of relevance, as well as the relevance score. The relevance score is a number from 1-10 based on how
    relevant you think the document is to the question. Do not include any documents that are not relevant.

    Example format:
    Document 1: <summary of document 1>
    Document 2: <summary of document 2>
    ...
    Document 10: <summary of document 10>

    Question: <question>
    Answer:
    Doc: 9, Relevance: 7
    Doc: 3, Relevance: 4
    Doc: 7, Relevance: 3
    """
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for reranker and query API calls
    reranker_mock_response = {
        "choices": [{"text": "Doc: 4, Relevance: 10\nDoc: 5, Relevance: 10"}]
    }
    query_mock_response = {"choices": [{"text": "his is the completion from the API"}]}

    # Mock reranker response
    respx.post(
        "http://localhost:5000/v1/completions",
        content__contains="A list of documents is shown below",
    ).mock(return_value=httpx.Response(200, json=reranker_mock_response))

    # Mock query response
    respx.post("http://localhost:5000/v1/completions").mock(
        return_value=httpx.Response(200, json=query_mock_response)
    )

    # Define input documents for indexing
    documents = [
        "The capital of France is great.",
        "The capital of France is huge.",
        "The capital of France is beautiful.",
        "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
        "I really enjoyed my trip to Paris, France. The city is beautiful and the food is delicious. I would love to visit again. "
        "Such a great capital city.",
    ]

    # Indexing request payload
    index_request_payload = {
        "index_name": "test_index",
        "documents": [{"text": doc.strip()} for doc in documents],
    }

    # Perform indexing
    response = await async_client.post("/index", json=index_request_payload)
    assert response.status_code == 200
    index_response = response.json()

    # Query request payload with reranking
    top_n = 2  # The number of relevant docs returned in reranker response
    query_request_payload = {
        "index_name": "test_index",
        "query": "what is the capital of france?",
        "top_k": 5,
        "llm_params": {"temperature": 0, "max_tokens": 2000},
        "rerank_params": {"top_n": top_n},
    }

    # Perform query
    response = await async_client.post("/query", json=query_request_payload)
    assert response.status_code == 200
    query_response = response.json()

    # Validate query response
    assert len(query_response["source_nodes"]) == top_n

    # Validate each source node in the query response
    expected_source_nodes = [
        {
            "text": "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
            "score": 10.0,
            "metadata": {},
            "doc_id": index_response[3]["doc_id"],
        },
        {
            "text": "I really enjoyed my trip to Paris, France. The city is beautiful and the "
            "food is delicious. I would love to visit again. Such a great capital city.",
            "score": 10.0,
            "metadata": {},
            "doc_id": index_response[4]["doc_id"],
        },
    ]
    for i, expected_node in enumerate(expected_source_nodes):
        actual_node = query_response["source_nodes"][i]
        assert actual_node["text"] == expected_node["text"]
        assert actual_node["score"] == expected_node["score"]
        assert actual_node["metadata"] == expected_node["metadata"]
        assert actual_node["doc_id"] == expected_node["doc_id"]

    # Ensure HTTPX requests were made
    assert respx.calls.call_count == 2  # One for rerank, one for query completion

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
                r'rag_query_requests_total{status="success"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
async def test_reranker_failed_and_query_with_index(mock_get, async_client):
    """
    Test a failed reranker request with query.
    """
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for reranker and query API calls
    reranker_mock_response = {"choices": [{"text": "Empty Response"}]}

    # Mock reranker response
    respx.post(
        "http://localhost:5000/v1/completions",
        content__contains="A list of documents is shown below",
    ).mock(return_value=httpx.Response(200, json=reranker_mock_response))

    # Define input documents for indexing
    documents = [
        "The capital of France is great.",
        "The capital of France is huge.",
        "The capital of France is beautiful.",
        "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
        "I really enjoyed my trip to Paris, France. The city is beautiful and the food is delicious. I would love to visit again. "
        "Such a great capital city.",
    ]

    # Indexing request payload
    index_request_payload = {
        "index_name": "test_index",
        "documents": [{"text": doc.strip()} for doc in documents],
    }

    # Perform indexing
    response = await async_client.post("/index", json=index_request_payload)
    assert response.status_code == 200

    # Query request payload with reranking
    top_n = 2  # The number of relevant docs returned in reranker response
    query_request_payload = {
        "index_name": "test_index",
        "query": "what is the capital of france?",
        "top_k": 5,
        "llm_params": {"temperature": 0, "max_tokens": 2000},
        "rerank_params": {"top_n": top_n},
    }

    # Perform query
    response = await async_client.post("/query", json=query_request_payload)
    assert response.status_code == 422
    assert (
        response.content
        == b'{"detail":"Rerank operation failed: Invalid response from LLM. This feature is experimental."}'
    )

    # Ensure HTTPX requests were made
    assert respx.calls.call_count == 1  # One for rerank

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
                r'rag_query_requests_total{status="failure"} ([1-9]\d*).0',
                response.text,
            )
        )
        == 1
    )


@pytest.mark.asyncio
async def test_query_index_failure(async_client):
    # Prepare request data for querying.
    request_data = {
        "index_name": "non_existent_index",  # Use an index name that doesn't exist
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7},
    }

    response = await async_client.post("/query", json=request_data)
    assert response.status_code == 404
    assert response.json()["detail"] == "No such index: 'non_existent_index' exists."

    response = await async_client.get("/metrics")
    assert response.status_code == 200
    assert (
        len(
            re.findall(
                r'rag_query_requests_total{status="failure"} ([1-9]\d*).0',
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


"""
Example of a live query test. This test is currently commented out as it requires a valid 
INFERENCE_URL in config.py. To run the test, ensure that a valid INFERENCE_URL is provided. 
Upon execution, RAG results should be observed.

def test_live_query_test():
    # Index
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "Polar bear – can lift 450Kg (approximately 0.7 times their body weight) \
                Adult male polar bears can grow to be anywhere between 300 and 700kg"},
            {"text": "Giraffes are the tallest mammals and are well-adapted to living in trees. \
                They have few predators as adults."}
        ]
    }

    response = client.post("/index", json=request_data)
    assert response.status_code == 200

    # Query
    request_data = {
        "index_name": "test_index",
        "query": "What is the strongest bear?",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = client.post("/query", json=request_data)
    assert response.status_code == 200
"""
