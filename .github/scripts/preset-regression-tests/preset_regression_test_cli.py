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

"""Manage preset regression test baselines from validated artifacts."""

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
DEFAULT_GUIDELLM_BASELINES = REPO_ROOT / "benchmarks/guidellm/baselines.yaml"


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
    artifact_roots: list[Path], field: str, include_failed_results: bool = False
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
                if not isinstance(result, dict) or (
                    result.get("status") != "passed" and not include_failed_results
                ):
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


def collect_guidellm(
    artifact_roots: list[Path], include_regressions: bool = False
) -> list[dict[str, Any]]:
    additions: dict[str, dict[str, Any]] = {}
    summaries = _load_summaries(artifact_roots, "guidellm-summary.json")
    summaries.extend(
        _load_aggregate_summaries(
            artifact_roots,
            "performance",
            include_failed_results=include_regressions,
        )
    )
    for path, summary in summaries:
        if not summary.get("passed") and not (
            include_regressions and summary.get("status") == "performance-regressed"
        ):
            continue
        values = {
            "tpm": float(summary.get("peakTokensPerMinute", 0)),
            "ttftMs": float(summary.get("averageTimeToFirstToken", 0)),
            "tpotMs": float(summary.get("averageTimePerOutputToken", 0)),
        }
        if any(value <= 0 for value in values.values()):
            raise ValueError(f"invalid GuideLLM summary: {path}")
        target = {
            "model": str(summary["model"]),
            "instanceType": str(summary["instanceType"]),
            "nodes": int(summary["nodes"]),
            "profile": str(summary["profile"]),
            **values,
        }
        additions[deployment_key(target)] = target
    return [additions[key] for key in sorted(additions)]


def write_yaml(path: Path, value: dict[str, Any]) -> None:
    path.write_text(yaml.safe_dump(value, sort_keys=False), encoding="utf-8")


def promote_baselines(
    artifact_roots: list[Path],
    suite: str,
    gsm8k_baselines: Path,
    guidellm_baselines: Path,
    dry_run: bool,
    include_regressions: bool = False,
) -> dict[str, Any]:
    gsm_additions = collect_gsm8k(artifact_roots) if suite in {"all", "gsm8k"} else []
    guidellm_additions = (
        collect_guidellm(artifact_roots, include_regressions=include_regressions)
        if suite in {"all", "guidellm"}
        else []
    )
    if not gsm_additions and not guidellm_additions:
        raise ValueError("no valid benchmark summaries found")

    if suite in {"all", "gsm8k"} and gsm_additions:
        merged = _merge_targets(load_yaml(gsm8k_baselines), gsm_additions)
        if not dry_run:
            write_yaml(gsm8k_baselines, merged)
    if suite in {"all", "guidellm"} and guidellm_additions:
        merged = _merge_targets(load_yaml(guidellm_baselines), guidellm_additions)
        if not dry_run:
            write_yaml(guidellm_baselines, merged)

    result: dict[str, Any] = {
        "dryRun": dry_run,
        "includedRegressions": include_regressions,
        "gsm8kPromoted": len(gsm_additions),
        "guidellmPromoted": len(guidellm_additions),
    }
    if dry_run:
        result["candidates"] = {
            "gsm8k": gsm_additions,
            "guidellm": guidellm_additions,
        }
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Promote passed preset-regression artifacts into reviewed baseline "
            "manifests. Artifact inputs may be directories, per-model summary "
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
        "--suite",
        choices=("all", "gsm8k", "guidellm"),
        default="all",
        help="Baseline suite to promote",
    )
    parser.add_argument(
        "--gsm8k-baselines",
        type=Path,
        default=DEFAULT_GSM8K_BASELINES,
        help="GSM8K baseline manifest",
    )
    parser.add_argument(
        "--guidellm-baselines",
        type=Path,
        default=DEFAULT_GUIDELLM_BASELINES,
        help="GuideLLM baseline manifest",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print promotion candidates without changing baseline files",
    )
    parser.add_argument(
        "--include-regressions",
        action="store_true",
        help=(
            "Promote valid performance-regressed measurements; use only after "
            "reviewing an intentional benchmark or runtime change"
        ),
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    result = promote_baselines(
        artifact_roots=args.artifacts,
        suite=args.suite,
        gsm8k_baselines=args.gsm8k_baselines,
        guidellm_baselines=args.guidellm_baselines,
        dry_run=args.dry_run,
        include_regressions=args.include_regressions,
    )
    print(json.dumps(result, indent=2 if args.dry_run else None))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
