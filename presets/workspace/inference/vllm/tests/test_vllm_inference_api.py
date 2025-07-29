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

import os
import shutil
import socket
import subprocess
import sys
import time
from pathlib import Path

import pytest
import requests
from huggingface_hub import snapshot_download

# Get the parent directory of the current file
parent_dir = str(Path(__file__).resolve().parent.parent)
# Add the parent directory to sys.path
sys.path.append(parent_dir)

try:
    from inference_api import KaitoConfig, binary_search_with_limited_steps
except ImportError as e:
    raise ImportError(f"Error importing required module(s): {e}") from e

TEST_MODEL = "facebook/opt-125m"
TEST_ADAPTER_NAME1 = "mylora1"
TEST_ADAPTER_NAME2 = "mylora2"
TEST_MODEL_NAME = "mymodel"
TEST_MODEL_LEN = 1024
CHAT_TEMPLATE = (
    "{{ bos_token }}{% for message in messages %}{% if (message['role'] == 'user') %}"
    "{{'<|user|>' + '\n' + message['content'] + '<|end|>' + '\n' + '<|assistant|>' + '\n'}}"
    "{% elif (message['role'] == 'assistant') %}{{message['content'] + '<|end|>' + '\n'}}{% endif %}{% endfor %}"
)


@pytest.fixture(scope="session", autouse=True)
def setup_server(request, tmp_path_factory, autouse=True):
    if os.getenv("DEVICE") == "cpu":
        pytest.skip("Skipping test on cpu device")
    print("\n>>> Doing setup")
    port = find_available_port()
    global TEST_PORT
    TEST_PORT = port

    # prepare testing adapter images
    tmp_file_dir = tmp_path_factory.mktemp("adapter")
    print(f"Downloading adapter image to {tmp_file_dir}")
    snapshot_download(
        repo_id="slall/facebook-opt-125M-imdb-lora",
        local_dir=str(tmp_file_dir / TEST_ADAPTER_NAME1),
    )
    snapshot_download(
        repo_id="slall/facebook-opt-125M-imdb-lora",
        local_dir=str(tmp_file_dir / TEST_ADAPTER_NAME2),
    )

    # prepare testing config file
    config_file = tmp_file_dir / "config.yaml"
    kaito_config = KaitoConfig(
        vllm={"max-model-len": TEST_MODEL_LEN, "served-model-name": TEST_MODEL_NAME},
        max_probe_steps=0,
        kv_cache_cpu_memory_utilization=0.6,
    )
    with open(config_file, "w") as f:
        f.write(kaito_config.to_yaml())

    args = [
        "python3",
        os.path.join(parent_dir, "inference_api.py"),
        "--model",
        TEST_MODEL,
        "--chat-template",
        CHAT_TEMPLATE,
        "--max-model-len",
        "2048",  # expected to be overridden by config file
        "--port",
        str(TEST_PORT),
        "--kaito-adapters-dir",
        tmp_file_dir,
        "--kaito-config-file",
        config_file,
    ]
    print(f">>> Starting server on port {TEST_PORT}")
    env = os.environ.copy()
    env["MAX_PROBE_STEPS"] = "0"
    process = subprocess.Popen(
        args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env
    )

    def fin():
        process.terminate()
        process.wait()
        stderr = process.stderr.read().decode()
        print(f">>> Server stderr: {stderr}")
        stdout = process.stdout.read().decode()
        print(f">>> Server stdout: {stdout}")
        print("\n>>> Doing teardown")
        print(f"Removing adapter image from {tmp_file_dir}")
        shutil.rmtree(tmp_file_dir)

    if not is_port_open("localhost", TEST_PORT):
        fin()
        pytest.fail("failed to launch vllm server")

    request.addfinalizer(fin)


def is_port_open(host, port, timeout=60):
    start_time = time.time()
    while time.time() - start_time < timeout:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.settimeout(1)  # Set a short timeout for each connection attempt
            result = sock.connect_ex((host, port))
            print(">>> waiting for server to start")
            if result == 0:
                print(f">>> server started in {int(time.time() - start_time)} seconds")
                return True
            time.sleep(1)  # Wait a bit before retrying
    return False


