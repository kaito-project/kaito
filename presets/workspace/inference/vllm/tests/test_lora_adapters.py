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

"""Tests for LoRA adapter registration with the adapter name prefix."""

import argparse
import json
import sys
from pathlib import Path
from unittest.mock import Mock

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import inference_api  # noqa: E402


class _LoRAModulePath:
    def __init__(self, name, path, base_model_name=None):
        self.name = name
        self.path = path


@pytest.fixture
def parse(monkeypatch):
    parser = argparse.ArgumentParser()
    parser.add_argument("--gpu-memory-utilization", type=float, default=0.92)
    parser.add_argument("--device", default="auto")
    parser.add_argument(
        "--served-model-name", "--served_model_name", nargs="+", default=None
    )
    parser.add_argument("--max-loras", type=int, default=1)
    parser.add_argument("--max-lora-rank", "--max_lora_rank", type=int, default=16)
    parser.add_argument("--lora-modules", nargs="+", default=None)
    parser.add_argument("--enable-lora", action="store_true")
    monkeypatch.setattr(inference_api.KAITOArgumentParser, "vllm_parser", parser)
    monkeypatch.setattr(
        inference_api.launcher_cli_args, "make_arg_parser", lambda parser: parser
    )
    monkeypatch.setattr(
        inference_api, "get_max_gpu_memory_utilization", Mock(return_value=0.9)
    )
    monkeypatch.setattr(inference_api, "LoRAModulePath", _LoRAModulePath)

    def _parse(argv):
        return inference_api.KAITOArgumentParser().parse_args(argv)

    return _parse


def _adapter(root: Path, name: str, config) -> Path:
    path = root / name
    path.mkdir(parents=True)
    if isinstance(config, dict):
        config = {"lora_alpha": 16, "target_modules": ["q_proj"], **config}
    content = config if isinstance(config, str) else json.dumps(config)
    (path / "adapter_config.json").write_text(content)
    (path / "adapter_model.safetensors").write_bytes(b"x")
    return path


def _argv(adapters_dir: Path, names, *extra, served="phi4", prefix=True):
    argv = [
        "--enable-lora",
        f"--kaito-adapters-dir={adapters_dir}",
        f"--kaito-expected-adapters={','.join(names)}",
    ]
    if prefix:
        argv.append("--kaito-adapter-name-prefix")
    if served:
        argv += ["--served-model-name", served]
    return argv + list(extra)


def _config_file(tmp_path: Path, vllm: str) -> str:
    path = tmp_path / "inference_config.yaml"
    path.write_text("vllm:\n" + vllm)
    return f"--kaito-config-file={path}"


def _configure(args):
    inference_api.configure_lora_adapters(args)
    return {m.name: m.path for m in args.lora_modules}


def test_prefix_registers_declared_adapters(parse, tmp_path):
    a = _adapter(tmp_path, "sql", {"r": 8})
    b = _adapter(tmp_path, "chat", {"r": 64})

    args = parse(_argv(tmp_path, ["sql", "chat"]))
    modules = _configure(args)

    assert modules == {"phi4/sql": str(a), "phi4/chat": str(b)}
    assert args.max_loras == 2
    assert args.max_lora_rank == 64


def test_prefix_uses_first_served_model_name(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})

    args = parse(
        _argv(tmp_path, ["sql"], served=None) + ["--served-model-name", "a", "b"]
    )

    assert list(_configure(args)) == ["a/sql"]


def test_without_prefix_keeps_bare_discovery_and_defaults(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 300})
    _adapter(tmp_path, "undeclared", {"r": 8})

    args = parse(_argv(tmp_path, ["sql"], prefix=False))
    modules = _configure(args)

    assert set(modules) == {"sql", "undeclared"}
    assert (args.max_loras, args.max_lora_rank) == (1, 16)


