#!/usr/bin/env python3
"""End-to-end guardrails benchmark with real models.

Four modes:
  record  — Send prompts to a real model, record SSE streaming traces
  replay  — Replay recorded traces through guardrails (no GPU needed)
  live    — Full live e2e: client → guardrails → real model → response
  compare — Compare results across experiment groups (raw / azure-cs / kaito)

Usage with AI Foundry endpoints:

    # Step 1: Raw baseline (content filtering OFF, no guardrails)
    python -m benchmarks.ragengine_guardrails.bench_e2e live \
        --model-url https://<endpoint>.models.ai.azure.com/v1/chat/completions \
        --api-key <key> --model-name phi-4 --label raw \
        --prompts datasets/benchmark_prompts.jsonl

    # Step 2: Azure Content Safety (content filtering ON, no guardrails)
    python -m benchmarks.ragengine_guardrails.bench_e2e live \
        --model-url https://<endpoint-with-filter>.models.ai.azure.com/v1/chat/completions \
        --api-key <key> --model-name phi-4 --label azure-cs \
        --prompts datasets/benchmark_prompts.jsonl

    # Step 3: Kaito Guardrails (content filtering OFF, guardrails ON)
    PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_e2e live \
        --model-url https://<endpoint>.models.ai.azure.com/v1/chat/completions \
        --api-key <key> --model-name phi-4 --label kaito-guardrails \
        --prompts datasets/benchmark_prompts.jsonl --policy redact_block.yaml

    # Step 4: Compare all three groups
    python -m benchmarks.ragengine_guardrails.bench_e2e compare

Recording traces (for offline replay):
    python -m benchmarks.ragengine_guardrails.bench_e2e record \
        --model-url https://<endpoint>.models.ai.azure.com/v1/chat/completions \
        --api-key <key> --model-name phi-4 \
        --prompts datasets/benchmark_prompts.jsonl --output traces/phi-4/

    PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_e2e replay \
        --traces traces/phi-4/ --policy redact_block.yaml
"""

import argparse
import asyncio
import json
import os
import statistics
import sys
import time
from pathlib import Path

import httpx

BENCH_DIR = Path(__file__).resolve().parent
POLICIES_DIR = BENCH_DIR / "policies"
RESULTS_DIR = BENCH_DIR / "results"
DATASETS_DIR = BENCH_DIR / "datasets"

sys.path.insert(0, str(BENCH_DIR.parent.parent / "presets"))


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _stats(values: list[float]) -> dict:
    if not values:
        return {"p50": 0, "p95": 0, "p99": 0, "mean": 0}
    s = sorted(values)
    n = len(s)
    return {
        "p50": round(s[int(n * 0.50)], 3),
        "p95": round(s[int(n * 0.95)] if n >= 20 else s[-1], 3),
        "p99": round(s[int(n * 0.99)] if n >= 100 else s[-1], 3),
        "mean": round(statistics.mean(s), 3),
    }


def _build_guardrails(policy_file: str | None):
    if not policy_file:
        return None
    import ragengine.config as cfg
    from ragengine.guardrails.output_guardrails import OutputGuardrails

    policy_path = str(POLICIES_DIR / policy_file)
    cfg.OUTPUT_GUARDRAILS_ENABLED = True
    cfg.OUTPUT_GUARDRAILS_POLICY_PATH = policy_path
    os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "true"
    os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = policy_path
    return OutputGuardrails.from_config()


def load_prompts(path: str) -> list[dict]:
    prompts = []
    p = Path(path) if Path(path).is_absolute() else BENCH_DIR / path
    with open(p) as f:
        for line in f:
            line = line.strip()
            if line:
                prompts.append(json.loads(line))
    return prompts


def _build_chat_body(
    prompt_text: str, model: str = "default", stream: bool = True
) -> dict:
    return {
        "model": model,
        "messages": [
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": prompt_text},
        ],
        "stream": stream,
        "max_tokens": 512,
    }


def _build_client(api_key: str | None = None) -> httpx.AsyncClient:
    headers = {}
    if api_key:
        headers["api-key"] = api_key
    return httpx.AsyncClient(headers=headers)


# ---------------------------------------------------------------------------
# Mode 1: RECORD — send prompts to real model, save SSE traces
# ---------------------------------------------------------------------------


