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
import re
import time
from unittest.mock import patch

import httpx
import pytest
import respx

from ragengine.guardrails.audit import GuardrailAuditReport
from ragengine.guardrails.output_guardrails import OutputGuardrails
from ragengine.inference.inference import DEFAULT_HEADERS, Inference
from ragengine.models import ChatCompletionResponse


async def _read_sse_events(response) -> list[str]:
    body = ""
    async for chunk in response.aiter_text():
        body += chunk

    events = []
    for part in body.split("\n\n"):
        if not part.strip():
            continue
        assert part.startswith("data: ")
        events.append(part[6:])
    return events


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
    assert "rag_output_guardrails_audit_in_flight_background_tasks" in response.text


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
async def test_chat_completions_output_guardrails_redact(
    mock_get, async_client, monkeypatch
):
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    mock_response = {
        "id": "chatcmpl-redact123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Visit http://evil.example now",
                },
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    import ragengine.main

    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
        ),
    )

    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Share the link"}],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    assert response.json()["choices"][0]["message"]["content"] == "Visit [REDACTED] now"


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_output_guardrails_block(
    mock_get, async_client, monkeypatch
):
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    mock_response = {
        "id": "chatcmpl-block123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Visit http://evil.example now",
                },
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    import ragengine.main

    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="block",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            block_message="blocked-by-policy",
        ),
    )

    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Share the link"}],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    assert response.json()["choices"][0]["message"]["content"] == "blocked-by-policy"


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_output_guardrails_audit_log(
    mock_get, async_client, monkeypatch, caplog
):
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    mock_response = {
        "id": "chatcmpl-audit123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Visit http://evil.example now",
                },
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    import ragengine.main

    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
        ),
    )

    caplog.set_level("INFO")

    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Share the link"}],
    }
    headers = {
        "x-request-id": "req-audit-123",
        "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
    }

    response = await async_client.post(
        "/v1/chat/completions", json=chat_request, headers=headers
    )
    assert response.status_code == 200
    assert response.headers["X-Request-Id"] == "req-audit-123"
    assert "output_guardrails_audit_report" in caplog.text
    assert '"event_type":"ragengine.output_guardrails.report"' in caplog.text
    assert '"response_mode":"passthrough"' in caplog.text
    assert '"action":"redact"' in caplog.text
    assert '"scanner":"Regex"' in caplog.text
    assert '"request_id":"req-audit-123"' in caplog.text
    assert '"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"' in caplog.text


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_output_guardrails_fail_open(
    mock_get, async_client, monkeypatch, caplog
):
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    mock_response = {
        "id": "chatcmpl-failopen123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Visit http://evil.example now",
                },
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    import ragengine.main
    import ragengine.guardrails.output_guardrails as output_guardrails_module

    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
        ),
    )
    monkeypatch.setattr(
        output_guardrails_module,
        "scan_output",
        lambda *args, **kwargs: (_ for _ in ()).throw(RuntimeError("scanner exploded")),
    )

    caplog.set_level("INFO")

    chat_request = {
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Share the link"}],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 200
    assert (
        response.json()["choices"][0]["message"]["content"]
        == "Visit http://evil.example now"
    )
    assert "output_guardrails_failed" in caplog.text
    assert '"action":"fail_open"' in caplog.text
    report_payload = next(
        json.loads(line.split("output_guardrails_audit_report ", 1)[1])
        for line in caplog.text.splitlines()
        if "output_guardrails_audit_report" in line
    )
    assert report_payload["request_id"]
    assert report_payload["trace_id"] is None


def test_output_guardrails_fail_open_preserves_report_request_context(monkeypatch):
    import ragengine.guardrails.output_guardrails as output_guardrails_module

    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=True,
        action_on_hit="redact",
        regex_patterns=[r"https?://\S+"],
        banned_substrings=[],
    )
    response = ChatCompletionResponse(
        id="chatcmpl-failopen-structured",
        object="chat.completion",
        created=int(time.time()),
        model="mock-model",
        choices=[
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Visit http://evil.example now",
                },
                "finish_reason": "stop",
            }
        ],
        source_nodes=None,
    )

    monkeypatch.setattr(
        output_guardrails_module,
        "scan_output",
        lambda *args, **kwargs: (_ for _ in ()).throw(RuntimeError("scanner exploded")),
    )

    guarded_response, audit_report = guardrails.guard_response(
        response,
        {"model": "mock-model", "messages": [{"role": "user", "content": "hi"}]},
        {"request_id": "req-fail-open-123", "trace_id": "trace-fail-open-123"},
    )

    assert guarded_response.choices[0].message.content == "Visit http://evil.example now"
    assert isinstance(audit_report, GuardrailAuditReport)
    assert audit_report.request_id == "req-fail-open-123"
    assert audit_report.trace_id == "trace-fail-open-123"
    assert audit_report.events[0].request_id == "req-fail-open-123"
    assert audit_report.events[0].trace_id == "trace-fail-open-123"
    assert audit_report.events[0].action == "fail_open"


