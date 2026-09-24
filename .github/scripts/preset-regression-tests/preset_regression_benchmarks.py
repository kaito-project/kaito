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

"""Load, validate, and compare preset regression benchmark data."""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path
from typing import Any

import yaml

PERFORMANCE_METRICS = {
    "peakTokensPerMinute": ("tpm", "minimum", "baselinePeakTokensPerMinute"),
    "averageTimeToFirstToken": (
        "ttftMs",
        "maximum",
        "baselineAverageTimeToFirstToken",
    ),
    "averageTimePerOutputToken": (
        "tpotMs",
        "maximum",
        "baselineAverageTimePerOutputToken",
    ),
}
PERFORMANCE_DEFINITIONS = {
    "peakTokensPerMinute": {"unit": "tokens/min", "better": "higher"},
    "averageTimeToFirstToken": {"unit": "ms", "better": "lower"},
    "averageTimePerOutputToken": {"unit": "ms", "better": "lower"},
}


def load_yaml(path: Path) -> dict[str, Any]:
    value = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a YAML object")
    return value


def resolve_profile(
    config: dict[str, Any], model: str | None = None
) -> tuple[str, dict[str, Any]]:
    benchmark = config.get("benchmark", {})
    profile_name = benchmark.get("defaultProfile")
    if model is not None:
        profile_name = config.get("modelProfileOverrides", {}).get(model, profile_name)
    profiles = config.get("profiles", {})
    if not profile_name or profile_name not in profiles:
        raise ValueError(f"resolved profile {profile_name!r} is not defined")
    profile = profiles[profile_name]
    if not isinstance(profile, dict):
        raise ValueError(f"profile {profile_name!r} must be an object")
    return profile_name, profile


def deployment_key(target: dict[str, Any]) -> tuple[str, str, int]:
    try:
        return (
            str(target["model"]),
            str(target["instanceType"]),
            int(target["nodes"]),
        )
    except (KeyError, TypeError, ValueError) as error:
        raise ValueError(f"invalid target identity: {target}") from error


def find_baseline(
    baselines: dict[str, Any], model: str, instance_type: str, nodes: int
) -> dict[str, Any] | None:
    wanted = (model, instance_type, nodes)
    return next(
        (
            target
            for target in baselines.get("targets", [])
            if deployment_key(target) == wanted
        ),
        None,
    )


def related_baselines(
    baselines: dict[str, Any], model: str, instance_type: str
) -> list[dict[str, Any]]:
    return [
        target
        for target in baselines.get("targets", [])
        if str(target.get("model")) == model
        and str(target.get("instanceType")) == instance_type
    ]


def validate_tolerance(value: float, name: str) -> float:
    if not 0 <= value <= 1:
        raise ValueError(f"{name} tolerance must be between 0 and 1")
    return value


def validate_gsm8k_policy(config: dict[str, Any]) -> None:
    execution = config.get("execution", {})
    for key in (
        "numConcurrent",
        "requestTimeoutSeconds",
        "timeoutSeconds",
        "maxRetries",
    ):
        if int(execution.get(key, 0)) <= 0:
            raise ValueError(f"GSM8K execution.{key} must be positive")
    comparison = config.get("comparison", {})
    validate_tolerance(float(comparison.get("defaultMaxRegression", -1)), "GSM8K")
    for key, value in comparison.get("maxRegressionOverrides", {}).items():
        validate_tolerance(float(value), f"GSM8K override {key}")


def validate_guidellm_policy(config: dict[str, Any]) -> None:
    comparison = config.get("comparison", {})
    defaults = comparison.get("defaultMaxRegressionRatios", {})
    if set(defaults) != set(PERFORMANCE_METRICS):
        raise ValueError(
            "GuideLLM comparison must define tolerances for TPM, TTFT, and TPOT"
        )
    for metric_name, value in defaults.items():
        validate_tolerance(float(value), metric_name)
    for key, overrides in comparison.get("maxRegressionRatioOverrides", {}).items():
        if not set(overrides).issubset(PERFORMANCE_METRICS):
            raise ValueError(f"GuideLLM override {key} contains an unknown metric")
        for metric_name, value in overrides.items():
            validate_tolerance(float(value), f"{key} {metric_name}")


def _validate_common(config: dict[str, Any], baselines: dict[str, Any]) -> None:
    if config.get("schemaVersion") != 1 or baselines.get("schemaVersion") != 1:
        raise ValueError("benchmark config and baselines must use schemaVersion 1")
    if not isinstance(config.get("profiles"), dict) or not config["profiles"]:
        raise ValueError("benchmark config must define at least one profile")
    resolve_profile(config)
    targets = baselines.get("targets")
    if not isinstance(targets, list):
        raise ValueError("baselines.targets must be an array")
    keys = [deployment_key(target) for target in targets]
    if len(keys) != len(set(keys)):
        raise ValueError("baseline target identities must be unique")


