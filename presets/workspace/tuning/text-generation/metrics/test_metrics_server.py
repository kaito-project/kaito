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
from unittest.mock import MagicMock, patch

import pytest
from fastapi.testclient import TestClient
from metrics_server import app

client = TestClient(app)


def mock_getGPUs():
    class MockGPU:
        id = "GPU-1234"
        name = "GeForce GTX 950"
        load = 0.25  # 25%
        temperature = 55  # 55 C
        memoryUsed = 1 * (1024**3)  # 1 GB
        memoryTotal = 2 * (1024**3)  # 2 GB

    return [MockGPU()]


def mock_cpu_percent(interval=1, percpu=False):
    return 20.0


def mock_cpu_count(logical=True):
    return 8 if logical else 4


def mock_virtual_memory():
    svmem = MagicMock()
    svmem.used = 4 * (1024**3)  # 4 GB
    svmem.total = 16 * (1024**3)  # 16 GB
    return svmem


@pytest.fixture
def set_env_vars():
    os.environ["DEBUG_MODE"] = "true"
    yield
    os.environ.pop("DEBUG_MODE")


def test_metrics_endpoint_with_gpu(set_env_vars):
    with (
        patch("GPUtil.getGPUs", side_effect=mock_getGPUs),
        patch("torch.cuda.is_available", return_value=True),
    ):
        response = client.get("/metrics")
        print("response:", response)
        print("response data:", response.json())
        assert response.status_code == 200
        data = response.json()
        assert "gpu_info" in data
        assert data["gpu_info"][0]["id"] == "GPU-1234"
        assert data["gpu_info"][0]["name"] == "GeForce GTX 950"
        assert data["gpu_info"][0]["load"] == "25.00%"
        assert data["gpu_info"][0]["temperature"] == "55 C"
        assert data["gpu_info"][0]["memory"]["used"] == "1.00 GB"
        assert data["gpu_info"][0]["memory"]["total"] == "2.00 GB"
        assert data["cpu_info"] is None


def test_metrics_endpoint_with_cpu(set_env_vars):
    with (
        patch("torch.cuda.is_available", return_value=False),
        patch("psutil.cpu_percent", side_effect=mock_cpu_percent),
        patch("psutil.cpu_count", side_effect=mock_cpu_count),
        patch("psutil.virtual_memory", side_effect=mock_virtual_memory),
    ):
        response = client.get("/metrics")
        assert response.status_code == 200
        data = response.json()
        assert "cpu_info" in data
        assert data["cpu_info"]["load_percentage"] == 20.0
        assert data["cpu_info"]["physical_cores"] == 4
        assert data["cpu_info"]["total_cores"] == 8
        assert data["cpu_info"]["memory"]["used"] == "4.00 GB"
        assert data["cpu_info"]["memory"]["total"] == "16.00 GB"
        assert data["gpu_info"] is None


def test_metrics_endpoint_failure(set_env_vars):
    with patch("torch.cuda.is_available", side_effect=Exception("Test Exception")):
        response = client.get("/metrics")
        assert response.status_code == 500
        assert response.json() == {"detail": "Test Exception"}
