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

"""Exec startup probe script for KAITO benchmark workspaces.

Called by kubelet as the container's StartupProbe exec command on every probe tick:

  - Model still loading: GET /health returns non-200  →  exit 1
    (tick fails; failureThreshold budget consumed; kubelet retries next period)

  - Model ready: GET /health returns 200  →  run benchmark  →  drain  →
    print KAITO_BENCHMARK_RESULT  →  exit 0
    (startup probe passes; readiness probe activates; pod becomes Ready)

Exits 0 on success (benchmark completed, result emitted).
Exits 1 on benchmark failure (result emitted with tpm=-1, then probe fails so kubelet retries).
This gives the user a clear failure signal while still writing a parseable result line.

stdout/stderr from exec probe processes are NOT captured by kubectl logs (they go to
kubelet's own pipe).  To make diagnostic logs and the result line visible, we write
through /proc/1/fd/1 (PID 1 = vLLM's stdout, which IS captured).  Falls back to our
own sys.stdout if /proc/1/fd/1 is not accessible.
"""

import asyncio
import glob
import os
import sys
import time
import urllib.request
from pathlib import Path

# Inject guidellm's isolated venv into sys.path before any huggingface_hub
# import so that guidellm's newer huggingface_hub (which exports is_offline_mode
# and other symbols) is loaded instead of the older system-installed version.
_sp = glob.glob("/opt/guidellm-venv/lib/python*/site-packages")
if _sp and _sp[0] not in sys.path:
    sys.path.insert(0, _sp[0])

from huggingface_hub import scan_cache_dir  # noqa: E402

# ── Configuration ─────────────────────────────────────────────────────────────
BENCHMARK_DURATION = 60
BENCHMARK_INPUT_LEN = 2048
BENCHMARK_OUTPUT_LEN = 256
VLLM_BASE_URL = "http://localhost:5000"


# ── Logging ───────────────────────────────────────────────────────────────────


def _write_to_pid1(line: str, fd: int = 1) -> None:
    """Write *line* through PID 1's stdout (fd=1) or stderr (fd=2).

    This makes output visible in ``kubectl logs`` even though this script runs
    as an exec probe child process whose own stdio is captured by kubelet, not
    by the container log driver.
    """
    try:
        with open(f"/proc/1/fd/{fd}", "a") as fh:
            fh.write(line)
            fh.flush()
    except OSError:
        # Fallback: write to our own fd (useful in tests / bare-metal runs).
        if fd == 1:
            sys.stdout.write(line)
            sys.stdout.flush()
        else:
            sys.stderr.write(line)
            sys.stderr.flush()


def _log(msg: str) -> None:
    ts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    _write_to_pid1(f"KAITO_BENCHMARK {ts} {msg}\n", fd=1)


# ── vLLM helpers ─────────────────────────────────────────────────────────────


def _health_check() -> bool:
    """Return True if vLLM /health responds with HTTP 200."""
    try:
        with urllib.request.urlopen(f"{VLLM_BASE_URL}/health", timeout=5) as resp:
            return resp.status == 200
    except Exception:
        return False


def _read_counter(metric: str) -> int:
    """Parse a Prometheus counter value from vLLM /metrics.

    Handles both labelled (``metric{...} value``) and bare (``metric value``) forms.
    Returns 0 if the metric is absent or the endpoint is unreachable.
    """
    try:
        data = (
            urllib.request.urlopen(f"{VLLM_BASE_URL}/metrics", timeout=5)
            .read()
            .decode()
        )
    except Exception:
        return 0
    for line in data.splitlines():
        if line.startswith(f"{metric}{{") or line.startswith(f"{metric} "):
            try:
                return int(float(line.split()[-1]))
            except (ValueError, IndexError):
                return 0
    return 0


# ── Benchmark configuration ───────────────────────────────────────────────────


def _compute_max_concurrency() -> int:
    """Return the pre-computed saturation concurrency injected by the controller.

    The controller always sets BENCHMARK_MAX_CONCURRENCY (defaulting to 256 when
    the model or SKU metadata is unavailable). Values <= 0 are rejected.
    """
    val = os.environ.get("BENCHMARK_MAX_CONCURRENCY", "")
    try:
        concurrency = int(val)
    except ValueError:
        raise RuntimeError(f"invalid BENCHMARK_MAX_CONCURRENCY={val!r}: must be a positive integer")
    if concurrency <= 0:
        raise RuntimeError(f"invalid BENCHMARK_MAX_CONCURRENCY={val!r}: must be a positive integer")
    return concurrency


def _resolve_processor() -> str:
    """Resolve the --processor value for guidellm.

    Case 1 — Baked-in model: tokenizer at ``/workspace/weights`` root
              (``config.json`` present directly under weights).
    Case 2 — DAR / HF cache: use ``scan_cache_dir`` to read the repo_id
              directly from the cache metadata (handles both snapshot and
              ``models--org--name`` layouts).
    Case 3 — Nothing found: return ``""`` and let guidellm auto-detect from
              ``/v1/models`` (may fail for unknown models).
    """
    weights = Path("/workspace/weights")

    # Case 1: baked-in weights (tokenizer lives at the weights root)
    if (weights / "config.json").exists():
        return str(weights)

    # Case 2: DAR or HF cache — ask huggingface_hub for the repo_id
    try:
        cache_info = scan_cache_dir(cache_dir=str(weights))
        repos = sorted(cache_info.repos, key=lambda r: r.repo_id)
        if repos:
            # repo_id is the HuggingFace model identifier (e.g. "microsoft/Phi-3-mini-4k-instruct"),
            # not a local path. guidellm/vLLM accept it as the --processor value and resolve the
            # tokenizer from the HF Hub (or local cache if HF_HUB_OFFLINE is set).
            return repos[0].repo_id
    except Exception:
        pass

    return ""


