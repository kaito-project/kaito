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

import re
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
            {"text": "This is a test document about AI and machine learning."},
            {"text": "Another document discussing natural language processing."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion request
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "What can you tell me about AI?"}],
        "temperature": 0.7,
        "max_tokens": 100,
        "top_k": 2,
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
    assert len(response_data["source_nodes"]) > 0

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
                r'rag_chat_requests_total{status="success"} ([1-9]\d*).0', response.text
            )
        )
        == 1
    )


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
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "This is a direct LLM response",
                },
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Test request without index_name (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Hello, how are you?"}],
        "temperature": 0.7,
        "max_tokens": 100,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert response_data["id"] == "chatcmpl-test123"
    assert (
        response_data["choices"][0]["message"]["content"]
        == "This is a direct LLM response"
    )
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
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": None,
                    "tool_calls": [
                        {
                            "id": "call123",
                            "type": "function",
                            "function": {
                                "name": "test_tool",
                                "arguments": '{"param1": "value1"}',
                            },
                        }
                    ],
                },
                "finish_reason": "tool_calls",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Test request with tools (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Use a tool to help me"}],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "test_tool",
                    "description": "A test tool",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "param1": {
                                "type": "string",
                                "description": "A test parameter",
                            }
                        },
                    },
                },
            }
        ],
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
        "messages": [{"role": "user", "content": "Test question"}],
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
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(400, json={"error": "Invalid request"})
    )

    # Test missing messages
    chat_request = {"model": "mock-model"}

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert "Invalid request" in response.json()["detail"]


@pytest.mark.asyncio
async def test_chat_completions_missing_role_in_message(async_client):
    """Test chat completion with message missing role."""
    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document about AI and machine learning."},
            {"text": "Another document discussing natural language processing."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"content": "What can you tell me about AI?"}],
        "temperature": 0.7,
        "max_tokens": 100,
        "top_k": 2,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert "messages must contain 'role'" in response.json()["detail"]


@pytest.mark.asyncio
async def test_chat_completions_missing_content_for_user_role(async_client):
    """Test chat completion with user message missing content."""
    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document about AI and machine learning."},
            {"text": "Another document discussing natural language processing."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"role": "user"}],
        "temperature": 0.7,
        "max_tokens": 100,
        "top_k": 2,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400
    assert (
        "messages must contain 'content' for role 'user'" in response.json()["detail"]
    )


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
        "documents": [{"text": "Document about machine learning algorithms."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with system message
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {
                "role": "system",
                "content": "You are a helpful AI assistant specializing in machine learning.",
            },
            {"role": "user", "content": "Tell me about algorithms."},
        ],
        "temperature": 0.5,
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert (
        response_data["choices"][0]["message"]["content"]
        == "This is a helpful response about the test document."
    )
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
    mock_response = {"detail": "bad request format"}
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(400, json=mock_response)
    )

    # Test request with unsupported role (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {
                "role": "function",
                "content": "Function response",
                "name": "test_function",
            }
        ],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400

    response_data = response.json()
    assert "bad request format" in response_data["detail"]


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
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": "Complex content response"},
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Test request with complex content (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "What's in this image?"},
                    {
                        "type": "image_url",
                        "image_url": {"url": "data:image/jpeg;base64,..."},
                    },
                ],
            }
        ],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert (
        response_data["choices"][0]["message"]["content"] == "Complex content response"
    )
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

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [{"text": "Technical documentation about APIs."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with developer message
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "developer", "content": "Debug information: API call failed"},
            {"role": "user", "content": "Help me understand the API."},
        ],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert (
        response_data["choices"][0]["message"]["content"]
        == "This is a helpful response about the test document."
    )


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
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index multiple test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "First document about machine learning."},
            {"text": "Second document about deep learning."},
            {"text": "Third document about neural networks."},
            {"text": "Fourth document about AI applications."},
            {"text": "Fifth document about data science."},
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with custom top_k
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Tell me about machine learning."}],
        "top_k": 3,
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
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(500, json={"error": "Internal server error"})
    )

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [{"text": "Test document."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion that should fail
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Test question."}],
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

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [{"text": "Conversation about AI."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test chat completion with assistant message without content
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "user", "content": "Hello"},
            {
                "role": "assistant",
                "content": "Hello! How can I help you?",
            },  # Assistant message with content
            {"role": "user", "content": "Can you help me?"},
        ],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert (
        response_data["choices"][0]["message"]["content"]
        == "This is a helpful response about the test document."
    )


@pytest.mark.asyncio
async def test_chat_completions_metrics_tracking(async_client):
    """Test that metrics are properly tracked for chat completions."""
    # Test successful request
    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Hello"}],
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

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [{"text": "Technical documentation about software development."}],
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
            {
                "role": "developer",
                "content": "Debug: User asking about software development",
            },
        ],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert (
        response_data["choices"][0]["message"]["content"]
        == "This is a helpful response about the test document."
    )
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
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(400, json={"error": "Invalid request"})
    )

    chat_request = {"model": "mock-model", "messages": []}

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
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Function-enabled response",
                },
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Test request with functions (should trigger passthrough)
    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Use a function to help me"}],
        "functions": [{"name": "test_function", "description": "A test function"}],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200

    response_data = response.json()
    assert (
        response_data["choices"][0]["message"]["content"] == "Function-enabled response"
    )
    # Should have source_nodes field but it should be None for passthrough requests
    assert response_data["source_nodes"] is None
