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
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml
from fastapi import FastAPI, HTTPException, Request, Response
from llm_guard import scan_output
from llm_guard.input_scanners.ban_substrings import MatchType as BanMatchType
from llm_guard.input_scanners.regex import MatchType as RegexMatchType
from llm_guard.output_scanners import BanSubstrings, Regex

DEFAULT_BLOCK_MESSAGE = "The model output was blocked by gateway guardrails."


@dataclass(frozen=True)
class ScannerRule:
    scanner: Any
    action: str


@dataclass(frozen=True)
class Policy:
    scanners: tuple[ScannerRule, ...]
    block_message: str = DEFAULT_BLOCK_MESSAGE


@dataclass(frozen=True)
class ScanResult:
    body: bytes
    action: str


def load_policy(path: str) -> Policy:
    raw = yaml.safe_load(Path(path).read_text(encoding="utf-8")) or {}
    rules = tuple(_build_rule(item) for item in raw.get("scanners", []))
    return Policy(
        scanners=rules,
        block_message=str(raw.get("blockMessage", DEFAULT_BLOCK_MESSAGE)),
    )


def scan_openai_response(body: bytes, policy: Policy) -> ScanResult:
    try:
        payload = json.loads(body)
    except (TypeError, ValueError) as exc:
        raise ValueError("response is not valid JSON") from exc

    final_action = "allow"
    for choice in payload.get("choices", []):
        message = choice.get("message") or {}
        content = message.get("content")
        if message.get("role") != "assistant" or not isinstance(content, str):
            continue

        sanitized = content
        for rule in policy.scanners:
            sanitized, valid, _ = scan_output(
                [rule.scanner], "", sanitized, fail_fast=False
            )
            if all(valid.values()):
                continue
            if rule.action == "block":
                message["content"] = policy.block_message
                final_action = "block"
                break
            message["content"] = sanitized
            if final_action != "block":
                final_action = "redact"

    return ScanResult(
        body=json.dumps(payload, separators=(",", ":")).encode(),
        action=final_action,
    )


def _build_rule(raw: dict[str, Any]) -> ScannerRule:
    scanner_type = raw.get("type")
    action = str(raw.get("action", "redact")).lower()
    if action not in {"block", "redact"}:
        raise ValueError(f"unsupported scanner action: {action}")

    if scanner_type == "ban_substrings":
        scanner = BanSubstrings(
            substrings=list(raw.get("substrings", [])),
            match_type=BanMatchType(str(raw.get("match_type", "word"))),
            case_sensitive=bool(raw.get("case_sensitive", False)),
            contains_all=bool(raw.get("contains_all", False)),
            redact=action == "redact",
        )
    elif scanner_type == "regex":
        scanner = Regex(
            patterns=list(raw.get("patterns", [])),
            is_blocked=bool(raw.get("is_blocked", True)),
            match_type=RegexMatchType(str(raw.get("match_type", "search"))),
            redact=action == "redact",
        )
    else:
        raise ValueError(f"unsupported scanner type in PoC: {scanner_type}")
    return ScannerRule(scanner=scanner, action=action)


POLICY_PATH = os.getenv("POLICY_PATH", "/etc/llm-guard/policy.yaml")
app = FastAPI()


@app.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/scan")
async def scan(request: Request) -> Response:
    try:
        result = scan_openai_response(await request.body(), load_policy(POLICY_PATH))
    except (OSError, ValueError) as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return Response(
        content=result.body,
        media_type="application/json",
        headers={"x-llm-guard-action": result.action},
    )
