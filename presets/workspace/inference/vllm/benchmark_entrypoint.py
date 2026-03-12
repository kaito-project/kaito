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

Always exits 0 once /health passes, even on benchmark failure (result value is -1).
This guarantees the pod eventually becomes Ready and the controller can parse the result.

stdout/stderr from exec probe processes are NOT captured by kubectl logs (they go to
kubelet's own pipe).  To make diagnostic logs and the result line visible, we write
through /proc/1/fd/1 (PID 1 = vLLM's stdout, which IS captured).  Falls back to our
own sys.stdout if /proc/1/fd/1 is not accessible.
"""

import contextlib
import os
import random
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

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


def _compute_rate() -> int:
    """Return the pre-computed saturation concurrency injected by the controller.

    The controller calculates:
        kv_per_req = BENCHMARK_INPUT_LEN * bytesPerToken / 1 GiB
        rate       = ceil(availableVRAMGiB / kv_per_req)

    Falls back to 512 if the env var is absent (unknown SKU / model).
    """
    val = os.environ.get("BENCHMARK_RATE", "")
    if not val:
        return 512
    try:
        return max(1, int(val))
    except ValueError:
        return 512


def _resolve_processor() -> str:
    """Resolve the --processor value for guidellm.

    Case 1 — Baked-in model: tokenizer at ``/workspace/weights`` root
              (``config.json`` present directly under weights).
    Case 2 — Standard DAR:   tokenizer in an HF snapshot subdir
              (``config.json`` under ``*/snapshots/*/``).
    Case 3 — No local tokenizer: derive HF repo ID from cache dir name
              (``models--openai--gpt-oss-120b``  →  ``openai/gpt-oss-120b``).
    Case 4 — Nothing found: return ``""`` and let guidellm auto-detect from
              ``/v1/models`` (may fail for unknown models).
    """
    weights = Path("/workspace/weights")

    # Case 1: baked-in weights
    if (weights / "config.json").exists():
        return str(weights)

    # Case 2: standard DAR — config.json inside a snapshots subdir
    snaps = list(weights.glob("*/snapshots/*/config.json"))
    if snaps:
        return str(snaps[0].parent)

    # Case 3: HF cache dir  (models--org--name → org/name)
    cache_dirs = [
        d for d in weights.iterdir() if d.is_dir() and d.name.startswith("models--")
    ]
    if cache_dirs:
        parts = cache_dirs[0].name[len("models--") :].split("--", 1)
        if len(parts) == 2:
            return f"{parts[0]}/{parts[1]}"

    return ""


# ── guidellm runner ───────────────────────────────────────────────────────────


def _run_guidellm(processor: str, rate: int) -> bool:
    """Run guidellm as the load generator.

    --profile throughput drives max concurrency up to ``--rate`` (saturating the model).
    --data-num-workers 0 prevents subprocess workers that can fail in containers.

    GUIDELLM__REPORT_GENERATION__SOURCE overrides the URL guidellm uses to fetch its
    HTML report template.  Without this, guidellm crashes post-benchmark with an httpx
    HTTPStatusError on a 301 redirect from blog.vllm.ai.  We don't use guidellm's HTML
    output — our TPM comes from vLLM's Prometheus counters — so a stub file is fine.

    Returns True on success, False on guidellm non-zero exit.
    """
    guidellm_bin = "/opt/guidellm-venv/bin/guidellm"
    if not os.path.isfile(guidellm_bin) or not os.access(guidellm_bin, os.X_OK):
        guidellm_bin = "guidellm"

    # guidellm's load_text() checks Path.is_file() which returns False for character
    # devices (/dev/null).  A real regular file is required.
    with tempfile.NamedTemporaryFile(
        suffix=".html", prefix="guidellm-stub-", mode="w", delete=False
    ) as f:
        f.write("<html></html>")
        stub_path = f.name

    try:
        cmd = [
            guidellm_bin,
            "benchmark",
            "run",
            "--target",
            VLLM_BASE_URL,
            "--profile",
            "throughput",
            "--rate",
            str(rate),
            "--max-seconds",
            str(BENCHMARK_DURATION),
            "--data",
            f"prompt_tokens={BENCHMARK_INPUT_LEN},output_tokens={BENCHMARK_OUTPUT_LEN}",
            # data-num-workers=0 prevents guidellm from spawning subprocesses that can fail in container environments
            "--data-num-workers",
            "0",
            "--random-seed",
            str(random.randint(0, 2**31 - 1)),
            "--disable-console",
        ]
        if processor:
            cmd += ["--processor", processor]

        env = os.environ.copy()
        env["GUIDELLM__REPORT_GENERATION__SOURCE"] = stub_path

        result = subprocess.run(
            cmd,
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        if result.returncode != 0:
            _log(f"guidellm_failed rc={result.returncode}")
            return False
        return True
    finally:
        with contextlib.suppress(OSError):
            os.unlink(stub_path)


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

    rate = _compute_rate()
    _log(f"rate_set BENCHMARK_RATE={rate} INPUT_LEN={BENCHMARK_INPUT_LEN}")

    if not _run_guidellm(processor, rate):
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


def _drain() -> None:
    """Spin until vllm:num_requests_running reaches zero.

    No timeout — the model must not become Ready while requests are still running,
    as that would compete with real traffic.
    """
    _log("drain_start")
    while True:
        if _read_counter("vllm:num_requests_running") == 0:
            break
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

    # /health passed.  Commit to exit 0 from here on so the startup probe eventually
    # passes and the readiness probe can activate regardless of benchmark outcome.
    t_bench_start = time.time()
    _log(
        f"benchmark_start DURATION={BENCHMARK_DURATION}s "
        f"INPUT_LEN={BENCHMARK_INPUT_LEN} OUTPUT_LEN={BENCHMARK_OUTPUT_LEN}"
    )

    tpm: float = -1.0
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

    _write_to_pid1(f"KAITO_BENCHMARK_RESULT vllm_total_tpm={tpm}\n", fd=1)
    sys.exit(0)


if __name__ == "__main__":
    main()