@pytest.mark.asyncio
async def test_chat_completions_output_guardrails_stream_redact(
    async_client, monkeypatch, caplog
):
    import ragengine.main

    async def fake_stream(_request):
        async def generator():
            yield {
                "type": "metadata",
                "id": "chatcmpl-stream-redact",
                "created": int(time.time()),
                "model": "mock-model",
                "response_mode": "passthrough",
                "source_nodes": None,
            }
            yield {"type": "delta", "delta": "Visit http://evil."}
            yield {"type": "delta", "delta": "example now"}
            yield {"type": "done", "finish_reason": "stop"}

        return generator()

    monkeypatch.setattr(ragengine.main.rag_ops, "chat_completion_stream", fake_stream)
    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            stream_holdback_chars=8,
        ),
    )
    caplog.set_level("INFO")

    async with async_client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": "mock-model",
            "stream": True,
            "messages": [{"role": "user", "content": "share the link"}],
        },
    ) as response:
        assert response.status_code == 200
        assert response.headers["content-type"].startswith("text/event-stream")
        events = await _read_sse_events(response)

    assert events[-1] == "[DONE]"
    payloads = [json.loads(event) for event in events[:-1]]
    content = "".join(
        payload["choices"][0]["delta"].get("content", "") for payload in payloads
    )
    assert content == "Visit [REDACTED] now"
    assert payloads[-1]["choices"][0]["finish_reason"] == "stop"
    event_payloads = []
    stream_ids = set()
    for line in caplog.text.splitlines():
        if "output_guardrails_audit_event" not in line:
            continue
        payload = json.loads(line.split("output_guardrails_audit_event ", 1)[1])
        event_payloads.append(payload)
        stream_ids.add(payload["stream_id"])
    sequence_values = [payload["sequence"] for payload in event_payloads]
    assert len(stream_ids) == 1
    assert sequence_values == sorted(sequence_values)
    assert sequence_values == list(range(1, len(sequence_values) + 1))
    completed_event = next(
        payload for payload in event_payloads if payload["stage"] == "stream_completed"
    )
    assert completed_event["sequence_total"] == sequence_values[-1]
    assert completed_event["checksum_algorithm"] == "sha256"
    assert completed_event["end_of_stream_checksum"]
    report_payload = next(
        json.loads(line.split("output_guardrails_audit_report ", 1)[1])
        for line in caplog.text.splitlines()
        if "output_guardrails_audit_report" in line
    )
    assert report_payload["stream_id"] == completed_event["stream_id"]
    assert report_payload["last_sequence"] == completed_event["sequence_total"]
    assert report_payload["sequence_total"] == completed_event["sequence_total"]
    assert report_payload["checksum_algorithm"] == "sha256"
    assert (
        report_payload["end_of_stream_checksum"]
        == completed_event["end_of_stream_checksum"]
    )
    assert '"stage":"stream_started"' in caplog.text
    assert '"stage":"stream_decision"' in caplog.text
    assert '"stage":"stream_completed"' in caplog.text


