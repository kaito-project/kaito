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

from unittest.mock import patch

import pytest
import httpx
import respx
import time
import re

@pytest.fixture(autouse=True)
def overwrite_inference_url(monkeypatch):
    import ragengine.inference.inference
    import ragengine.config
    monkeypatch.setattr(ragengine.config, "LLM_INFERENCE_URL", "http://localhost:5000/v1/chat/completions")
    monkeypatch.setattr(ragengine.inference.inference, "LLM_INFERENCE_URL", "http://localhost:5000/v1/chat/completions")
    monkeypatch.setattr(ragengine.config, "LLM_CONTEXT_WINDOW", 2500)
    monkeypatch.setattr(ragengine.inference.inference, "LLM_CONTEXT_WINDOW", 2500)


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_with_node_processing(mock_get, async_client):
    """Test the RAG functionality with node processing."""

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
                    "content": "This is a helpful response about the test document."
                },
                "finish_reason": "stop"
            }
        ],
        "usage": {
            "prompt_tokens": 25,
            "completion_tokens": 12,
            "total_tokens": 37
        }
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(return_value=httpx.Response(200, json=mock_response))

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
            {"text": "Photography captures precious moments."}
        ]
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion request
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "What can you tell me about KAITO?"}
        ],
        "temperature": 0.7,
        "max_tokens": 100,
        "top_k": 100
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
    assert response_data["choices"][0]["message"]["content"] == "This is a helpful response about the test document."
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