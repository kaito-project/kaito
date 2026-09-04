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

#!/usr/bin/env python3
"""RAGEngine Guardrails Benchmark Driver.

Runs all 5 profiles (NS0, NS1, S0, S1, S2) against a deterministic mock
OpenAI upstream and measures latency, TTFT delay, throughput, CPU/memory,
and correctness.

Usage:
    python -m benchmarks.ragengine_guardrails.benchmark [OPTIONS]

Requires the ragengine package on sys.path (run from repo root with
PYTHONPATH=presets/ragengine).
"""

import argparse
import asyncio
import contextlib
import json
import logging
import os
import platform
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

import httpx

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
BENCH_DIR = Path(__file__).resolve().parent
POLICIES_DIR = BENCH_DIR / "policies"
CASES_DIR = BENCH_DIR / "cases"
RESULTS_DIR = BENCH_DIR / "results"

logger = logging.getLogger("guardrails_benchmark")


# ---------------------------------------------------------------------------
# Profile definitions
# ---------------------------------------------------------------------------
@dataclass
class ProfileConfig:
    name: str
    stream: bool
    guardrails_enabled: bool
    policy_file: str  # relative to POLICIES_DIR, empty = no policy


PROFILES = {
    "NS0": ProfileConfig("NS0", stream=False, guardrails_enabled=False, policy_file=""),
    "NS1": ProfileConfig(
        "NS1", stream=False, guardrails_enabled=True, policy_file="redact_block.yaml"
    ),
    "S0": ProfileConfig("S0", stream=True, guardrails_enabled=False, policy_file=""),
    "S1": ProfileConfig(
        "S1", stream=True, guardrails_enabled=True, policy_file="block_only.yaml"
    ),
    "S2": ProfileConfig(
        "S2", stream=True, guardrails_enabled=True, policy_file="redact_block.yaml"
    ),
}


# ---------------------------------------------------------------------------
# Mock server lifecycle
# ---------------------------------------------------------------------------
class MockServerProcess:
    """Run mock_server.py in a subprocess on a given port."""

    def __init__(self, port: int = 9100):
        self.port = port
        self._proc: subprocess.Popen | None = None

    def start(self) -> None:
        self._proc = subprocess.Popen(
            [
                sys.executable,
                "-c",
                f"import uvicorn; from benchmarks.ragengine_guardrails.mock_server import app; "
                f"uvicorn.run(app, host='127.0.0.1', port={self.port}, log_level='warning')",
            ],
            cwd=str(BENCH_DIR.parent.parent),  # repo root
        )

    def stop(self) -> None:
        if self._proc:
            self._proc.terminate()
            self._proc.wait(timeout=5)
            self._proc = None

    @property
    def base_url(self) -> str:
        return f"http://127.0.0.1:{self.port}"


async def wait_for_server(url: str, timeout: float = 10.0) -> None:
    """Poll until the server is responding."""
    deadline = time.monotonic() + timeout
    async with httpx.AsyncClient() as client:
        while time.monotonic() < deadline:
            try:
                resp = await client.get(url + "/docs")
                if resp.status_code == 200:
                    return
            except httpx.ConnectError:
                pass
            await asyncio.sleep(0.1)
    raise TimeoutError(f"Server at {url} did not start within {timeout}s")


# ---------------------------------------------------------------------------
# Unit-level benchmarking (Level A)
# ---------------------------------------------------------------------------
async def bench_unit_nonstreaming(
    guardrails,
    request_dict: dict,
    response_text: str,
    iterations: int,
    warmup: int,
) -> list[float]:
    """Benchmark guard_response() directly. Returns list of latency_ms."""
    from ragengine.models import ChatCompletionResponse

    # Build a ChatCompletionResponse
    response_data = {
        "id": "chatcmpl-bench",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": "mock-model",
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": response_text},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
    }
    latencies = []
    for i in range(warmup + iterations):
        # Rebuild response each time (guard_response may mutate the dump)
        resp = ChatCompletionResponse(**response_data)
        start = time.perf_counter()
        guardrails.guard_response(resp, request_dict)
        elapsed_ms = (time.perf_counter() - start) * 1000
        if i >= warmup:
            latencies.append(elapsed_ms)

    return latencies


