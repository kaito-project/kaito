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


"""Regression tests for CPU/GPU memory utilization parser defaults."""

import argparse
import sys
from pathlib import Path
from unittest.mock import Mock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import inference_api  # noqa: E402


@pytest.fixture
def gpu_probe(monkeypatch):
    parser = argparse.ArgumentParser()
    parser.add_argument("--gpu-memory-utilization", type=float, default=0.92)
    monkeypatch.setattr(inference_api.KAITOArgumentParser, "vllm_parser", parser)
    monkeypatch.setattr(
        inference_api.launcher_cli_args, "make_arg_parser", lambda parser: parser
    )
    probe = Mock(return_value=0.87)
    monkeypatch.setattr(inference_api, "get_max_gpu_memory_utilization", probe)
    return probe


@pytest.mark.parametrize("device", ["cpu", "CPU", " cpu "])
def test_cpu_defaults_to_08_without_probing_gpu(monkeypatch, gpu_probe, device):
    monkeypatch.setenv("VLLM_TARGET_DEVICE", device)

    args = inference_api.KAITOArgumentParser().parse_args([])

    assert args.gpu_memory_utilization == 0.8
    gpu_probe.assert_not_called()


@pytest.mark.parametrize("device", [None, "cuda"])
def test_non_cpu_keeps_gpu_memory_probe_default(monkeypatch, gpu_probe, device):
    if device is None:
        monkeypatch.delenv("VLLM_TARGET_DEVICE", raising=False)
    else:
        monkeypatch.setenv("VLLM_TARGET_DEVICE", device)

    args = inference_api.KAITOArgumentParser().parse_args([])

    assert args.gpu_memory_utilization == 0.87
    gpu_probe.assert_called_once_with()


@pytest.mark.parametrize("source", ["cli", "config"])
@pytest.mark.parametrize("utilization", [0.7, 0.9])
def test_cpu_respects_explicit_memory_utilization(
    monkeypatch, gpu_probe, tmp_path, source, utilization
):
    monkeypatch.setenv("VLLM_TARGET_DEVICE", "cpu")
    if source == "cli":
        argv = [f"--gpu-memory-utilization={utilization}"]
    else:
        config = tmp_path / "config.yaml"
        config.write_text(f"vllm:\n  gpu-memory-utilization: {utilization}\n")
        argv = ["--kaito-config-file", str(config)]

    args = inference_api.KAITOArgumentParser().parse_args(argv)

    assert args.gpu_memory_utilization == utilization
    gpu_probe.assert_not_called()
