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
"""Generate overhead-vs-tokens figure for the blog.

Plots guardrail overhead (ms) vs output tokens for three configurations:
  - Block only (1 scanner)
  - Redact + block (3 scanners)
  - All scanners (8 scanners, excluding secrets)

Computes the rate (μs/token) for each line.

Usage:
    PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.plot_overhead
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

TOKEN_COUNTS = [1024, 2048, 4096, 8192, 16384]
CHUNK_SIZE = 20
ITERATIONS = 100
WARMUP = 10

CONFIGS = [
    ("scanner_1.yaml", "Block only (1 scanner)"),
    ("redact_block.yaml", "Redact + block (3 scanners)"),
    ("scanner_8.yaml", "All scanners (8)"),
]


def _build_guardrails(policy_file):
    import ragengine.config as cfg
    from ragengine.guardrails.output_guardrails import OutputGuardrails

    policy_path = str(POLICIES_DIR / policy_file)
    cfg.OUTPUT_GUARDRAILS_ENABLED = True
    cfg.OUTPUT_GUARDRAILS_POLICY_PATH = policy_path
    os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "true"
    os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = policy_path
    return OutputGuardrails.from_config()


def _build_baseline_guardrails():
    import ragengine.config as cfg

    cfg.OUTPUT_GUARDRAILS_ENABLED = False
    os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "false"
    return None


async def _bench(guardrails, tokens, chunk_size, iterations, warmup):
    from ragengine.streaming.guardrails import apply_streaming_guardrails
    from ragengine.streaming.openai import (
        build_openai_chat_delta_sse_chunk,
        build_openai_chat_finish_reason_sse_chunk,
        build_sse_done_chunk,
    )

    response_text = "A" * (tokens * 4)
    request_dict = {
        "model": "test",
        "messages": [{"role": "user", "content": "test"}],
        "stream": True,
    }

    sse_chunks = []
    for i in range(0, len(response_text), chunk_size):
        sse_chunks.append(
            build_openai_chat_delta_sse_chunk(response_text[i : i + chunk_size])
        )
    sse_chunks.append(build_openai_chat_finish_reason_sse_chunk(finish_reason="stop"))
    sse_chunks.append(build_sse_done_chunk())

    latencies = []
    for iteration in range(warmup + iterations):

        async def upstream():
            for c in sse_chunks:
                yield c

        if guardrails and guardrails.enabled:
            start = time.perf_counter()
            async for _ in apply_streaming_guardrails(
                upstream(), guardrails, request_dict
            ):
                pass
            elapsed = (time.perf_counter() - start) * 1000
        else:
            start = time.perf_counter()
            async for _ in upstream():
                pass
            elapsed = (time.perf_counter() - start) * 1000

        if iteration >= warmup:
            latencies.append(elapsed)

    s = sorted(latencies)
    n = len(s)
    return {
        "p50": round(s[int(n * 0.50)], 3),
        "p99": round(s[int(n * 0.99)] if n >= 100 else s[-1], 3),
        "mean": round(sum(s) / n, 3),
    }


async def main():
    print("=== Overhead vs Tokens Benchmark ===")
    print(
        f"Tokens: {TOKEN_COUNTS}, chunk_size={CHUNK_SIZE}, N={ITERATIONS}, warmup={WARMUP}\n"
    )

    # Run baseline
    print("Running baseline (no guardrails)...")
    baseline = {}
    for tokens in TOKEN_COUNTS:
        result = await _bench(None, tokens, CHUNK_SIZE, ITERATIONS, WARMUP)
        baseline[tokens] = result
        print(f"  {tokens} tokens: P50={result['p50']:.3f}ms")

    # Run each config
    all_data = {}
    for policy_file, label in CONFIGS:
        print(f"\nRunning {label} ({policy_file})...")
        guardrails = _build_guardrails(policy_file)
        data = {}
        for tokens in TOKEN_COUNTS:
            result = await _bench(guardrails, tokens, CHUNK_SIZE, ITERATIONS, WARMUP)
            overhead_p50 = round(result["p50"] - baseline[tokens]["p50"], 3)
            overhead_p99 = round(result["p99"] - baseline[tokens]["p99"], 3)
            data[tokens] = {
                "raw": result,
                "overhead_p50": overhead_p50,
                "overhead_p99": overhead_p99,
            }
            print(
                f"  {tokens} tokens: P50={result['p50']:.3f}ms (overhead +{overhead_p50:.3f}ms)"
            )
        all_data[label] = data

    # Compute rates (μs/token) using linear regression
    print("\n=== Rates (μs per output token) ===\n")
    rates = {}
    for label, data in all_data.items():
        tokens_list = sorted(data.keys())
        overheads = [data[t]["overhead_p50"] for t in tokens_list]
        n = len(tokens_list)
        sum_x = sum(tokens_list)
        sum_y = sum(overheads)
        sum_xy = sum(t * o for t, o in zip(tokens_list, overheads))
        sum_xx = sum(t * t for t in tokens_list)
        slope = (n * sum_xy - sum_x * sum_y) / (n * sum_xx - sum_x * sum_x)
        rate_us = round(slope * 1000, 2)  # ms/token -> μs/token
        rates[label] = rate_us
        print(f"  {label}: {rate_us} μs/token")

    # Save results
    output = {
        "config": {
            "token_counts": TOKEN_COUNTS,
            "chunk_size": CHUNK_SIZE,
            "iterations": ITERATIONS,
            "warmup": WARMUP,
        },
        "baseline": {str(k): v for k, v in baseline.items()},
        "data": {
            label: {str(k): v for k, v in data.items()}
            for label, data in all_data.items()
        },
        "rates_us_per_token": rates,
    }
    output_path = RESULTS_DIR / "overhead_vs_tokens.json"
    with open(output_path, "w") as f:
        json.dump(output, f, indent=2)
    print(f"\nResults saved to {output_path}")

    # Generate ASCII table for blog
    print("\n=== Blog Table ===\n")
    print("| Output tokens | ", end="")
    for label, _ in CONFIGS:
        print(f"{label} | ", end="")
    print()
    print(f"|{'-' * 16}|", end="")
    for _ in CONFIGS:
        print(f"{'-' * 30}|", end="")
    print()
    for tokens in TOKEN_COUNTS:
        print(f"| {tokens:<14} | ", end="")
        for label, _ in CONFIGS:
            v = all_data.get(label, {}).get(tokens, {}).get("overhead_p50", 0)
            print(f"{v:>26.2f} ms | ", end="")
        print()
    print()
    print("Rate (μs/token):")
    for label, rate in rates.items():
        print(f"  {label}: {rate} μs/token")


if __name__ == "__main__":
    asyncio.run(main())