async def bench_unit_streaming(
    guardrails,
    request_dict: dict,
    response_text: str,
    chunk_size: int,
    iterations: int,
    warmup: int,
) -> tuple[list[float], list[float]]:
    """Benchmark apply_streaming_guardrails() directly.

    Returns (latency_ms_list, ttft_delay_ms_list).
    """
    from ragengine.streaming.guardrails import apply_streaming_guardrails
    from ragengine.streaming.openai import (
        build_openai_chat_delta_sse_chunk,
        build_openai_chat_finish_reason_sse_chunk,
        build_sse_done_chunk,
    )

    # Pre-build SSE chunks
    chunks = []
    for i in range(0, len(response_text), chunk_size):
        chunks.append(
            build_openai_chat_delta_sse_chunk(response_text[i : i + chunk_size])
        )
    chunks.append(build_openai_chat_finish_reason_sse_chunk(finish_reason="stop"))
    chunks.append(build_sse_done_chunk())

    latencies = []
    ttft_delays = []

    for iteration in range(warmup + iterations):

        async def upstream_iter():
            for c in chunks:
                yield c

        start = time.perf_counter()
        first_content_time = None
        collected = []
        async for emitted in apply_streaming_guardrails(
            upstream_iter(), guardrails, request_dict
        ):
            if first_content_time is None and "content" in emitted:
                first_content_time = time.perf_counter()
            collected.append(emitted)
        end = time.perf_counter()

        if iteration >= warmup:
            latencies.append((end - start) * 1000)
            if first_content_time:
                ttft_delays.append((first_content_time - start) * 1000)

    return latencies, ttft_delays


# ---------------------------------------------------------------------------
# Integration benchmarking (Level B) — HTTP round-trip
# ---------------------------------------------------------------------------
async def bench_integration(
    client: httpx.AsyncClient,
    endpoint: str,
    request_body: dict,
    iterations: int,
    warmup: int,
) -> tuple[list[float], list[float], list[str]]:
    """Run HTTP benchmarks. Returns (latency_ms, ttft_ms, last_collected_chunks)."""
    latencies = []
    ttft_list = []
    last_chunks: list[str] = []
    stream = request_body.get("stream", False)

    for i in range(warmup + iterations):
        start = time.perf_counter()
        first_content_time = None
        chunks_collected: list[str] = []

        if stream:
            async with client.stream("POST", endpoint, json=request_body) as resp:
                async for chunk in resp.aiter_text():
                    if first_content_time is None and chunk.strip():
                        first_content_time = time.perf_counter()
                    chunks_collected.append(chunk)
        else:
            resp = await client.post(endpoint, json=request_body)
            chunks_collected.append(resp.text)

        end = time.perf_counter()

        if i >= warmup:
            latencies.append((end - start) * 1000)
            if first_content_time:
                ttft_list.append((first_content_time - start) * 1000)
            last_chunks = chunks_collected

    return latencies, ttft_list, last_chunks


