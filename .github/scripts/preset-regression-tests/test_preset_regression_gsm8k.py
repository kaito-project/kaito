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

from __future__ import annotations

import asyncio
import copy
import json
import os
import subprocess
import unittest
from pathlib import Path

from preset_regression_gsm8k import (
    EvaluationDeadlineExceeded,
    compare_accuracy,
    effective_max_gen_tokens,
    failed_samples,
    generation_kwargs,
    request_or_empty,
    resolve_gsm8k_execution,
    resolve_profile,
    responses_by_document,
    should_retry_api_error,
    validate_gsm8k_data,
)
from preset_regression_test_utils import load_yaml, validate_coverage

ROOT = Path(__file__).resolve().parents[3]
MODEL_CATALOG = ROOT / "presets/workspace/models/model_catalog.yaml"
GSM_CONFIG = ROOT / "benchmarks/gsm8k/config.yaml"
GSM_BASELINES = ROOT / "benchmarks/gsm8k/baselines.yaml"


class PresetRegressionGSM8KTest(unittest.TestCase):
    def test_repository_manifests_are_valid(self):
        validate_gsm8k_data(load_yaml(GSM_CONFIG), load_yaml(GSM_BASELINES))

    def test_each_catalog_model_has_a_baseline(self):
        catalog_models = {
            str(model["name"]) for model in load_yaml(MODEL_CATALOG)["models"]
        }
        baseline_models = {
            str(target["model"])
            for target in load_yaml(GSM_BASELINES).get("targets", [])
        }
        self.assertEqual([], sorted(catalog_models - baseline_models))

    def test_models_use_large_default_thinking_profile(self):
        name, profile = resolve_profile(
            load_yaml(GSM_CONFIG), "deepseek-ai/DeepSeek-V4-Flash-0731"
        )
        self.assertEqual("chat-thinking-v1", name)
        self.assertEqual(8192, profile["maxGenTokens"])

    def test_models_use_default_concurrency(self):
        config = load_yaml(GSM_CONFIG)
        self.assertEqual(
            16,
            resolve_gsm8k_execution(config, "microsoft/Phi-4-mini-instruct")[
                "numConcurrent"
            ],
        )

    def test_mistral_medium_uses_higher_concurrency(self):
        self.assertEqual(
            32,
            resolve_gsm8k_execution(
                load_yaml(GSM_CONFIG), "mistralai/Mistral-Medium-3.5-128B"
            )["numConcurrent"],
        )

    def test_retry_policy_excludes_timeouts(self):
        self.assertFalse(
            should_retry_api_error(None, is_connection_error=True, is_timeout=True)
        )
        self.assertTrue(
            should_retry_api_error(None, is_connection_error=True, is_timeout=False)
        )
        self.assertTrue(
            should_retry_api_error(429, is_connection_error=False, is_timeout=False)
        )
        self.assertFalse(
            should_retry_api_error(400, is_connection_error=False, is_timeout=False)
        )
        self.assertTrue(
            should_retry_api_error(503, is_connection_error=False, is_timeout=False)
        )

    def test_request_timeout_becomes_empty_response(self):
        timeout_count = 0

        async def timeout():
            raise TimeoutError("deadline")

        def record_timeouts(count):
            nonlocal timeout_count
            timeout_count += count

        result = asyncio.run(request_or_empty(timeout(), 1, record_timeouts))
        self.assertEqual([""], result)
        self.assertEqual(1, timeout_count)

    def test_suite_deadline_is_not_converted_to_empty_response(self):
        timeout_count = 0

        async def deadline():
            raise EvaluationDeadlineExceeded("suite deadline")

        def record_timeouts(count):
            nonlocal timeout_count
            timeout_count += count

        with self.assertRaises(EvaluationDeadlineExceeded):
            asyncio.run(request_or_empty(deadline(), 1, record_timeouts))
        self.assertEqual(0, timeout_count)

    def test_model_profile_overrides(self):
        config = load_yaml(GSM_CONFIG)
        ministral_name, ministral = resolve_profile(
            config, "mistralai/Ministral-3-14B-Instruct-2512"
        )
        mistral_name, mistral = resolve_profile(
            config, "mistralai/Mistral-Medium-3.5-128B"
        )
        gemma_name, gemma = resolve_profile(config, "google/gemma-4-12B-it")
        self.assertEqual("chat-nonthinking-v1", ministral_name)
        self.assertNotIn("chatTemplateKwargs", ministral)
        self.assertEqual("mistral-thinking-v1", mistral_name)
        self.assertNotIn("chatTemplateKwargs", mistral)
        self.assertEqual({"reasoning_effort": "high"}, mistral["requestKwargs"])
        self.assertEqual("chat-thinking-v1", gemma_name)
        self.assertEqual({"enable_thinking": True}, gemma["chatTemplateKwargs"])

        nemotron_name, nemotron = resolve_profile(
            config, "nvidia/NVIDIA-Nemotron-Nano-9B-v2"
        )
        self.assertEqual("chat-thinking-v1", nemotron_name)
        self.assertEqual({"enable_thinking": True}, nemotron["chatTemplateKwargs"])

    def test_mistral_thinking_uses_native_request_field(self):
        _, profile = resolve_profile(
            load_yaml(GSM_CONFIG), "mistralai/Mistral-Medium-3.5-128B"
        )
        kwargs = generation_kwargs(profile)
        self.assertEqual("high", kwargs["reasoning_effort"])
        self.assertNotIn("chat_template_kwargs", kwargs)

    def test_duplicate_baseline_identity_is_rejected(self):
        config = load_yaml(GSM_CONFIG)
        baselines = load_yaml(GSM_BASELINES)
        baselines["targets"].append(copy.deepcopy(baselines["targets"][0]))
        with self.assertRaisesRegex(ValueError, "unique"):
            validate_gsm8k_data(config, baselines)

    def test_missing_empty_response_count_is_rejected(self):
        config = load_yaml(GSM_CONFIG)
        baselines = load_yaml(GSM_BASELINES)
        del baselines["targets"][0]["emptyResponses"]
        with self.assertRaisesRegex(ValueError, "invalid GSM8K result"):
            validate_gsm8k_data(config, baselines)

    def test_accuracy_threshold_boundary(self):
        baseline = {"profile": "chat-thinking-v1", "accuracy": 0.8}
        self.assertTrue(
            compare_accuracy(0.75, baseline, "chat-thinking-v1", 0.05, True)["passed"]
        )
        self.assertFalse(
            compare_accuracy(0.749, baseline, "chat-thinking-v1", 0.05, True)["passed"]
        )

    def test_invalid_accuracy_tolerance_is_rejected(self):
        baseline = {"profile": "chat-thinking-v1", "accuracy": 0.8}
        with self.assertRaisesRegex(ValueError, "between 0 and 1"):
            compare_accuracy(0.8, baseline, "chat-thinking-v1", -0.1, True)

    def test_empty_responses_are_grouped_by_document(self):
        results = {
            "samples": {
                "gsm8k": [
                    {"doc_id": 1, "resps": [[""]]},
                    {"doc_id": 1, "resps": [[""]]},
                    {"doc_id": 2, "resps": [["#### 4"]]},
                ]
            }
        }
        self.assertEqual(
            {1: [""], 2: ["#### 4"]}, responses_by_document(results, "gsm8k")
        )

    def test_failed_samples_report_selected_filter_and_reason(self):
        results = {
            "samples": {
                "gsm8k": [
                    {
                        "doc_id": 1,
                        "filter": "strict-match",
                        "exact_match": 0.0,
                        "resps": [["The answer is 4"]],
                        "filtered_resps": ["[invalid]"],
                        "target": "work\n#### 4",
                        "doc": {"question": "ignored strict sample"},
                    },
                    {
                        "doc_id": 1,
                        "filter": "flexible-extract",
                        "exact_match": 0.0,
                        "resps": [["No numeric answer"]],
                        "filtered_resps": ["[invalid]"],
                        "target": "work\n#### 4",
                        "doc": {"question": "What is two plus two?"},
                    },
                    {
                        "doc_id": 2,
                        "filter": "flexible-extract",
                        "exact_match": 0.0,
                        "resps": [["The answer is 5"]],
                        "filtered_resps": ["5"],
                        "target": "work\n#### 4",
                        "doc": {"question": "What is two plus two?"},
                    },
                    {
                        "doc_id": 3,
                        "filter": "flexible-extract",
                        "exact_match": 0.0,
                        "resps": [[""]],
                        "filtered_resps": ["[invalid]"],
                        "target": "work\n#### 4",
                        "doc": {"question": "What is two plus two?"},
                    },
                ]
            }
        }
        failures = failed_samples(results, "gsm8k", "exact_match,flexible-extract")
        self.assertEqual(3, len(failures))
        self.assertEqual("answer-extraction-failed", failures[0]["reason"])
        self.assertEqual("exact-match-failed", failures[1]["reason"])
        self.assertEqual("4", failures[1]["expectedAnswer"])
        self.assertEqual("empty-response", failures[2]["reason"])

    def test_generation_ceiling_is_bounded_by_served_model(self):
        import preset_regression_gsm8k

        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_):
                return None

        original_urlopen = preset_regression_gsm8k.urllib.request.urlopen
        original_json_load = preset_regression_gsm8k.json.load
        try:
            preset_regression_gsm8k.urllib.request.urlopen = lambda *_args, **_kwargs: (
                Response()
            )
            preset_regression_gsm8k.json.load = lambda _response: {
                "data": [{"max_model_len": 4096}]
            }
            self.assertEqual(
                2048,
                effective_max_gen_tokens(
                    "http://localhost/v1/chat/completions", 8192, 2048
                ),
            )
        finally:
            preset_regression_gsm8k.urllib.request.urlopen = original_urlopen
            preset_regression_gsm8k.json.load = original_json_load

    def test_matrix_applies_safety_factor_and_minimum_gpu_overrides(self):
        targets = json.loads(
            subprocess.check_output(
                [
                    "bash",
                    ".github/scripts/preset-regression-tests/preset-regression-matrix.sh",
                ],
                cwd=ROOT,
                env={
                    **os.environ,
                    "REGRESSION_PROFILE": "standard",
                    "GPU": "",
                },
                text=True,
            )
        )
        instance_types = {
            (target["model"], target["gpu"]): target["instanceType"]
            for target in targets
        }
        self.assertEqual(
            "Standard_NC48ads_A100_v4",
            instance_types[("Qwen/Qwen3.6-35B-A3B", "a100")],
        )
        self.assertEqual(
            "Standard_NV72ads_A10_v5",
            instance_types[("nvidia/NVIDIA-Nemotron-Nano-9B-v2", "a10")],
        )
        self.assertEqual(
            "Standard_NC24ads_A100_v4",
            instance_types[("openai/gpt-oss-120b", "a100")],
        )

    def test_matrix_has_complete_baseline_coverage(self):
        targets = []
        for profile in ("standard", "8xh100"):
            targets.extend(
                json.loads(
                    subprocess.check_output(
                        [
                            "bash",
                            ".github/scripts/preset-regression-tests/preset-regression-matrix.sh",
                        ],
                        cwd=ROOT,
                        env={
                            **os.environ,
                            "REGRESSION_PROFILE": profile,
                            "GPU": "",
                        },
                        text=True,
                    )
                )
            )
        config = load_yaml(GSM_CONFIG)
        gaps = validate_coverage(
            targets,
            load_yaml(GSM_BASELINES),
            config["comparison"]["requireBaselines"],
        )
        self.assertEqual([], gaps)


if __name__ == "__main__":
    unittest.main()
