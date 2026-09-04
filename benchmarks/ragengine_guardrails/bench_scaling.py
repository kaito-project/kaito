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
"""Phase 2 scaling experiments: policy complexity, concurrency, holdback.

Usage:
    PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_scaling
"""

import asyncio
import json
import os
import sys
import time
from pathlib import Path

BENCH_DIR = Path(__file__).resolve().parent
POLICIES_DIR = BENCH_DIR / "policies"
RESULTS_DIR = BENCH_DIR / "results"

sys.path.insert(0, str(BENCH_DIR.parent.parent / "presets"))


def _build_guardrails_with_policy(policy_file: str):
    """Build OutputGuardrails from a specific policy file."""
    import ragengine.config as cfg
    from ragengine.guardrails.output_guardrails import OutputGuardrails

    policy_path = str(POLICIES_DIR / policy_file)
    cfg.OUTPUT_GUARDRAILS_ENABLED = True
    cfg.OUTPUT_GUARDRAILS_POLICY_PATH = policy_path
    os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "true"
    os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = policy_path
    return OutputGuardrails.from_config()


async def _bench_streaming_latency(
    guardrails, request_dict, response_text, chunk_size, iterations, warmup
):
    """Return (latencies_ms, ttft_ms)."""
    from ragengine.streaming.guardrails import apply_streaming_guardrails
    from ragengine.streaming.openai import (
        build_openai_chat_delta_sse_chunk,
        build_openai_chat_finish_reason_sse_chunk,
        build_sse_done_chunk,
    )

    sse_chunks = []
    for i in range(0, len(response_text), chunk_size):
        sse_chunks.append(
            build_openai_chat_delta_sse_chunk(response_text[i : i + chunk_size])
        )
    sse_chunks.append(build_openai_chat_finish_reason_sse_chunk(finish_reason="stop"))
    sse_chunks.append(build_sse_done_chunk())

    latencies = []
    ttft_list = []

    for iteration in range(warmup + iterations):

        async def upstream():
            for c in sse_chunks:
                yield c

        start = time.perf_counter()
        first_content = None
        async for emitted in apply_streaming_guardrails(
            upstream(), guardrails, request_dict
        ):
            if first_content is None and "content" in emitted:
                first_content = time.perf_counter()

        elapsed = (time.perf_counter() - start) * 1000
        if iteration >= warmup:
            latencies.append(elapsed)
            if first_content:
                ttft_list.append((first_content - start) * 1000)

    return latencies, ttft_list


def _stats(values):
    if not values:
        return {"p50": 0, "p95": 0, "p99": 0, "mean": 0}
    s = sorted(values)
    n = len(s)
    import statistics

    return {
        "p50": round(s[int(n * 0.50)], 3),
        "p95": round(s[int(n * 0.95)] if n >= 20 else s[-1], 3),
        "p99": round(s[int(n * 0.99)] if n >= 100 else s[-1], 3),
        "mean": round(statistics.mean(s), 3),
    }


async def run_per_scanner_cost():
    """Measure isolated cost of each scanner type."""
    from benchmarks.ragengine_guardrails.workloads import (
        build_standard_request,
        generate_safe_text,
    )

    print("\n=== PER-SCANNER TYPE COST (streaming, 512 tokens, chunk=20) ===")
    response_text = generate_safe_text(512)
    request_dict = build_standard_request(stream=True)

    scanners = [
        ("scanner_ban_substrings.yaml", "ban_substrings"),
        ("scanner_regex.yaml", "regex"),
        ("scanner_invisible_text.yaml", "invisible_text"),
        ("scanner_json.yaml", "json"),
        ("scanner_reading_time.yaml", "reading_time"),
        ("scanner_token_limit.yaml", "token_limit"),
        ("scanner_sensitive.yaml", "sensitive"),
        ("scanner_secrets.yaml", "secrets"),
    ]

    results = {}
    for policy_file, label in scanners:
        guardrails = _build_guardrails_with_policy(policy_file)
        print(f"  Running {label} ({policy_file})...")
        latencies, ttft = await _bench_streaming_latency(
            guardrails,
            request_dict,
            response_text,
            chunk_size=20,
            iterations=100,
            warmup=10,
        )
        lat_stats = _stats(latencies)
        ttft_stats = _stats(ttft)
        results[label] = {"latency_ms": lat_stats, "ttft_ms": ttft_stats}
        print(
            f"    Latency: P50={lat_stats['p50']:.2f}ms P95={lat_stats['p95']:.2f}ms Mean={lat_stats['mean']:.2f}ms"
        )
        print(
            f"    TTFT:    P50={ttft_stats['p50']:.2f}ms P95={ttft_stats['p95']:.2f}ms Mean={ttft_stats['mean']:.2f}ms"
        )

    return results


