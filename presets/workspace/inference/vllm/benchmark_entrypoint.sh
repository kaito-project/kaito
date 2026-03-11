#!/bin/bash
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

# benchmark_entrypoint.sh — wraps the vLLM entrypoint with a post-load benchmark stage.
#
# This script is used as the container CMD when the Workspace annotation
# kaito.sh/run-benchmark=true is set. The original vLLM command is forwarded
# as positional arguments ("$@") and run in the background.
#
# Stages:
#   Init      — startup probe (HTTPGet /health) fires until vLLM is up; pod not Ready.
#   Benchmark — readiness probe (test -f /tmp/kaito_benchmark_done) fails; benchmark runs.
#   Ready     — sentinel written; readiness probe passes; pod becomes Ready.
#
# Always emits exactly one "KAITO_BENCHMARK_RESULT ..." line to stdout so the controller
# can always parse a result. Values are -1 on any benchmark failure.

set -u

# Standardised stderr logger — all diagnostic output uses this
_log() { echo "KAITO_BENCHMARK $(date -u +%Y-%m-%dT%H:%M:%SZ) $*" >&2; }

# ── Benchmark configuration ───────────────────────────────────────────────────
BENCHMARK_DURATION=60   # seconds to run the benchmark
BENCHMARK_INPUT_LEN=2048  # synthetic prompt token length
BENCHMARK_OUTPUT_LEN=256  # synthetic output token length

