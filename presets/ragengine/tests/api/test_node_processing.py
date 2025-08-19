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

import time
from unittest.mock import patch

import httpx
import pytest
import respx


@pytest.fixture(autouse=True)
def overwrite_inference_url(monkeypatch):
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
    monkeypatch.setattr(ragengine.config, "LLM_CONTEXT_WINDOW", 10000)
    monkeypatch.setattr(ragengine.inference.inference, "LLM_CONTEXT_WINDOW", 10000)


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_with_node_processing(mock_get, async_client):
    """Test the RAG functionality with node processing."""

    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 10000}]
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

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            # KAITO-related documents
            {"text": "KAITO is a Kubernetes operator for AI workloads."},
            {"text": "KAITO simplifies AI model deployment on Kubernetes."},
            {"text": "KAITO supports GPU provisioning for AI inference."},
            {"text": "KAITO manages machine learning workloads efficiently."},
            {"text": "KAITO provides automated scaling for AI models."},
            {"text": "KAITO integrates with Azure Kubernetes Service."},
            {"text": "KAITO handles AI model lifecycle management."},
            {"text": "KAITO enables rapid AI prototype deployment."},
            {"text": "KAITO optimizes resource allocation for ML."},
            {"text": "KAITO supports distributed AI training."},
            # Unrelated documents about cooking
            {"text": "Chocolate chip cookies need butter and sugar."},
            {"text": "Pasta boiling requires salted water."},
            {"text": "Pizza dough needs yeast to rise properly."},
            {"text": "Grilled chicken tastes better with herbs."},
            {"text": "Fresh vegetables make salads more nutritious."},
            # Unrelated documents about weather
            {"text": "Rain clouds form when air cools rapidly."},
            {"text": "Sunny days are perfect for outdoor activities."},
            {"text": "Snow falls when temperatures drop below freezing."},
            {"text": "Wind patterns affect local weather conditions."},
            {"text": "Humidity levels influence comfort indoors."},
            # Unrelated documents about sports
            {"text": "Soccer players need good ball control skills."},
            {"text": "Basketball requires precise shooting technique."},
            {"text": "Tennis serves depend on proper grip."},
            {"text": "Swimming strokes vary in efficiency."},
            {"text": "Running form affects speed and endurance."},
            # More unrelated content
            {"text": "Books provide knowledge and entertainment."},
            {"text": "Music helps people relax and focus."},
            {"text": "Art expresses creativity and emotion."},
            {"text": "Travel broadens cultural understanding."},
            {"text": "Photography captures precious moments."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion request
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "What can you tell me about KAITO?"}],
        "temperature": 0.7,
        "max_tokens": 400,
        "context_token_ratio": 0.8,
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
    assert (
        response_data["choices"][0]["message"]["content"]
        == "This is a helpful response about the test document."
    )
    assert response_data["choices"][0]["finish_reason"] == "stop"
    assert response_data["choices"][0]["index"] == 0
    assert "source_nodes" in response_data
    assert len(response_data["source_nodes"]) == 10
    for node in response_data["source_nodes"]:
        assert "node_id" in node
        assert "score" in node
        assert "text" in node
        assert "metadata" in node
        assert "KAITO" in node["text"]


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_context_selection_with_limited_tokens(mock_get, async_client):
    """Test node processing with limited token context window."""

    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [
            {"id": "mock-model", "max_model_len": 500}
        ]  # Very small context window
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
                    "content": "Limited context response.",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 15, "completion_tokens": 8, "total_tokens": 23},
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index documents with varying lengths
    index_request = {
        "index_name": "limited_context_index",
        "documents": [
            {"text": "KAITO is great."},
            {
                "text": "KAITO provides excellent Kubernetes AI orchestration capabilities for machine learning workloads with advanced GPU provisioning and automated scaling features."
            },
            {"text": "KAITO rocks."},
            {"text": "KAITO handles deployment."},
            {
                "text": "KAITO manages resources efficiently with sophisticated algorithms and optimization techniques for distributed computing environments."
            },
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test with small max_tokens to trigger context selection
    chat_request = {
        "index_name": "limited_context_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Tell me about KAITO"}],
        "max_tokens": 50,  # Very small to force node selection
        "context_token_ratio": 0.5,  # Only use 50% of available context
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert "source_nodes" in response_data
    # Should select fewer nodes due to token constraints
    assert len(response_data["source_nodes"]) < 5


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_similarity_threshold_filtering(mock_get, async_client):
    """Test node processing with similarity threshold filtering."""

    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 10000}]
    }

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
                    "content": "Response about specific topic.",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30},
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index documents with very specific query
    index_request = {
        "index_name": "similarity_test_index",
        "documents": [
            {"text": "Kubernetes operator for machine learning workloads"},
            {"text": "KAITO simplifies AI deployment on Kubernetes"},
            {"text": "Cooking pasta with tomato sauce"},
            {"text": "Weather patterns in tropical regions"},
            {"text": "Sports equipment for basketball"},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Query with very specific terms
    chat_request = {
        "index_name": "similarity_test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Kubernetes machine learning operator"}
        ],
        "context_token_ratio": 0.7,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert "source_nodes" in response_data
    # Should return relevant nodes, filtering out unrelated content
    relevant_nodes = [
        node
        for node in response_data["source_nodes"]
        if "Kubernetes" in node["text"]
        or "machine learning" in node["text"]
        or "KAITO" in node["text"]
    ]
    assert len(relevant_nodes) >= 1


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_empty_query_handling(mock_get, async_client):
    """Test node processing with edge cases like empty results."""

    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 10000}]
    }

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
                    "content": "I don't have information about that topic.",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 10, "completion_tokens": 15, "total_tokens": 25},
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index documents
    index_request = {
        "index_name": "edge_case_index",
        "documents": [
            {"text": "Computer programming languages"},
            {"text": "Database management systems"},
            {"text": "Network security protocols"},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Query for something completely unrelated
    chat_request = {
        "index_name": "edge_case_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "ancient Egyptian hieroglyphics"}],
        "context_token_ratio": 0.6,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert "source_nodes" in response_data
    # May return nodes but they should have low relevance scores


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_large_document_handling(mock_get, async_client):
    """Test node processing with documents of varying sizes."""

    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 1000}]  # Medium context window
    }

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
                    "content": "Response about document processing.",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 50, "completion_tokens": 20, "total_tokens": 70},
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index documents with very different sizes
    large_text = (
        "KAITO "
        + "provides advanced Kubernetes orchestration capabilities for AI and machine learning workloads with sophisticated GPU provisioning, automated scaling, resource optimization, and comprehensive lifecycle management features. "
        * 5
    )

    index_request = {
        "index_name": "size_test_index",
        "documents": [
            {"text": "KAITO is simple."},
            {"text": large_text},
            {"text": "KAITO works well."},
            {
                "text": "KAITO scales AI workloads efficiently on Kubernetes clusters with GPU support and automated resource management."
            },
            {"text": "KAITO helps."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    chat_request = {
        "index_name": "size_test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "What does KAITO do?"}],
        "max_tokens": 100,
        "context_token_ratio": 0.3,  # Use only 30% of available context to force selection
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert "source_nodes" in response_data
    # Should prefer smaller, more relevant documents due to token constraints
    assert len(response_data["source_nodes"]) >= 1

    # Verify that at least some returned nodes are not the very large document
    node_texts = [node["text"] for node in response_data["source_nodes"]]
    short_nodes = [text for text in node_texts if len(text) < 100]
    assert len(short_nodes) >= 1


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_context_token_ratio_variations(mock_get, async_client):
    """Test node processing with different context_token_ratio values."""

    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2000}]
    }

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
                    "content": "Response based on context ratio.",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 30, "completion_tokens": 15, "total_tokens": 45},
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index multiple KAITO documents
    index_request = {
        "index_name": "context_ratio_test_index",
        "documents": [
            {"text": "KAITO simplifies Kubernetes AI workloads."},
            {"text": "KAITO provides GPU provisioning capabilities."},
            {"text": "KAITO automates ML model deployment."},
            {"text": "KAITO scales applications efficiently."},
            {"text": "KAITO integrates with cloud platforms."},
            {"text": "KAITO manages resource allocation."},
            {"text": "KAITO supports distributed training."},
            {"text": "KAITO optimizes inference performance."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test with high context_token_ratio (should include more nodes)
    chat_request_high = {
        "index_name": "context_ratio_test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Tell me about KAITO"}],
        "max_tokens": 200,
        "context_token_ratio": 0.8,  # Use 90% of available context
    }

    response_high = await async_client.post(
        "/v1/chat/completions", json=chat_request_high
    )
    assert response_high.status_code == 200
    response_data_high = response_high.json()

    # Test with low context_token_ratio (should include fewer nodes)
    chat_request_low = {
        "index_name": "context_ratio_test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Tell me about KAITO"}],
        "max_tokens": 200,
        "context_token_ratio": 0.2,  # Use only 20% of available context
    }

    response_low = await async_client.post(
        "/v1/chat/completions", json=chat_request_low
    )
    assert response_low.status_code == 200
    response_data_low = response_low.json()

    # High ratio should return more source nodes than low ratio
    assert len(response_data_high["source_nodes"]) > len(
        response_data_low["source_nodes"]
    )
    assert len(response_data_low["source_nodes"]) >= 1  # Should have at least one node

    # Both should contain KAITO-related content
    for node in response_data_high["source_nodes"]:
        assert "KAITO" in node["text"]
    for node in response_data_low["source_nodes"]:
        assert "KAITO" in node["text"]