def test_prefix_without_expected_adapters_is_a_no_op(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})

    args = parse(_argv(tmp_path, []))
    modules = _configure(args)

    assert set(modules) == {"sql"}
    assert (args.max_loras, args.max_lora_rank) == (1, 16)


def test_parser_defaults_are_restored_after_prefix_parse(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})
    parse(_argv(tmp_path, ["sql"]))

    args = parse([])

    assert (args.max_loras, args.max_lora_rank) == (1, 16)


@pytest.mark.parametrize(
    ("ranks", "expected"),
    [
        ([8], 8),
        ([8, 64], 64),
        ([2], 8),
        ([12], 16),
        ([1], 1),
        ([320], 320),
        ([321], 512),
        ([512], 512),
    ],
)
def test_prefix_rounds_max_effective_rank_up(parse, tmp_path, ranks, expected):
    names = [f"a{i}" for i in range(len(ranks))]
    for name, rank in zip(names, ranks):
        _adapter(tmp_path, name, {"r": rank})

    args = parse(_argv(tmp_path, names))
    _configure(args)

    assert args.max_lora_rank == expected
    assert args.max_loras == len(ranks)


def test_rank_pattern_raises_effective_rank(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8, "rank_pattern": {"q_proj": 100}})

    args = parse(_argv(tmp_path, ["sql"]))
    _configure(args)

    assert args.max_lora_rank == 128


@pytest.mark.parametrize("source", ["cli", "config"])
def test_user_lora_sizing_wins(parse, tmp_path, source):
    adapters = tmp_path / "adapters"
    _adapter(adapters, "sql", {"r": 8})
    _adapter(adapters, "chat", {"r": 8})
    if source == "cli":
        extra = ["--max-loras", "1", "--max-lora-rank", "32"]
    else:
        extra = [_config_file(tmp_path, "  max-loras: 1\n  max_lora_rank: 32\n")]

    args = parse(_argv(adapters, ["sql", "chat"], *extra))
    _configure(args)

    assert (args.max_loras, args.max_lora_rank) == (1, 32)


@pytest.mark.parametrize("value", ["1", "16"])
def test_user_value_equal_to_vllm_default_is_kept(parse, tmp_path, value):
    _adapter(tmp_path, "sql", {"r": 8})
    _adapter(tmp_path, "chat", {"r": 8})

    args = parse(_argv(tmp_path, ["sql", "chat"], "--max-loras", value))
    _configure(args)

    assert args.max_loras == int(value)


def _expect_failure(args, *fragments):
    with pytest.raises(inference_api.LoRAAdapterError) as exc:
        inference_api.configure_lora_adapters(args)
    for fragment in fragments:
        assert fragment in str(exc.value)


def test_user_max_lora_rank_below_adapter_rank_fails(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 64})

    args = parse(_argv(tmp_path, ["sql"], "--max-lora-rank", "16"))

    _expect_failure(args, "'sql'", "rank 64 exceeds max-lora-rank 16")


def test_rank_above_512_fails(parse, tmp_path):
    _adapter(tmp_path, "big", {"r": 513})

    _expect_failure(parse(_argv(tmp_path, ["big"])), "'big'", "513")


@pytest.mark.parametrize(
    "config",
    [
        {},
        {"r": 0},
        {"r": -8},
        {"r": "8"},
        {"r": 8, "lora_alpha": None},
        {"r": 8, "rank_pattern": {"q_proj": 0}},
        {"r": 8, "rank_pattern": ["q_proj"]},
        "not json",
        "[8]",
    ],
)
def test_invalid_adapter_config_fails(parse, tmp_path, config):
    _adapter(tmp_path, "sql", config)

    _expect_failure(parse(_argv(tmp_path, ["sql"])), "'sql'")


def test_missing_required_peft_field_fails(parse, tmp_path):
    path = tmp_path / "sql"
    path.mkdir()
    (path / "adapter_config.json").write_text(json.dumps({"r": 8, "lora_alpha": 16}))

    _expect_failure(parse(_argv(tmp_path, ["sql"])), "'sql'", "target_modules")