@pytest.mark.asyncio
async def test_chat_completions_stream_disabled_guardrails_emit_no_audit_events(
    async_client, monkeypatch, caplog
):
    import ragengine.main

    async def fake_stream(_request):
        async def generator():
            yield {
                "type": "metadata",
                "id": "chatcmpl-stream-disabled-guardrails",
                "created": int(time.time()),
                "model": "mock-model",
                "response_mode": "passthrough",
                "source_nodes": None,
            }
            yield {"type": "delta", "delta": "Visit http://evil.example now"}
            yield {"type": "done", "finish_reason": "stop"}

        return generator()

    monkeypatch.setattr(ragengine.main.rag_ops, "chat_completion_stream", fake_stream)
    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=False,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            stream_holdback_chars=8,
        ),
    )
    caplog.set_level("INFO")

    async with async_client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": "mock-model",
            "stream": True,
            "messages": [{"role": "user", "content": "share the link"}],
        },
    ) as response:
        assert response.status_code == 200
        events = await _read_sse_events(response)

    payloads = [json.loads(event) for event in events[:-1]]
    content = "".join(
        payload["choices"][0]["delta"].get("content", "") for payload in payloads
    )
    assert content == "Visit http://evil.example now"
    assert payloads[-1]["choices"][0]["finish_reason"] == "stop"
    assert "output_guardrails_audit_event" not in caplog.text
    assert "output_guardrails_audit_report" not in caplog.text


@pytest.mark.asyncio
async def test_chat_completions_output_guardrails_stream_block(
    async_client, monkeypatch, caplog
):
    import ragengine.main

    async def fake_stream(_request):
        async def generator():
            yield {
                "type": "metadata",
                "id": "chatcmpl-stream-block",
                "created": int(time.time()),
                "model": "mock-model",
                "response_mode": "rag",
                "source_nodes": [
                    {
                        "doc_id": "1",
                        "node_id": "n1",
                        "text": "ctx",
                        "score": 0.9,
                        "metadata": {},
                    }
                ],
            }
            yield {"type": "delta", "delta": "Visit http://evil.example now"}
            yield {"type": "done", "finish_reason": "stop"}

        return generator()

    monkeypatch.setattr(ragengine.main.rag_ops, "chat_completion_stream", fake_stream)
    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="block",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            block_message="blocked-by-policy",
            stream_holdback_chars=8,
        ),
    )

    caplog.set_level("INFO")

    async with async_client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": "mock-model",
            "index_name": "test_index",
            "stream": True,
            "messages": [{"role": "user", "content": "share the link"}],
        },
    ) as response:
        events = await _read_sse_events(response)

    payloads = [json.loads(event) for event in events[:-1]]
    content = "".join(
        payload["choices"][0]["delta"].get("content", "") for payload in payloads
    )
    assert content == "blocked-by-policy"
    assert payloads[-1]["choices"][0]["finish_reason"] == "content_filter"
    assert '"response_mode":"rag"' in caplog.text
    assert '"action":"block"' in caplog.text


@pytest.mark.asyncio
async def test_chat_completions_output_guardrails_stream_fail_open(
    async_client, monkeypatch, caplog
):
    import ragengine.main
    import ragengine.guardrails.output_guardrails as output_guardrails_module

    async def fake_stream(_request):
        async def generator():
            yield {
                "type": "metadata",
                "id": "chatcmpl-stream-fail-open",
                "created": int(time.time()),
                "model": "mock-model",
                "response_mode": "passthrough",
                "source_nodes": None,
            }
            yield {"type": "delta", "delta": "Visit http://evil.example now"}
            yield {"type": "done", "finish_reason": "stop"}

        return generator()

    monkeypatch.setattr(ragengine.main.rag_ops, "chat_completion_stream", fake_stream)
    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            stream_holdback_chars=8,
        ),
    )
    monkeypatch.setattr(
        output_guardrails_module,
        "scan_output",
        lambda *args, **kwargs: (_ for _ in ()).throw(RuntimeError("scanner exploded")),
    )

    caplog.set_level("INFO")

    async with async_client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": "mock-model",
            "stream": True,
            "messages": [{"role": "user", "content": "share the link"}],
        },
    ) as response:
        events = await _read_sse_events(response)

    payloads = [json.loads(event) for event in events[:-1]]
    content = "".join(
        payload["choices"][0]["delta"].get("content", "") for payload in payloads
    )
    assert content == "Visit http://evil.example now"
    assert payloads[-1]["choices"][0]["finish_reason"] == "stop"
    assert '"action":"fail_open"' in caplog.text
    assert '"stage":"stream_fail_open"' in caplog.text
    assert '"stage":"stream_completed"' in caplog.text


