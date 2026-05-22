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
import math
import os
import sys
import time
import urllib.request
from pathlib import Path

from huggingface_hub import scan_cache_dir
from huggingface_hub.constants import HF_HUB_CACHE
from prometheus_client.parser import text_string_to_metric_families

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
    with open(f"/proc/1/fd/{fd}", "a") as fh:
        fh.write(line)
        fh.flush()


def _log(msg: str, tag: str = "KAITO_BENCHMARK") -> None:
    """Format and emit a log line through PID 1's stdout.

    If the write fails (e.g. ``/proc/1/fd/1`` is inaccessible), the script exits
    immediately with code 1.
    """
    ts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    try:
        _write_to_pid1(f"{tag} {ts} {msg}\n", fd=1)
    except Exception:
        sys.exit(1)


# ── vLLM helpers ─────────────────────────────────────────────────────────────


def _health_check() -> bool:
    """Return True if vLLM /health responds with HTTP 200."""
    try:
        with urllib.request.urlopen(f"{VLLM_BASE_URL}/health", timeout=5) as resp:
            return resp.status == 200
    except Exception:
        return False


def _sum_counter_metric(metric: str) -> int:
    """Sum all samples for an additive Prometheus metric from vLLM /metrics.

    Aggregates across label sets (e.g. multiple ``engine`` shards in a
    tensor/pipeline-parallel deployment).  Returns 0 if the metric is absent
    or the endpoint is unreachable.

    Intended for **additive** metric types only:

      * counters (``..._total``) — summing yields the total count
      * gauges where summing is meaningful (e.g. ``vllm:num_requests_running``
        — total in-flight requests across shards)

    NOT suitable for:

      * histograms / summaries (exposed as ``_bucket`` / ``_sum`` / ``_count``
        — this function will return 0 for the bare metric name)
      * non-additive gauges such as utilization percentages
      * info metrics (label-only; value is always 1)

    Example /metrics output (multi-engine deployment)::

        vllm:generation_tokens_total{engine="0",model_name="gemma-4-e4b-it"} 408832.0
        vllm:generation_tokens_total{engine="1",model_name="gemma-4-e4b-it"} 407040.0

    For ``metric="vllm:generation_tokens_total"`` this returns ``815872``
    (408832 + 407040).
    """
    try:
        data = (
            urllib.request.urlopen(f"{VLLM_BASE_URL}/metrics", timeout=5)
            .read()
            .decode()
        )
    except Exception:
        return 0
    total = 0.0
    found = False
    for family in text_string_to_metric_families(data):
        for sample in family.samples:
            # The prometheus_client parser strips the ``_total`` suffix from
            # counter family names (e.g. family ``vllm:generation_tokens`` for
            # metric ``vllm:generation_tokens_total``), so match on the raw
            # ``sample.name`` instead.  This also naturally skips the synthetic
            # ``_created`` timestamp samples.
            if sample.name != metric:
                continue
            found = True
            total += sample.value
    return int(total) if found else 0


# ── Benchmark configuration ───────────────────────────────────────────────────


def _compute_max_concurrency(processor: str | None = None) -> int:
    """Compute saturation concurrency from vLLM's actual KV cache allocation.

    Parses ``vllm:cache_config_info`` from /metrics to get block pool parameters,
    then computes blocks-per-request for both attention and Mamba layer groups.

    For hybrid Mamba models (e.g. NemotronH), attention and Mamba layers allocate
    blocks **additively** from a shared pool (see ``KVCacheCoordinator.
    get_num_blocks_to_allocate`` in vLLM).  For pure-attention models, only
    attention blocks are counted.

    Raises ``RuntimeError`` if the metric is absent or unparsable.
    """
    try:
        content = (
            urllib.request.urlopen(f"{VLLM_BASE_URL}/metrics", timeout=5)
            .read()
            .decode()
        )
    except Exception as exc:
        raise RuntimeError(f"failed to fetch /metrics: {exc}") from exc

    labels = _get_cache_config_labels(content)
    if labels is None:
        raise RuntimeError(
            "vllm:cache_config_info metric or required labels not found in /metrics output"
        )

    num_gpu_blocks = labels["num_gpu_blocks"]
    block_size = labels["block_size"]
    seq_len = BENCHMARK_INPUT_LEN + BENCHMARK_OUTPUT_LEN

    config = _load_model_config_dict(processor)
    layer_counts = (
        _infer_kv_cache_layer_counts_from_dict(config) if config is not None else None
    )
    attn_group_count, mamba_group_count = _derive_kv_cache_group_counts(
        labels, layer_counts
    )
    attn_blocks_per_group = math.ceil(seq_len / block_size)
    mamba_blocks_per_group = _compute_mamba_blocks(labels, seq_len)
    attn_blocks = attn_group_count * attn_blocks_per_group
    mamba_blocks = mamba_group_count * mamba_blocks_per_group

    blocks_per_request = attn_blocks + mamba_blocks
    concurrency = num_gpu_blocks // blocks_per_request
    if concurrency <= 0:
        raise RuntimeError(
            f"computed max_concurrency={concurrency} <= 0 "
            f"(num_gpu_blocks={num_gpu_blocks}, "
            f"attn_blocks={attn_blocks}, "
            f"mamba_blocks={mamba_blocks}, "
            f"attn_group_count={attn_group_count}, "
            f"mamba_group_count={mamba_group_count}, "
            f"seq_len={seq_len})"
        )
    return concurrency


