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

"""Unit tests for KAITOArgumentParser.parse_args.

Focuses on the parse_known_args-based forwarding to the vLLM parser, which
must tolerate unknown vLLM flags so stale entries in either the CLI or the
KAITO config file do not crash the server.
"""

import argparse
import sys
from pathlib import Path
from unittest.mock import patch

import pytest

parent_dir = str(Path(__file__).resolve().parent.parent)
sys.path.insert(0, parent_dir)

from inference_api import KAITOArgumentParser, KaitoConfig  # noqa: E402


def _build_vllm_parser():
    """Build a small real ArgumentParser used to stand in for the vLLM parser."""
    p = argparse.ArgumentParser(add_help=False)
    p.add_argument("--model", type=str, default="/workspace/vllm/weights")
    p.add_argument("--max-model-len", type=int, default=None)
    p.add_argument("--port", type=int, default=5000)
    p.add_argument("--chat-template", type=str, default=None)
    return p


@pytest.fixture
def make_parser():
    """Construct a KAITOArgumentParser whose vllm_parser is a real ArgumentParser."""

    def _factory():
        vllm_parser = _build_vllm_parser()
        # api_server.make_arg_parser is mocked at module level; force it to
        # return our small real parser so set_defaults / parse_known_args work.
        # get_max_gpu_memory_utilization touches pynvml (also mocked), so stub
        # it out to keep _reset_vllm_defaults importable in a CPU-only env.
        with (
            patch(
                "inference_api.api_server.make_arg_parser",
                side_effect=lambda _p: vllm_parser,
            ),
            patch("inference_api.get_max_gpu_memory_utilization", return_value=0.9),
        ):
            parser = KAITOArgumentParser()
        return parser, vllm_parser

    return _factory


class TestParseArgsUnknownFlags:
    """Behavior of parse_args around unknown vLLM flags."""

    def test_unknown_cli_flag_is_ignored(self, make_parser, caplog):
        parser, _ = make_parser()
        with caplog.at_level("WARNING", logger="inference_api"):
            ns = parser.parse_args(
                [
                    "--model",
                    "facebook/opt-125m",
                    "--definitely-not-a-real-flag",
                    "value",
                ]
            )
        assert ns.model == "facebook/opt-125m"
        assert any(
            "Ignoring unknown vLLM args" in rec.message
            and "--definitely-not-a-real-flag" in rec.message
            for rec in caplog.records
        )

    def test_known_only_flags_emit_no_warning(self, make_parser, caplog):
        parser, _ = make_parser()
        with caplog.at_level("WARNING", logger="inference_api"):
            ns = parser.parse_args(
                ["--model", "facebook/opt-125m", "--max-model-len", "1024"]
            )
        assert ns.model == "facebook/opt-125m"
        assert ns.max_model_len == 1024
        assert not any(
            "Ignoring unknown vLLM args" in rec.message for rec in caplog.records
        )

    def test_unknown_flag_from_config_file_is_ignored(
        self, make_parser, tmp_path, caplog
    ):
        config_file = tmp_path / "kaito.yaml"
        config_file.write_text(
            KaitoConfig(
                vllm={"max-model-len": 2048, "removed-vllm-flag": "ignored"},
                max_probe_steps=3,
                kv_cache_cpu_memory_utilization=0.4,
            ).to_yaml()
        )

        parser, _ = make_parser()
        with caplog.at_level("WARNING", logger="inference_api"):
            ns = parser.parse_args(
                [
                    "--model",
                    "facebook/opt-125m",
                    "--kaito-config-file",
                    str(config_file),
                ]
            )

        # Known config-file flag flows through to vllm_args.
        assert ns.max_model_len == 2048
        # KAITO-only config values picked up from the config file.
        assert ns.kaito_max_probe_steps == 3
        assert ns.kaito_kv_cache_cpu_memory_utilization == 0.4
        # Unknown vllm flag from config is reported but not fatal.
        assert any(
            "Ignoring unknown vLLM args" in rec.message
            and "--removed-vllm-flag" in rec.message
            for rec in caplog.records
        )

    def test_cli_kaito_overrides_config_kaito_values(self, make_parser, tmp_path):
        config_file = tmp_path / "kaito.yaml"
        config_file.write_text(
            KaitoConfig(
                vllm={},
                max_probe_steps=3,
                kv_cache_cpu_memory_utilization=0.4,
            ).to_yaml()
        )

        parser, _ = make_parser()
        ns = parser.parse_args(
            [
                "--kaito-config-file",
                str(config_file),
                "--kaito-max-probe-steps",
                "9",
                "--kaito-kv-cache-cpu-memory-utilization",
                "0.7",
            ]
        )
        assert ns.kaito_max_probe_steps == 9
        assert ns.kaito_kv_cache_cpu_memory_utilization == 0.7

    def test_returned_namespace_merges_kaito_and_vllm(self, make_parser):
        parser, _ = make_parser()
        ns = parser.parse_args(
            [
                "--model",
                "facebook/opt-125m",
                "--max-model-len",
                "512",
                "--kaito-adapters-dir",
                "/tmp/adapters",
            ]
        )
        assert ns.model == "facebook/opt-125m"
        assert ns.max_model_len == 512
        assert ns.kaito_adapters_dir == "/tmp/adapters"
