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
    DEFAULT_GUIDELLM_BASELINES,
    build_parser,
    promote_baselines,
)


class PresetRegressionTestCLITest(unittest.TestCase):
    def test_defaults_to_repository_baselines(self):
        args = build_parser().parse_args(["--artifacts", "artifacts"])
        self.assertEqual("all", args.suite)
        self.assertEqual(DEFAULT_GSM8K_BASELINES, args.gsm8k_baselines)
        self.assertEqual(DEFAULT_GUIDELLM_BASELINES, args.guidellm_baselines)
        self.assertFalse(args.dry_run)
        self.assertFalse(args.include_regressions)

    def test_dry_run_accepts_direct_aggregate_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            aggregate = root / "results.json"
            aggregate.write_text(
                json.dumps(
                    [
                        {
                            "status": "passed",
                            "performance": {
                                "passed": True,
                                "model": "org/model",
                                "instanceType": "gpu",
                                "nodes": 1,
                                "profile": "stress-high-concurrency-v1",
                                "peakTokensPerMinute": 100,
                                "averageTimeToFirstToken": 10,
                                "averageTimePerOutputToken": 5,
                            },
                        }
                    ]
                )
            )
            guidellm_baselines = root / "guidellm.yaml"
            original = "schemaVersion: 1\ntargets: []\n"
            guidellm_baselines.write_text(original)

            result = promote_baselines(
                artifact_roots=[aggregate],
                suite="guidellm",
                gsm8k_baselines=root / "unused.yaml",
                guidellm_baselines=guidellm_baselines,
                dry_run=True,
            )

            self.assertTrue(result["dryRun"])
            self.assertFalse(result["includedRegressions"])
            self.assertEqual(0, result["gsm8kPromoted"])
            self.assertEqual(1, result["guidellmPromoted"])
            self.assertEqual("org/model", result["candidates"]["guidellm"][0]["model"])
            self.assertEqual(original, guidellm_baselines.read_text())

    def test_regressions_require_explicit_opt_in(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            aggregate = root / "results.json"
            aggregate.write_text(
                json.dumps(
                    [
                        {
                            "status": "failed",
                            "performance": {
                                "status": "performance-regressed",
                                "passed": False,
                                "model": "org/model",
                                "instanceType": "gpu",
                                "nodes": 1,
                                "profile": "stress-high-concurrency-v1",
                                "peakTokensPerMinute": 100,
                                "averageTimeToFirstToken": 10,
                                "averageTimePerOutputToken": 5,
                            },
                        }
                    ]
                )
            )
            guidellm_baselines = root / "guidellm.yaml"
            original = "schemaVersion: 1\ntargets: []\n"
            guidellm_baselines.write_text(original)

            with self.assertRaisesRegex(ValueError, "no valid benchmark summaries"):
                promote_baselines(
                    artifact_roots=[aggregate],
                    suite="guidellm",
                    gsm8k_baselines=root / "unused.yaml",
                    guidellm_baselines=guidellm_baselines,
                    dry_run=True,
                )

            result = promote_baselines(
                artifact_roots=[aggregate],
                suite="guidellm",
                gsm8k_baselines=root / "unused.yaml",
                guidellm_baselines=guidellm_baselines,
                dry_run=True,
                include_regressions=True,
            )

            self.assertTrue(result["includedRegressions"])
            self.assertEqual(1, result["guidellmPromoted"])
            self.assertEqual("org/model", result["candidates"]["guidellm"][0]["model"])
            self.assertEqual(original, guidellm_baselines.read_text())


if __name__ == "__main__":
    unittest.main()
