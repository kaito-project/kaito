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


from unittest.mock import patch, ANY

from llama_index.core.storage.index_store import SimpleIndexStore

from ragengine.main import app, vector_store_handler, rag_ops
from ragengine.config import DEFAULT_VECTOR_DB_PERSIST_DIR

import os

import pytest
import pytest_asyncio
import asyncio
import httpx
import respx
import json
import re
import time

AUTO_GEN_DOC_ID_LEN = 64

@pytest_asyncio.fixture
async def async_client():
    """Use an async HTTP client to interact with FastAPI app."""
    async with httpx.AsyncClient(app=app, base_url="http://localhost") as client:
        yield client

@pytest_asyncio.fixture(scope="session")
def event_loop():
    loop = asyncio.new_event_loop()
    yield loop
    loop.close()

@pytest_asyncio.fixture(autouse=True)
def clear_index():
    vector_store_handler.index_map.clear()

@pytest.mark.asyncio
async def test_index_documents_success(async_client):
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert (doc1["text"] == "This is a test document")
    assert len(doc1["doc_id"]) == AUTO_GEN_DOC_ID_LEN
    assert not doc1["metadata"]

    assert (doc2["text"] == "Another test document")
    assert len(doc2["doc_id"]) == AUTO_GEN_DOC_ID_LEN
    assert not doc2["metadata"]

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

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
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index Request
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Query Request
    request_data = {
        "index_name": "test_index",
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = await async_client.post("/query", json=request_data)
    assert response.status_code == 200
    assert response.json()["response"] == "{'result': 'This is the completion from the API'}"
    assert len(response.json()["source_nodes"]) == 1
    assert response.json()["source_nodes"][0]["text"] == "This is a test document"
    assert response.json()["source_nodes"][0]["score"] == pytest.approx(0.5354418754577637, rel=1e-6)
    assert response.json()["source_nodes"][0]["metadata"] == {}

    # Ensure HTTPX was called once
    assert respx.calls.call_count == 1

    # Ensure the model fetch was called once
    mock_get.assert_called_once_with("http://localhost:5000/v1/models", headers=ANY)

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_query_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
async def test_chat_completions_failure(mock_get, async_client):
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "This is the completion from the API"}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index Request
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Query Request
    request_data = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "What is RAG?"}
        ],
        "temperature": 0.7,
        "max_tokens": 2048,
        "top_k": 1,
    }

    response = await async_client.post("/v1/chat/completions", json=request_data)
    assert response.status_code == 200
    assert response.json()["choices"][0]["message"]["content"] == "{'result': 'This is the completion from the API'}"
    assert len(response.json()["source_nodes"]) == 1
    assert response.json()["source_nodes"][0]["text"] == "This is a test document"
    assert response.json()["source_nodes"][0]["score"] == pytest.approx(0.9756928086280823, rel=1e-6)
    assert response.json()["source_nodes"][0]["metadata"] == {}

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_chat_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
async def test_document_update_success(mock_get, async_client):
    #Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "This is the completion from the API"}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index Request
    request_data = {
        "index_name": "test_update_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert doc2["doc_id"] != ""

    doc2["text"] = "This is an updated test document"
    not_existing_doc = {"doc_id": "nonexistingdoc", "text": "This is a new test document"}
    update_request_data = {
        "documents": [
            doc2,
            not_existing_doc,
            doc1
        ],
    }
    response = await async_client.post(f"/indexes/test_update_index/documents", json=update_request_data)
    assert response.status_code == 200
    assert response.json()["updated_documents"][0]["text"] == "This is an updated test document"
    assert response.json()["not_found_documents"][0]["doc_id"] == not_existing_doc["doc_id"]
    assert response.json()["unchanged_documents"][0]["text"] == doc1["text"]

    # Query Request
    request_data = {
        "index_name": "test_update_index",
        "query": "updates test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = await async_client.post("/query", json=request_data)
    assert response.status_code == 200
    assert response.json()["response"] == "{'result': 'This is the completion from the API'}"
    assert len(response.json()["source_nodes"]) == 1
    assert response.json()["source_nodes"][0]["text"] == "This is an updated test document"
    assert response.json()["source_nodes"][0]["score"] == pytest.approx(0.48061275482177734, rel=1e-6)
    assert response.json()["source_nodes"][0]["metadata"] == {}

    assert respx.calls.call_count == 1

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_query_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_indexes_update_document_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
async def test_document_delete_success(async_client):
    # Index Request
    request_data = {
        "index_name": "test_delete_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert doc2["doc_id"] != ""


    delete_request_data = {
        "doc_ids": [doc2["doc_id"], "nonexistingdoc"],
    }
    response = await async_client.post(f"/indexes/test_delete_index/documents/delete", json=delete_request_data)
    assert response.status_code == 200
    assert response.json()["deleted_doc_ids"] == [doc2["doc_id"]]
    assert response.json()["not_found_doc_ids"] == ["nonexistingdoc"]

    response = await async_client.get(f"/indexes/test_delete_index/documents")
    assert response.status_code == 200
    assert response.json()["count"] == 1
    assert len(response.json()["documents"]) == 1
    assert response.json()["documents"][0]["text"] == "This is a test document"

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_indexes_delete_document_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_indexes_document_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

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
    reranker_mock_response = {"choices": [{"text": "Doc: 4, Relevance: 10\nDoc: 5, Relevance: 10"}]}
    query_mock_response = {"choices": [{"text": "his is the completion from the API"}]}

    # Mock reranker response
    respx.post("http://localhost:5000/v1/completions", content__contains="A list of documents is shown below") \
        .mock(return_value=httpx.Response(200, json=reranker_mock_response))

    # Mock query response
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=query_mock_response))

    # Define input documents for indexing
    documents = [
        "The capital of France is great.",
        "The capital of France is huge.",
        "The capital of France is beautiful.",
        "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
        "I really enjoyed my trip to Paris, France. The city is beautiful and the food is delicious. I would love to visit again. "
        "Such a great capital city."
    ]

    # Indexing request payload
    index_request_payload = {
        "index_name": "test_index",
        "documents": [{"text": doc.strip()} for doc in documents]
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
        "rerank_params": {"top_n": top_n}
    }

    # Perform query
    response = await async_client.post("/query", json=query_request_payload)
    assert response.status_code == 200
    query_response = response.json()

    # Validate query response
    assert len(query_response["source_nodes"]) == top_n

    # Validate each source node in the query response
    expected_source_nodes = [
        {"text": "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
         "score": 10.0, "metadata": {}, "doc_id": index_response[3]["doc_id"]},
        {"text": "I really enjoyed my trip to Paris, France. The city is beautiful and the "
                 "food is delicious. I would love to visit again. Such a great capital city.",
         "score": 10.0, "metadata": {}, "doc_id": index_response[4]["doc_id"]},
    ]
    for i, expected_node in enumerate(expected_source_nodes):
        actual_node = query_response["source_nodes"][i]
        assert actual_node["text"] == expected_node["text"]
        assert actual_node["score"] == expected_node["score"]
        assert actual_node["metadata"] == expected_node["metadata"]
        assert actual_node["doc_id"] == expected_node["doc_id"]

    # Ensure HTTPX requests were made
    assert respx.calls.call_count == 2  # One for rerank, one for query completion

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_query_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

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
    respx.post("http://localhost:5000/v1/completions", content__contains="A list of documents is shown below") \
        .mock(return_value=httpx.Response(200, json=reranker_mock_response))

    # Define input documents for indexing
    documents = [
        "The capital of France is great.",
        "The capital of France is huge.",
        "The capital of France is beautiful.",
        "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
        "I really enjoyed my trip to Paris, France. The city is beautiful and the food is delicious. I would love to visit again. "
        "Such a great capital city."
    ]

    # Indexing request payload
    index_request_payload = {
        "index_name": "test_index",
        "documents": [{"text": doc.strip()} for doc in documents]
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
        "rerank_params": {"top_n": top_n}
    }

    # Perform query
    response = await async_client.post("/query", json=query_request_payload)
    assert response.status_code == 422
    assert response.content == b'{"detail":"Rerank operation failed: Invalid response from LLM. This feature is experimental."}'

    # Ensure HTTPX requests were made
    assert respx.calls.call_count == 1  # One for rerank

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_query_requests_total{status="failure"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
async def test_query_index_failure(async_client):
    # Prepare request data for querying.
    request_data = {
        "index_name": "non_existent_index",  # Use an index name that doesn't exist
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = await async_client.post("/query", json=request_data)
    assert response.status_code == 404
    assert response.json()["detail"] == "No such index: 'non_existent_index' exists."

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_query_requests_total{status="failure"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
async def test_list_documents_in_index_success(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {'detail': "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
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
    assert all(((item['doc_id'] == doc1['doc_id'] and item['text'] == doc1['text']) or
               (item['doc_id'] == doc2['doc_id'] and item['text'] == doc2['text'])) for item in response_json["documents"])
    
    assert ({item["text"] for item in response_json["documents"]}
            == {item["text"] for item in request_data["documents"]})

@pytest.mark.asyncio
async def test_list_documents_with_metadata_filter_success(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {'detail': "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document", "metadata": {"filename": "test.txt", "branch": "main"}},
            {"text": "Another test document", "metadata": {"filename": "main.py", "branch": "main"}}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Retrieve documents for the specific index
    filters = {
        "filename": "test.txt",
    }
    response = await async_client.get(f"/indexes/{index_name}/documents?metadata_filter={json.dumps(filters)}")
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
    assert response.json() == {'detail': "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document", "metadata": {"filename": "test.txt", "branch": "main"}},
            {"text": "Another test document", "metadata": {"filename": "main.py", "branch": "main"}}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    response = await async_client.get(f"/indexes/{index_name}/documents?metadata_filter=invalidjsonstring")
    assert response.status_code == 400

@pytest.mark.asyncio
async def test_persist_documents(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {'detail': "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = await async_client.post("/index", json=request_data)
    assert response.status_code == 200

    # Persist documents for the specific index
    response = await async_client.post(f"/persist/{index_name}")
    assert response.status_code == 200
    response_json = response.json()
    assert response_json == {"message": f"Successfully persisted index {index_name} to {DEFAULT_VECTOR_DB_PERSIST_DIR}/{index_name}."}
    assert os.path.exists(os.path.join(DEFAULT_VECTOR_DB_PERSIST_DIR, index_name))

    # Persist documents for the specific index at a custom path
    custom_path = "./custom_test_path"
    response = await async_client.post(f"/persist/{index_name}?path={custom_path}")
    assert response.status_code == 200
    response_json = response.json()
    assert response_json == {"message": f"Successfully persisted index {index_name} to {custom_path}."}
    assert os.path.exists(custom_path)

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_indexes_document_requests_total{status="failure"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_persist_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
async def test_load_documents(async_client):
    index_name = "test_index"
    response = await async_client.post(f"/load/{index_name}?path={DEFAULT_VECTOR_DB_PERSIST_DIR}/{index_name}")

    assert response.status_code == 200
    assert response.json() == {'message': 'Successfully loaded index test_index from storage/test_index.'}

    response = await async_client.get(f"/indexes")
    assert response.status_code == 200
    assert response.json() == [index_name]

    response = await async_client.get(f"/indexes/test_index/documents")
    assert response.status_code == 200
    response_data = response.json()

    assert response_data["count"] == 2
    assert len(response_data["documents"]) == 2
    assert response_data["documents"][0]["text"] == "This is a test document"
    assert response_data["documents"][1]["text"] == "Another test document"

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_load_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_indexes_document_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_indexes_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

@pytest.mark.asyncio
async def test_delete_index(async_client):
    index_name = "test_index"

    # Ensure no documents are present initially
    response = await async_client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 404
    assert response.json() == {'detail': "No such index: 'test_index' exists."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
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
    assert response.json() == {'detail': "No such index: 'test_index' exists."}

    response = await async_client.get(f"/metrics")
    assert response.status_code == 200
    assert len(re.findall(r'rag_indexes_document_requests_total{status="failure"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_delete_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1
    assert len(re.findall(r'rag_index_requests_total{status="success"} ([1-9]\d*).0', response.text)) == 1

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


# ==========================

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_basic_success(mock_get, async_client):
    """Test basic successful chat completion with RAG functionality."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "This is a helpful response about the test document."}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document about AI and machine learning."},
            {"text": "Another document discussing natural language processing."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion request
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "What can you tell me about AI?"}
        ],
        "temperature": 0.7,
        "max_tokens": 100,
        "top_k": 2
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert "id" in response_data
    assert response_data["object"] == "chat.completion"
    assert "created" in response_data
    assert response_data["model"] == "mock-model"
    assert len(response_data["choices"]) == 1
    assert response_data["choices"][0]["message"]["role"] == "assistant"
    assert response_data["choices"][0]["message"]["content"] == "{'result': 'This is a helpful response about the test document.'}"
    assert response_data["choices"][0]["finish_reason"] == "stop"
    assert response_data["choices"][0]["index"] == 0
    assert "source_nodes" in response_data
    assert len(response_data["source_nodes"]) > 0

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_without_index_name(mock_get, async_client):
    """Test chat completion request without index_name (should passthrough to LLM)."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call
    mock_response = {
        "id": "chatcmpl-test123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "This is a direct LLM response"},
            "finish_reason": "stop"
        }]
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Test request without index_name (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Hello, how are you?"}
        ],
        "temperature": 0.7,
        "max_tokens": 100
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["id"] == "chatcmpl-test123"
    assert response_data["choices"][0]["message"]["content"] == "This is a direct LLM response"
    # Should have source_nodes field but it should be None for passthrough requests
    assert response_data["source_nodes"] is None

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_with_tools(mock_get, async_client):
    """Test chat completion with tools (should passthrough to LLM)."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call
    mock_response = {
        "id": "chatcmpl-tools123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [{
            "index": 0,
            "message": {
                "role": "assistant", 
                "content": None,
                "tool_calls": [{"id": "call123", "type": "function", "function": {"name": "test_tool", "arguments": '{"param1": "value1"}'}}]
            },
            "finish_reason": "tool_calls"
        }]
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Test request with tools (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Use a tool to help me"}
        ],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "test_tool",
                    "description": "A test tool",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "param1": {"type": "string", "description": "A test parameter"}
                        },
                    }
                }
            }
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["finish_reason"] == "tool_calls"
    assert "tool_calls" in response_data["choices"][0]["message"]

@pytest.mark.asyncio
async def test_chat_completions_nonexistent_index(async_client):
    """Test chat completion with non-existent index."""
    chat_request = {
        "index_name": "nonexistent_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Test question"}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 404
    assert "No such index: 'nonexistent_index' exists" in response.json()["detail"]

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_invalid_request_format(mock_get, async_client):
    """Test chat completion with invalid request format."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call (in case it gets that far)
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(400, json={"error": "Invalid request"}))

    # Test missing messages
    chat_request = {
        "model": "mock-model"
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert "Invalid request" in response.json()["detail"]

@pytest.mark.asyncio
async def test_chat_completions_missing_role_in_message(async_client):
    """Test chat completion with message missing role."""
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"content": "Hello without role"}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert "messages must contain 'role'" in response.json()["detail"]

@pytest.mark.asyncio
async def test_chat_completions_missing_content_for_user_role(async_client):
    """Test chat completion with user message missing content."""
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"role": "user"}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert "messages must contain 'content' for role 'user'" in response.json()["detail"]

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_system_message(mock_get, async_client):
    """Test chat completion with system message."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "System-guided response about the documents."}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "Document about machine learning algorithms."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with system message
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "system", "content": "You are a helpful AI assistant specializing in machine learning."},
            {"role": "user", "content": "Tell me about algorithms."}
        ],
        "temperature": 0.5
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "{'result': 'System-guided response about the documents.'}"
    assert len(response_data["source_nodes"]) > 0

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_unsupported_message_role(mock_get, async_client):
    """Test chat completion with unsupported message role (should passthrough)."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call
    mock_response = {
        "id": "chatcmpl-passthrough",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "Passthrough response"},
            "finish_reason": "stop"
        }]
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Test request with unsupported role (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"role": "function", "content": "Function response", "name": "test_function"}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "Passthrough response"
    # Should have source_nodes field but it should be None for passthrough requests
    assert response_data["source_nodes"] is None

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_complex_user_content(mock_get, async_client):
    """Test chat completion with complex user content (should passthrough)."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call
    mock_response = {
        "id": "chatcmpl-complex",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "Complex content response"},
            "finish_reason": "stop"
        }]
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Test request with complex content (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {
                "role": "user", 
                "content": [
                    {"type": "text", "text": "What's in this image?"},
                    {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,..."}}
                ]
            }
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "Complex content response"
    # Should have source_nodes field but it should be None for passthrough requests
    assert response_data["source_nodes"] is None

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_developer_role(mock_get, async_client):
    """Test chat completion with developer role message."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "Developer-guided response."}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "Technical documentation about APIs."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with developer message
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "developer", "content": "Debug information: API call failed"},
            {"role": "user", "content": "Help me understand the API."}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "{'result': 'Developer-guided response.'}"

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_with_top_k_parameter(mock_get, async_client):
    """Test chat completion with custom top_k parameter."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "Response based on top documents."}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index multiple test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "First document about machine learning."},
            {"text": "Second document about deep learning."},
            {"text": "Third document about neural networks."},
            {"text": "Fourth document about AI applications."},
            {"text": "Fifth document about data science."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with custom top_k
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Tell me about machine learning."}
        ],
        "top_k": 3
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    # Should return up to 3 source nodes based on top_k parameter
    assert len(response_data["source_nodes"]) <= 3

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_error_handling(mock_get, async_client):
    """Test chat completion error handling when LLM call fails."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response with error
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(500, json={"error": "Internal server error"}))

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "Test document."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion that should fail
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Test question."}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 500
    assert "An unexpected error occurred" in response.json()["detail"]

@pytest.mark.asyncio
@respx.mock 
@patch("requests.get")
async def test_chat_completions_assistant_message_with_content(mock_get, async_client):
    """Test chat completion with assistant message that has content."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "Response to conversation."}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "Conversation about AI."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with assistant message without content
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Hello"},
            {"role": "assistant", "content": "Hello! How can I help you?"},  # Assistant message with content
            {"role": "user", "content": "Can you help me?"}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "{'result': 'Response to conversation.'}"

@pytest.mark.asyncio
async def test_chat_completions_metrics_tracking(async_client):
    """Test that metrics are properly tracked for chat completions."""
    # Test successful request
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Hello"}
        ]
    }

    # This will fail but should still track metrics
    await async_client.post("/v1/chat/completions", json=chat_request)
    # Should fail due to validation error or missing setup
    
    # Check metrics endpoint
    metrics_response = await async_client.get("/metrics")
    assert metrics_response.status_code == 200
    
    # Should have at least one chat request recorded (success or failure)
    metrics_text = metrics_response.text
    assert "rag_chat_requests_total" in metrics_text
    assert "rag_chat_latency" in metrics_text

@pytest.mark.asyncio 
@respx.mock
@patch("requests.get")
async def test_chat_completions_mixed_message_types(mock_get, async_client):
    """Test chat completion with mixed message types."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {"result": "Mixed conversation response."}
    respx.post("http://localhost:5000/v1/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "Technical documentation about software development."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with mixed message types
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "I need help with coding."},
            {"role": "assistant", "content": "I'd be happy to help with coding."},
            {"role": "user", "content": "What about software development?"},
            {"role": "developer", "content": "Debug: User asking about software development"}
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "{'result': 'Mixed conversation response.'}"
    assert len(response_data["source_nodes"]) > 0

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_empty_messages_list(mock_get, async_client):
    """Test chat completion with empty messages list."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call (in case it gets that far)
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(400, json={"error": "Invalid request"}))

    chat_request = {
        "model": "mock-model",
        "messages": []
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert "Invalid request" in response.json()["detail"]

@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_with_functions(mock_get, async_client):
    """Test chat completion with functions parameter (should passthrough)."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for passthrough LLM call
    mock_response = {
        "id": "chatcmpl-functions",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "Function-enabled response"},
            "finish_reason": "stop"
        }]
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(200, json=mock_response))

    # Test request with functions (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Use a function to help me"}
        ],
        "functions": [
            {
                "name": "test_function",
                "description": "A test function"
            }
        ]
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    
    response_data = response.json()
    assert response_data["choices"][0]["message"]["content"] == "Function-enabled response"
    # Should have source_nodes field but it should be None for passthrough requests
    assert response_data["source_nodes"] is None