# ---------------------------------------------------------------------------
# Correctness runner
# ---------------------------------------------------------------------------
async def run_correctness_checks(
    guardrails,
    request_dict: dict,
    block_message: str,
) -> dict:
    """Run correctness validation suite."""
    from ragengine.streaming.guardrails import apply_streaming_guardrails
    from ragengine.streaming.openai import (
        build_openai_chat_delta_sse_chunk,
        build_openai_chat_finish_reason_sse_chunk,
        build_sse_done_chunk,
    )

    from benchmarks.ragengine_guardrails.validate import (
        CorrectnessReport,
        validate_allow_unchanged,
        validate_block_response,
        validate_no_false_positive,
        validate_no_leakage,
        validate_redaction,
        validate_streaming_block,
    )
    from benchmarks.ragengine_guardrails.workloads import (
        build_cross_chunk_cases,
        generate_safe_text,
        generate_text_with_banned_word,
        generate_text_with_email,
    )

    report = CorrectnessReport()

    # --- Allow path ---
    safe_text = generate_safe_text(128)
    # Unit-level non-streaming
    from ragengine.models import ChatCompletionResponse

    resp_data = {
        "id": "test",
        "object": "chat.completion",
        "created": 0,
        "model": "m",
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": safe_text},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
    }
    guarded = guardrails.guard_response(
        ChatCompletionResponse(**resp_data), request_dict
    )
    output = guarded.choices[0].message.content
    report.add(validate_allow_unchanged(safe_text, output))
    report.add(validate_no_false_positive(safe_text, output))

    # --- Block path (non-streaming) ---
    block_text = generate_text_with_banned_word(128, "SECRET_PROJECT")
    resp_data["choices"][0]["message"]["content"] = block_text
    guarded = guardrails.guard_response(
        ChatCompletionResponse(**resp_data), request_dict
    )
    output = guarded.choices[0].message.content
    report.add(validate_block_response(output, block_message))

    # --- Redaction path (non-streaming) ---
    email_text = generate_text_with_email(128)
    resp_data["choices"][0]["message"]["content"] = email_text
    guarded = guardrails.guard_response(
        ChatCompletionResponse(**resp_data), request_dict
    )
    output = guarded.choices[0].message.content
    report.add(
        validate_redaction(
            email_text,
            output,
            expected_removals=["user@example.com"],
            expected_tags=["<EMAIL>"],
        )
    )

    # --- Streaming cross-chunk leakage ---
    cross_chunk_cases = build_cross_chunk_cases("SECRET_PROJECT")
    for case in cross_chunk_cases:
        if case.expected_action == "allow":
            continue  # Skip non-match cases for leakage test

        # Build chunks that split the banned word
        text = case.full_text
        cs = case.chunk_size
        sse_chunks = []
        for j in range(0, len(text), cs):
            sse_chunks.append(build_openai_chat_delta_sse_chunk(text[j : j + cs]))
        sse_chunks.append(
            build_openai_chat_finish_reason_sse_chunk(finish_reason="stop")
        )
        sse_chunks.append(build_sse_done_chunk())

        async def upstream():
            for c in sse_chunks:
                yield c

        collected = []
        async for emitted in apply_streaming_guardrails(
            upstream(), guardrails, request_dict
        ):
            collected.append(emitted)

        report.add(validate_no_leakage(collected, case.banned_word))
        report.add(validate_streaming_block(collected, block_message))

    report.print_report()
    return report.summary()


# ---------------------------------------------------------------------------
# Profile runner
# ---------------------------------------------------------------------------
async def run_profile(
    profile: ProfileConfig,
    mock_url: str,
    response_text: str,
    chunk_size: int,
    iterations: int,
    warmup: int,
) -> dict:
    """Run a single profile and return results dict."""
    from benchmarks.ragengine_guardrails.metrics import (
        ProfileResult,
    )
    from benchmarks.ragengine_guardrails.resource_collector import measure_resources
    from benchmarks.ragengine_guardrails.workloads import build_standard_request

    logger.info(
        "Running profile %s (iterations=%d, warmup=%d)",
        profile.name,
        iterations,
        warmup,
    )

    # Build guardrails instance
    guardrails = _build_guardrails(profile)
    request_dict = build_standard_request(stream=profile.stream)

    result = ProfileResult(profile=profile.name)

    # --- Level A: Unit-level ---
    if not profile.stream:
        unit_latencies = await bench_unit_nonstreaming(
            guardrails, request_dict, response_text, iterations, warmup
        )
        # Use unit latencies as e2e for unit-level measurement
        for lat in unit_latencies:
            result.e2e_latency_ms.append(lat)
    else:
        unit_latencies, unit_ttft = await bench_unit_streaming(
            guardrails, request_dict, response_text, chunk_size, iterations, warmup
        )
        for lat in unit_latencies:
            result.e2e_latency_ms.append(lat)
        for ttft in unit_ttft:
            result.ttft_delay_ms.append(ttft)
            result.client_ttft_ms.append(ttft)

    # --- Resource measurement (sample of 10) ---
    resource_iters = min(10, iterations)
    for _ in range(resource_iters):
        with measure_resources() as snap:
            if not profile.stream:
                await bench_unit_nonstreaming(
                    guardrails, request_dict, response_text, 1, 0
                )
            else:
                await bench_unit_streaming(
                    guardrails, request_dict, response_text, chunk_size, 1, 0
                )
        result.cpu_time_ms.append(snap.cpu_time_ms)
        result.memory_peak_kb.append(snap.memory_peak_kb)

    # --- Throughput (total time for all iterations) ---
    if result.e2e_latency_ms:
        total_time_s = sum(result.e2e_latency_ms) / 1000
        if total_time_s > 0:
            result.throughput_rps = len(result.e2e_latency_ms) / total_time_s
            result.total_response_bytes = len(response_text) * len(
                result.e2e_latency_ms
            )
            result.throughput_bytes_per_sec = result.total_response_bytes / total_time_s

    stats = result.stats()
    logger.info("Profile %s complete: %s", profile.name, json.dumps(stats, indent=2))
    return stats