def validate_gsm8k_data(config: dict[str, Any], baselines: dict[str, Any]) -> None:
    _validate_common(config, baselines)
    validate_gsm8k_policy(config)
    benchmark = config.get("benchmark", {})
    sample_count = int(benchmark.get("sampleSelection", {}).get("count", 0))
    if sample_count <= 0:
        raise ValueError("GSM8K sample count must be positive")
    for model, profile_name in config.get("modelProfileOverrides", {}).items():
        if profile_name not in config["profiles"]:
            raise ValueError(
                f"model override {model!r} references undefined profile {profile_name!r}"
            )
    for target in baselines["targets"]:
        profile_name, _ = resolve_profile(config, str(target["model"]))
        if target.get("profile") != profile_name:
            raise ValueError(
                f"GSM8K baseline profile for {target['model']} is {target.get('profile')!r}, "
                f"expected {profile_name!r}"
            )
        accuracy = float(target.get("accuracy", -1))
        correct = int(target.get("correct", -1))
        evaluated = int(target.get("evaluated", -1))
        if not 0 <= accuracy <= 1 or evaluated != sample_count or correct < 0:
            raise ValueError(f"invalid GSM8K result for {deployment_key(target)}")
        if not math.isclose(accuracy, correct / evaluated, abs_tol=1e-12):
            raise ValueError(
                f"GSM8K accuracy does not equal correct/evaluated for {deployment_key(target)}"
            )


def validate_guidellm_data(config: dict[str, Any], baselines: dict[str, Any]) -> None:
    _validate_common(config, baselines)
    validate_guidellm_policy(config)
    configured_metrics = config.get("benchmark", {}).get("metrics", {})
    if configured_metrics != PERFORMANCE_DEFINITIONS:
        raise ValueError("GuideLLM config must define TPM, TTFT, and TPOT metrics")

    profiles = config["profiles"]
    for target in baselines["targets"]:
        profile_name = target.get("profile")
        if profile_name not in profiles:
            raise ValueError(
                f"GuideLLM baseline references undefined profile {profile_name!r}"
            )
        for baseline_key, _, _ in PERFORMANCE_METRICS.values():
            if float(target.get(baseline_key, 0)) <= 0:
                raise ValueError(
                    f"GuideLLM {baseline_key} must be positive for {deployment_key(target)}"
                )


def compare_accuracy(
    accuracy: float,
    baseline: dict[str, Any] | None,
    profile_name: str,
    max_regression: float,
    require_baseline: bool,
) -> dict[str, Any]:
    max_regression = validate_tolerance(max_regression, "GSM8K")
    if baseline is None:
        return {
            "status": "baseline-missing",
            "passed": not require_baseline,
            "profile": profile_name,
            "baselineAccuracy": None,
            "minimumAccuracy": None,
            "maxRegression": max_regression,
        }
    if baseline.get("profile") != profile_name:
        return {
            "status": "baseline-config-mismatch",
            "passed": False,
            "profile": profile_name,
            "baselineProfile": baseline.get("profile"),
        }
    baseline_accuracy = float(baseline["accuracy"])
    minimum = max(0.0, baseline_accuracy - max_regression)
    passed = accuracy + 1e-12 >= minimum
    return {
        "status": "passed" if passed else "correctness-regressed",
        "passed": passed,
        "profile": profile_name,
        "baselineAccuracy": baseline_accuracy,
        "minimumAccuracy": minimum,
        "maxRegression": max_regression,
        "delta": accuracy - baseline_accuracy,
    }