async def record_single(
    client: httpx.AsyncClient,
    model_url: str,
    prompt_entry: dict,
    idx: int,
    model: str = "default",
) -> dict:
    """Send one prompt and record the streaming trace."""
    prompt_text = prompt_entry["prompt"]
    body = _build_chat_body(prompt_text, model=model, stream=True)

    events = []
    full_response_parts = []

    start = time.perf_counter()
    first_content_time = None

    async with client.stream("POST", model_url, json=body, timeout=120.0) as resp:
        async for raw_chunk in resp.aiter_text():
            now = time.perf_counter()
            elapsed_ms = (now - start) * 1000

            for line in raw_chunk.strip().split("\n"):
                line = line.strip()
                if not line or not line.startswith("data: "):
                    continue
                data_str = line[len("data: ") :]
                if data_str == "[DONE]":
                    events.append(
                        {"timestamp_ms": round(elapsed_ms, 3), "type": "done"}
                    )
                    continue

                try:
                    data = json.loads(data_str)
                except json.JSONDecodeError:
                    continue

                choices = data.get("choices", [])
                if not choices:
                    continue

                delta = choices[0].get("delta", {})
                content = delta.get("content")
                finish_reason = choices[0].get("finish_reason")

                if content:
                    if first_content_time is None:
                        first_content_time = now
                    events.append(
                        {
                            "timestamp_ms": round(elapsed_ms, 3),
                            "content": content,
                        }
                    )
                    full_response_parts.append(content)

                if finish_reason:
                    events.append(
                        {
                            "timestamp_ms": round(elapsed_ms, 3),
                            "type": "finish",
                            "finish_reason": finish_reason,
                        }
                    )

    total_ms = (time.perf_counter() - start) * 1000
    ttft_ms = (first_content_time - start) * 1000 if first_content_time else None
    full_response = "".join(full_response_parts)

    trace = {
        "index": idx,
        "prompt": prompt_text,
        "prompt_category": prompt_entry.get(
            "bench_category", prompt_entry.get("category", "unknown")
        ),
        "source": prompt_entry.get("source", "unknown"),
        "events": events,
        "full_response": full_response,
        "response_chars": len(full_response),
        "num_chunks": sum(1 for e in events if "content" in e),
        "ttft_ms": round(ttft_ms, 3) if ttft_ms else None,
        "total_ms": round(total_ms, 3),
    }
    return trace


async def cmd_record(args):
    """Record streaming traces from a real model."""
    prompts = load_prompts(args.prompts)
    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    print("=== RECORDING TRACES ===")
    print(f"  Model URL: {args.model_url}")
    print(f"  Prompts: {len(prompts)} from {args.prompts}")
    print(f"  Output: {output_dir}/\n")

    async with _build_client(getattr(args, "api_key", None)) as client:
        for i, prompt_entry in enumerate(prompts):
            cat = prompt_entry.get("bench_category", prompt_entry.get("category", "?"))
            print(
                f"  [{i + 1}/{len(prompts)}] ({cat}) {prompt_entry['prompt'][:60]}...",
                end="",
                flush=True,
            )
            try:
                trace = await record_single(
                    client,
                    args.model_url,
                    prompt_entry,
                    i,
                    model=args.model_name or "default",
                )
                trace["model_name"] = getattr(args, "model_name", None)
                trace_file = output_dir / f"trace_{i:04d}.json"
                with open(trace_file, "w") as f:
                    json.dump(trace, f, indent=2, ensure_ascii=False)
                print(
                    f"  {trace['num_chunks']} chunks, "
                    f"TTFT={trace['ttft_ms']}ms, "
                    f"total={trace['total_ms']:.0f}ms"
                )
            except Exception as e:
                print(f"  ERROR: {e}")

    print(f"\nDone. {len(prompts)} traces saved to {output_dir}/")


# ---------------------------------------------------------------------------
# Mode 2: REPLAY — feed recorded traces through guardrails (no GPU)
# ---------------------------------------------------------------------------


