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

import json

from scanner_service import Policy, _build_rule, scan_openai_response


def _body(content: str) -> bytes:
    return json.dumps(
        {
            "id": "chatcmpl-poc",
            "choices": [{"message": {"role": "assistant", "content": content}}],
        }
    ).encode()


def _content(body: bytes) -> str:
    return json.loads(body)["choices"][0]["message"]["content"]


def test_allows_clean_response() -> None:
    policy = Policy(
        scanners=(_build_rule({"type": "ban_substrings", "substrings": ["deny"]}),)
    )

    result = scan_openai_response(_body("safe response"), policy)

    assert result.action == "allow"
    assert _content(result.body) == "safe response"


def test_redacts_regex_match() -> None:
    policy = Policy(
        scanners=(
            _build_rule(
                {
                    "type": "regex",
                    "action": "redact",
                    "patterns": [r"DEMO-[0-9]{4}"],
                }
            ),
        )
    )

    result = scan_openai_response(_body("token DEMO-1234"), policy)

    assert result.action == "redact"
    assert "DEMO-1234" not in _content(result.body)


def test_blocks_banned_substring() -> None:
    policy = Policy(
        scanners=(
            _build_rule(
                {
                    "type": "ban_substrings",
                    "action": "block",
                    "substrings": ["INTERNAL_ONLY"],
                }
            ),
        ),
        block_message="blocked by PoC",
    )

    result = scan_openai_response(_body("INTERNAL_ONLY material"), policy)

    assert result.action == "block"
    assert _content(result.body) == "blocked by PoC"