def compare_performance(
    workspace: dict[str, Any],
    model: str,
    instance_type: str,
    nodes: int,
    config: dict[str, Any],
    baselines: dict[str, Any],
    tolerances: dict[str, float],
    require_baseline: bool,
) -> dict[str, Any]:
    validate_guidellm_data(config, baselines)
    profile_name, profile = resolve_profile(config)
    identity = {"model": model, "instanceType": instance_type, "nodes": nodes}
    metrics = workspace.get("status", {}).get("performance", {}).get("metrics", {})
    observed: dict[str, float] = {}
    for metric_name in PERFORMANCE_METRICS:
        metric = metrics.get(metric_name, {})
        try:
            value = float(metric.get("value", 0))
        except (TypeError, ValueError) as error:
            raise ValueError(f"Workspace metric {metric_name} is malformed") from error
        if value <= 0:
            raise ValueError(f"Workspace metric {metric_name} must be positive")
        if metric.get("description") != profile.get("description"):
            raise ValueError(
                f"Workspace metric {metric_name} has a different benchmark description"
            )
        metric_config = metric.get("config", {})
        for key in ("durationSec", "inputTokens", "outputTokens"):
            if str(metric_config.get(key, "")) != str(profile.get(key, "")):
                raise ValueError(f"Workspace metric {metric_name} has mismatched {key}")
        observed[metric_name] = value

    baseline = find_baseline(baselines, model, instance_type, nodes)
    if baseline is None:
        related = related_baselines(baselines, model, instance_type)
        if related:
            return {
                **identity,
                "status": "baseline-config-mismatch",
                "passed": False,
                "profile": profile_name,
                "actualNodes": nodes,
                "baselineNodes": sorted(int(item["nodes"]) for item in related),
                **observed,
            }
        return {
            **identity,
            "status": "baseline-missing",
            "passed": not require_baseline,
            "profile": profile_name,
            **observed,
        }
    if baseline.get("profile") != profile_name:
        return {
            **identity,
            "status": "baseline-config-mismatch",
            "passed": False,
            "profile": profile_name,
            "baselineProfile": baseline.get("profile"),
            **observed,
        }

    result: dict[str, Any] = {
        **identity,
        "status": "passed",
        "passed": True,
        "profile": profile_name,
    }
    for metric_name, (
        baseline_key,
        direction,
        result_baseline_key,
    ) in PERFORMANCE_METRICS.items():
        tolerance = validate_tolerance(float(tolerances[metric_name]), metric_name)
        baseline_value = float(baseline[baseline_key])
        value = observed[metric_name]
        limit = baseline_value * (
            1 - tolerance if direction == "minimum" else 1 + tolerance
        )
        metric_passed = value >= limit if direction == "minimum" else value <= limit
        result[metric_name] = value
        result[result_baseline_key] = baseline_value
        result[f"{direction}{metric_name[0].upper()}{metric_name[1:]}"] = limit
        result["passed"] = result["passed"] and metric_passed
    if not result["passed"]:
        result["status"] = "performance-regressed"
    return result


def coverage_gaps(
    targets: list[dict[str, Any]], baselines: dict[str, Any]
) -> list[tuple[str, str]]:
    covered = {
        (str(item["model"]), str(item["instanceType"]))
        for item in baselines.get("targets", [])
    }
    expected = {
        (str(item["model"]), str(item["instanceType"]))
        for item in targets
        if not item.get("skipReason")
    }
    return sorted(expected - covered)


def validate_coverage(
    targets: list[dict[str, Any]],
    baselines: dict[str, Any],
    require_baselines: bool,
) -> list[tuple[str, str]]:
    expected = {
        (str(item["model"]), str(item["instanceType"]))
        for item in targets
        if not item.get("skipReason")
    }
    covered = {
        (str(item["model"]), str(item["instanceType"]))
        for item in baselines.get("targets", [])
    }
    stale = sorted(covered - expected)
    if stale:
        raise ValueError(
            f"baseline entries do not match active matrix targets: {stale}"
        )
    gaps = sorted(expected - covered)
    if require_baselines and gaps:
        raise ValueError(f"missing required baseline entries: {gaps}")
    return gaps


def target_policy_key(model: str, instance_type: str, nodes: int) -> str:
    return f"{model}|{instance_type}|{nodes}"


def _main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--model", required=True)
    parser.add_argument("--instance-type", required=True)
    parser.add_argument("--nodes", required=True, type=int)
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--baselines", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        config = load_yaml(args.config)
        comparison = config["comparison"]
        tolerances = dict(comparison["defaultMaxRegressionRatios"])
        tolerances.update(
            comparison.get("maxRegressionRatioOverrides", {}).get(
                target_policy_key(args.model, args.instance_type, args.nodes), {}
            )
        )
        result = compare_performance(
            json.loads(args.workspace.read_text(encoding="utf-8")),
            args.model,
            args.instance_type,
            args.nodes,
            config,
            load_yaml(args.baselines),
            tolerances,
            bool(comparison["requireBaselines"]),
        )
    except Exception as error:  # noqa: BLE001 - persist comparison failures for CI
        result = {
            "model": args.model,
            "instanceType": args.instance_type,
            "nodes": args.nodes,
            "status": "performance-invalid",
            "passed": False,
            "error": f"{type(error).__name__}: {error}",
        }
    args.output.write_text(
        json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    print(json.dumps(result, sort_keys=True))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(_main())