async def replay_single(trace: dict, guardrails) -> dict:
    """Replay a recorded trace through guardrails and measure overhead."""
    from ragengine.streaming.guardrails import apply_streaming_guardrails
    from ragengine.streaming.openai import (
        build_openai_chat_delta_sse_chunk,
        build_openai_chat_finish_reason_sse_chunk,
        build_sse_done_chunk,
    )

    content_events = [e for e in trace["events"] if "content" in e]
    sse_chunks = []
    for event in content_events:
        sse_chunks.append(build_openai_chat_delta_sse_chunk(event["content"]))
    finish_events = [e for e in trace["events"] if e.get("type") == "finish"]
    finish_reason = finish_events[0]["finish_reason"] if finish_events else "stop"
    sse_chunks.append(
        build_openai_chat_finish_reason_sse_chunk(finish_reason=finish_reason)
    )
    sse_chunks.append(build_sse_done_chunk())

    request_dict = _build_chat_body(trace["prompt"], stream=True)

    async def upstream():
        for c in sse_chunks:
            yield c

    start = time.perf_counter()
    first_content = None
    output_parts = []
    was_blocked = False

    if guardrails and guardrails.enabled:
        async for emitted in apply_streaming_guardrails(
            upstream(), guardrails, request_dict
        ):
            if first_content is None and "content" in emitted:
                first_content = time.perf_counter()
            if isinstance(emitted, str) and "blocked" in emitted.lower():
                was_blocked = True
            if isinstance(emitted, str):
                output_parts.append(emitted)
    else:
        async for chunk in upstream():
            if first_content is None:
                first_content = time.perf_counter()

    elapsed_ms = (time.perf_counter() - start) * 1000
    ttft_ms = (first_content - start) * 1000 if first_content else None

    return {
        "prompt_category": trace["prompt_category"],
        "response_chars": trace["response_chars"],
        "num_chunks": trace["num_chunks"],
        "original_total_ms": trace["total_ms"],
        "original_ttft_ms": trace["ttft_ms"],
        "guardrails_latency_ms": round(elapsed_ms, 3),
        "guardrails_ttft_ms": round(ttft_ms, 3) if ttft_ms else None,
        "was_blocked": was_blocked,
    }


async def cmd_replay(args):
    """Replay recorded traces through guardrails."""
    traces_dir = Path(args.traces)
    trace_files = sorted(traces_dir.glob("trace_*.json"))

    if not trace_files:
        print(f"ERROR: No trace files found in {traces_dir}")
        sys.exit(1)

    policies = [
        (None, "no_guardrails"),
        ("block_only.yaml", "block_only"),
        ("redact_block.yaml", "redact_block"),
    ]
    if args.policy:
        policies = [(args.policy, Path(args.policy).stem)]

    print("=== REPLAY BENCHMARK ===")
    print(f"  Traces: {len(trace_files)} from {traces_dir}")
    print(f"  Policies: {[p[1] for p in policies]}\n")

    all_results = {
        "metadata": {"traces_dir": str(traces_dir), "num_traces": len(trace_files)}
    }

    for policy_file, policy_label in policies:
        guardrails = _build_guardrails(policy_file)
        label = policy_label
        print(f"  --- Policy: {label} ---")

        latencies = []
        ttft_delays = []
        blocked_count = 0
        category_latencies: dict[str, list[float]] = {}

        for tf in trace_files:
            with open(tf) as f:
                trace = json.load(f)

            result = await replay_single(trace, guardrails)
            latencies.append(result["guardrails_latency_ms"])

            cat = result["prompt_category"]
            category_latencies.setdefault(cat, []).append(
                result["guardrails_latency_ms"]
            )

            if result["guardrails_ttft_ms"] and result["original_ttft_ms"]:
                ttft_delays.append(result["guardrails_ttft_ms"])

            if result["was_blocked"]:
                blocked_count += 1

        lat_stats = _stats(latencies)
        ttft_stats = _stats(ttft_delays)

        print(
            f"    Latency: P50={lat_stats['p50']:.2f}ms "
            f"P95={lat_stats['p95']:.2f}ms Mean={lat_stats['mean']:.2f}ms"
        )
        print(
            f"    TTFT:    P50={ttft_stats['p50']:.2f}ms "
            f"P95={ttft_stats['p95']:.2f}ms Mean={ttft_stats['mean']:.2f}ms"
        )
        print(f"    Blocked: {blocked_count}/{len(trace_files)}")

        per_category = {}
        for cat, lats in sorted(category_latencies.items()):
            cat_stats = _stats(lats)
            per_category[cat] = {"latency_ms": cat_stats, "count": len(lats)}
            print(f"    [{cat}] P50={cat_stats['p50']:.2f}ms (n={len(lats)})")

        all_results[label] = {
            "policy": policy_file or "none",
            "latency_ms": lat_stats,
            "ttft_ms": ttft_stats,
            "blocked_count": blocked_count,
            "total_traces": len(trace_files),
            "per_category": per_category,
        }

    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    output_path = RESULTS_DIR / "e2e_replay_results.json"
    with open(output_path, "w") as f:
        json.dump(all_results, f, indent=2)
    print(f"\nResults saved to {output_path}")


