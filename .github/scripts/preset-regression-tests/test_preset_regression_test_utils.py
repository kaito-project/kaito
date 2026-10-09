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

import tempfile
import unittest
from pathlib import Path

from preset_regression_test_utils import (
    coverage_gaps,
    deployment_key,
    find_baseline,
    load_yaml,
    related_baselines,
    validate_coverage,
)


class PresetRegressionTestUtilsTest(unittest.TestCase):
    def test_load_yaml_requires_object(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "value.yaml"
            path.write_text("- item\n")
            with self.assertRaisesRegex(ValueError, "must contain a YAML object"):
                load_yaml(path)

    def test_deployment_key_normalizes_identity(self):
        self.assertEqual(
            ("org/model", "gpu", 2),
            deployment_key({"model": "org/model", "instanceType": "gpu", "nodes": "2"}),
        )

    def test_invalid_deployment_key_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "invalid target identity"):
            deployment_key({"model": "org/model"})

    def test_find_baseline_requires_complete_identity(self):
        baselines = {
            "targets": [
                {"model": "org/model", "instanceType": "gpu", "nodes": 1},
                {"model": "org/model", "instanceType": "gpu", "nodes": 2},
            ]
        }
        self.assertEqual(
            2,
            find_baseline(baselines, "org/model", "gpu", 2)["nodes"],
        )
        self.assertIsNone(find_baseline(baselines, "org/model", "other", 2))

    def test_related_baselines_match_model_and_instance_type(self):
        baselines = {
            "targets": [
                {"model": "org/model", "instanceType": "gpu", "nodes": 1},
                {"model": "org/model", "instanceType": "gpu", "nodes": 2},
                {"model": "org/other", "instanceType": "gpu", "nodes": 1},
            ]
        }
        self.assertEqual(
            [1, 2],
            [
                item["nodes"]
                for item in related_baselines(baselines, "org/model", "gpu")
            ],
        )

    def test_coverage_gaps_ignore_skipped_targets(self):
        targets = [
            {"model": "org/covered", "instanceType": "gpu", "skipReason": ""},
            {"model": "org/missing", "instanceType": "gpu", "skipReason": ""},
            {
                "model": "org/skipped",
                "instanceType": "gpu",
                "skipReason": "unsupported",
            },
        ]
        baselines = {
            "targets": [{"model": "org/covered", "instanceType": "gpu", "nodes": 1}]
        }
        self.assertEqual([("org/missing", "gpu")], coverage_gaps(targets, baselines))

    def test_required_coverage_gap_is_rejected(self):
        targets = [{"model": "org/missing", "instanceType": "gpu"}]
        with self.assertRaisesRegex(ValueError, "missing required baseline"):
            validate_coverage(targets, {"targets": []}, True)

    def test_stale_baseline_is_ignored(self):
        self.assertEqual(
            [("org/current", "gpu")],
            validate_coverage(
                [{"model": "org/current", "instanceType": "gpu"}],
                {
                    "targets": [
                        {"model": "org/removed", "instanceType": "gpu", "nodes": 1}
                    ]
                },
                False,
            ),
        )


if __name__ == "__main__":
    unittest.main()