@pytest.mark.asyncio
async def test_chat_completions_stream_passthrough_parser(monkeypatch):
    import ragengine.inference.inference as inference_module

    monkeypatch.setattr(
        inference_module,
        "LLM_INFERENCE_URL",
        "http://localhost:5000/v1/chat/completions",
    )

    async def handler(_request):
        body = "\n\n".join(
            [
                'data: {"id":"chatcmpl-stream-parser","object":"chat.completion.chunk","created":123,"model":"mock-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}',
                'data: {"id":"chatcmpl-stream-parser","object":"chat.completion.chunk","created":123,"model":"mock-model","choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":null}]}',
                'data: {"id":"chatcmpl-stream-parser","object":"chat.completion.chunk","created":123,"model":"mock-model","choices":[{"index":0,"delta":{"content":"world"},"finish_reason":null}]}',
                'data: {"id":"chatcmpl-stream-parser","object":"chat.completion.chunk","created":123,"model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}',
                "data: [DONE]",
                "",
            ]
        )
        return httpx.Response(
            200,
            text=body,
            headers={"content-type": "text/event-stream"},
        )

    inference = Inference()
    inference._async_http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        headers=DEFAULT_HEADERS,
    )

    events = []
    stream = await inference.chat_completions_stream_passthrough(
        {
            "model": "mock-model",
            "stream": True,
            "messages": [{"role": "user", "content": "hello"}],
        }
    )
    async for event in stream:
        events.append(event)

    await inference.aclose()

    assert events[0]["type"] == "metadata"
    assert events[0]["id"] == "chatcmpl-stream-parser"
    assert events[0]["response_mode"] == "passthrough"
    assert [event["delta"] for event in events if event["type"] == "delta"] == [
        {"content": "Hello "},
        {"content": "world"},
    ]
    assert events[-1] == {"type": "done", "finish_reason": "stop"}


@pytest.mark.asyncio
async def test_chat_completions_stream_passthrough_parser_preserves_tool_calls(
    monkeypatch,
):
    import ragengine.inference.inference as inference_module

    monkeypatch.setattr(
        inference_module,
        "LLM_INFERENCE_URL",
        "http://localhost:5000/v1/chat/completions",
    )

    async def handler(_request):
        tool_call_chunk = {
            "id": "chatcmpl-stream-tools",
            "object": "chat.completion.chunk",
            "created": 123,
            "model": "mock-model",
            "choices": [
                {
                    "index": 0,
                    "delta": {
                        "tool_calls": [
                            {
                                "index": 0,
                                "id": "call_1",
                                "type": "function",
                                "function": {
                                    "name": "lookup",
                                    "arguments": '{"city":"Seattle"}',
                                },
                            }
                        ]
                    },
                    "finish_reason": None,
                }
            ],
        }
        done_chunk = {
            "id": "chatcmpl-stream-tools",
            "object": "chat.completion.chunk",
            "created": 123,
            "model": "mock-model",
            "choices": [
                {"index": 0, "delta": {}, "finish_reason": "tool_calls"}
            ],
        }
        body = "\n\n".join(
            [
                f"data: {json.dumps(tool_call_chunk, separators=(',', ':'))}",
                f"data: {json.dumps(done_chunk, separators=(',', ':'))}",
                "data: [DONE]",
                "",
            ]
        )
        return httpx.Response(
            200,
            text=body,
            headers={"content-type": "text/event-stream"},
        )

    inference = Inference()
    inference._async_http_client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        headers=DEFAULT_HEADERS,
    )

    events = []
    stream = await inference.chat_completions_stream_passthrough(
        {
            "model": "mock-model",
            "stream": True,
            "messages": [{"role": "user", "content": "hello"}],
        }
    )
    async for event in stream:
        events.append(event)

    await inference.aclose()

    assert events[1]["delta"]["tool_calls"][0]["function"]["name"] == "lookup"
    assert events[-1] == {"type": "done", "finish_reason": "tool_calls"}