async def run_policy_complexity():
    """Vary scanner count: 1, 4, 8."""
    from benchmarks.ragengine_guardrails.workloads import (
        build_standard_request,
        generate_safe_text,
    )

    print("\n=== POLICY COMPLEXITY SENSITIVITY (streaming, 512 tokens, chunk=20) ===")
    response_text = generate_safe_text(512)
    request_dict = build_standard_request(stream=True)

    results = {}
    for policy_file, label in [
        ("scanner_1.yaml", "1 scanner"),
        ("scanner_4.yaml", "4 scanners"),
        ("scanner_8.yaml", "8 scanners"),
    ]:
        guardrails = _build_guardrails_with_policy(policy_file)
        print(f"  Running {label} ({policy_file})...")
        latencies, ttft = await _bench_streaming_latency(
            guardrails,
            request_dict,
            response_text,
            chunk_size=20,
            iterations=100,
            warmup=10,
        )
        lat_stats = _stats(latencies)
        ttft_stats = _stats(ttft)
        results[label] = {"latency_ms": lat_stats, "ttft_ms": ttft_stats}
        print(
            f"    Latency: P50={lat_stats['p50']:.2f}ms P95={lat_stats['p95']:.2f}ms Mean={lat_stats['mean']:.2f}ms"
        )
        print(
            f"    TTFT:    P50={ttft_stats['p50']:.2f}ms P95={ttft_stats['p95']:.2f}ms Mean={ttft_stats['mean']:.2f}ms"
        )

    return results


async def run_concurrency_scaling():
    """Vary concurrency: 1, 4, 8, 16."""
    from benchmarks.ragengine_guardrails.workloads import (
        build_standard_request,
        generate_safe_text,
    )

    print("\n=== CONCURRENCY SCALING (streaming S2, 512 tokens, chunk=20) ===")
    response_text = generate_safe_text(512)
    request_dict = build_standard_request(stream=True)
    guardrails = _build_guardrails_with_policy("redact_block.yaml")

    from ragengine.streaming.guardrails import apply_streaming_guardrails
    from ragengine.streaming.openai import (
        build_openai_chat_delta_sse_chunk,
        build_openai_chat_finish_reason_sse_chunk,
        build_sse_done_chunk,
    )

    sse_chunks = []
    for i in range(0, len(response_text), 20):
        sse_chunks.append(build_openai_chat_delta_sse_chunk(response_text[i : i + 20]))
    sse_chunks.append(build_openai_chat_finish_reason_sse_chunk(finish_reason="stop"))
    sse_chunks.append(build_sse_done_chunk())

    results = {}
    total_requests = 100  # Fixed total requests

    for concurrency in [1, 4, 8, 16]:
        print(f"  Running concurrency={concurrency}...")
        requests_per_worker = total_requests // concurrency

        async def single_worker():
            latencies = []
            for _ in range(requests_per_worker):

                async def upstream():
                    for c in sse_chunks:
                        yield c

                start = time.perf_counter()
                async for _ in apply_streaming_guardrails(
                    upstream(), guardrails, request_dict
                ):
                    pass
                latencies.append((time.perf_counter() - start) * 1000)
            return latencies

        wall_start = time.perf_counter()
        worker_results = await asyncio.gather(
            *[single_worker() for _ in range(concurrency)]
        )
        wall_elapsed = time.perf_counter() - wall_start

        all_latencies = [lat for worker in worker_results for lat in worker]
        throughput_rps = len(all_latencies) / wall_elapsed
        lat_stats = _stats(all_latencies)

        results[f"c{concurrency}"] = {
            "concurrency": concurrency,
            "latency_ms": lat_stats,
            "throughput_rps": round(throughput_rps, 2),
            "wall_time_s": round(wall_elapsed, 3),
            "total_requests": len(all_latencies),
        }
        print(f"    Latency P50={lat_stats['p50']:.2f}ms P95={lat_stats['p95']:.2f}ms")
        print(f"    Throughput={throughput_rps:.1f} rps, wall_time={wall_elapsed:.2f}s")

    return results