# ---------------------------------------------------------------------------
# Mode 3: LIVE — full e2e through real model with guardrails
# ---------------------------------------------------------------------------


async def live_single(
    client: httpx.AsyncClient,
    model_url: str,
    prompt_text: str,
    guardrails,
    model: str = "default",
) -> dict:
    """Send one prompt through guardrails proxy to real model."""
    from ragengine.streaming.guardrails import apply_streaming_guardrails

    body = _build_chat_body(prompt_text, model=model, stream=True)

    start = time.perf_counter()
    first_content = None
    was_blocked = False
    chunk_count = 0

    async with client.stream("POST", model_url, json=body, timeout=120.0) as resp:
        raw_chunks = []
        async for raw in resp.aiter_text():
            raw_chunks.append(raw)

    full_raw = "".join(raw_chunks)
    if "content_filter" in full_raw:
        was_blocked = True
        block_reason = "content_filter"
    elif resp.status_code != 200:
        was_blocked = True
        block_reason = f"http_{resp.status_code}"
    else:
        block_reason = None

    async def upstream():
        for c in raw_chunks:
            yield c

    guarded_parts = []
    if guardrails and guardrails.enabled:
        async for emitted in apply_streaming_guardrails(upstream(), guardrails, body):
            now = time.perf_counter()
            if isinstance(emitted, str):
                guarded_parts.append(emitted)
                if first_content is None and "content" in emitted:
                    first_content = now
                if "guardrail" in emitted.lower() or "blocked" in emitted.lower():
                    was_blocked = True
                    block_reason = "guardrails"
            chunk_count += 1
    else:
        async for chunk in upstream():
            now = time.perf_counter()
            if first_content is None:
                first_content = now
            chunk_count += 1

    guarded_text = "".join(guarded_parts)
    redact_count = guarded_text.count("[REDACTED]")

    total_ms = (time.perf_counter() - start) * 1000
    ttft_ms = (first_content - start) * 1000 if first_content else None

    return {
        "total_ms": round(total_ms, 3),
        "ttft_ms": round(ttft_ms, 3) if ttft_ms else None,
        "chunk_count": chunk_count,
        "was_blocked": was_blocked,
        "block_reason": block_reason,
        "redact_count": redact_count,
    }


