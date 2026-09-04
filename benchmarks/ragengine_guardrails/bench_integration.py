#!/usr/bin/env python3
"""Integration-level benchmark: full HTTP round-trip through guardrails proxy.

Starts mock OpenAI upstream + a thin guardrails proxy server locally,
then benchmarks the HTTP path: client → proxy (guardrails) → mock upstream.

This validates that unit-level overhead numbers hold up under real HTTP/SSE.

Usage:
    PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_integration
"""

import asyncio
import json
import logging
import os
import subprocess
import sys
import time
from pathlib import Path

import httpx
import uvicorn
from fastapi import FastAPI, Request
from starlette.responses import JSONResponse, StreamingResponse

BENCH_DIR = Path(__file__).resolve().parent
POLICIES_DIR = BENCH_DIR / "policies"
RESULTS_DIR = BENCH_DIR / "results"

sys.path.insert(0, str(BENCH_DIR.parent.parent / "presets"))

logger = logging.getLogger("bench_integration")

MOCK_PORT = 9100
PROXY_PORT = 9200

# ---------------------------------------------------------------------------
# Guardrails proxy server
# ---------------------------------------------------------------------------

_proxy_guardrails = None  # Set before starting proxy
_proxy_stream_mode = False

proxy_app = FastAPI()


@proxy_app.post("/v1/chat/completions")
async def proxy_chat(request: Request):
    body = await request.json()
    stream = body.get("stream", False)
    mock_url = f"http://127.0.0.1:{MOCK_PORT}/v1/chat/completions"

    if stream:
        # Collect upstream chunks first to avoid context manager lifetime issues
        upstream_chunks = await _collect_upstream_stream(mock_url, body)

        async def iter_chunks():
            for chunk in upstream_chunks:
                yield chunk

        if _proxy_guardrails and _proxy_guardrails.enabled:
            from ragengine.streaming.guardrails import (
                apply_streaming_guardrails,
                raise_if_streaming_guardrails_unsupported,
            )

            raise_if_streaming_guardrails_unsupported(_proxy_guardrails)
            guarded = apply_streaming_guardrails(iter_chunks(), _proxy_guardrails, body)
            return StreamingResponse(
                guarded,
                media_type="text/event-stream",
                headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
            )
        return StreamingResponse(
            iter_chunks(),
            media_type="text/event-stream",
            headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
        )
    else:
        async with httpx.AsyncClient(timeout=30.0) as client:
            resp = await client.post(mock_url, json=body)
            resp_data = resp.json()
            if _proxy_guardrails and _proxy_guardrails.enabled:
                from ragengine.models import ChatCompletionResponse

                chat_resp = ChatCompletionResponse(**resp_data)
                chat_resp = _proxy_guardrails.guard_response(chat_resp, body)
                return JSONResponse(chat_resp.model_dump(mode="python"))
            return JSONResponse(resp_data)


async def _collect_upstream_stream(url, body):
    """Collect all upstream SSE chunks into a list."""
    chunks = []
    async with (
        httpx.AsyncClient(timeout=30.0) as client,
        client.stream("POST", url, json=body) as resp,
    ):
        async for chunk in resp.aiter_text():
            chunks.append(chunk)
    return chunks


# ---------------------------------------------------------------------------
# Server lifecycle
# ---------------------------------------------------------------------------


def _start_mock_server():
    """Start mock OpenAI server as subprocess."""
    proc = subprocess.Popen(
        [
            sys.executable,
            "-c",
            f"import uvicorn; from benchmarks.ragengine_guardrails.mock_server import app; "
            f"uvicorn.run(app, host='127.0.0.1', port={MOCK_PORT}, log_level='warning')",
        ],
        cwd=str(BENCH_DIR.parent.parent),
    )
    return proc


def _start_proxy_server():
    """Start guardrails proxy as subprocess."""
    proc = subprocess.Popen(
        [
            sys.executable,
            "-c",
            f"import uvicorn; from benchmarks.ragengine_guardrails.bench_integration import proxy_app; "
            f"uvicorn.run(proxy_app, host='127.0.0.1', port={PROXY_PORT}, log_level='warning')",
        ],
        cwd=str(BENCH_DIR.parent.parent),
        env={**os.environ, "PYTHONPATH": str(BENCH_DIR.parent.parent / "presets")},
    )
    return proc