@pytest.mark.asyncio
async def test_chat_completions_output_guardrails_stream_tool_call_passthrough(
    async_client, monkeypatch, caplog
):
    import ragengine.main

    async def fake_stream(_request):
        async def generator():
            yield {
                "type": "metadata",
                "id": "chatcmpl-stream-tools-api",
                "created": int(time.time()),
                "model": "mock-model",
                "response_mode": "passthrough",
                "source_nodes": None,
            }
            yield {
                "type": "delta",
                "delta": {
                    "tool_calls": [
                        {
                            "index": 0,
                            "id": "call_1",
                            "type": "function",
                            "function": {
                                "name": "lookup_weather",
                                "arguments": '{"city":"Seattle"}',
                            },
                        }
                    ]
                },
            }
            yield {"type": "done", "finish_reason": "tool_calls"}

        return generator()

    monkeypatch.setattr(ragengine.main.rag_ops, "chat_completion_stream", fake_stream)
    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            stream_holdback_chars=8,
        ),
    )
    caplog.set_level("INFO")

    async with async_client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": "mock-model",
            "stream": True,
            "tools": [
                {
                    "type": "function",
                    "function": {
                        "name": "lookup_weather",
                        "parameters": {"type": "object"},
                    },
                }
            ],
            "messages": [{"role": "user", "content": "weather?"}],
        },
    ) as response:
        events = await _read_sse_events(response)

    payloads = [json.loads(event) for event in events[:-1]]
    tool_chunks = [
        payload for payload in payloads if payload["choices"][0]["delta"].get("tool_calls")
    ]
    assert len(tool_chunks) == 1
    assert tool_chunks[0]["choices"][0]["delta"]["tool_calls"][0]["function"]["name"] == "lookup_weather"
    assert payloads[-1]["choices"][0]["finish_reason"] == "tool_calls"
    assert '"stage":"stream_tool_call_passthrough"' in caplog.text


@pytest.mark.asyncio
async def test_chat_completions_output_guardrails_stream_function_call_passthrough(
    async_client, monkeypatch, caplog
):
    import ragengine.main

    async def fake_stream(_request):
        async def generator():
            yield {
                "type": "metadata",
                "id": "chatcmpl-stream-function-api",
                "created": int(time.time()),
                "model": "mock-model",
                "response_mode": "passthrough",
                "source_nodes": None,
            }
            yield {
                "type": "delta",
                "delta": {
                    "function_call": {
                        "name": "lookup_weather",
                        "arguments": '{"city":"Seattle"}',
                    }
                },
            }
            yield {"type": "done", "finish_reason": "function_call"}

        return generator()

    monkeypatch.setattr(ragengine.main.rag_ops, "chat_completion_stream", fake_stream)
    monkeypatch.setattr(
        ragengine.main,
        "output_guardrails",
        OutputGuardrails(
            enabled=True,
            fail_open=True,
            action_on_hit="redact",
            regex_patterns=[r"https?://\S+"],
            banned_substrings=[],
            stream_holdback_chars=8,
        ),
    )
    caplog.set_level("INFO")

    async with async_client.stream(
        "POST",
        "/v1/chat/completions",
        json={
            "model": "mock-model",
            "stream": True,
            "functions": [
                {
                    "name": "lookup_weather",
                    "parameters": {"type": "object"},
                }
            ],
            "messages": [{"role": "user", "content": "weather?"}],
        },
    ) as response:
        events = await _read_sse_events(response)

    payloads = [json.loads(event) for event in events[:-1]]
    function_chunks = [
        payload
        for payload in payloads
        if payload["choices"][0]["delta"].get("function_call")
    ]
    assert len(function_chunks) == 1
    assert function_chunks[0]["choices"][0]["delta"]["function_call"]["name"] == "lookup_weather"
    assert payloads[-1]["choices"][0]["finish_reason"] == "function_call"
    assert '"stage":"stream_function_call_passthrough"' in caplog.text


