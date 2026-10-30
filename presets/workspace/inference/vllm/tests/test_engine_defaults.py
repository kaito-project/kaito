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
    parser.add_argument("--device", default="auto")
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
    assert args.kaito_kv_cache_cpu_memory_utilization is None
    gpu_probe.assert_not_called()


@pytest.mark.parametrize("device", [None, "cuda"])
def test_non_cpu_keeps_gpu_memory_probe_default(monkeypatch, gpu_probe, device):
    if device is None:
        monkeypatch.delenv("VLLM_TARGET_DEVICE", raising=False)
    else:
        monkeypatch.setenv("VLLM_TARGET_DEVICE", device)

    args = inference_api.KAITOArgumentParser().parse_args([])

    assert args.gpu_memory_utilization == 0.87
    assert args.kaito_kv_cache_cpu_memory_utilization == 0.5
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


@pytest.mark.parametrize("source", ["cli", "config"])
def test_explicit_cpu_device_has_no_offload_default(
    monkeypatch, gpu_probe, tmp_path, source
):
    monkeypatch.delenv("VLLM_TARGET_DEVICE", raising=False)
    if source == "cli":
        argv = ["--device=cpu"]
    else:
        config = tmp_path / "config.yaml"
        config.write_text("vllm:\n  device: cpu\n")
        argv = ["--kaito-config-file", str(config)]

    args = inference_api.KAITOArgumentParser().parse_args(argv)

    assert args.device == "cpu"
    assert args.kaito_kv_cache_cpu_memory_utilization is None
    assert args.gpu_memory_utilization == 0.8
    gpu_probe.assert_not_called()


@pytest.mark.parametrize("device", ["cpu", "cuda"])
@pytest.mark.parametrize("source", ["cli", "config"])
@pytest.mark.parametrize("utilization", [0.0, 0.6])
def test_explicit_offload_utilization_survives_config_merge(
    monkeypatch, gpu_probe, tmp_path, device, source, utilization
):
    monkeypatch.setenv("VLLM_TARGET_DEVICE", device)
    if source == "cli":
        argv = [f"--kaito-kv-cache-cpu-memory-utilization={utilization}"]
    else:
        config = tmp_path / "config.yaml"
        config.write_text(f"kv_cache_cpu_memory_utilization: {utilization}\n")
        argv = ["--kaito-config-file", str(config)]

    args = inference_api.KAITOArgumentParser().parse_args(argv)

    assert args.kaito_kv_cache_cpu_memory_utilization == utilization


def test_cli_offload_utilization_overrides_config(monkeypatch, gpu_probe, tmp_path):
    monkeypatch.setenv("VLLM_TARGET_DEVICE", "cuda")
    config = tmp_path / "config.yaml"
    config.write_text("kv_cache_cpu_memory_utilization: 0.6\n")

    args = inference_api.KAITOArgumentParser().parse_args(
        [
            "--kaito-config-file",
            str(config),
            "--kaito-kv-cache-cpu-memory-utilization=0",
        ]
    )

    assert args.kaito_kv_cache_cpu_memory_utilization == 0


@pytest.mark.parametrize("device,expected", [("cpu", None), ("cuda", 0.5)])
def test_config_without_offload_setting_uses_runtime_default(
    monkeypatch, gpu_probe, tmp_path, device, expected
):
    monkeypatch.setenv("VLLM_TARGET_DEVICE", device)
    config = tmp_path / "config.yaml"
    config.write_text("vllm: {}\n")

    args = inference_api.KAITOArgumentParser().parse_args(
        ["--kaito-config-file", str(config)]
    )

    assert args.kaito_kv_cache_cpu_memory_utilization == expected


@pytest.mark.parametrize("device", ["cpu", "cuda"])
@pytest.mark.parametrize("source", ["cli", "config"])
@pytest.mark.parametrize("utilization", [0.7, 0.9])
def test_explicit_device_and_memory_budget_skip_gpu_probe(
    monkeypatch, gpu_probe, tmp_path, device, source, utilization
):
    monkeypatch.delenv("VLLM_TARGET_DEVICE", raising=False)
    if source == "cli":
        argv = [f"--device={device}", f"--gpu-memory-utilization={utilization}"]
    else:
        config = tmp_path / "config.yaml"
        config.write_text(
            f"vllm:\n  device: {device}\n  gpu-memory-utilization: {utilization}\n"
        )
        argv = ["--kaito-config-file", str(config)]

    args = inference_api.KAITOArgumentParser().parse_args(argv)

    assert args.device == device
    assert args.gpu_memory_utilization == utilization
    gpu_probe.assert_not_called()


def test_parser_reuse_resolves_each_runtime_memory_default(monkeypatch, gpu_probe):
    monkeypatch.delenv("VLLM_TARGET_DEVICE", raising=False)
    parser = inference_api.KAITOArgumentParser()
    gpu_probe.assert_not_called()
    assert parser.parse_args(["--device=cpu"]).gpu_memory_utilization == 0.8
    gpu_probe.assert_not_called()
    assert parser.parse_args(["--device=cuda"]).gpu_memory_utilization == 0.87
    gpu_probe.assert_called_once_with()
    assert parser.parse_args(["--device=cpu"]).gpu_memory_utilization == 0.8
    gpu_probe.assert_called_once_with()
