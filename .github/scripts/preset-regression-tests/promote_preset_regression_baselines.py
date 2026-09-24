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

"""Promote validated regression artifacts into reviewed baseline manifests."""

from __future__ import annotations

import argparse
import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import yaml
from preset_regression_benchmarks import deployment_key, load_yaml


def _load_summaries(
    artifact_roots: list[Path], name: str
) -> list[tuple[Path, dict[str, Any]]]:
    summaries: list[tuple[Path, dict[str, Any]]] = []
    for root in artifact_roots:
        for path in sorted(root.rglob(name)):
            summaries.append((path, json.loads(path.read_text(encoding="utf-8"))))
    return summaries


def _merge_targets(
    existing: dict[str, Any], additions: list[dict[str, Any]]
) -> dict[str, Any]:
    merged = {deployment_key(target): target for target in existing.get("targets", [])}
    for target in additions:
        merged[deployment_key(target)] = target
    return {
        "schemaVersion": 1,
        "targets": [merged[key] for key in sorted(merged)],
    }


def collect_gsm8k(artifact_roots: list[Path]) -> list[dict[str, Any]]:
    additions: list[dict[str, Any]] = []
    for path, summary in _load_summaries(artifact_roots, "gsm8k-summary.json"):
        if not summary.get("passed") or summary.get("emptyResponses") != 0:
            continue
        evaluated = int(summary.get("evaluated", 0))
        correct = int(summary.get("correct", -1))
        accuracy = float(summary.get("accuracy", -1))
        if evaluated <= 0 or correct < 0 or not 0 <= accuracy <= 1:
            raise ValueError(f"invalid GSM8K summary: {path}")
        measured_at = datetime.fromtimestamp(path.stat().st_mtime, UTC).isoformat()
        additions.append(
            {
                "model": str(summary["model"]),
                "instanceType": str(summary["instanceType"]),
                "nodes": int(summary["nodes"]),
                "profile": str(summary["profile"]),
                "accuracy": accuracy,
                "correct": correct,
                "evaluated": evaluated,
                "measuredAt": measured_at,
            }
        )
    return additions


def collect_guidellm(artifact_roots: list[Path]) -> list[dict[str, Any]]:
    additions: list[dict[str, Any]] = []
    for path, summary in _load_summaries(artifact_roots, "guidellm-summary.json"):
        if not summary.get("passed"):
            continue
        values = {
            "tpm": float(summary.get("peakTokensPerMinute", 0)),
            "ttftMs": float(summary.get("averageTimeToFirstToken", 0)),
            "tpotMs": float(summary.get("averageTimePerOutputToken", 0)),
        }
        if any(value <= 0 for value in values.values()):
            raise ValueError(f"invalid GuideLLM summary: {path}")
        additions.append(
            {
                "model": str(summary["model"]),
                "instanceType": str(summary["instanceType"]),
                "nodes": int(summary["nodes"]),
                "profile": str(summary["profile"]),
                **values,
            }
        )
    return additions


def write_yaml(path: Path, value: dict[str, Any]) -> None:
    path.write_text(yaml.safe_dump(value, sort_keys=False), encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--artifacts", action="append", required=True, type=Path)
    parser.add_argument("--gsm8k-baselines", required=True, type=Path)
    parser.add_argument("--guidellm-baselines", required=True, type=Path)
    args = parser.parse_args()

    gsm_additions = collect_gsm8k(args.artifacts)
    guidellm_additions = collect_guidellm(args.artifacts)
    if not gsm_additions and not guidellm_additions:
        raise ValueError("no valid benchmark summaries found")

    write_yaml(
        args.gsm8k_baselines,
        _merge_targets(load_yaml(args.gsm8k_baselines), gsm_additions),
    )
    write_yaml(
        args.guidellm_baselines,
        _merge_targets(load_yaml(args.guidellm_baselines), guidellm_additions),
    )
    print(
        json.dumps(
            {
                "gsm8kPromoted": len(gsm_additions),
                "guidellmPromoted": len(guidellm_additions),
            }
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