async def _wait_for_server(port, timeout=15.0):
    deadline = time.monotonic() + timeout
    async with httpx.AsyncClient() as client:
        while time.monotonic() < deadline:
            try:
                resp = await client.get(f"http://127.0.0.1:{port}/docs")
                if resp.status_code == 200:
                    return True
            except (httpx.ConnectError, httpx.ReadError):
                pass
            await asyncio.sleep(0.2)
    return False


# ---------------------------------------------------------------------------
# Benchmark runner
# ---------------------------------------------------------------------------


def _build_guardrails(enabled: bool, policy_file: str = ""):
    import ragengine.config as cfg
    from ragengine.guardrails.output_guardrails import OutputGuardrails

    if not enabled:
        return OutputGuardrails(enabled=False)

    policy_path = str(POLICIES_DIR / policy_file) if policy_file else ""
    cfg.OUTPUT_GUARDRAILS_ENABLED = True
    cfg.OUTPUT_GUARDRAILS_POLICY_PATH = policy_path
    os.environ["OUTPUT_GUARDRAILS_ENABLED"] = "true"
    os.environ["OUTPUT_GUARDRAILS_POLICY_PATH"] = policy_path
    return OutputGuardrails.from_config()


def _stats(values):
    if not values:
        return {"p50": 0, "p95": 0, "p99": 0, "mean": 0, "count": 0}
    import statistics

    s = sorted(values)
    n = len(s)
    return {
        "p50": round(s[int(n * 0.50)], 3),
        "p95": round(s[int(n * 0.95)] if n >= 20 else s[-1], 3),
        "p99": round(s[int(n * 0.99)] if n >= 100 else s[-1], 3),
        "mean": round(statistics.mean(s), 3),
        "count": n,
    }


async def _bench_http(stream: bool, iterations: int, warmup: int):
    """Benchmark HTTP round-trip to proxy. Returns (latencies_ms, ttft_ms)."""
    url = f"http://127.0.0.1:{PROXY_PORT}/v1/chat/completions"
    body = {
        "model": "mock",
        "messages": [{"role": "user", "content": "benchmark test"}],
        "stream": stream,
    }

    latencies = []
    ttft_list = []

    async with httpx.AsyncClient(timeout=30.0) as client:
        for i in range(warmup + iterations):
            start = time.perf_counter()
            first_content = None

            if stream:
                async with client.stream("POST", url, json=body) as resp:
                    async for chunk in resp.aiter_text():
                        if (
                            first_content is None
                            and chunk.strip()
                            and "content" in chunk
                        ):
                            first_content = time.perf_counter()
            else:
                await client.post(url, json=body)

            elapsed = (time.perf_counter() - start) * 1000

            if i >= warmup:
                latencies.append(elapsed)
                if first_content:
                    ttft_list.append((first_content - start) * 1000)

    return latencies, ttft_list


PROFILES = [
    ("NS0", False, False, ""),
    ("NS1", False, True, "redact_block.yaml"),
    ("S0", True, False, ""),
    ("S1", True, True, "block_only.yaml"),
    ("S2", True, True, "redact_block.yaml"),
]


async def run_all(iterations=100, warmup=10):
    global _proxy_guardrails

    results = {}

    for name, stream, enabled, policy in PROFILES:
        print(f"\n  Running {name} (stream={stream}, guardrails={enabled})...")
        _proxy_guardrails = _build_guardrails(enabled, policy)

        latencies, ttft = await _bench_http(stream, iterations, warmup)
        lat_stats = _stats(latencies)
        ttft_stats = _stats(ttft) if ttft else None

        results[name] = {
            "latency_ms": lat_stats,
            "ttft_ms": ttft_stats,
            "stream": stream,
            "guardrails": enabled,
        }
        print(
            f"    Latency: P50={lat_stats['p50']:.2f}ms P95={lat_stats['p95']:.2f}ms Mean={lat_stats['mean']:.2f}ms"
        )
        if ttft_stats:
            print(
                f"    TTFT:    P50={ttft_stats['p50']:.2f}ms P95={ttft_stats['p95']:.2f}ms Mean={ttft_stats['mean']:.2f}ms"
            )

    # Compute overheads
    overheads = {}
    pairs = [("NS1", "NS0"), ("S1", "S0"), ("S2", "S0")]
    for guarded, baseline in pairs:
        if guarded in results and baseline in results:
            g = results[guarded]["latency_ms"]
            b = results[baseline]["latency_ms"]
            overheads[f"{guarded}_vs_{baseline}"] = {
                "abs_p50": round(g["p50"] - b["p50"], 3),
                "abs_p95": round(g["p95"] - b["p95"], 3),
                "rel_p50_pct": round((g["p50"] - b["p50"]) / b["p50"] * 100, 1)
                if b["p50"] > 0
                else 0,
                "rel_p95_pct": round((g["p95"] - b["p95"]) / b["p95"] * 100, 1)
                if b["p95"] > 0
                else 0,
            }

    return {"profiles": results, "overheads": overheads}


