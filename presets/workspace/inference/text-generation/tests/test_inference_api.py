# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
import importlib
import sys
from pathlib import Path
from unittest.mock import patch

import pytest
from fastapi.testclient import TestClient
from transformers import AutoTokenizer

# Get the parent directory of the current file
parent_dir = str(Path(__file__).resolve().parent.parent)
# Add the parent directory to sys.path
sys.path.append(parent_dir)

CHAT_TEMPLATE = ("{{ bos_token }}{% for message in messages %}{% if (message['role'] == 'user') %}"
    "{{'<|user|>' + '\n' + message['content'] + '<|end|>' + '\n' + '<|assistant|>' + '\n'}}"
    "{% elif (message['role'] == 'assistant') %}{{message['content'] + '<|end|>' + '\n'}}{% endif %}{% endfor %}")

@pytest.fixture(params=[
    {"pipeline": "text-generation", "model_path": "stanford-crfm/alias-gpt2-small-x21", "device": "cpu"},
    {"pipeline": "conversational", "model_path": "stanford-crfm/alias-gpt2-small-x21", "device": "cpu"},
])
def configured_app(request):
    original_argv = sys.argv.copy()
    # Use request.param to set correct test arguments for each configuration
    test_args = [
        'program_name',
        '--pipeline', request.param['pipeline'],
        '--pretrained_model_name_or_path', request.param['model_path'],
        '--device_map', request.param['device'],
        '--allow_remote_files', 'True',
        '--chat_template', CHAT_TEMPLATE
    ]
    sys.argv = test_args

    import inference_api
    importlib.reload(inference_api) # Reload to prevent module caching
    from inference_api import app

    # Attach the request params to the app instance for access in tests
    app.test_config = request.param
    yield app

    sys.argv = original_argv

def test_conversational(configured_app):
    if configured_app.test_config['pipeline'] != 'conversational':
        pytest.skip("Skipping non-conversational tests")
    client = TestClient(configured_app)
    messages = [
        {"role": "user", "content": "What is your favourite condiment?"},
        {"role": "assistant", "content": "Well, im quite partial to a good squeeze of fresh lemon juice. It adds just the right amount of zesty flavour to whatever im cooking up in the kitchen!"},
        {"role": "user", "content": "Do you have mayonnaise recipes?"}
    ]
    request_data = {
        "messages": messages,
        "generate_kwargs": {"max_new_tokens": 20, "do_sample": True}
    }
    response = client.post("/chat", json=request_data)

    assert response.status_code == 200
    data = response.json()
    assert "Result" in data
    assert len(data["Result"]) > 0  # Check if the conversation result is not empty

def test_missing_messages_for_conversation(configured_app):
    if configured_app.test_config['pipeline'] != 'conversational':
        pytest.skip("Skipping non-conversational tests")
    client = TestClient(configured_app)
    request_data = {
        # "messages" is missing for conversational pipeline
    }
    response = client.post("/chat", json=request_data)
    assert response.status_code == 400  # Expecting a Bad Request response due to missing messages
    assert "Conversational parameter messages required" in response.json().get("detail", "")

def test_text_generation(configured_app):
    if configured_app.test_config['pipeline'] != 'text-generation':
        pytest.skip("Skipping non-text-generation tests")
    client = TestClient(configured_app)
    request_data = {
        "prompt": "Hello, world!",
        "return_full_text": True,
        "clean_up_tokenization_spaces": False,
        "generate_kwargs": {"max_length": 50, "min_length": 10}  # Example generate_kwargs
    }
    response = client.post("/chat", json=request_data)
    assert response.status_code == 200
    data = response.json()
    assert "Result" in data
    assert len(data["Result"]) > 0  # Check if the result text is not empty

def test_missing_prompt(configured_app):
    if configured_app.test_config['pipeline'] != 'text-generation':
        pytest.skip("Skipping non-text-generation tests")
    client = TestClient(configured_app)
    request_data = {
        # "prompt" is missing
        "return_full_text": True,
        "clean_up_tokenization_spaces": False,
        "generate_kwargs": {"max_length": 50}
    }
    response = client.post("/chat", json=request_data)
    assert response.status_code == 400  # Expecting a Bad Request response due to missing prompt
    assert "Text generation parameter prompt required" in response.json().get("detail", "")

def test_read_main(configured_app):
    client = TestClient(configured_app)
    response = client.get("/")
    assert response.status_code == 200
    assert response.json() == {"message": "Server is running"}

def test_health_check(configured_app):
    client = TestClient(configured_app)
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "Healthy"}

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
        memoryUsed=1 * (1024 ** 3),  # 1 GB
        memoryTotal=2 * (1024 ** 3)  # 2 GB
    )

    # Mock torch.cuda.is_available to simulate an environment with GPUs
    # Mock GPUtil.getGPUs to return a list containing the mock GPU object
    with patch('torch.cuda.is_available', return_value=True), \
            patch('GPUtil.getGPUs', return_value=[mock_gpu]):
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
        assert data["cpu_info"] is None  # Assuming CPU info is not present when GPUs are available