def _build_guardrails(profile: ProfileConfig):
    """Build an OutputGuardrails instance for the given profile."""
    from ragengine.guardrails.output_guardrails import OutputGuardrails

    if not profile.guardrails_enabled:
        return OutputGuardrails(enabled=False)

    # Temporarily set env vars and use from_config
    policy_path = str(POLICIES_DIR / profile.policy_file) if profile.policy_file else ""
    orig_enabled = os.environ.get("OUTPUT_GUARDRAILS_ENABLED")
    orig_path = os.environ.get("OUTPUT_GUARDRAILS_POLICY_PATH")

    try:
        os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "true"
        os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = policy_path
        # Reload config module to pick up new env vars
        import ragengine.config as cfg

        cfg.OUTPUT_GUARDRAILS_ENABLED = True
        cfg.OUTPUT_GUARDRAILS_POLICY_PATH = policy_path
        return OutputGuardrails.from_config()
    finally:
        # Restore
        if orig_enabled is not None:
            os.environ["OUTPUT_GUARDRAILS_ENABLED"] = orig_enabled
        elif "OUTPUT_GUARDRAILS_ENABLED" in os.environ:
            del os.environ["OUTPUT_GUARDRAILS_ENABLED"]
        if orig_path is not None:
            os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = orig_path
        elif "OUTPUT_GUARDRAILS_POLICY_PATH" in os.environ:
            del os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"]


# ---------------------------------------------------------------------------
# Reload benchmark
# ---------------------------------------------------------------------------
async def bench_reload(iterations: int = 100) -> dict:
    """Benchmark policy reload time."""
    from benchmarks.ragengine_guardrails.metrics import LatencyStats

    logger.info("Running reload benchmark (%d iterations)", iterations)

    policy_files = ["simple.yaml", "block_only.yaml", "redact_block.yaml", "full.yaml"]
    results = {}

    for policy_file in policy_files:
        policy_path = str(POLICIES_DIR / policy_file)
        os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "true"
        os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = policy_path

        import ragengine.config as cfg

        cfg.OUTPUT_GUARDRAILS_ENABLED = True
        cfg.OUTPUT_GUARDRAILS_POLICY_PATH = policy_path

        from ragengine.guardrails.output_guardrails import OutputGuardrails
        from ragengine.guardrails.reload import GuardrailsReloader

        latencies = []
        for _ in range(iterations):
            reloader = GuardrailsReloader(
                policy_path=policy_path,
                factory=OutputGuardrails.from_config,
            )
            start = time.perf_counter()
            reloader._reload()
            elapsed_ms = (time.perf_counter() - start) * 1000
            latencies.append(elapsed_ms)

        stats = LatencyStats.from_values(latencies)
        results[policy_file] = stats.to_dict()
        logger.info("Reload %s: %s", policy_file, json.dumps(stats.to_dict()))

    return results


