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

import json
import tempfile
import unittest
from pathlib import Path

from preset_regression_test_cli import (
    DEFAULT_GSM8K_BASELINES,
    build_parser,
    promote_baselines,
)


class PresetRegressionTestCLITest(unittest.TestCase):
    def test_defaults_to_repository_baselines(self):
        args = build_parser().parse_args(["--artifacts", "artifacts"])
        self.assertEqual(DEFAULT_GSM8K_BASELINES, args.gsm8k_baselines)
        self.assertFalse(args.dry_run)

    def test_dry_run_accepts_direct_aggregate_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            aggregate = root / "results.json"
            aggregate.write_text(
                json.dumps(
                    [
                        {
                            "status": "passed",
                            "correctness": {
                                "passed": True,
                                "emptyResponses": 1,
                                "model": "org/model",
                                "instanceType": "gpu",
                                "nodes": 1,
                                "profile": "chat-thinking-v1",
                                "accuracy": 0.75,
                                "correct": 96,
                                "evaluated": 128,
                            },
                        }
                    ]
                )
            )
            gsm8k_baselines = root / "gsm8k.yaml"
            original = "schemaVersion: 1\ntargets: []\n"
            gsm8k_baselines.write_text(original)

            result = promote_baselines(
                artifact_roots=[aggregate],
                gsm8k_baselines=gsm8k_baselines,
                dry_run=True,
            )

            self.assertTrue(result["dryRun"])
            self.assertEqual(1, result["gsm8kPromoted"])
            self.assertEqual("org/model", result["candidates"][0]["model"])
            self.assertEqual(original, gsm8k_baselines.read_text())

    def test_failed_results_are_not_promoted(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            aggregate = root / "results.json"
            aggregate.write_text(
                json.dumps(
                    [
                        {
                            "status": "failed",
                            "correctness": {
                                "status": "correctness-regressed",
                                "passed": False,
                                "model": "org/model",
                                "instanceType": "gpu",
                                "nodes": 1,
                                "profile": "chat-thinking-v1",
                                "accuracy": 0.7,
                                "correct": 90,
                                "evaluated": 128,
                                "emptyResponses": 1,
                            },
                        }
                    ]
                )
            )
            with self.assertRaisesRegex(ValueError, "no valid benchmark summaries"):
                promote_baselines(
                    artifact_roots=[aggregate],
                    gsm8k_baselines=root / "unused.yaml",
                    dry_run=True,
                )


if __name__ == "__main__":
    unittest.main()
