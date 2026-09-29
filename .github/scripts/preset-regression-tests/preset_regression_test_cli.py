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

"""Manage GSM8K preset regression baselines from validated artifacts."""

from __future__ import annotations

import argparse
import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import yaml
from preset_regression_benchmarks import deployment_key, load_yaml

REPO_ROOT = Path(__file__).resolve().parents[3]
DEFAULT_GSM8K_BASELINES = REPO_ROOT / "benchmarks/gsm8k/baselines.yaml"


def _matching_paths(root: Path, pattern: str) -> list[Path]:
    if not root.exists():
        raise ValueError(f"artifact path does not exist: {root}")
    if root.is_file():
        return [root] if root.match(pattern) else []
    return sorted(root.rglob(pattern))


def _load_summaries(
    artifact_roots: list[Path], name: str
) -> list[tuple[Path, dict[str, Any]]]:
    summaries: list[tuple[Path, dict[str, Any]]] = []
    for root in artifact_roots:
        for path in _matching_paths(root, name):
            summaries.append((path, json.loads(path.read_text(encoding="utf-8"))))
    return summaries


def _load_aggregate_summaries(
    artifact_roots: list[Path], field: str
) -> list[tuple[Path, dict[str, Any]]]:
    summaries: list[tuple[Path, dict[str, Any]]] = []
    for root in artifact_roots:
        paths = [root] if root.is_file() else _matching_paths(root, "results-*.json")
        for path in paths:
            results = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(results, list):
                if root.is_file():
                    continue
                raise ValueError(f"aggregate results must be a list: {path}")
            for result in results:
                if not isinstance(result, dict) or result.get("status") != "passed":
                    continue
                summary = result.get(field)
                if isinstance(summary, dict):
                    summaries.append((path, summary))
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
    additions: dict[str, dict[str, Any]] = {}
    summaries = _load_summaries(artifact_roots, "gsm8k-summary.json")
    summaries.extend(_load_aggregate_summaries(artifact_roots, "correctness"))
    for path, summary in summaries:
        if not summary.get("passed"):
            continue
        evaluated = int(summary.get("evaluated", 0))
        correct = int(summary.get("correct", -1))
        accuracy = float(summary.get("accuracy", -1))
        empty_responses = int(summary.get("emptyResponses", -1))
        if (
            evaluated <= 0
            or correct < 0
            or empty_responses < 0
            or not 0 <= accuracy <= 1
        ):
            raise ValueError(f"invalid GSM8K summary: {path}")
        measured_at = (
            datetime.fromtimestamp(path.stat().st_mtime, UTC).date().isoformat()
        )
        target = {
            "model": str(summary["model"]),
            "instanceType": str(summary["instanceType"]),
            "nodes": int(summary["nodes"]),
            "profile": str(summary["profile"]),
            "accuracy": accuracy,
            "correct": correct,
            "evaluated": evaluated,
            "emptyResponses": empty_responses,
            "measuredAt": measured_at,
        }
        additions[deployment_key(target)] = target
    return [additions[key] for key in sorted(additions)]


def write_yaml(path: Path, value: dict[str, Any]) -> None:
    path.write_text(yaml.safe_dump(value, sort_keys=False), encoding="utf-8")


def promote_baselines(
    artifact_roots: list[Path],
    gsm8k_baselines: Path,
    dry_run: bool,
) -> dict[str, Any]:
    gsm_additions = collect_gsm8k(artifact_roots)
    if not gsm_additions:
        raise ValueError("no valid benchmark summaries found")

    merged = _merge_targets(load_yaml(gsm8k_baselines), gsm_additions)
    if not dry_run:
        write_yaml(gsm8k_baselines, merged)

    result: dict[str, Any] = {
        "dryRun": dry_run,
        "gsm8kPromoted": len(gsm_additions),
    }
    if dry_run:
        result["candidates"] = gsm_additions
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Promote passed GSM8K regression artifacts into the reviewed baseline "
            "manifest. Artifact inputs may be directories, per-model summary "
            "files, or aggregate results JSON files."
        ),
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--artifacts",
        action="append",
        required=True,
        type=Path,
        metavar="PATH",
        help="Artifact directory or JSON file; repeat for multiple GPU pools",
    )
    parser.add_argument(
        "--gsm8k-baselines",
        type=Path,
        default=DEFAULT_GSM8K_BASELINES,
        help="GSM8K baseline manifest",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print promotion candidates without changing baseline files",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    result = promote_baselines(
        artifact_roots=args.artifacts,
        gsm8k_baselines=args.gsm8k_baselines,
        dry_run=args.dry_run,
    )
    print(json.dumps(result, indent=2 if args.dry_run else None))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