def _get_cache_config_labels(metrics_text: str) -> dict | None:
    """Extract validated cache_config_info labels as a typed dict.

    Returns None if the metric is missing or required fields are not valid ints.
    """
    for family in text_string_to_metric_families(metrics_text):
        if family.name != "vllm:cache_config_info":
            continue
        for sample in family.samples:
            raw = sample.labels
            try:
                return {
                    "num_gpu_blocks": int(raw["num_gpu_blocks"]),
                    "block_size": int(raw["block_size"]),
                    "mamba_block_size": raw.get("mamba_block_size"),
                    "mamba_cache_mode": raw.get("mamba_cache_mode"),
                }
            except (KeyError, ValueError):
                continue
    return None


def _compute_mamba_blocks(labels: dict, seq_len: int) -> int:
    """Return Mamba blocks per request for one Mamba KV-cache group."""
    raw_size = labels.get("mamba_block_size")
    mode = labels.get("mamba_cache_mode")
    try:
        mamba_block_size = int(raw_size)  # type: ignore[arg-type]
    except (TypeError, ValueError):
        return 0

    if mode == "none":
        return math.ceil(seq_len / mamba_block_size)
    if mode == "align":
        return 2
    if mode == "all":
        return math.ceil(seq_len / mamba_block_size)
    return 0


def _derive_kv_cache_group_counts(
    labels: dict, layer_counts: tuple[int, int] | None
) -> tuple[int, int]:
    """Derive group counts from layer counts, with metrics fallback."""
    if layer_counts is None:
        try:
            int(labels.get("mamba_block_size"))  # type: ignore[arg-type]
            has_mamba = labels.get("mamba_cache_mode") is not None
        except (TypeError, ValueError):
            has_mamba = False
        return 1, 1 if has_mamba else 0

    attn_layers, mamba_layers = layer_counts
    if attn_layers <= 0:
        return 0, 1 if mamba_layers > 0 else 0
    if mamba_layers <= 0:
        return 1, 0

    min_num_layers = min(attn_layers, mamba_layers)
    max_num_layers = max(attn_layers, mamba_layers)
    group_size = min_num_layers
    if max_num_layers < min_num_layers * 1.5:
        group_size = max_num_layers

    return math.ceil(attn_layers / group_size), math.ceil(mamba_layers / group_size)


def _load_model_config_dict(processor: str | None) -> dict | None:
    """Load the model's ``config.json`` as a dict.

    Two mutually-exclusive paths:

    1. **Baked-in weights** — ``processor`` is the local path
       ``/workspace/weights`` (or any path containing ``config.json``).
       Read the file directly; no HF cache scan needed.
    2. **HF cache (DAR or normal cache)** — ``processor`` is a repo id like
       ``microsoft/Phi-3-mini-4k-instruct``.  Use ``scan_cache_dir`` to find
       the snapshot directory and read its ``config.json``.

    Reading the raw JSON bypasses ``transformers`` entirely, so generic
    ``PretrainedConfig`` degradation cannot strip custom fields like
    ``hybrid_override_pattern`` or ``layer_types``.

    Returns ``None`` if no config.json can be located or parsed.
    """
    import json

    if not processor:
        return None

    # Case 1: baked-in weights — processor is a local directory path.
    weights_cfg = Path(processor) / "config.json"
    if weights_cfg.is_file():
        try:
            with open(weights_cfg) as f:
                return json.load(f)
        except Exception:
            return None

    # Case 2: HF cache — processor is a repo_id.
    try:
        cache_info = scan_cache_dir()
    except Exception:
        return None
    for repo in cache_info.repos:
        if repo.repo_id != processor:
            continue
        for rev in repo.revisions:
            cfg = Path(rev.snapshot_path) / "config.json"
            if cfg.is_file():
                try:
                    with open(cfg) as f:
                        return json.load(f)
                except Exception:
                    return None

    return None