def test_get_metrics_no_gpus(configured_app):
    client = TestClient(configured_app)
    # Mock GPUtil.getGPUs to simulate an environment without GPUs
    with patch('torch.cuda.is_available', return_value=False), \
            patch('psutil.cpu_percent', return_value=20.0), \
            patch('psutil.cpu_count', side_effect=[4, 8]), \
            patch('psutil.virtual_memory') as mock_virtual_memory:
        mock_virtual_memory.return_value.used = 4 * (1024 ** 3)  # 4 GB
        mock_virtual_memory.return_value.total = 16 * (1024 ** 3)  # 16 GB
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

def test_default_generation_params(configured_app):
    if configured_app.test_config['pipeline'] != 'text-generation':
        pytest.skip("Skipping non-text-generation tests")

    client = TestClient(configured_app)

    request_data = {
        "prompt": "Test default params",
        "return_full_text": True,
        "clean_up_tokenization_spaces": False
        # Note: generate_kwargs is not provided, so defaults should be used
    }

    with patch('inference_api.pipeline') as mock_pipeline:
        mock_pipeline.return_value = [{"generated_text": "Mocked response"}]  # Mock the response of the pipeline function

        response = client.post("/chat", json=request_data)

        assert response.status_code == 200
        data = response.json()
        assert "Result" in data
        assert data["Result"] == "Mocked response", "The response content doesn't match the expected mock response"

        # Check the default args
        _, kwargs = mock_pipeline.call_args
        assert kwargs['max_length'] == 200
        assert kwargs['min_length'] == 0
        assert kwargs['do_sample'] is True
        assert kwargs['temperature'] == 1.0
        assert kwargs['top_k'] == 10
        assert kwargs['top_p'] == 1
        assert kwargs['typical_p'] == 1
        assert kwargs['repetition_penalty'] == 1
        assert kwargs['num_beams'] == 1
        assert kwargs['early_stopping'] is False

def test_generation_with_max_length(configured_app):
    if configured_app.test_config['pipeline'] != 'text-generation':
        pytest.skip("Skipping non-text-generation tests")

    client = TestClient(configured_app)
    prompt = "This prompt requests a response of a certain minimum length to test the functionality."
    avg_res_len = 15
    max_length = 40  # Set to lower than default (200) to prevent test hanging

    request_data = {
        "prompt": prompt,
        "return_full_text": True,
        "clean_up_tokenization_spaces": False,
        "generate_kwargs": {"max_length": max_length}
    }

    response = client.post("/chat", json=request_data)

    assert response.status_code == 200
    data = response.json()
    print("Response: ", data["Result"])
    assert "Result" in data, "The response should contain a 'Result' key"

    tokenizer = AutoTokenizer.from_pretrained(configured_app.test_config['model_path'])
    prompt_tokens = tokenizer.tokenize(prompt)
    total_tokens = tokenizer.tokenize(data["Result"])  # data["Result"] includes the input prompt

    prompt_tokens_len = len(prompt_tokens)
    max_new_tokens = max_length - prompt_tokens_len
    new_tokens = len(total_tokens) - prompt_tokens_len

    assert avg_res_len <= new_tokens, f"Ideally response should generate at least 15 tokens"
    assert new_tokens <= max_new_tokens, "Response must not generate more than max new tokens"
    assert len(total_tokens) <= max_length, "Total # of tokens has to be less than or equal to max_length"

def test_generation_with_min_length(configured_app):
    if configured_app.test_config['pipeline'] != 'text-generation':
        pytest.skip("Skipping non-text-generation tests")

    client = TestClient(configured_app)
    prompt = "This prompt requests a response of a certain minimum length to test the functionality."
    min_length = 30
    max_length = 40

    request_data = {
        "prompt": prompt,
        "return_full_text": True,
        "clean_up_tokenization_spaces": False,
        "generate_kwargs": {"min_length": min_length, "max_length": max_length}
    }

    response = client.post("/chat", json=request_data)

    assert response.status_code == 200
    data = response.json()
    assert "Result" in data, "The response should contain a 'Result' key"

    tokenizer = AutoTokenizer.from_pretrained(configured_app.test_config['model_path'])
    prompt_tokens = tokenizer.tokenize(prompt)
    total_tokens = tokenizer.tokenize(data["Result"])  # data["Result"] includes the input prompt

    prompt_tokens_len = len(prompt_tokens)

    min_new_tokens = min_length - prompt_tokens_len
    max_new_tokens = max_length - prompt_tokens_len
    new_tokens = len(total_tokens) - prompt_tokens_len

    assert min_new_tokens <= new_tokens <= max_new_tokens, "Response should generate at least min_new_tokens and at most max_new_tokens new tokens"
    assert len(total_tokens) <= max_length, "Total # of tokens has to be less than or equal to max_length"
