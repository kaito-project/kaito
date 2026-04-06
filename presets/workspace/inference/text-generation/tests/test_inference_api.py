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

"""Tests for the OpenAI-compatible inference server (inference_api.py)."""

import importlib
import json
import sys
import uuid
from pathlib import Path
from unittest.mock import patch

import pytest
from fastapi.testclient import TestClient

# Get the parent directory of the current file
parent_dir = str(Path(__file__).resolve().parent.parent)
# Add the parent directory to sys.path
sys.path.append(parent_dir)


@pytest.fixture(
    scope="module",
    params=[
        {
            "pipeline": "text-generation",
            "model_path": "HuggingFaceTB/SmolLM2-135M-Instruct",
            "device": "cpu",
        },
    ],
)
def configured_app(request):
    original_argv = sys.argv.copy()
    # Use request.param to set correct test arguments for each configuration
    test_args = [
        "program_name",
        "--pipeline",
        request.param["pipeline"],
        "--pretrained_model_name_or_path",
        request.param["model_path"],
        "--device_map",
        request.param["device"],
        "--allow_remote_files",
        "True",
    ]
    sys.argv = test_args

    import inference_api

    importlib.reload(inference_api)  # Reload to prevent module caching
    from inference_api import app

    # Attach the request params to the app instance for access in tests
    app.test_config = request.param
    yield app

    # Cancel the TimedModel timer to prevent leaked threads blocking exit
    for timed_model in inference_api.serve_command.loaded_models.values():
        timed_model._timer.cancel()
    sys.argv = original_argv


def _make_sse_chunk(
    role="assistant",
    content="Hello",
    finish_reason=None,
    model="HuggingFaceTB/SmolLM2-135M-Instruct",
    tool_calls=None,
):
    """Build a single SSE chunk string matching the transformers serve format."""
    delta = {}
    if role is not None:
        delta["role"] = role
    if content is not None:
        delta["content"] = content
    if tool_calls is not None:
        delta["tool_calls"] = tool_calls

    chunk = {
        "id": f"chatcmpl-{uuid.uuid4().hex[:8]}",
        "object": "chat.completion.chunk",
        "created": 1700000000,
        "model": model,
        "choices": [
            {
                "index": 0,
                "delta": delta,
                "finish_reason": finish_reason,
            }
        ],
    }
    return f"data: {json.dumps(chunk)}\n\n"


# ---------------------------------------------------------------------------
# /v1/chat/completions
# ---------------------------------------------------------------------------


