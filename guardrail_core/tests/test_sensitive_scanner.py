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

import pytest
from guardrail_core.config import SensitiveConfig, build_scanner, parse_scanner_config
from guardrail_core.scanning import ScanResult


@pytest.mark.parametrize("action", ["block", "redact"])
@pytest.mark.parametrize(
    "detector,text,redacted",
    [
        ("email", "Email alice@example.com", "Email <EMAIL>"),
        ("phone", "Call +1 (202) 555-0123", "Call <PHONE>"),
        ("credit_card", "Card 4111 1111 1111 1111", "Card <CREDIT_CARD>"),
        ("ip_address", "Host 192.0.2.1", "Host <IP_ADDRESS>"),
        (
            "email",
            "alice@example.com and bob@example.com",
            "<EMAIL> and <EMAIL>",
        ),
    ],
)
def test_detection_and_redaction_are_preserved(action, detector, text, redacted):
    scanner = build_scanner(
        parse_scanner_config(
            {"type": "sensitive", "detectors": [detector], "action": action}
        )
    )
    assert scanner.scan("ignored prompt", text) == ScanResult(
        redacted if action == "redact" else text, False, 1.0
    )


@pytest.mark.parametrize("action", ["block", "redact"])
@pytest.mark.parametrize(
    "detector,text",
    [
        ("email", ""),
        ("email", " \t\n"),
        ("email", "safe output"),
        ("phone", "1234567890123456"),
        ("phone", "123-45-6789"),
        ("phone", "1234-5678-9012-3456"),
        ("credit_card", "4111 1111 1111 1112"),
        ("ip_address", "999.0.2.1"),
    ],
)
def test_non_findings_are_preserved(action, detector, text):
    scanner = SensitiveConfig(detectors=[detector]).build(action)
    assert scanner.scan("", text) == ScanResult(text, True, -1.0)


@pytest.mark.parametrize(
    "detectors,replacement",
    [
        (["phone", "ip_address"], "<PHONE>"),
        (["ip_address", "phone"], "<IP_ADDRESS>"),
    ],
)
def test_overlapping_matches_preserve_detector_order(detectors, replacement):
    scanner = SensitiveConfig(detectors=detectors).build("redact")
    assert scanner.scan("", "Host 192.168.100.10") == ScanResult(
        f"Host {replacement}", False, 1.0
    )


def test_default_detectors_and_multiple_replacements():
    scanner = SensitiveConfig.from_dict({}).build("redact")
    assert scanner.scan("", "alice@example.com at 192.0.2.1") == ScanResult(
        "<EMAIL> at <IP_ADDRESS>", False, 1.0
    )