def test_unreadable_vllm_rank_list_fails(parse, tmp_path, monkeypatch):
    _adapter(tmp_path, "sql", {"r": 8})
    monkeypatch.setattr(inference_api, "MaxLoRARanks", int)

    _expect_failure(parse(_argv(tmp_path, ["sql"])), "accepted max-lora-rank values")


def test_explicit_max_lora_rank_skips_vllm_rank_list(parse, tmp_path, monkeypatch):
    _adapter(tmp_path, "sql", {"r": 8})
    monkeypatch.setattr(inference_api, "MaxLoRARanks", int)

    args = parse(_argv(tmp_path, ["sql"], "--max-lora-rank", "16"))
    _configure(args)

    assert args.max_lora_rank == 16


def test_missing_adapter_config_fails(parse, tmp_path):
    (tmp_path / "sql").mkdir()
    (tmp_path / "sql" / "adapter_model.safetensors").write_bytes(b"x")

    _expect_failure(
        parse(_argv(tmp_path, ["sql"])), "'sql'", "adapter_config.json is missing"
    )


def test_empty_adapter_dir_fails(parse, tmp_path):
    (tmp_path / "sql").mkdir()

    _expect_failure(parse(_argv(tmp_path, ["sql"])), "'sql'", "is empty")


def test_missing_declared_adapter_fails_even_with_extra_dir(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})
    _adapter(tmp_path, "other", {"r": 8})

    _expect_failure(parse(_argv(tmp_path, ["sql", "chat"])), "'chat'", "missing")


def test_missing_adapters_dir_fails(parse, tmp_path):
    _expect_failure(parse(_argv(tmp_path / "absent", ["sql"])), "'sql'", "missing")


def test_adapter_name_cannot_escape_adapters_dir(parse, tmp_path):
    _adapter(tmp_path, "outside", {"r": 8})
    adapters = tmp_path / "adapters"
    adapters.mkdir()

    _expect_failure(parse(_argv(adapters, ["../outside"])), "invalid adapter name")


def test_extra_dirs_are_ignored_with_prefix(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})
    _adapter(tmp_path, "other", {"r": 256})

    args = parse(_argv(tmp_path, ["sql"]))

    assert list(_configure(args)) == ["phi4/sql"]
    assert (args.max_loras, args.max_lora_rank) == (1, 8)


@pytest.mark.parametrize("source", ["cli", "config"])
def test_user_lora_modules_with_prefix_fails(parse, tmp_path, source):
    adapters = tmp_path / "adapters"
    _adapter(adapters, "sql", {"r": 8})
    if source == "cli":
        extra = ["--lora-modules", "x=/tmp/x"]
    else:
        extra = [_config_file(tmp_path, "  lora-modules: x=/tmp/x\n")]

    _expect_failure(parse(_argv(adapters, ["sql"], *extra)), "lora-modules")


def test_user_lora_modules_without_prefix_is_kept(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})

    args = parse(_argv(tmp_path, ["sql"], "--lora-modules", "x=/tmp/x", prefix=False))
    inference_api.configure_lora_adapters(args)

    assert args.lora_modules == ["x=/tmp/x"]


@pytest.mark.parametrize("key", ["served-model-name", "served_model_name"])
def test_config_served_model_name_with_prefix_fails(parse, tmp_path, key):
    adapters = tmp_path / "adapters"
    _adapter(adapters, "sql", {"r": 8})

    args = parse(_argv(adapters, ["sql"], _config_file(tmp_path, f"  {key}: other\n")))

    _expect_failure(args, "served-model-name")


def test_missing_served_model_name_with_prefix_fails(parse, tmp_path):
    _adapter(tmp_path, "sql", {"r": 8})

    _expect_failure(parse(_argv(tmp_path, ["sql"], served=None)), "served-model-name")