def test_chat_completions(configured_app):
    """POST /v1/chat/completions returns an OpenAI-format SSE-streamed response."""
    fake_sse = iter([_make_sse_chunk(content="Hi"), "data: [DONE]\n\n"])

    client = TestClient(configured_app)
    request_data = {
        "model": configured_app.test_config["model_path"],
        "messages": [{"role": "user", "content": "Say hello in one word."}],
        "max_tokens": 10,
        "temperature": 0.0,
    }
    with (
        patch("inference_api.serve_command.validate_chat_completion_request"),
        patch(
            "inference_api.serve_command.generate_chat_completion",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/chat/completions", json=request_data)
    assert response.status_code == 200

    # The response is SSE-streamed; collect all data lines
    chunks = []
    for line in response.text.splitlines():
        if line.startswith("data: ") and line.strip() != "data: [DONE]":
            chunks.append(json.loads(line[len("data: ") :]))

    assert len(chunks) > 0, "Expected at least one SSE chunk"
    first_chunk = chunks[0]
    assert first_chunk["object"] == "chat.completion.chunk"
    assert "choices" in first_chunk
    assert first_chunk["choices"][0]["delta"]["role"] == "assistant"


def test_chat_completions_multi_turn(configured_app):
    """POST /v1/chat/completions works with multi-turn conversations."""
    fake_sse = iter([_make_sse_chunk(content="Sure!"), "data: [DONE]\n\n"])

    client = TestClient(configured_app)
    messages = [
        {"role": "user", "content": "What is your favourite condiment?"},
        {
            "role": "assistant",
            "content": "Well, im quite partial to a good squeeze of fresh lemon juice. It adds just the right amount of zesty flavour to whatever im cooking up in the kitchen!",
        },
        {"role": "user", "content": "Do you have mayonnaise recipes?"},
    ]
    request_data = {
        "model": configured_app.test_config["model_path"],
        "messages": messages,
        "max_tokens": 20,
        "temperature": 0.0,
    }
    with (
        patch("inference_api.serve_command.validate_chat_completion_request"),
        patch(
            "inference_api.serve_command.generate_chat_completion",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/chat/completions", json=request_data)
    assert response.status_code == 200

    # Verify we got at least one content chunk
    content_pieces = []
    for line in response.text.splitlines():
        if line.startswith("data: ") and line.strip() != "data: [DONE]":
            chunk = json.loads(line[len("data: ") :])
            delta = chunk["choices"][0].get("delta", {})
            if "content" in delta and delta["content"]:
                content_pieces.append(delta["content"])

    assert len(content_pieces) > 0, "Expected generated content in the response"


def test_chat_completions_stream_false_returns_json(configured_app):
    """POST /v1/chat/completions returns a single JSON response when stream=false."""
    fake_sse = iter(
        [
            _make_sse_chunk(content="Hi"),
            _make_sse_chunk(role=None, content=" there", finish_reason="stop"),
        ]
    )

    client = TestClient(configured_app)
    request_data = {
        "model": configured_app.test_config["model_path"],
        "messages": [{"role": "user", "content": "Say hello in two words."}],
        "max_tokens": 10,
        "temperature": 0.0,
        "stream": False,
    }
    with (
        patch("inference_api.serve_command.validate_chat_completion_request"),
        patch(
            "inference_api.serve_command.generate_chat_completion",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/chat/completions", json=request_data)

    assert response.status_code == 200
    data = response.json()
    assert data["object"] == "chat.completion"
    assert data["model"] == configured_app.test_config["model_path"]
    assert data["choices"][0]["message"] == {
        "role": "assistant",
        "content": "Hi there",
    }
    assert data["choices"][0]["finish_reason"] == "stop"


def test_chat_completions_stream_false_aggregates_tool_calls(configured_app):
    """POST /v1/chat/completions preserves streamed tool-call deltas when stream=false."""
    fake_sse = iter(
        [
            _make_sse_chunk(content=None),
            _make_sse_chunk(
                role=None,
                content=None,
                tool_calls=[
                    {
                        "index": 0,
                        "id": "call_123",
                        "type": "function",
                        "function": {"name": "get_weather"},
                    }
                ],
            ),
            _make_sse_chunk(
                role=None,
                content=None,
                tool_calls=[
                    {
                        "index": 0,
                        "type": "function",
                        "function": {"arguments": '{"city":"'},
                    }
                ],
            ),
            _make_sse_chunk(
                role=None,
                content=None,
                finish_reason="tool_calls",
                tool_calls=[
                    {
                        "index": 0,
                        "type": "function",
                        "function": {"arguments": 'Paris"}'},
                    }
                ],
            ),
        ]
    )

    client = TestClient(configured_app)
    request_data = {
        "model": configured_app.test_config["model_path"],
        "messages": [{"role": "user", "content": "What's the weather in Paris?"}],
        "stream": False,
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "get_weather",
                    "description": "Get the weather for a city.",
                    "parameters": {
                        "type": "object",
                        "properties": {"city": {"type": "string"}},
                    },
                },
            }
        ],
    }
    with (
        patch("inference_api.serve_command.validate_chat_completion_request"),
        patch(
            "inference_api.serve_command.generate_chat_completion",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/chat/completions", json=request_data)

    assert response.status_code == 200
    data = response.json()
    assert data["choices"][0]["finish_reason"] == "tool_calls"
    assert data["choices"][0]["message"]["content"] is None
    assert data["choices"][0]["message"]["tool_calls"] == [
        {
            "id": "call_123",
            "type": "function",
            "function": {
                "name": "get_weather",
                "arguments": '{"city":"Paris"}',
            },
        }
    ]


# ---------------------------------------------------------------------------
# /v1/responses
# ---------------------------------------------------------------------------


def test_responses(configured_app):
    """POST /v1/responses returns an SSE-streamed response."""
    fake_sse = iter(["data: {}\n\n", "data: [DONE]\n\n"])

    client = TestClient(configured_app)
    request_data = {
        "model": configured_app.test_config["model_path"],
        "input": "Hello",
    }
    with (
        patch("inference_api.serve_command.validate_response_request"),
        patch(
            "inference_api.serve_command.generate_response",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/responses", json=request_data)
    assert response.status_code == 200


# ---------------------------------------------------------------------------
# /v1/models
# ---------------------------------------------------------------------------


def test_list_models(configured_app):
    """GET /v1/models returns a list containing the served model."""
    client = TestClient(configured_app)
    response = client.get("/v1/models")
    assert response.status_code == 200
    data = response.json()
    assert data["object"] == "list"
    assert len(data["data"]) >= 1
    assert data["data"][0]["object"] == "model"
    # The model id should match the configured model name
    model_path = configured_app.test_config["model_path"]
    assert model_path in data["data"][0]["id"]


# ---------------------------------------------------------------------------
# /health
# ---------------------------------------------------------------------------


def test_health_check(configured_app):
    client = TestClient(configured_app)
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "Healthy"}


# ---------------------------------------------------------------------------
# /metrics
# ---------------------------------------------------------------------------


def test_get_metrics(configured_app):
    client = TestClient(configured_app)
    response = client.get("/metrics")
    assert response.status_code == 200
    assert "gpu_info" in response.json()


def test_get_metrics_with_gpus(configured_app):
    client = TestClient(configured_app)

    # Define a simple mock GPU object with the necessary attributes
    class MockGPU:
        def __init__(self, id, name, load, temperature, memoryUsed, memoryTotal):
            self.id = id
            self.name = name
            self.load = load
            self.temperature = temperature
            self.memoryUsed = memoryUsed
            self.memoryTotal = memoryTotal

    # Create a mock GPU object with the desired attributes
    mock_gpu = MockGPU(
        id="GPU-1234",
        name="GeForce GTX 950",
        load=0.25,  # 25%
        temperature=55,  # 55 C
        memoryUsed=1 * (1024**3),  # 1 GB
        memoryTotal=2 * (1024**3),  # 2 GB
    )

    # Mock torch.cuda.is_available to simulate an environment with GPUs
    # Mock GPUtil.getGPUs to return a list containing the mock GPU object
    with (
        patch("torch.cuda.is_available", return_value=True),
        patch("GPUtil.getGPUs", return_value=[mock_gpu]),
    ):
        response = client.get("/metrics")
        assert response.status_code == 200
        data = response.json()

        # Assertions to verify that the GPU info is correctly returned in the response
        assert data["gpu_info"] != []
        assert len(data["gpu_info"]) == 1
        gpu_data = data["gpu_info"][0]

        assert gpu_data["id"] == "GPU-1234"
        assert gpu_data["name"] == "GeForce GTX 950"
        assert gpu_data["load"] == "25.00%"
        assert gpu_data["temperature"] == "55 C"
        assert gpu_data["memory"]["used"] == "1.00 GB"
        assert gpu_data["memory"]["total"] == "2.00 GB"
        assert (
            data["cpu_info"] is None
        )  # Assuming CPU info is not present when GPUs are available


def test_get_metrics_no_gpus(configured_app):
    client = TestClient(configured_app)
    # Mock GPUtil.getGPUs to simulate an environment without GPUs
    with (
        patch("torch.cuda.is_available", return_value=False),
        patch("psutil.cpu_percent", return_value=20.0),
        patch("psutil.cpu_count", side_effect=[4, 8]),
        patch("psutil.virtual_memory") as mock_virtual_memory,
    ):
        mock_virtual_memory.return_value.used = 4 * (1024**3)  # 4 GB
        mock_virtual_memory.return_value.total = 16 * (1024**3)  # 16 GB
        response = client.get("/metrics")
        assert response.status_code == 200
        data = response.json()
        assert data["gpu_info"] is None  # No GPUs available
        assert data["cpu_info"] is not None  # CPU info should be present
        assert data["cpu_info"]["load_percentage"] == 20.0
        assert data["cpu_info"]["physical_cores"] == 4
        assert data["cpu_info"]["total_cores"] == 8
        assert data["cpu_info"]["memory"]["used"] == "4.00 GB"
        assert data["cpu_info"]["memory"]["total"] == "16.00 GB"


# ---------------------------------------------------------------------------
# served_model_name (e2e-aligned test)
# ---------------------------------------------------------------------------


@pytest.fixture(scope="module")
def local_model_app(tmp_path_factory):
    """Load model to a local path and start the server with --served_model_name,
    simulating the e2e flow where weights are pre-downloaded."""
    from huggingface_hub import snapshot_download

    local_dir = str(tmp_path_factory.mktemp("weights"))
    snapshot_download("HuggingFaceTB/SmolLM2-135M-Instruct", local_dir=local_dir)

    original_argv = sys.argv.copy()
    sys.argv = [
        "program_name",
        "--pipeline",
        "text-generation",
        "--pretrained_model_name_or_path",
        local_dir,
        "--served_model_name",
        "smollm2",
        "--device_map",
        "cpu",
    ]

    import inference_api

    importlib.reload(inference_api)
    from inference_api import app

    app.test_config = {"model_path": local_dir, "served_name": "smollm2"}
    yield app

    for timed_model in inference_api.serve_command.loaded_models.values():
        timed_model._timer.cancel()
    sys.argv = original_argv


def test_served_model_name_in_models_endpoint(local_model_app):
    """GET /v1/models returns the served_model_name, not the local path."""
    client = TestClient(local_model_app)
    response = client.get("/v1/models")
    assert response.status_code == 200
    data = response.json()
    assert data["data"][0]["id"] == "smollm2"


def test_served_model_name_in_chat_completions(local_model_app):
    """POST /v1/chat/completions works with the served model name."""
    fake_sse = iter([_make_sse_chunk(content="Hi"), "data: [DONE]\n\n"])

    client = TestClient(local_model_app)
    request_data = {
        "model": "smollm2",
        "messages": [{"role": "user", "content": "Hi"}],
        "max_tokens": 5,
    }
    with (
        patch("inference_api.serve_command.validate_chat_completion_request"),
        patch(
            "inference_api.serve_command.generate_chat_completion",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/chat/completions", json=request_data)
    assert response.status_code == 200


def test_served_model_name_in_non_stream_chat_completions(local_model_app):
    """POST /v1/chat/completions keeps the served model name when stream=false."""
    fake_sse = iter(
        [
            _make_sse_chunk(content="Hi", model=local_model_app.test_config["model_path"]),
            _make_sse_chunk(
                role=None,
                content=" there",
                finish_reason="stop",
                model=local_model_app.test_config["model_path"],
            ),
        ]
    )

    client = TestClient(local_model_app)
    request_data = {
        "model": "smollm2",
        "messages": [{"role": "user", "content": "Hi"}],
        "max_tokens": 5,
        "stream": False,
    }
    with (
        patch("inference_api.serve_command.validate_chat_completion_request"),
        patch(
            "inference_api.serve_command.generate_chat_completion",
            return_value=fake_sse,
        ),
    ):
        response = client.post("/v1/chat/completions", json=request_data)

    assert response.status_code == 200
    data = response.json()
    assert data["model"] == "smollm2"
    assert data["choices"][0]["message"]["content"] == "Hi there"