def find_available_port(start_port=5000, end_port=8000):
    for port in range(start_port, end_port + 1):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            if s.connect_ex(("localhost", port)) != 0:
                return port
    raise RuntimeError("No available ports found")


def test_completions_api(setup_server):
    request_data = {
        "model": TEST_MODEL_NAME,
        "prompt": "Say this is a test",
        "max_tokens": 7,
        "temperature": 0.5,
        "n": 2,
    }

    response = requests.post(
        f"http://127.0.0.1:{TEST_PORT}/v1/completions", json=request_data
    )
    data = response.json()
    assert "choices" in data, "The response should contain a 'choices' key"
    assert len(data["choices"]) == 2, "The response should contain two completion"

    for choice in data["choices"]:
        assert "text" in choice, "Each choice should contain a 'text' key"
        assert len(choice["text"]) > 0, "The completion text should not be empty"


def test_chat_completions_api(setup_server):
    request_data = {
        "model": TEST_MODEL_NAME,
        "messages": [
            {"role": "user", "content": "Hello!"},
            {"role": "assistant", "content": "Hi there! How can I help you today?"},
        ],
        "max_tokens": 7,
        "temperature": 0.5,
        "n": 2,
    }

    response = requests.post(
        f"http://127.0.0.1:{TEST_PORT}/v1/chat/completions", json=request_data
    )
    data = response.json()

    assert "choices" in data, "The response should contain a 'choices' key"
    assert len(data["choices"]) == 2, "The response should contain two completion"

    for choice in data["choices"]:
        assert "message" in choice, "Each choice should contain a 'message' key"
        assert "content" in choice["message"], (
            "Each message should contain a 'content' key"
        )
        assert len(choice["message"]["content"]) > 0, (
            "The completion text should not be empty"
        )


def test_model_list(setup_server):
    response = requests.get(f"http://127.0.0.1:{TEST_PORT}/v1/models")
    data = response.json()

    assert "data" in data, f"The response should contain a 'data' key, but got {data}"
    assert len(data["data"]) == 3, (
        f"The response should contain three models, but got {data['data']}"
    )
    assert data["data"][0]["id"] == TEST_MODEL_NAME, (
        f"The first model should be the test model, but got {data['data'][0]['id']}"
    )
    assert data["data"][0]["max_model_len"] == TEST_MODEL_LEN, (
        f"The first model should have the test model length, but got {data['data'][0]['max_model_len']}"
    )
    assert data["data"][1]["id"] == TEST_ADAPTER_NAME2, (
        f"The second model should be the test adapter, but got {data['data'][1]['id']}"
    )
    assert data["data"][1]["parent"] == TEST_MODEL_NAME, (
        f"The second model should have the test model as parent, but got {data['data'][1]['parent']}"
    )
    assert data["data"][2]["id"] == TEST_ADAPTER_NAME1, (
        f"The third model should be the test adapter, but got {data['data'][2]['id']}"
    )
    assert data["data"][2]["parent"] == TEST_MODEL_NAME, (
        f"The third model should have the test model as parent, but got {data['data'][2]['parent']}"
    )


def test_binary_search_with_limited_steps():
    def is_safe_fn(x):
        return x <= 10

    # Test case 1: all values are safe
    result = binary_search_with_limited_steps(10, 1, is_safe_fn)
    assert result == 10, f"Expected 10, but got {result}"

    result = binary_search_with_limited_steps(10, 10, is_safe_fn)
    assert result == 10, f"Expected 10, but got {result}"

    # Test case 2: partial safe, find the exact value
    result = binary_search_with_limited_steps(20, 3, is_safe_fn)
    assert result == 10, f"Expected 10, but got {result}"

    result = binary_search_with_limited_steps(30, 6, is_safe_fn)
    assert result == 10, f"Expected 10, but got {result}"

    # Test case 3: partial safe, find an approximate value
    result = binary_search_with_limited_steps(30, 3, is_safe_fn)
    assert result == 7, f"Expected 7, but got {result}"

    # Test case 4: all values are unsafe
    result = binary_search_with_limited_steps(10, 1, lambda x: False)
    assert result == 0, f"Expected 0, but got {result}"

    result = binary_search_with_limited_steps(20, 100, lambda x: False)
    assert result == 0, f"Expected 0, but got {result}"