# Compute saturation concurrency from controller-injected env vars.
# AVAILABLE_VRAM_GIB = (totalVRAM - modelWeights) × 0.9, computed by the controller.
# BENCHMARK_INPUT_LEN is kept here so it can be tuned.
# Falls back to 512 if any variable is absent (e.g. unknown SKU or model weight size).
BENCHMARK_RATE=512
if [ -n "${AVAILABLE_VRAM_GIB:-}" ] && [ -n "${KAITO_BYTES_PER_TOKEN:-}" ]; then
    BENCHMARK_RATE=$(python3 -c "
import math
kv_per_req = ${BENCHMARK_INPUT_LEN} * ${KAITO_BYTES_PER_TOKEN} / (1024**3)
print(max(1, math.ceil(${AVAILABLE_VRAM_GIB} / kv_per_req)))
") || BENCHMARK_RATE=512
fi
_log "rate_set BENCHMARK_RATE=${BENCHMARK_RATE} AVAILABLE_VRAM_GIB=${AVAILABLE_VRAM_GIB:-unset} BYTES_PER_TOKEN=${KAITO_BYTES_PER_TOKEN:-unset} INPUT_LEN=${BENCHMARK_INPUT_LEN}"

BENCHMARK_TOTAL_TPM="-1"
SENTINEL="/tmp/kaito_benchmark_done"

_on_exit() {
    echo "KAITO_BENCHMARK_RESULT vllm_total_tpm=${BENCHMARK_TOTAL_TPM}"
    touch "${SENTINEL}"
}
trap _on_exit EXIT

# Launch vLLM (or Ray worker) in the background. Positional args are the original CMD.
"$@" &
VLLM_PID=$!

# Forward SIGTERM to vLLM so Kubernetes pod termination triggers a graceful shutdown
# rather than SIGKILL after the grace period. bash does not propagate signals to
# background children by default when it is PID 1.
trap 'kill -TERM "${VLLM_PID}" 2>/dev/null; wait "${VLLM_PID}"; exit $?' TERM

# Multi-node workers (POD_INDEX != 0): skip benchmark entirely.
# Their readiness probe uses the standard distributed health check (multi-node-health-check.py).
if [ "${POD_INDEX:-0}" != "0" ]; then
    trap - EXIT
    wait "${VLLM_PID}"
    exit $?
fi

# ── Leader path (single-node: POD_INDEX unset; multi-node: POD_INDEX=0) ──────

# Poll until vLLM health endpoint is up. This gates the benchmark start until model is up.
until curl -sf http://localhost:5000/health >/dev/null 2>&1; do
    sleep 5
done

_read_counter() {
    local metric="$1"
    python3 - "$metric" <<'PYEOF'
import sys, urllib.request
metric = sys.argv[1]
data = urllib.request.urlopen("http://localhost:5000/metrics").read().decode()
for line in data.splitlines():
    if line.startswith(metric + "{") or line.startswith(metric + " "):
        print(int(float(line.split()[-1])))
        sys.exit(0)
print(0)
PYEOF
}

# Path to the isolated guidellm venv that ships transformers>=5.0.0.
GUIDELLM_BIN="/opt/guidellm-venv/bin/guidellm"
if [ ! -x "${GUIDELLM_BIN}" ]; then
    GUIDELLM_BIN="guidellm"
fi

# Resolve the --processor value for guidellm:
#   1. Baked-in model: tokenizer at /workspace/weights root
#   2. Standard DAR:   tokenizer in HF snapshot subdir (has config.json)
#   3. No local tokenizer (e.g. gpt-oss old pin): derive HF repo ID from cache
#      dir name  models--openai--gpt-oss-120b → openai/gpt-oss-120b  and let
#      guidellm download the tokenizer from HF.
_resolve_processor() {
    # Case 1: baked-in
    if [ -f /workspace/weights/config.json ]; then
        echo /workspace/weights
        return
    fi

    # Case 2: standard DAR — config.json inside a snapshots subdir
    local snap
    snap=$(find /workspace/weights -name config.json -path "*/snapshots/*" 2>/dev/null | head -1)
    if [ -n "$snap" ]; then
        echo "$(dirname "$snap")"
        return
    fi

    # Case 3: no local tokenizer — derive HF repo ID from cache dir name
    local cache_dir
    cache_dir=$(find /workspace/weights -maxdepth 1 -name "models--*" -type d 2>/dev/null | head -1)
    if [ -n "$cache_dir" ]; then
        # models--openai--gpt-oss-120b → openai/gpt-oss-120b
        basename "$cache_dir" | sed 's/^models--//; s/--/\//'
        return
    fi

    # No processor — guidellm will auto-detect from /v1/models (may fail for
    # unknown models).
    echo ""
}

# Run the full benchmark sequence. Any failure causes return 1, which keeps
# BENCHMARK_TOTAL_TPM at its -1 default value.
_run_benchmark() {
    local t0_gen t0_prompt t1_gen t1_prompt t0_epoch t1_epoch

    t0_gen=$(_read_counter vllm:generation_tokens_total)   || { _log "counter_read_failed metric=vllm:generation_tokens_total phase=t0"; return 1; }
    t0_prompt=$(_read_counter vllm:prompt_tokens_total)    || { _log "counter_read_failed metric=vllm:prompt_tokens_total phase=t0"; return 1; }
    t0_epoch=$(date +%s)

    local processor
    processor=$(_resolve_processor)
    _log "processor_resolved PROCESSOR=${processor:-<auto>}"

    # Run guidellm as the load generator.
    # The actual TPM measurement uses vLLM's Prometheus counters (below).
    # --profile throughput drives max concurrency up to --rate (saturating the model).
    # --data-num-workers 0 prevents subprocess workers that can fail in containers.
    # GUIDELLM__REPORT_GENERATION__SOURCE overrides the URL guidellm uses to fetch its
    # HTML report template. Without this, guidellm crashes post-benchmark with httpx
    # HTTPStatusError on a 301 redirect from blog.vllm.ai. We don't use guidellm's HTML
    # output — our TPM comes from vLLM's Prometheus counters — so a stub file is fine.
    # NOTE: prefix is GUIDELLM__ (double underscore); env_prefix="GUIDELLM__" in settings.py.
    # NOTE: cannot use /dev/null — guidellm's load_text checks Path.is_file() which is
    # False for character devices. A real regular file (even empty) is required.
    local _gl_stub
    _gl_stub=$(mktemp /tmp/guidellm-stub.XXXXXX.html)
    echo '<html></html>' > "${_gl_stub}"
    local guidellm_rc=0
    GUIDELLM__REPORT_GENERATION__SOURCE="${_gl_stub}" \
    "${GUIDELLM_BIN}" benchmark run \
        --target http://localhost:5000 \
        --profile throughput \
        --rate "${BENCHMARK_RATE}" \
        --max-seconds "${BENCHMARK_DURATION}" \
        --data "prompt_tokens=${BENCHMARK_INPUT_LEN},output_tokens=${BENCHMARK_OUTPUT_LEN}" \
        ${processor:+--processor "${processor}"} \
        --data-num-workers 0 \
        --random-seed $RANDOM \
        --disable-console \
        > /dev/null 2>&1 || guidellm_rc=$?
    rm -f "${_gl_stub}"
    if [ "${guidellm_rc}" -ne 0 ]; then
        _log "guidellm_failed rc=${guidellm_rc}"
        return 1
    fi

    t1_epoch=$(date +%s)
    t1_gen=$(_read_counter vllm:generation_tokens_total)   || { _log "counter_read_failed metric=vllm:generation_tokens_total phase=t1"; return 1; }
    t1_prompt=$(_read_counter vllm:prompt_tokens_total)    || { _log "counter_read_failed metric=vllm:prompt_tokens_total phase=t1"; return 1; }

    # Require that at least one token was generated; pure prefill with zero generation
    # means the load did not reach the model (e.g. wrong endpoint, auth failure).
    local delta_gen=$(( t1_gen - t0_gen ))
    if [ "${delta_gen}" -eq 0 ]; then
        _log "benchmark_no_generation delta_gen=0 — model produced no output tokens"
        return 1
    fi

    local elapsed_sec=$(( t1_epoch - t0_epoch ))
    _log "benchmark_window elapsed_sec=${elapsed_sec} delta_gen=${delta_gen} delta_prompt=$(( t1_prompt - t0_prompt ))"
    BENCHMARK_TOTAL_TPM=$(python3 -c "print(round(($t1_gen - $t0_gen + $t1_prompt - $t0_prompt) * 60 / float(${elapsed_sec}), 2))") \
        || { _log "tpm_calc_failed elapsed_sec=${elapsed_sec} delta_gen=${delta_gen}"; return 1; }
}

_t_bench_start=$(date +%s)
_log "benchmark_start RATE=${BENCHMARK_RATE} DURATION=${BENCHMARK_DURATION}s INPUT_LEN=${BENCHMARK_INPUT_LEN} OUTPUT_LEN=${BENCHMARK_OUTPUT_LEN}"

if ! _run_benchmark; then
    _log "benchmark_failed result will be -1"
fi

_t_bench_end=$(date +%s)
_log "benchmark_done elapsed=$(( _t_bench_end - _t_bench_start ))s"

# Wait for all in-flight benchmark requests to drain, with a 5-minute timeout.
_log "drain_start"
_drain_deadline=$(( $(date +%s) + 300 ))
until [ "$(_read_counter vllm:num_requests_running)" -eq 0 ] 2>/dev/null; do
    if [ "$(date +%s)" -ge "${_drain_deadline}" ]; then
        _log "drain_timeout exceeded 300s — proceeding anyway"
        break
    fi
    sleep 2
done
_t_drain_end=$(date +%s)
_log "drain_done elapsed=$(( _t_drain_end - _t_bench_end ))s total_phase_elapsed=$(( _t_drain_end - _t_bench_start ))s"

# Disable trap and emit explicitly to prevent double-print
trap - EXIT
_on_exit

# Stay alive for vLLM
wait "${VLLM_PID}"