async def main():
    iterations = 100
    warmup = 10

    if "--quick" in sys.argv:
        iterations = 10
        warmup = 3

    print("=" * 70)
    print("INTEGRATION BENCHMARK — HTTP round-trip through guardrails proxy")
    print("=" * 70)

    # Start servers
    print("\nStarting mock upstream server...")
    mock_proc = _start_mock_server()

    print("Waiting for mock server...")
    if not await _wait_for_server(MOCK_PORT):
        print("ERROR: Mock server failed to start")
        mock_proc.terminate()
        return

    print(f"Mock server ready on port {MOCK_PORT}")

    # For the proxy, we run it in-process since we need to swap _proxy_guardrails
    # Start proxy in a background task
    config = uvicorn.Config(
        proxy_app, host="127.0.0.1", port=PROXY_PORT, log_level="warning"
    )
    server = uvicorn.Server(config)
    proxy_task = asyncio.create_task(server.serve())

    print("Waiting for proxy server...")
    if not await _wait_for_server(PROXY_PORT):
        print("ERROR: Proxy server failed to start")
        mock_proc.terminate()
        return

    print(f"Proxy server ready on port {PROXY_PORT}")

    try:
        results = await run_all(iterations, warmup)

        # Print summary
        print("\n" + "=" * 70)
        print("INTEGRATION RESULTS (HTTP round-trip, ms)")
        print("=" * 70)
        print(f"{'Profile':<8} {'P50':>10} {'P95':>10} {'P99':>10} {'Mean':>10}")
        print("-" * 50)
        for name in ["NS0", "NS1", "S0", "S1", "S2"]:
            if name in results["profiles"]:
                s = results["profiles"][name]["latency_ms"]
                print(
                    f"{name:<8} {s['p50']:>10.2f} {s['p95']:>10.2f} {s['p99']:>10.2f} {s['mean']:>10.2f}"
                )

        ttft_profiles = [
            n
            for n in ["S0", "S1", "S2"]
            if results["profiles"].get(n, {}).get("ttft_ms")
        ]
        if ttft_profiles:
            print(
                f"\n{'Profile':<8} {'TTFT P50':>10} {'TTFT P95':>10} {'TTFT Mean':>10}"
            )
            print("-" * 42)
            for name in ttft_profiles:
                t = results["profiles"][name]["ttft_ms"]
                print(
                    f"{name:<8} {t['p50']:>10.2f} {t['p95']:>10.2f} {t['mean']:>10.2f}"
                )

        if results["overheads"]:
            print(
                f"\n{'Comparison':<12} {'Abs P50':>10} {'Abs P95':>10} {'Rel P50':>10} {'Rel P95':>10}"
            )
            print("-" * 55)
            for key, o in results["overheads"].items():
                print(
                    f"{key:<12} {o['abs_p50']:>10.2f} {o['abs_p95']:>10.2f} {o['rel_p50_pct']:>9.1f}% {o['rel_p95_pct']:>9.1f}%"
                )

        print("=" * 70)

        # Save
        RESULTS_DIR.mkdir(parents=True, exist_ok=True)
        output = RESULTS_DIR / "integration_results.json"
        with open(output, "w") as f:
            json.dump(results, f, indent=2)
        print(f"\nResults saved to {output}")

    finally:
        server.should_exit = True
        await proxy_task
        mock_proc.terminate()
        mock_proc.wait(timeout=5)


if __name__ == "__main__":
    logging.basicConfig(level=logging.WARNING)
    asyncio.run(main())