async def run_holdback_sensitivity():
    """Vary holdback via policy substring lengths: ~256, ~512, ~1024 chars."""
    from benchmarks.ragengine_guardrails.workloads import (
        build_standard_request,
        generate_safe_text,
    )

    print("\n=== HOLDBACK SENSITIVITY (streaming, 512 tokens, chunk=20) ===")
    response_text = generate_safe_text(512)
    request_dict = build_standard_request(stream=True)

    from ragengine.guardrails.output_guardrails import OutputGuardrails
    from ragengine.guardrails.scanner_schemas import (
        BanSubstringsConfig,
        ParsedScannerConfig,
    )
    from ragengine.streaming.guardrails import _get_streaming_guardrails_holdback_len

    results = {}
    # Holdback = max(256, longest_substring) for word match.
    # So we create substrings of length 256, 512, 1024 to force different holdbacks.
    for target_holdback in [256, 512, 1024]:
        # Create a banned word of exactly target_holdback length (won't match anything)
        long_word = "X" * target_holdback
        guardrails = OutputGuardrails(
            enabled=True,
            fail_open=False,
            action_on_hit="block",
            block_message="Blocked.",
            scanner_configs=(
                ParsedScannerConfig(
                    type="ban_substrings",
                    config=BanSubstringsConfig(
                        substrings=[long_word],
                        match_type="word",
                        case_sensitive=False,
                    ),
                    action_on_hit="block",
                ),
            ),
            policy_hash="bench",
            policy_path="bench",
        )
        actual_holdback = _get_streaming_guardrails_holdback_len(guardrails)
        print(f"  Running holdback={actual_holdback} (target={target_holdback})...")

        latencies, ttft = await _bench_streaming_latency(
            guardrails,
            request_dict,
            response_text,
            chunk_size=20,
            iterations=100,
            warmup=10,
        )
        lat_stats = _stats(latencies)
        ttft_stats = _stats(ttft)
        results[f"holdback_{actual_holdback}"] = {
            "holdback_len": actual_holdback,
            "latency_ms": lat_stats,
            "ttft_ms": ttft_stats,
        }
        print(f"    Latency: P50={lat_stats['p50']:.2f}ms P95={lat_stats['p95']:.2f}ms")
        print(
            f"    TTFT:    P50={ttft_stats['p50']:.2f}ms P95={ttft_stats['p95']:.2f}ms"
        )

    return results


async def main():
    all_results = {}

    all_results["per_scanner_cost"] = await run_per_scanner_cost()
    all_results["policy_complexity"] = await run_policy_complexity()
    all_results["concurrency"] = await run_concurrency_scaling()
    all_results["holdback"] = await run_holdback_sensitivity()

    # Save
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    output_path = RESULTS_DIR / "scaling_results.json"
    with open(output_path, "w") as f:
        json.dump(all_results, f, indent=2)
    print(f"\nScaling results saved to {output_path}")

    return all_results


if __name__ == "__main__":
    asyncio.run(main())