def _infer_kv_cache_layer_counts_from_dict(root: dict) -> tuple[int, int] | None:
    """Infer ``(attention_layers, mamba_layers)`` from one config dict."""

    for config in _iter_config_dicts(root):
        pattern = config.get("hybrid_override_pattern")
        if isinstance(pattern, str):
            attn_layers = pattern.count("*")
            mamba_layers = pattern.count("M")
            if attn_layers or mamba_layers:
                return attn_layers, mamba_layers

        layer_types = config.get("layer_types")
        if isinstance(layer_types, list) and all(
            isinstance(layer_type, str) for layer_type in layer_types
        ):
            attn_layers = sum(
                1 for layer_type in layer_types if layer_type == "full_attention"
            )
            mamba_layers = len(layer_types) - attn_layers
            if attn_layers or mamba_layers:
                return attn_layers, mamba_layers

        architectures = config.get("architectures") or []
        if isinstance(architectures, str):
            architectures = [architectures]
        model_type = str(config.get("model_type", "")).lower()
        num_hidden_layers = config.get("num_hidden_layers")
        if isinstance(num_hidden_layers, int) and num_hidden_layers > 0:
            if any("FalconH1" in arch for arch in architectures):
                return num_hidden_layers, num_hidden_layers
            if "mamba" in model_type and "num_attention_heads" not in config:
                return 0, num_hidden_layers

    return None


def _iter_config_dicts(config: dict):
    yield config
    for key in ("text_config", "llm_config", "model_config"):
        nested = config.get(key)
        if isinstance(nested, dict):
            yield nested


def _resolve_processor() -> str:
    """Resolve the --processor value for guidellm.

    Case 1 — Baked-in model: tokenizer at ``/workspace/weights`` root
              (``config.json`` present directly under weights).
    Case 2 — DAR / HF cache: use ``scan_cache_dir`` to read the repo_id from
              HF cache metadata without parsing cache paths or blob names.
    Case 3 — Nothing found: return ``""`` and let guidellm auto-detect from
              ``/v1/models`` (may fail for unknown models).
    """
    weights = Path("/workspace/weights")

    # Case 1: baked-in weights (tokenizer lives at the weights root)
    if (weights / "config.json").exists():
        return str(weights)

    # Case 2: DAR or HF cache — ask huggingface_hub for the repo_id.
    processor = _resolve_processor_from_hf_cache(weights)
    if processor:
        return processor
    processor = _resolve_processor_from_hf_cache(Path(HF_HUB_CACHE))
    if processor:
        return processor

    return ""


def _resolve_processor_from_hf_cache(cache_dir: Path) -> str:
    """Resolve the first cached HF repo id from a cache directory."""
    if not cache_dir.exists():
        return ""
    try:
        cache_info = scan_cache_dir(cache_dir=str(cache_dir))
        repos = sorted(cache_info.repos, key=lambda r: r.repo_id)
        if repos:
            # repo_id is the HuggingFace model identifier (e.g. "microsoft/Phi-3-mini-4k-instruct"),
            # not a local path. guidellm/vLLM accept it as the --processor value and resolve the
            # tokenizer from the HF Hub (or local cache if HF_HUB_OFFLINE is set).
            return repos[0].repo_id
    except Exception:
        pass
    return ""


def _predownload_processor(processor: str) -> str:
    """Pre-download the tokenizer so the download doesn't count toward benchmark time.

    GuideLLM calls ``AutoTokenizer.from_pretrained(processor)`` lazily during
    synthetic data generation.  Calling it here ensures the files are cached
    locally before the benchmark clock starts.

    Returns the processor string unchanged (guidellm will hit the local cache).
    """
    if not processor:
        return processor
    try:
        from transformers import AutoTokenizer

        _log(f"predownloading_processor processor={processor}")
        AutoTokenizer.from_pretrained(processor, trust_remote_code=True)
        _log("predownload_done")
    except Exception as exc:
        _log(f"predownload_failed (non-fatal): {exc}")
    return processor


# ── guidellm runner ───────────────────────────────────────────────────────────


def _run_guidellm(processor: str, max_concurrency: int):
    """Run guidellm via its Python API as the load generator.

    Uses ``benchmark_generative_text`` directly rather than a subprocess so there
    is no process-spawn overhead and no need for the HTML-stub workaround.
    ``outputs=[]`` suppresses all report file generation.

    guidellm lives in an isolated venv at ``/opt/guidellm-venv``.  Its
    site-packages are injected into ``sys.path`` at module load time (before the
    ``huggingface_hub`` import) so every import in this process uses the venv's
    versions.

    Returns the ``GenerativeBenchmarksReport`` on success, ``None`` on failure.
    """
    try:
        from guidellm.benchmark import BenchmarkGenerativeTextArgs
        from guidellm.benchmark.entrypoints import benchmark_generative_text
    except ImportError as exc:
        _log(f"guidellm_import_failed: {exc}")
        return None

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
        report, _outputs = asyncio.run(benchmark_generative_text(args=args))
        return report
    except Exception as exc:
        _log(f"guidellm_failed: {exc}")
        return None