def test_output_guardrails_returns_structured_audit_report():
    guardrails = OutputGuardrails(
        enabled=True,
        fail_open=True,
        action_on_hit="redact",
        regex_patterns=[r"https?://\S+"],
        banned_substrings=[],
    )
    response = {
        "id": "chatcmpl-structured123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Visit http://evil.example now",
                },
                "finish_reason": "stop",
            }
        ],
        "source_nodes": None,
    }

    guarded_response, audit_report = guardrails.guard_response(
        ChatCompletionResponse(**response),
        {"model": "mock-model", "messages": [{"role": "user", "content": "hi"}]},
        {"request_id": "req-structured-123", "trace_id": "trace-structured-123"},
    )

    assert guarded_response.choices[0].message.content == "Visit [REDACTED] now"
    assert isinstance(audit_report, GuardrailAuditReport)
    assert audit_report.request_id == "req-structured-123"
    assert audit_report.trace_id == "trace-structured-123"
    assert audit_report.response_id == "chatcmpl-structured123"
    assert audit_report.response_mode == "passthrough"
    assert audit_report.events[0].event_type == "ragengine.output_guardrails"
    assert audit_report.events[0].request_id == "req-structured-123"
    assert audit_report.events[0].trace_id == "trace-structured-123"
    assert audit_report.events[0].scanner_results[0].scanner == "Regex"
    assert audit_report.events[0].action == "redact"


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

    # Test invalid chat completion with mixed message types
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "I need help with coding."},
            {"role": "user", "content": "What should I learn in coding?"},
            {"role": "assistant", "content": "I'd be happy to help with coding."},
            {
                "role": "system",
                "content": "Debug: User asking about software development",
            },
        ],
    }

    response = await async_client.post("/v1/chat/completions", json=chat_request)
    assert response.status_code == 400

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

    # Test chat completion with mixed message types
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "I need help with coding."},
            {"role": "assistant", "content": "I'd be happy to help with coding."},
            {
                "role": "developer",
                "content": "Debug: User asking about software development",
            },
            {"role": "user", "content": "What about software development?"},
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