# ---------------------------------------------------------------------------
# Main orchestrator
# ---------------------------------------------------------------------------
async def run_benchmark(args: argparse.Namespace) -> dict:
    """Run the full benchmark suite."""
    from benchmarks.ragengine_guardrails.metrics import (
        ProfileResult,
        save_results,
    )
    from benchmarks.ragengine_guardrails.workloads import generate_safe_text

    response_text = generate_safe_text(args.response_tokens)
    logger.info(
        "Benchmark config: tokens=%d, chunk_size=%d, iterations=%d, warmup=%d",
        args.response_tokens,
        args.chunk_size,
        args.iterations,
        args.warmup,
    )

    # --- Run profiles ---
    profile_results = {}
    profile_objects = {}
    selected = args.profiles.split(",") if args.profiles else list(PROFILES.keys())

    for pname in selected:
        if pname not in PROFILES:
            logger.warning("Unknown profile: %s", pname)
            continue
        stats = await run_profile(
            PROFILES[pname],
            mock_url="",  # unit-level only for Phase 1
            response_text=response_text,
            chunk_size=args.chunk_size,
            iterations=args.iterations,
            warmup=args.warmup,
        )
        profile_results[pname] = stats
        # Re-create ProfileResult from stats for overhead computation
        pr = ProfileResult(profile=pname)
        pr.e2e_latency_ms = [stats["e2e_latency_ms"]["mean"]] * stats["e2e_latency_ms"][
            "count"
        ]
        profile_objects[pname] = pr

    # --- Compute overheads ---
    overhead_pairs = [("NS1", "NS0"), ("S1", "S0"), ("S2", "S0")]
    overhead_results = []
    for guarded, baseline in overhead_pairs:
        if guarded in profile_results and baseline in profile_results:
            g_stats = profile_results[guarded]["e2e_latency_ms"]
            b_stats = profile_results[baseline]["e2e_latency_ms"]

            def safe_pct(g, b):
                return round((g - b) / b * 100, 2) if b > 0 else 0

            overhead_result = {
                "guarded": guarded,
                "baseline": baseline,
                "absolute_overhead_ms": {
                    "p50": round(g_stats["p50"] - b_stats["p50"], 3),
                    "p95": round(g_stats["p95"] - b_stats["p95"], 3),
                    "p99": round(g_stats["p99"] - b_stats["p99"], 3),
                },
                "relative_overhead_pct": {
                    "p50": safe_pct(g_stats["p50"], b_stats["p50"]),
                    "p95": safe_pct(g_stats["p95"], b_stats["p95"]),
                    "p99": safe_pct(g_stats["p99"], b_stats["p99"]),
                },
            }
            overhead_results.append(overhead_result)

    # --- Correctness checks ---
    correctness = {}
    if not args.skip_correctness:
        logger.info("Running correctness checks...")
        guardrails = _build_guardrails(PROFILES["S2"])
        from benchmarks.ragengine_guardrails.workloads import build_standard_request

        request_dict = build_standard_request(stream=True)
        correctness = await run_correctness_checks(
            guardrails,
            request_dict,
            block_message="The model output was blocked by output guardrails.",
        )

    # --- Reload benchmark ---
    reload_results = {}
    if not args.skip_reload:
        reload_results = await bench_reload(iterations=args.reload_iterations)

    # --- Assemble results ---
    results = {
        "metadata": _build_metadata(args),
        "profiles": profile_results,
        "overheads": overhead_results,
        "correctness": correctness,
        "reload": reload_results,
    }

    # --- Save and print ---
    output_path = save_results(results, RESULTS_DIR)
    logger.info("Results saved to %s", output_path)

    # Print summary
    print("\n" + "=" * 90)
    print("PROFILE RESULTS (unit-level, ms)")
    print("=" * 90)
    print(
        f"{'Profile':<8} {'P50':>10} {'P95':>10} {'P99':>10} {'Mean':>10} {'StdDev':>10} {'N':>6}"
    )
    print("-" * 90)
    for pname in selected:
        if pname in profile_results:
            s = profile_results[pname]["e2e_latency_ms"]
            print(
                f"{pname:<8} {s['p50']:>10.3f} {s['p95']:>10.3f} {s['p99']:>10.3f} {s['mean']:>10.3f} {s['stddev']:>10.3f} {s['count']:>6}"
            )

    # TTFT
    streaming_profiles = [
        p
        for p in selected
        if p in profile_results and "ttft_delay_ms" in profile_results[p]
    ]
    if streaming_profiles:
        print(f"\n{'Profile':<8} {'TTFT P50':>10} {'TTFT P95':>10} {'TTFT Mean':>10}")
        print("-" * 50)
        for pname in streaming_profiles:
            t = profile_results[pname]["ttft_delay_ms"]
            print(f"{pname:<8} {t['p50']:>10.3f} {t['p95']:>10.3f} {t['mean']:>10.3f}")

    # Overheads
    if overhead_results:
        print(
            f"\n{'Guarded':<8} {'vs':<8} {'Abs P50':>10} {'Abs P95':>10} {'Rel P50':>10} {'Rel P95':>10}"
        )
        print("-" * 60)
        for o in overhead_results:
            print(
                f"{o['guarded']:<8} {o['baseline']:<8} "
                f"{o['absolute_overhead_ms']['p50']:>10.3f} {o['absolute_overhead_ms']['p95']:>10.3f} "
                f"{o['relative_overhead_pct']['p50']:>9.1f}% {o['relative_overhead_pct']['p95']:>9.1f}%"
            )

    # Resources
    profiles_with_resources = [
        p
        for p in selected
        if p in profile_results and "cpu_time_ms" in profile_results[p]
    ]
    if profiles_with_resources:
        print(
            f"\n{'Profile':<8} {'CPU P50':>10} {'CPU Mean':>10} {'Mem P50':>12} {'Mem Mean':>12}"
        )
        print("-" * 60)
        for pname in profiles_with_resources:
            cpu = profile_results[pname]["cpu_time_ms"]
            mem = profile_results[pname]["memory_peak_kb"]
            print(
                f"{pname:<8} {cpu['p50']:>10.3f} {cpu['mean']:>10.3f} {mem['p50']:>10.1f}KB {mem['mean']:>10.1f}KB"
            )

    # Reload
    if reload_results:
        print(
            f"\n{'Policy':<24} {'Reload P50':>12} {'Reload P95':>12} {'Reload Mean':>12}"
        )
        print("-" * 65)
        for policy, stats in reload_results.items():
            print(
                f"{policy:<24} {stats['p50']:>12.3f} {stats['p95']:>12.3f} {stats['mean']:>12.3f}"
            )

    print("=" * 90)
    return results


