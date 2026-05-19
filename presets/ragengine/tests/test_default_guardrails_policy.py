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

from pathlib import Path

import yaml

from ragengine.guardrails.output_guardrails import _parse_policy_scanner_configs

CHART_TEMPLATE = (
    Path(__file__).resolve().parents[3]
    / "charts"
    / "kaito"
    / "ragengine"
    / "templates"
    / "guardrails-policy-configmap.yaml"
)


def _extract_default_policy_text() -> str:
    lines = CHART_TEMPLATE.read_text(encoding="utf-8").splitlines()
    start = None
    base_indent = None
    for index, line in enumerate(lines):
        if line.strip() == "guardrails.yaml: |":
            start = index + 1
            base_indent = len(line) - len(line.lstrip(" ")) + 2
            break

    if start is None or base_indent is None:
        raise AssertionError("guardrails.yaml block not found in chart template")

    block_lines = []
    for line in lines[start:]:
        if not line.strip():
            block_lines.append("")
            continue

        indent = len(line) - len(line.lstrip(" "))
        if indent < base_indent:
            break
        block_lines.append(line[base_indent:])

    return "\n".join(block_lines).strip() + "\n"


def test_default_guardrails_policy_template_has_non_empty_scanners():
    policy = yaml.safe_load(_extract_default_policy_text())

    scanners = policy.get("scanners")
    assert isinstance(scanners, list)
    assert scanners

    parsed = _parse_policy_scanner_configs(scanners, str(CHART_TEMPLATE))
    assert parsed
    assert [scanner.type for scanner in parsed] == ["regex"]
    assert [scanner.action_on_hit for scanner in parsed] == ["redact"]