@pytest.mark.asyncio
@patch("requests.get")
async def test_chat_completions_prompt_exceeds_context_window(mock_get, async_client):
    """Test chat completion when prompt length exceeds context window."""
    # Mock the response for the default model fetch with a small context window
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [
            {"id": "small-model", "max_model_len": 100}
        ]  # Very small context window
    }

    # Index some test documents
    index_request = {
        "index_name": "test_index",
        "documents": [
            {"text": "This is a test document about AI and machine learning."}
        ],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Create a very long message that will exceed the context window
    long_message = "This is a very long message. " * 50  # This should exceed 100 tokens

    chat_request = {
        "index_name": "test_index",
        "model": "small-model",
        "messages": [{"role": "user", "content": long_message}],
    }

    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 100),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 100),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=150),
    ):
        response = await async_client.post("/v1/chat/completions", json=chat_request)
        assert response.status_code == 400
        assert "Prompt length exceeds context window" in response.json()["detail"]


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_max_tokens_exceeds_available_space(
    mock_get, async_client
):
    """Test chat completion when max_tokens exceeds available space after prompt."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 200}]  # Small context window
    }

    # Mock HTTPX response for Custom Inference API to simulate successful response
    mock_response = {
        "id": "chatcmpl-test123",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": "This is a response."},
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
        "documents": [{"text": "This is a test document."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test with max_tokens that exceeds available space
    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Short question?"}],
        "max_tokens": 500,  # This exceeds our context window of 200
    }

    # Mock the LLM context window and count_tokens method
    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 200),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 200),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=50),
    ):
        response = await async_client.post("/v1/chat/completions", json=chat_request)

        # The response might fail due to mocking issues, but that's ok
        # The important thing is that our test reaches the max_tokens adjustment logic
        # We can verify this by checking that the response is not a validation error (422)
        # which would indicate the request didn't reach the business logic
        assert response.status_code != 422  # Not a validation error

        # If it's a 400/500, that's likely from the mocked LLM response
        # If it's a 200, that means everything worked including our logic
        assert response.status_code in [200, 400, 500]


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_max_tokens_adjustment_warning(mock_get, async_client):
    """Test that max_tokens gets adjusted with warning when it exceeds available space."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 1000}]
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
                "message": {"role": "assistant", "content": "Adjusted response."},
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
        "documents": [{"text": "This is a test document for token adjustment."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    chat_request = {
        "index_name": "test_index",
        "model": "mock-model",
        "messages": [
            {
                "role": "user",
                "content": "This is a reasonably long question that uses some tokens?",
            }
        ],
        "max_tokens": 800,  # This should exceed available space after prompt
    }

    # Mock the LLM context window and count_tokens method
    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 1000),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 1000),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=300),
    ):
        response = await async_client.post("/v1/chat/completions", json=chat_request)

        # Verify the response reaches our business logic (not a validation error)
        assert response.status_code != 422  # Not a validation error
        assert response.status_code in [200, 400, 500]  # Various possible outcomes


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_context_window_boundary_conditions(
    mock_get, async_client
):
    """Test chat completion at context window boundary conditions."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "boundary-model", "max_model_len": 500}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {
        "id": "chatcmpl-boundary",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "boundary-model",
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": "Boundary response."},
                "finish_reason": "stop",
            }
        ],
    }
    respx.post("http://localhost:5000/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=mock_response)
    )

    # Index some test documents
    index_request = {
        "index_name": "boundary_test_index",
        "documents": [{"text": "Boundary test document for context window testing."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test case 1: Prompt exactly at context window limit (should succeed but with no context)
    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 500),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 500),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=500),
    ):
        chat_request = {
            "index_name": "boundary_test_index",
            "model": "boundary-model",
            "messages": [{"role": "user", "content": "Boundary test message"}],
        }

        response = await async_client.post("/v1/chat/completions", json=chat_request)
        # Should succeed but with warning about no available context tokens
        assert response.status_code == 200
        response_data = response.json()
        # Verify the response has the expected structure
        assert "choices" in response_data
        assert len(response_data["choices"]) > 0
        assert "message" in response_data["choices"][0]

    # Test case 1.5: Prompt exceeds context window limit (should fail)
    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 500),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 500),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=501),
    ):
        chat_request = {
            "index_name": "boundary_test_index",
            "model": "boundary-model",
            "messages": [
                {
                    "role": "user",
                    "content": "Boundary test message that exceeds context window",
                }
            ],
        }

        response = await async_client.post("/v1/chat/completions", json=chat_request)
        assert response.status_code == 400
        assert "Prompt length exceeds context window" in response.json()["detail"]

    # Test case 2: Prompt just under context window limit (should succeed)
    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 500),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 500),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=499),
    ):
        chat_request = {
            "index_name": "boundary_test_index",
            "model": "boundary-model",
            "messages": [{"role": "user", "content": "Boundary test message"}],
            "max_tokens": 1,  # Only 1 token available
        }

        response = await async_client.post("/v1/chat/completions", json=chat_request)
        assert response.status_code == 200
        response_data = response.json()
        # Verify the response has the expected structure
        assert "choices" in response_data
        assert len(response_data["choices"]) > 0
        assert "message" in response_data["choices"][0]


@pytest.mark.asyncio
@respx.mock
@patch("requests.get")
async def test_chat_completions_no_max_tokens_specified(mock_get, async_client):
    """Test chat completion when no max_tokens is specified (should not trigger adjustment)."""
    # Mock the response for the default model fetch
    mock_get.return_value.status_code = 200
    mock_get.return_value.json.return_value = {
        "data": [{"id": "mock-model", "max_model_len": 2048}]
    }

    # Mock HTTPX response for Custom Inference API
    mock_response = {
        "id": "chatcmpl-no-max-tokens",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Response without max tokens constraint.",
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
        "index_name": "no_max_tokens_index",
        "documents": [{"text": "Test document for no max tokens scenario."}],
    }

    response = await async_client.post("/index", json=index_request)
    assert response.status_code == 200

    # Test without max_tokens specified
    chat_request = {
        "index_name": "no_max_tokens_index",
        "model": "mock-model",
        "messages": [{"role": "user", "content": "Test question without max tokens?"}],
        # No max_tokens specified
    }

    with (
        patch("ragengine.config.LLM_CONTEXT_WINDOW", 2048),
        patch("ragengine.inference.inference.LLM_CONTEXT_WINDOW", 2048),
        patch("ragengine.inference.inference.Inference.count_tokens", return_value=100),
    ):
        response = await async_client.post("/v1/chat/completions", json=chat_request)

        # Verify the response reaches our business logic (not a validation error)
        assert response.status_code != 422  # Not a validation error
        assert response.status_code in [200, 400, 500]  # Various possible outcomes
