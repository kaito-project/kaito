# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from unittest.mock import patch, ANY

from llama_index.core.storage.index_store import SimpleIndexStore

from ragengine.main import app, vector_store_handler, rag_ops
from fastapi.testclient import TestClient
import pytest
import httpx
import respx

AUTO_GEN_DOC_ID_LEN = 64

client = TestClient(app)

@pytest.fixture(autouse=True)
def clear_index():
    vector_store_handler.index_map.clear()
    vector_store_handler.index_store = SimpleIndexStore()

def test_index_documents_success():
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = client.post("/index", json=request_data)
    assert response.status_code == 200
    doc1, doc2 = response.json()
    assert (doc1["text"] == "This is a test document")
    assert len(doc1["doc_id"]) == AUTO_GEN_DOC_ID_LEN
    assert not doc1["metadata"]

    assert (doc2["text"] == "Another test document")
    assert len(doc2["doc_id"]) == AUTO_GEN_DOC_ID_LEN
    assert not doc2["metadata"]

@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
def test_query_index_success(mock_get):
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

    response = client.post("/index", json=request_data)
    assert response.status_code == 200

    # Query Request
    request_data = {
        "index_name": "test_index",
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = client.post("/query", json=request_data)
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

@respx.mock
@patch("requests.get")  # Mock the requests.get call for fetching model metadata
def test_reranker_and_query_with_index(mock_get):
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
    response = client.post("/index", json=index_request_payload)
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
    response = client.post("/query", json=query_request_payload)
    assert response.status_code == 200
    query_response = response.json()

    # Validate query response
    assert len(query_response["source_nodes"]) == top_n

    # Validate each source node in the query response
    expected_source_nodes = [
        {"text": "Have you ever visited Paris? It is a beautiful city where you can eat delicious food and see the Eiffel Tower. I really enjoyed all the cities in France, but its capital with the Eiffel Tower is my favorite city.",
         "score": 10.0, "metadata": {}},
        {"text": "I really enjoyed my trip to Paris, France. The city is beautiful and the "
                 "food is delicious. I would love to visit again. Such a great capital city.",
         "score": 10.0, "metadata": {}},
    ]
    for i, expected_node in enumerate(expected_source_nodes):
        actual_node = query_response["source_nodes"][i]
        assert actual_node["text"] == expected_node["text"]
        assert actual_node["score"] == expected_node["score"]
        assert actual_node["metadata"] == expected_node["metadata"]

    # Ensure HTTPX requests were made
    assert respx.calls.call_count == 2  # One for rerank, one for query completion

    # Ensure the model fetch was called once
    mock_get.assert_called_once_with("http://localhost:5000/v1/models", headers=ANY)

def test_query_index_failure():
    # Prepare request data for querying.
    request_data = {
        "index_name": "non_existent_index",  # Use an index name that doesn't exist
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = client.post("/query", json=request_data)
    assert response.status_code == 400
    assert response.json()["detail"] == "No such index: 'non_existent_index' exists."


def test_list_documents_in_index_success():
    index_name = "test_index"

    # Ensure no documents are present initially
    response = client.get(f"/indexes/{index_name}/documents")

    assert response.status_code == 500
    assert response.json() == {'detail': "Index 'test_index' not found."}

    request_data = {
        "index_name": index_name,
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = client.post("/index", json=request_data)
    assert response.status_code == 200

    # Retrieve documents for the specific index
    response = client.get(f"/indexes/{index_name}/documents")
    assert response.status_code == 200
    response_json = response.json()

    # Ensure documents exist correctly in the specific index
    assert response_json["count"] == 2
    assert len(response_json["documents"]) == 2
    assert ({item["text"] for item in response_json["documents"]}
            == {item["text"] for item in request_data["documents"]})


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
