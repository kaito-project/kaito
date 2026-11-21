#!/usr/bin/env python3
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

"""Minimal SSE client for the streaming-guardrail harness.

Uses :mod:`http.client` so the stdlib decodes chunked, ``Content-Length`` and close-delimited bodies, and stamps each incremental read so ``run_experiments.py`` can report:

* ``joined_text`` -- concatenated ``choices[*].delta.content``, i.e. the stream a real client renders and therefore the security surface.
* ``ttft_ms`` -- POST to first downstream body bytes.
* ``arrival_ms`` -- timestamp of every incremental body read.
* ``blocked`` -- true when a ``guardrails_block`` error frame was seen.
* ``status`` -- HTTP status; the Envoy FD-streamed race in README Finding 1 surfaces as a local 400 error page.
"""

import http.client
import json
import time
from dataclasses import dataclass, field
from urllib.parse import urlencode


@dataclass
class SseResult:
    scenario: str
    status: int = 0
    raw: bytes = b""
    joined_text: str = ""
    ttft_ms: float | None = None
    total_ms: float = 0.0
    event_count: int = 0
    done: bool = False
    blocked: bool = False
    block_message: str = ""
    arrival_ms: list[float] = field(default_factory=list)
    attempts: int = 1


def parse_sse(raw: bytes) -> tuple[str, int, bool, bool, str]:
    """Return (joined_text, event_count, done, blocked, block_message)."""
    parts: list[str] = []
    count = 0
    done = False
    blocked = False
    block_message = ""
    for frame in raw.split(b"\n\n"):
        if not frame.strip():
            continue
        line = frame.decode("utf-8", errors="replace").strip()
        if not line.startswith("data:"):
            continue
        payload = line[len("data:") :].strip()
        if not payload:
            continue
        if payload == "[DONE]":
            done = True
            count += 1
            continue
        try:
            obj = json.loads(payload)
        except json.JSONDecodeError:
            continue
        if isinstance(obj, dict) and isinstance(obj.get("error"), dict):
            error = obj["error"]
            if error.get("type") == "guardrails_block":
                blocked = True
                block_message = str(error.get("message", ""))
            continue
        count += 1
        choices = obj.get("choices")
        if not isinstance(choices, list):
            continue
        for choice in choices:
            delta = (choice or {}).get("delta")
            if isinstance(delta, dict) and isinstance(delta.get("content"), str):
                parts.append(delta["content"])
    return "".join(parts), count, done, blocked, block_message


def fetch(
    scenario: str,
    delay_ms: float = 0.0,
    host: str = "localhost",
    port: int = 18080,
    timeout: float = 60.0,
) -> SseResult:
    """Issue one streaming chat completion and measure client metrics."""
    body = json.dumps({"model": "mock", "stream": True, "temperature": 0})
    query = urlencode({"scenario": scenario, "delay_ms": delay_ms})
    path = f"/v1/chat/completions?{query}"
    result = SseResult(scenario=scenario)

    conn = http.client.HTTPConnection(host, port, timeout=timeout)
    started = time.perf_counter()
    try:
        conn.request(
            "POST",
            path,
            body=body,
            headers={
                "Content-Type": "application/json",
                "Accept": "text/event-stream",
            },
        )
        resp = conn.getresponse()
        result.status = resp.status
        buf = bytearray()
        try:
            while True:
                batch = resp.read1(65536)
                if not batch:
                    break
                buf.extend(batch)
                result.arrival_ms.append(
                    round((time.perf_counter() - started) * 1000, 2)
                )
        except (TimeoutError, ConnectionError):
            # Envoy's FD-streamed race (Finding 1); the driver retries.
            pass
    finally:
        conn.close()

    result.raw = bytes(buf)
    result.total_ms = round((time.perf_counter() - started) * 1000, 2)
    if result.arrival_ms:
        result.ttft_ms = result.arrival_ms[0]
    (
        result.joined_text,
        result.event_count,
        result.done,
        result.blocked,
        result.block_message,
    ) = parse_sse(result.raw)
    return result


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="SSE guardrail harness client")
    parser.add_argument("--scenario", default="normal")
    parser.add_argument("--delay-ms", type=float, default=0.0)
    parser.add_argument("--port", type=int, default=18080)
    parser.add_argument("--host", default="localhost")
    args = parser.parse_args()
    res = fetch(args.scenario, args.delay_ms, host=args.host, port=args.port)
    print(
        f"scenario={res.scenario} status={res.status} ttft_ms={res.ttft_ms} "
        f"total_ms={res.total_ms} events={res.event_count} done={res.done} "
        f"blocked={res.blocked} raw_len={len(res.raw)}"
    )
    print(f"joined: {res.joined_text!r}")