# ── guidellm runner ───────────────────────────────────────────────────────────


def _run_guidellm(processor: str, max_concurrency: int) -> bool:
    """Run guidellm via its Python API as the load generator.

    Uses ``benchmark_generative_text`` directly rather than a subprocess so there
    is no process-spawn overhead and no need for the HTML-stub workaround.
    ``outputs=[]`` suppresses all report file generation.

    guidellm lives in an isolated venv at ``/opt/guidellm-venv``.  Its
    site-packages are injected into ``sys.path`` at module load time (before the
    ``huggingface_hub`` import) so every import in this process uses the venv's
    versions.

    Returns True on success, False if guidellm raises.
    """
    try:
        from guidellm.benchmark import BenchmarkGenerativeTextArgs
        from guidellm.benchmark.entrypoints import benchmark_generative_text
    except ImportError as exc:
        _log(f"guidellm_import_failed: {exc}")
        return False

    args = BenchmarkGenerativeTextArgs(
        target=VLLM_BASE_URL,
        data=[
            f"prompt_tokens={BENCHMARK_INPUT_LEN},output_tokens={BENCHMARK_OUTPUT_LEN}"
        ],
        profile="throughput",
        rate=[float(max_concurrency)],
        max_seconds=BENCHMARK_DURATION,
        processor=processor or None,
        data_num_workers=0,
        random_seed=int(time.time()),
        outputs=[],
    )

    try:
        asyncio.run(benchmark_generative_text(args=args))
        return True
    except Exception as exc:
        _log(f"guidellm_failed: {exc}")
        return False


# ── Core benchmark sequence ───────────────────────────────────────────────────


def _run_benchmark() -> float:
    """Run the full benchmark sequence.

    Snapshots vLLM Prometheus counters before and after the guidellm load run, then
    computes total TPM from the delta.  Raises ``RuntimeError`` on any failure so the
    caller can log it and fall back to the -1 result.
    """
    t0_gen = _read_counter("vllm:generation_tokens_total")
    t0_prompt = _read_counter("vllm:prompt_tokens_total")
    t0_epoch = time.time()

    processor = _resolve_processor()
    _log(f"processor_resolved PROCESSOR={processor or '<auto>'}")

    max_concurrency = _compute_max_concurrency()
    _log(f"max_concurrency_set BENCHMARK_MAX_CONCURRENCY={max_concurrency} INPUT_LEN={BENCHMARK_INPUT_LEN}")

    if not _run_guidellm(processor, max_concurrency):
        raise RuntimeError("guidellm exited non-zero")

    t1_epoch = time.time()
    t1_gen = _read_counter("vllm:generation_tokens_total")
    t1_prompt = _read_counter("vllm:prompt_tokens_total")

    delta_gen = t1_gen - t0_gen
    # Require at least one generated token.  Zero generation means the load did not
    # reach the model (e.g. wrong endpoint, auth failure, all requests are prefill-only).
    if delta_gen == 0:
        raise RuntimeError(
            "benchmark_no_generation delta_gen=0 — model produced no output tokens"
        )

    elapsed = t1_epoch - t0_epoch
    _log(
        f"benchmark_window elapsed_sec={elapsed:.1f} "
        f"delta_gen={delta_gen} delta_prompt={t1_prompt - t0_prompt}"
    )
    return round((delta_gen + (t1_prompt - t0_prompt)) * 60.0 / elapsed, 2)


def _drain(timeout: float = 300.0) -> None:
    """Spin until vllm:num_requests_running reaches zero.

    Raises ``TimeoutError`` after *timeout* seconds so the pod can still
    become Ready rather than hanging forever if vLLM gets stuck.
    """
    _log("drain_start")
    deadline = time.monotonic() + timeout
    while True:
        if _read_counter("vllm:num_requests_running") == 0:
            break
        if time.monotonic() >= deadline:
            raise TimeoutError(f"_drain timed out after {timeout}s")
        time.sleep(2)


# ── Entry point ───────────────────────────────────────────────────────────────


def main() -> None:
    # Multi-node workers (POD_INDEX != "0"): the distributed startup probe uses a
    # shell conditional to route workers to multi-node-health-check.py instead of
    # this script (see buildDistributedBenchmarkStartupProbe in preset_inferences.go).
    if os.environ.get("POD_INDEX", "0") != "0":
        sys.exit(0)

    if not _health_check():
        sys.exit(1)

    # /health passed — model is loaded.  Run the benchmark; fail the probe on error
    # so kubelet retries and the user gets a clear failure signal.
    t_bench_start = time.time()
    _log(
        f"benchmark_start DURATION={BENCHMARK_DURATION}s "
        f"INPUT_LEN={BENCHMARK_INPUT_LEN} OUTPUT_LEN={BENCHMARK_OUTPUT_LEN}"
    )

    tpm: float = -1.0
    failed = False
    try:
        tpm = _run_benchmark()
        t_bench_end = time.time()
        _log(f"benchmark_done elapsed={t_bench_end - t_bench_start:.1f}s")
        _drain()
        t_drain_end = time.time()
        _log(
            f"drain_done elapsed={t_drain_end - t_bench_end:.1f}s "
            f"total_phase_elapsed={t_drain_end - t_bench_start:.1f}s"
        )
    except Exception as exc:
        _log(f"benchmark_failed error={exc}")
        tpm = -1.0
        failed = True

    # Always emit the result line so the controller has a parseable record even on failure.
    # In case _write_to_pid1 is not working, controller should treat missing
    # KAITO_BENCHMARK_RESULT as a failure (equivalent to -1).
    _write_to_pid1(f'KAITO_BENCHMARK_RESULT {{"vllm_total_tpm":{tpm}}}\n', fd=1)
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
