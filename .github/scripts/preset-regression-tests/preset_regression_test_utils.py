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

"""Load, validate, and compare preset correctness regression data."""

from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml


def load_yaml(path: Path) -> dict[str, Any]:
    value = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a YAML object")
    return value


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
    gaps = coverage_gaps(targets, baselines)
    if require_baselines and gaps:
        raise ValueError(f"missing required baseline entries: {gaps}")
    return gaps
