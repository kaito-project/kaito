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

"""Drives the issue-#2358 streaming-guardrail experiments (Exp1-Exp5b).

Requires the compose stack to be up::

    docker compose up -d --build
    python scripts/run_experiments.py

    Each experiment sets the guard's runtime config (scan on/off, holdback window) and issues one SSE request through Envoy (:18080), comparing against a direct-to-mock (:18081) baseline. Results print as a table at the end. The tracked ``extproc-config/stream_guard.json`` is written per experiment and restored on exit, so a run leaves no diff. Exits non-zero if any experiment fails.
"""

import json
import os
import pathlib
import sys
import time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent / "client"))

from sse_client import SseResult, fetch  # noqa: E402

CROSS_SECRET = "sk-1234567890abcdef"
DIRECT_PORT = 18081
ENVOY_PORT = 18080
MAX_ATTEMPTS = 6
RUNTIME_PATH = pathlib.Path(
    pathlib.Path(__file__).resolve().parent.parent
    / "extproc-config"
    / "stream_guard.json"
)

RESULTS: list[dict] = []


def set_runtime(scan_enabled: bool, holdback_bytes: int) -> None:
    """Flip the guard's scan/holdback settings for the next requests.

    The runtime file is bind-mounted into the extproc container, which reloads it per stream on mtime change, so writing the host file is enough.
    """
    payload = {
        "scan_enabled": bool(scan_enabled),
        "holdback_bytes": int(holdback_bytes),
    }
    RUNTIME_PATH.write_text(json.dumps(payload), encoding="utf-8")
    now = time.time()
    os.utime(RUNTIME_PATH, (now, now))
    time.sleep(0.2)  # let the container observe the new mtime


def restore_runtime(original: bytes) -> None:
    """Put the tracked runtime file back to its committed contents."""
    RUNTIME_PATH.write_bytes(original)
    now = time.time()
    os.utime(RUNTIME_PATH, (now, now))


def fetch_envoy(scenario: str, **kw) -> SseResult:
    """Fetch through Envoy, retrying Envoy-side transport noise.

    A non-200 or incomplete stream is the HTTP/1.1 race in README Finding 1, not a guard verdict, so retry and record the attempt count.
    """
    for attempt in range(1, MAX_ATTEMPTS + 1):
        res = fetch(scenario, host="localhost", port=ENVOY_PORT, **kw)
        res.attempts = attempt
        if res.status == 200 or attempt == MAX_ATTEMPTS:
            return res
        time.sleep(0.2 * attempt)
    raise AssertionError("unreachable")


def record(
    exp: str,
    scenario: str,
    holdback: int,
    res: SseResult,
    expected: str,
    verdict: str,
    note: str = "",
) -> None:
    RESULTS.append(
        {
            "exp": exp,
            "scenario": scenario,
            "holdback": holdback,
            "status": res.status,
            "attempts": res.attempts,
            "ttft_ms": res.ttft_ms,
            "total_ms": res.total_ms,
            "events": res.event_count,
            "done": res.done,
            "blocked": res.blocked,
            "verdict": verdict,
            "note": note,
        }
    )
    retries = f" (retry x{res.attempts})" if res.attempts > 1 else ""
    print(
        f"  {exp:<5}{scenario:<8} holdback={holdback:<3} {verdict:<14}"
        f" ttft={res.ttft_ms}ms events={res.event_count} done={res.done} {note}{retries}"
    )


def main() -> None:
    original_runtime = RUNTIME_PATH.read_bytes()
    try:
        run()
    finally:
        restore_runtime(original_runtime)


def run() -> None:
    print("== issue #2358 streaming-guardrail experiments ==\n")

    base_normal = fetch("normal", host="localhost", port=DIRECT_PORT)
    base_slow = fetch("slow", delay_ms=120, host="localhost", port=DIRECT_PORT)
    print(
        f"baseline: normal_joined={base_normal.joined_text!r} "
        f"slow_ttft={base_slow.ttft_ms}ms\n"
    )

    set_runtime(scan_enabled=False, holdback_bytes=0)
    res1 = fetch_envoy("normal")
    ok = res1.joined_text == base_normal.joined_text and res1.done
    record(
        "Exp1",
        "normal",
        0,
        res1,
        base_normal.joined_text,
        "PASS" if ok else "FAIL",
        note="pass-through" if ok else "content mismatch",
    )

    set_runtime(scan_enabled=True, holdback_bytes=0)
    res2 = fetch_envoy("aligned")
    ok = "foo" not in res2.joined_text and res2.done
    record(
        "Exp2",
        "aligned",
        0,
        res2,
        "no 'foo'",
        "PASS" if ok else "FAIL",
        note=f"joined={res2.joined_text!r}",
    )

    res3 = fetch_envoy("split")
    expected3 = "reassembled across many writes"
    ok = res3.joined_text == expected3 and res3.done
    record(
        "Exp3",
        "split",
        0,
        res3,
        expected3,
        "PASS" if ok else "FAIL",
        note=f"joined={res3.joined_text!r}",
    )

    set_runtime(scan_enabled=True, holdback_bytes=64)
    res4 = fetch_envoy("cross")
    ok = (
        CROSS_SECRET not in res4.joined_text
        and "[REDACTED]" in res4.joined_text
        and res4.done
    )
    record(
        "Exp4",
        "cross",
        64,
        res4,
        "secret hidden",
        "PASS" if ok else "FAIL",
        note=f"joined={res4.joined_text!r}",
    )

    slow = fetch_envoy("slow", delay_ms=120)
    overhead = (slow.ttft_ms or 0) - (base_slow.ttft_ms or 0)
    ok = slow.done and slow.joined_text == base_slow.joined_text
    record(
        "Exp5a",
        "slow",
        64,
        slow,
        "TTFT overhead",
        "PASS" if ok else "FAIL",
        note=f"guard_ttft={slow.ttft_ms}ms baseline_ttft={base_slow.ttft_ms}ms overhead={overhead:.1f}ms",
    )

    set_runtime(scan_enabled=True, holdback_bytes=0)
    res5b = fetch_envoy("block")
    ok = res5b.blocked and "PROHIBITED" not in res5b.joined_text
    record(
        "Exp5b",
        "block",
        0,
        res5b,
        "block fail-closed",
        "PASS" if ok else "FAIL",
        note=f"blocked={res5b.blocked} msg={res5b.block_message!r}",
    )

    print("\n== results ==")
    header = (
        f"{'exp':<6}{'scenario':<9}{'hold':<6}{'status':<8}{'att':<5}"
        f"{'ttft_ms':<9}{'events':<8}{'verdict':<14}"
    )
    print(header)
    print("-" * len(header))
    for row in RESULTS:
        print(
            f"{row['exp']:<6}{row['scenario']:<9}{row['holdback']:<6}{row['status']:<8}"
            f"{row['attempts']:<5}{str(row['ttft_ms']):<9}{row['events']:<8}{row['verdict']:<14}"
        )

    failed = [r for r in RESULTS if r["verdict"] == "FAIL"]
    if failed:
        print(
            f"\n{len(failed)} experiment(s) FAILED: {', '.join(r['exp'] for r in failed)}"
        )
        sys.exit(1)
    print("\nall experiments behaved as expected")


if __name__ == "__main__":
    main()