def _build_metadata(args: argparse.Namespace) -> dict:
    """Build reproducibility metadata."""
    commit_sha = "unknown"
    with contextlib.suppress(Exception):
        commit_sha = subprocess.check_output(
            ["git", "rev-parse", "HEAD"],
            cwd=str(BENCH_DIR.parent.parent),
            text=True,
            stderr=subprocess.DEVNULL,
        ).strip()

    return {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "kaito_commit": commit_sha,
        "python_version": platform.python_version(),
        "platform": platform.platform(),
        "response_tokens": args.response_tokens,
        "chunk_size": args.chunk_size,
        "iterations": args.iterations,
        "warmup": args.warmup,
        "profiles": args.profiles or "all",
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="RAGEngine Guardrails Benchmark")
    parser.add_argument(
        "--response-tokens",
        type=int,
        default=512,
        help="Output tokens (~4 chars/token)",
    )
    parser.add_argument(
        "--chunk-size", type=int, default=20, help="SSE chunk size in chars"
    )
    parser.add_argument(
        "--iterations", type=int, default=100, help="Measured iterations per profile"
    )
    parser.add_argument(
        "--warmup", type=int, default=10, help="Warmup iterations (discarded)"
    )
    parser.add_argument(
        "--profiles",
        type=str,
        default="",
        help="Comma-separated profiles (default: all)",
    )
    parser.add_argument(
        "--skip-correctness", action="store_true", help="Skip correctness checks"
    )
    parser.add_argument(
        "--skip-reload", action="store_true", help="Skip reload benchmark"
    )
    parser.add_argument(
        "--reload-iterations", type=int, default=100, help="Reload benchmark iterations"
    )
    parser.add_argument("-v", "--verbose", action="store_true")
    return parser.parse_args()


def main():
    args = parse_args()
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(name)s %(levelname)s %(message)s",
    )
    asyncio.run(run_benchmark(args))


if __name__ == "__main__":
    main()