async def cmd_live(args):
    """Run live e2e benchmark against a real model."""
    prompts = load_prompts(args.prompts)
    iterations = args.iterations

    policies = [
        (None, "baseline"),
        ("block_only.yaml", "block_only"),
        ("redact_block.yaml", "redact_block"),
    ]
    if args.policy:
        policies = [
            (None, "baseline"),
            (args.policy, Path(args.policy).stem),
        ]

    print("=== LIVE E2E BENCHMARK ===")
    print(f"  Model URL: {args.model_url}")
    print(f"  Prompts: {len(prompts)} × {iterations} iterations")
    print(f"  Policies: {[p[1] for p in policies]}\n")

    label = getattr(args, "label", None)
    model_name = getattr(args, "model_name", None)

    all_results = {
        "metadata": {
            "model_url": args.model_url,
            "model_name": model_name,
            "label": label,
            "num_prompts": len(prompts),
            "iterations": iterations,
        }
    }

    for policy_file, policy_label in policies:
        guardrails = _build_guardrails(policy_file)
        print(f"  --- Policy: {policy_label} ---")

        latencies = []
        ttft_list = []
        blocked_count = 0
        block_reasons = {}
        total_redactions = 0

        async with _build_client(getattr(args, "api_key", None)) as client:
            for iteration in range(iterations):
                for pi, prompt_entry in enumerate(prompts):
                    for attempt in range(3):
                        try:
                            result = await live_single(
                                client,
                                args.model_url,
                                prompt_entry["prompt"],
                                guardrails,
                                model=args.model_name or "default",
                            )
                            latencies.append(result["total_ms"])
                            if result["ttft_ms"]:
                                ttft_list.append(result["ttft_ms"])
                            if result["was_blocked"]:
                                blocked_count += 1
                                reason = result.get("block_reason", "unknown")
                                block_reasons[reason] = block_reasons.get(reason, 0) + 1
                            total_redactions += result.get("redact_count", 0)
                            break
                        except Exception as e:
                            if attempt < 2:
                                print(f"    [retry {attempt + 1}/2] prompt {pi}: {e}")
                                await asyncio.sleep(2)
                            else:
                                print(f"    [SKIP] prompt {pi}: {e}")
                                break

                print(
                    f"    Iteration {iteration + 1}/{iterations} done "
                    f"({len(prompts)} prompts)"
                )

        lat_stats = _stats(latencies)
        ttft_stats = _stats(ttft_list)

        print(f"    Latency: P50={lat_stats['p50']:.1f}ms P95={lat_stats['p95']:.1f}ms")
        print(
            f"    TTFT:    P50={ttft_stats['p50']:.1f}ms P95={ttft_stats['p95']:.1f}ms"
        )
        print(
            f"    Blocked: {blocked_count}/{len(latencies)} "
            f"({block_reasons if block_reasons else 'none'})"
        )
        print(f"    Redactions: {total_redactions}")

        all_results[policy_label] = {
            "latency_ms": lat_stats,
            "ttft_ms": ttft_stats,
            "total_requests": len(latencies),
            "blocked": blocked_count,
            "block_reasons": block_reasons,
            "redactions": total_redactions,
        }

    if "baseline" in all_results:
        baseline = all_results["baseline"]
        for policy_file, policy_label in policies:
            if policy_label == "baseline":
                continue
            guarded = all_results.get(policy_label)
            if not guarded:
                continue
            overhead = {
                "abs_p50_ms": round(
                    guarded["latency_ms"]["p50"] - baseline["latency_ms"]["p50"], 3
                ),
                "abs_p95_ms": round(
                    guarded["latency_ms"]["p95"] - baseline["latency_ms"]["p95"], 3
                ),
                "rel_p50_pct": round(
                    (guarded["latency_ms"]["p50"] - baseline["latency_ms"]["p50"])
                    / max(baseline["latency_ms"]["p50"], 0.001)
                    * 100,
                    2,
                ),
                "ttft_abs_p50_ms": round(
                    guarded["ttft_ms"]["p50"] - baseline["ttft_ms"]["p50"], 3
                )
                if guarded["ttft_ms"]["p50"] and baseline["ttft_ms"]["p50"]
                else None,
            }
            all_results[f"overhead_{policy_label}"] = overhead
            print(
                f"\n  Overhead ({policy_label} vs baseline): "
                f"P50 +{overhead['abs_p50_ms']:.1f}ms "
                f"({overhead['rel_p50_pct']:+.1f}%)"
            )

    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    suffix = f"_{label}" if label else ""
    output_path = RESULTS_DIR / f"e2e_live_results{suffix}.json"
    with open(output_path, "w") as f:
        json.dump(all_results, f, indent=2)
    print(f"\nResults saved to {output_path}")


# ---------------------------------------------------------------------------
# Mode 4: COMPARE — three-way comparison across experiment groups
# ---------------------------------------------------------------------------


