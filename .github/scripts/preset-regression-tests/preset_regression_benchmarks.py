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

import math
from datetime import date
from pathlib import Path
from typing import Any

import yaml

GSM8K_EXECUTION_FIELDS = {
    "numConcurrent",
    "requestTimeoutSeconds",
    "timeoutSeconds",
    "maxRetries",
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


def resolve_gsm8k_execution(
    config: dict[str, Any], model: str | None = None
) -> dict[str, int]:
    execution = dict(config.get("execution", {}))
    if model is not None:
        execution.update(config.get("modelExecutionOverrides", {}).get(model, {}))
    return {key: int(execution.get(key, 0)) for key in GSM8K_EXECUTION_FIELDS}


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


def is_iso_date(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    try:
        return date.fromisoformat(value).isoformat() == value
    except ValueError:
        return False


def validate_gsm8k_policy(config: dict[str, Any]) -> None:
    executions = [("default", resolve_gsm8k_execution(config))]
    for model, overrides in config.get("modelExecutionOverrides", {}).items():
        unknown = set(overrides) - GSM8K_EXECUTION_FIELDS
        if unknown:
            raise ValueError(
                f"GSM8K execution override {model!r} contains unknown fields: "
                f"{sorted(unknown)}"
            )
        executions.append((str(model), resolve_gsm8k_execution(config, str(model))))
    for name, execution in executions:
        for key in GSM8K_EXECUTION_FIELDS:
            if execution[key] <= 0:
                raise ValueError(f"GSM8K execution {name!r}.{key} must be positive")
    comparison = config.get("comparison", {})
    validate_tolerance(float(comparison.get("defaultMaxRegression", -1)), "GSM8K")
    for key, value in comparison.get("maxRegressionOverrides", {}).items():
        validate_tolerance(float(value), f"GSM8K override {key}")


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
    for profile_name, profile in config["profiles"].items():
        if not isinstance(profile.get("requestKwargs", {}), dict):
            raise ValueError(
                f"GSM8K profile {profile_name!r}.requestKwargs must be an object"
            )
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
        empty_responses = int(target.get("emptyResponses", -1))
        if (
            not 0 <= accuracy <= 1
            or evaluated != sample_count
            or correct < 0
            or not 0 <= empty_responses <= evaluated
            or correct + empty_responses > evaluated
            or not is_iso_date(target.get("measuredAt"))
        ):
            raise ValueError(f"invalid GSM8K result for {deployment_key(target)}")
        if not math.isclose(accuracy, correct / evaluated, abs_tol=1e-12):
            raise ValueError(
                f"GSM8K accuracy does not equal correct/evaluated for {deployment_key(target)}"
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
