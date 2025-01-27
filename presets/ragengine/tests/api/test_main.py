# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from unittest.mock import patch

from llama_index.core.storage.index_store import SimpleIndexStore

from ragengine.main import app, vector_store_handler, rag_ops
from fastapi.testclient import TestClient
import pytest

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

@patch('requests.post')
def test_query_index_success(mock_post):
    # Define Mock Response for Custom Inference API
    mock_response = {
        "result": "This is the completion from the API"
    }
    mock_post.return_value.json.return_value = mock_response
     # Index
    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = client.post("/index", json=request_data)
    assert response.status_code == 200

    # Query
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
    assert mock_post.call_count == 1

def test_query_index_failure():
    # Prepare request data for querying.
    request_data = {
        "index_name": "non_existent_index",  # Use an index name that doesn't exist
        "query": "test query",
        "top_k": 1,
        "llm_params": {"temperature": 0.7}
    }

    response = client.post("/query", json=request_data)
    assert response.status_code == 500
    assert response.json()["detail"] == "No such index: 'non_existent_index' exists."


def test_list_all_indexed_documents_success():
    response = client.get("/indexed-documents")
    assert response.status_code == 200
    assert response.json() == {'documents': {}}

    request_data = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document"},
            {"text": "Another test document"}
        ]
    }

    response = client.post("/index", json=request_data)
    assert response.status_code == 200

    response = client.get("/indexed-documents")
    assert response.status_code == 200
    assert "test_index" in response.json()["documents"]
    response_idx = response.json()["documents"]["test_index"]
    assert len(response_idx) == 2 # Two Documents Indexed
    assert ({item["text"] for item in response_idx.values()}
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