def cmd_compare(args):
    """Compare results across experiment groups (raw / azure-cs / kaito-guardrails)."""
    result_files = sorted(RESULTS_DIR.glob("e2e_live_results_*.json"))
    if args.files:
        result_files = [Path(f) for f in args.files]

    if not result_files:
        print("ERROR: No e2e_live_results_*.json files found in results/")
        print("Run 'live' with --label for each experiment group first.")
        sys.exit(1)

    runs = {}
    for rf in result_files:
        with open(rf) as f:
            data = json.load(f)
        label = data.get("metadata", {}).get("label") or rf.stem.replace(
            "e2e_live_results_", ""
        )
        runs[label] = data

    print(f"=== COMPARISON: {', '.join(runs.keys())} ===\n")

    rows = []
    for label, data in runs.items():
        model = data.get("metadata", {}).get("model_name", "?")
        baseline = data.get("baseline", {})
        bl_p50 = baseline.get("latency_ms", {}).get("p50", 0)
        bl_ttft = baseline.get("ttft_ms", {}).get("p50", 0)

        guarded_keys = [
            k
            for k in data
            if k not in ("metadata", "baseline") and not k.startswith("overhead_")
        ]
        for gk in guarded_keys:
            gd = data[gk]
            gd_p50 = gd.get("latency_ms", {}).get("p50", 0)
            gd_ttft = gd.get("ttft_ms", {}).get("p50", 0)
            overhead_ms = round(gd_p50 - bl_p50, 2) if bl_p50 else None
            overhead_pct = (
                round((gd_p50 - bl_p50) / max(bl_p50, 0.001) * 100, 2)
                if bl_p50
                else None
            )
            ttft_overhead = round(gd_ttft - bl_ttft, 2) if bl_ttft and gd_ttft else None
            rows.append(
                {
                    "label": label,
                    "model": model,
                    "policy": gk,
                    "baseline_p50_ms": bl_p50,
                    "baseline_ttft_ms": bl_ttft,
                    "guarded_p50_ms": gd_p50,
                    "guarded_ttft_ms": gd_ttft,
                    "overhead_ms": overhead_ms,
                    "overhead_pct": overhead_pct,
                    "ttft_overhead_ms": ttft_overhead,
                }
            )

        if not guarded_keys:
            rows.append(
                {
                    "label": label,
                    "model": model,
                    "policy": "(baseline only)",
                    "baseline_p50_ms": bl_p50,
                    "baseline_ttft_ms": bl_ttft,
                    "guarded_p50_ms": None,
                    "guarded_ttft_ms": None,
                    "overhead_ms": None,
                    "overhead_pct": None,
                    "ttft_overhead_ms": None,
                }
            )

    hdr = (
        f"{'Label':<22} {'Model':<18} {'Policy':<16} "
        f"{'Base P50':>10} {'Guard P50':>10} {'Overhead':>10} {'Ovh %':>8} "
        f"{'TTFT Ovh':>10}"
    )
    print(hdr)
    print("-" * len(hdr))
    for r in rows:
        gp50 = f"{r['guarded_p50_ms']:.1f}" if r["guarded_p50_ms"] is not None else "-"
        ovh = f"+{r['overhead_ms']:.1f}" if r["overhead_ms"] is not None else "-"
        opct = f"{r['overhead_pct']:+.1f}%" if r["overhead_pct"] is not None else "-"
        tovh = (
            f"+{r['ttft_overhead_ms']:.1f}"
            if r["ttft_overhead_ms"] is not None
            else "-"
        )
        print(
            f"{r['label']:<22} {r['model']:<18} {r['policy']:<16} "
            f"{r['baseline_p50_ms']:>10.1f} {gp50:>10} {ovh:>10} {opct:>8} "
            f"{tovh:>10}"
        )

    # Cross-group comparison
    raw_run = runs.get("raw")
    azure_run = runs.get("azure-cs")
    kaito_run = runs.get("kaito-guardrails")

    if raw_run and (azure_run or kaito_run):
        print(f"\n{'=' * 60}")
        print("CROSS-GROUP COMPARISON (vs raw baseline)")
        print(f"{'=' * 60}")
        raw_bl = raw_run.get("baseline", {}).get("latency_ms", {}).get("p50", 0)
        raw_ttft = raw_run.get("baseline", {}).get("ttft_ms", {}).get("p50", 0)

        if azure_run:
            az_bl = azure_run.get("baseline", {}).get("latency_ms", {}).get("p50", 0)
            az_ttft = azure_run.get("baseline", {}).get("ttft_ms", {}).get("p50", 0)
            az_ovh = round(az_bl - raw_bl, 2) if raw_bl else None
            az_pct = (
                round((az_bl - raw_bl) / max(raw_bl, 0.001) * 100, 2)
                if raw_bl
                else None
            )
            az_ttft_ovh = round(az_ttft - raw_ttft, 2) if raw_ttft and az_ttft else None
            print("\n  Azure Content Safety overhead:")
            if az_ovh is not None:
                print(f"    E2E:  +{az_ovh:.1f}ms ({az_pct:+.1f}%)")
            if az_ttft_ovh is not None:
                print(f"    TTFT: +{az_ttft_ovh:.1f}ms")

        if kaito_run:
            for k in kaito_run:
                if k in ("metadata", "baseline") or k.startswith("overhead_"):
                    continue
                kt = kaito_run[k]
                kt_p50 = kt.get("latency_ms", {}).get("p50", 0)
                kt_ttft = kt.get("ttft_ms", {}).get("p50", 0)
                kt_ovh = round(kt_p50 - raw_bl, 2) if raw_bl else None
                kt_pct = (
                    round((kt_p50 - raw_bl) / max(raw_bl, 0.001) * 100, 2)
                    if raw_bl
                    else None
                )
                kt_ttft_ovh = (
                    round(kt_ttft - raw_ttft, 2) if raw_ttft and kt_ttft else None
                )
                print(f"\n  Kaito Guardrails ({k}) overhead:")
                if kt_ovh is not None:
                    print(f"    E2E:  +{kt_ovh:.1f}ms ({kt_pct:+.1f}%)")
                if kt_ttft_ovh is not None:
                    print(f"    TTFT: +{kt_ttft_ovh:.1f}ms")

    comparison = {
        "runs": {k: v.get("metadata", {}) for k, v in runs.items()},
        "rows": rows,
    }
    output_path = RESULTS_DIR / "e2e_comparison.json"
    with open(output_path, "w") as f:
        json.dump(comparison, f, indent=2)
    print(f"\nComparison saved to {output_path}")


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(
        description="End-to-end guardrails benchmark with real models"
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    # record
    rec = subparsers.add_parser("record", help="Record streaming traces")
    rec.add_argument(
        "--model-url",
        required=True,
        help="Model endpoint URL (e.g. http://localhost:8080/v1/chat/completions)",
    )
    rec.add_argument("--prompts", required=True, help="Path to prompts JSONL file")
    rec.add_argument("--output", required=True, help="Output directory for trace files")
    rec.add_argument(
        "--api-key",
        dest="api_key",
        default=None,
        help="API key for AI Foundry endpoints (sent as api-key header)",
    )
    rec.add_argument(
        "--model-name",
        dest="model_name",
        default=None,
        help="Model name for labeling (e.g. phi-4, llama-3.3-70b)",
    )

    # replay
    rep = subparsers.add_parser("replay", help="Replay traces through guardrails")
    rep.add_argument(
        "--traces", required=True, help="Directory containing trace_*.json files"
    )
    rep.add_argument(
        "--policy", default=None, help="Policy YAML file (default: test all policies)"
    )

    # live
    liv = subparsers.add_parser("live", help="Live e2e benchmark")
    liv.add_argument("--model-url", required=True, help="Model endpoint URL")
    liv.add_argument("--prompts", required=True, help="Path to prompts JSONL file")
    liv.add_argument(
        "--policy",
        default=None,
        help="Policy YAML file (default: test baseline + all policies)",
    )
    liv.add_argument(
        "--iterations",
        type=int,
        default=1,
        help="Number of iterations per prompt (default: 1)",
    )
    liv.add_argument(
        "--api-key",
        dest="api_key",
        default=None,
        help="API key for AI Foundry endpoints (sent as api-key header)",
    )
    liv.add_argument(
        "--model-name",
        dest="model_name",
        default=None,
        help="Model name for labeling (e.g. phi-4, llama-3.3-70b)",
    )
    liv.add_argument(
        "--label",
        default=None,
        help="Label for this run (e.g. raw, azure-cs, kaito-guardrails). "
        "Used in result filename: e2e_live_results_{label}.json",
    )

    # compare
    cmp = subparsers.add_parser(
        "compare", help="Compare results across experiment groups"
    )
    cmp.add_argument(
        "--files",
        nargs="*",
        default=None,
        help="Result JSON files to compare (default: all e2e_live_results_*.json)",
    )

    args = parser.parse_args()

    if args.command == "record":
        asyncio.run(cmd_record(args))
    elif args.command == "replay":
        asyncio.run(cmd_replay(args))
    elif args.command == "live":
        asyncio.run(cmd_live(args))
    elif args.command == "compare":
        cmd_compare(args)


if __name__ == "__main__":
    main()