def _extract_guidellm_metrics(report) -> tuple:
    """Extract TTFT and TPOT averages from a guidellm report.

    Uses the ``.total`` distribution bucket which includes both successful and
    incomplete requests, ensuring metrics are available even when requests are
    cancelled before completion (guidellm streams tokens, so per-token timestamps
    exist for partial responses too).

    Returns ``(ttft_avg_ms, tpot_avg_ms)``.  Raises ``RuntimeError`` if the
    report structure is empty or the fields are unavailable so the caller can
    fail the benchmark cleanly.
    """
    try:
        metrics = report.benchmarks[0].metrics
        ttft = metrics.time_to_first_token_ms.total.mean
        tpot = metrics.time_per_output_token_ms.total.mean
        return (round(ttft, 2), round(tpot, 2))
    except (IndexError, AttributeError, TypeError) as exc:
        raise RuntimeError(
            f"failed to extract TTFT/TPOT from guidellm report: {exc}"
        ) from exc


# ── Core benchmark sequence ───────────────────────────────────────────────────


def _run_benchmark() -> tuple:
    """Run the full benchmark sequence.

    Snapshots vLLM Prometheus counters before and after the guidellm load run, then
    computes total TPM from the delta.  Extracts TTFT and TPOT averages from the
    guidellm report (using the ``total`` bucket which includes both successful and
    incomplete requests).

    Returns ``(tpm, ttft_avg_ms, tpot_avg_ms)``.
    Raises ``RuntimeError`` on any failure so the caller can log it and fall back
    to the sentinel result.
    """
    processor = _predownload_processor(_resolve_processor())
    _log(f"processor_resolved PROCESSOR={processor or '<auto>'}")

    max_concurrency = _compute_max_concurrency(processor)
    _log(
        f'{{"duration_sec":{BENCHMARK_DURATION},"input_tokens":{BENCHMARK_INPUT_LEN},'
        f'"output_tokens":{BENCHMARK_OUTPUT_LEN},"max_concurrency":{max_concurrency}}}',
        tag="KAITO_BENCHMARK_CONFIG",
    )

    t0_gen = _sum_counter_metric("vllm:generation_tokens_total")
    t0_prompt = _sum_counter_metric("vllm:prompt_tokens_total")
    t0_epoch = time.time()
    _log(f"benchmark_start epoch={t0_epoch:.1f} t0_gen={t0_gen} t0_prompt={t0_prompt}")

    report = _run_guidellm(processor, max_concurrency)
    if report is None:
        raise RuntimeError("guidellm exited non-zero")

    ttft_ms, tpot_ms = _extract_guidellm_metrics(report)

    t1_epoch = time.time()
    t1_gen = _sum_counter_metric("vllm:generation_tokens_total")
    t1_prompt = _sum_counter_metric("vllm:prompt_tokens_total")
    _log(f"benchmark_end epoch={t1_epoch:.1f} t1_gen={t1_gen} t1_prompt={t1_prompt}")

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
    tpm = round((delta_gen + (t1_prompt - t0_prompt)) * 60.0 / elapsed, 2)
    return tpm, ttft_ms, tpot_ms, max_concurrency


def _drain(timeout: float = 300.0) -> None:
    """Spin until vllm:num_requests_running reaches zero.

    Raises ``TimeoutError`` after *timeout* seconds so the pod can still
    become Ready rather than hanging forever if vLLM gets stuck.
    """
    _log("drain_start")
    deadline = time.monotonic() + timeout
    while True:
        if _sum_counter_metric("vllm:num_requests_running") == 0:
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

    tpm: float = -1.0
    ttft_ms: float = -1.0
    tpot_ms: float = -1.0
    max_concurrency: int = 0
    failed = False
    try:
        tpm, ttft_ms, tpot_ms, max_concurrency = _run_benchmark()
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
        ttft_ms = -1.0
        tpot_ms = -1.0
        failed = True

    # Re-emit config immediately before the result so both lines land in the same
    # tail-log window read by the controller.
    if max_concurrency > 0:
        _log(
            f'{{"duration_sec":{BENCHMARK_DURATION},"input_tokens":{BENCHMARK_INPUT_LEN},'
            f'"output_tokens":{BENCHMARK_OUTPUT_LEN},"max_concurrency":{max_concurrency}}}',
            tag="KAITO_BENCHMARK_CONFIG",
        )

    # Always emit the result line so the controller has a parseable record even on failure.
    _log(
        f'{{"vllm_total_tpm":{tpm},"ttft_avg_ms":{ttft_ms},"tpot_avg_ms":{tpot_ms}}}',
        tag="KAITO_BENCHMARK_RESULT",
    )
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
