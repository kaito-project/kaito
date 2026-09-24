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

import copy
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from preset_regression_benchmarks import (
    compare_accuracy,
    compare_performance,
    load_yaml,
    resolve_profile,
    validate_coverage,
    validate_gsm8k_data,
    validate_guidellm_data,
)
from preset_regression_gsm8k import (
    effective_max_gen_tokens,
    failed_samples,
    responses_by_document,
)
from promote_preset_regression_baselines import collect_gsm8k, collect_guidellm

ROOT = Path(__file__).resolve().parents[3]
GSM_CONFIG = ROOT / "benchmarks/gsm8k/config.yaml"
GSM_BASELINES = ROOT / "benchmarks/gsm8k/baselines.yaml"
GUIDELLM_CONFIG = ROOT / "benchmarks/guidellm/config.yaml"
GUIDELLM_BASELINES = ROOT / "benchmarks/guidellm/baselines.yaml"


def workspace_metrics(tpm: float, ttft: float, tpot: float) -> dict:
    config = {
        "durationSec": "60",
        "inputTokens": "2048",
        "outputTokens": "256",
        "maxConcurrency": "128",
    }
    return {
        "status": {
            "performance": {
                "metrics": {
                    "peakTokensPerMinute": {
                        "description": "stress/high-concurrency",
                        "value": str(tpm),
                        "unit": "tokens/min",
                        "config": config,
                    },
                    "averageTimeToFirstToken": {
                        "description": "stress/high-concurrency",
                        "value": str(ttft),
                        "unit": "ms",
                        "config": config,
                    },
                    "averageTimePerOutputToken": {
                        "description": "stress/high-concurrency",
                        "value": str(tpot),
                        "unit": "ms",
                        "config": config,
                    },
                }
            }
        }
    }


class BenchmarkDataTest(unittest.TestCase):
    def test_repository_manifests_are_valid(self):
        validate_gsm8k_data(load_yaml(GSM_CONFIG), load_yaml(GSM_BASELINES))
        validate_guidellm_data(
            load_yaml(GUIDELLM_CONFIG), load_yaml(GUIDELLM_BASELINES)
        )

    def test_models_use_large_default_thinking_profile(self):
        name, profile = resolve_profile(
            load_yaml(GSM_CONFIG), "deepseek-ai/DeepSeek-V4-Flash-0731"
        )
        self.assertEqual("chat-thinking-v1", name)
        self.assertEqual(8192, profile["maxGenTokens"])

    def test_duplicate_baseline_identity_is_rejected(self):
        config = load_yaml(GSM_CONFIG)
        baselines = load_yaml(GSM_BASELINES)
        baselines["targets"].append(copy.deepcopy(baselines["targets"][0]))
        with self.assertRaisesRegex(ValueError, "unique"):
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
                ]
            }
        }
        failures = failed_samples(results, "gsm8k", "exact_match,flexible-extract")
        self.assertEqual(2, len(failures))
        self.assertEqual("answer-extraction-failed", failures[0]["reason"])
        self.assertEqual("exact-match-failed", failures[1]["reason"])
        self.assertEqual("4", failures[1]["expectedAnswer"])

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

    def test_performance_bounds_are_directional(self):
        config = load_yaml(GUIDELLM_CONFIG)
        baselines = {
            "schemaVersion": 1,
            "targets": [
                {
                    "model": "org/model",
                    "instanceType": "gpu",
                    "nodes": 1,
                    "profile": "stress-high-concurrency-v1",
                    "tpm": 100,
                    "ttftMs": 10,
                    "tpotMs": 5,
                }
            ],
        }
        tolerances = {
            "peakTokensPerMinute": 0.15,
            "averageTimeToFirstToken": 0.2,
            "averageTimePerOutputToken": 0.15,
        }
        passing = compare_performance(
            workspace_metrics(85, 12, 5.75),
            "org/model",
            "gpu",
            1,
            config,
            baselines,
            tolerances,
            True,
        )
        failing = compare_performance(
            workspace_metrics(84, 12.1, 5.76),
            "org/model",
            "gpu",
            1,
            config,
            baselines,
            tolerances,
            True,
        )
        self.assertTrue(passing["passed"])
        self.assertEqual("org/model", passing["model"])
        self.assertEqual("gpu", passing["instanceType"])
        self.assertEqual(1, passing["nodes"])
        self.assertFalse(failing["passed"])
        self.assertEqual("performance-regressed", failing["status"])

    def test_performance_topology_mismatch_is_distinct(self):
        config = load_yaml(GUIDELLM_CONFIG)
        baselines = {
            "schemaVersion": 1,
            "targets": [
                {
                    "model": "org/model",
                    "instanceType": "gpu",
                    "nodes": 2,
                    "profile": "stress-high-concurrency-v1",
                    "tpm": 100,
                    "ttftMs": 10,
                    "tpotMs": 5,
                }
            ],
        }
        result = compare_performance(
            workspace_metrics(100, 10, 5),
            "org/model",
            "gpu",
            1,
            config,
            baselines,
            {
                "peakTokensPerMinute": 0.15,
                "averageTimeToFirstToken": 0.2,
                "averageTimePerOutputToken": 0.15,
            },
            True,
        )
        self.assertFalse(result["passed"])
        self.assertEqual("baseline-config-mismatch", result["status"])
        self.assertEqual([2], result["baselineNodes"])

    def test_matrix_coverage_is_computed(self):
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
        for config_path, baseline_path in (
            (GSM_CONFIG, GSM_BASELINES),
            (GUIDELLM_CONFIG, GUIDELLM_BASELINES),
        ):
            config = load_yaml(config_path)
            gaps = validate_coverage(
                targets,
                load_yaml(baseline_path),
                config["comparison"]["requireBaselines"],
            )
            self.assertGreater(len(gaps), 0)

    def test_stale_baseline_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "active matrix targets"):
            validate_coverage(
                [{"model": "org/current", "instanceType": "gpu"}],
                {
                    "targets": [
                        {"model": "org/removed", "instanceType": "gpu", "nodes": 1}
                    ]
                },
                False,
            )

    def test_promotion_uses_only_valid_passed_summaries(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            valid = root / "valid"
            invalid = root / "invalid"
            valid.mkdir()
            invalid.mkdir()
            (valid / "gsm8k-summary.json").write_text(
                json.dumps(
                    {
                        "passed": True,
                        "emptyResponses": 0,
                        "model": "org/model",
                        "instanceType": "gpu",
                        "nodes": 1,
                        "profile": "chat-thinking-v1",
                        "accuracy": 0.75,
                        "correct": 96,
                        "evaluated": 128,
                    }
                )
            )
            (invalid / "gsm8k-summary.json").write_text(
                json.dumps({"passed": False, "emptyResponses": 1})
            )
            (valid / "guidellm-summary.json").write_text(
                json.dumps(
                    {
                        "passed": True,
                        "model": "org/model",
                        "instanceType": "gpu",
                        "nodes": 1,
                        "profile": "stress-high-concurrency-v1",
                        "peakTokensPerMinute": 100,
                        "averageTimeToFirstToken": 10,
                        "averageTimePerOutputToken": 5,
                    }
                )
            )
            self.assertEqual(1, len(collect_gsm8k([root])))
            self.assertEqual(1, len(collect_guidellm([root])))


if __name__ == "__main__":
    unittest.main()
