# syntax=docker/dockerfile:1
# Pinned to Debian bookworm (glibc 2.36). The default python:3.12-slim floats to
# Debian trixie (glibc 2.41), whose C23 cospi/sinpi math declarations are
# incompatible with CUDA 12.9's nvcc host-compile (used by the optional runtime
# CUDA toolkit install for DeepGEMM). bookworm avoids that conflict.
FROM python:3.12-slim-bookworm AS dependencies

ARG MODEL_TYPE
ARG VERSION

# https://docs.ray.io/en/latest/cluster/usage-stats.html
ENV RAY_USAGE_STATS_ENABLED="1"

RUN pip3 install --no-cache-dir --upgrade pip && \
    rm -f /usr/local/lib/python3.12/site-packages/pip/_vendor/bom.cdx.json

# Set the working directory
WORKDIR /workspace

# Required for torch.compile
RUN apt-get update -y && apt upgrade -y && \
    if dpkg -s libssl3t64 >/dev/null 2>&1; then \
        apt-get install --only-upgrade --no-install-recommends -y openssl libssl3t64 openssl-provider-legacy; \
    elif dpkg -s libssl3 >/dev/null 2>&1; then \
        apt-get install --only-upgrade --no-install-recommends -y openssl libssl3; \
    else \
        apt-get install --only-upgrade --no-install-recommends -y openssl; \
    fi && \
    apt-get install --no-install-recommends -y curl gcc g++ libc-dev perl libglib2.0-0 libgl1 libsm6 libxext6 libxrender1 && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

FROM dependencies AS base

ARG UV_VERSION=0.10.10

COPY presets/workspace/dependencies/requirements.txt /workspace/requirements.txt

# Install the CUDA 12.9 build of the stack (vLLM, torch, LMCache) instead of the
# default CUDA 13 wheels, because CUDA 13 requires 580+ Nvidia GPU drivers.
RUN --mount=type=cache,target=/root/.cache/pip \
    pip3 install --no-cache-dir "uv==${UV_VERSION}" && \
    VLLM_VERSION=$(grep 'vllm==' /workspace/requirements.txt | cut -d'=' -f3 | tr -d ' ') && \
    LMCACHE_VERSION=$(grep 'lmcache==' /workspace/requirements.txt | cut -d'=' -f3 | tr -d ' ') && \
    uv pip install --system -q -r /workspace/requirements.txt \
        --torch-backend=cu129 \
        --index-strategy unsafe-best-match \
        --extra-index-url "https://wheels.vllm.ai/${VLLM_VERSION}/cu129" \
        --extra-index-url https://download.pytorch.org/whl/cu129 \
        --find-links "https://github.com/LMCache/LMCache/releases/expanded_assets/v${LMCACHE_VERSION}-cu129" && \
    : "requirements.txt pins nixl-cu12 (the CUDA-12 NIXL bindings), installed above. vLLM's" \
      "NixlConnector (P/D disaggregation, used by MultiRoleInference prefill/decode roles)" \
      "imports the top-level 'nixl' module (nixl._api), which nixl-cu12 does NOT provide" \
      "(it ships 'nixl_cu12'/'nixl_ep_cu12'). Install the matching pure-python 'nixl'" \
      "dispatcher with --no-deps so it reuses the pinned cu12 bindings rather than also" \
      "pulling nixl-cu13 (which links libcudart.so.13, absent on this CUDA 12.9 image)." \
      "Without the dispatcher, prefill/decode pods crash at engine init with 'NIXL is not available'." && \
    NIXL_VERSION=$(grep 'nixl-cu12==' /workspace/requirements.txt | cut -d'=' -f3 | tr -d ' ') && \
    uv pip install --system -q --no-deps "nixl==${NIXL_VERSION}" && \
    pip3 uninstall -y uv

# 1. Huggingface transformers
COPY presets/workspace/inference/${MODEL_TYPE}/inference_api.py \
    presets/workspace/tuning/${MODEL_TYPE}/cli.py \
    presets/workspace/tuning/${MODEL_TYPE}/fine_tuning.py \
    presets/workspace/tuning/${MODEL_TYPE}/parser.py \
    presets/workspace/tuning/${MODEL_TYPE}/dataset.py \
    presets/workspace/tuning/${MODEL_TYPE}/metrics/metrics_server.py \
    /workspace/tfs/

# 2. vLLM
COPY presets/workspace/inference/vllm/inference_api.py \
    presets/workspace/inference/vllm/multi-node-health-check.py \
    presets/workspace/inference/vllm/list_supported_llm_archs.py \
    presets/workspace/inference/vllm/benchmark_entrypoint.py \
    presets/workspace/inference/vllm/rate_limit.py \
    presets/workspace/inference/vllm/export_sas_token_for_streaming.sh \
    /workspace/vllm/

RUN chmod +x /workspace/vllm/export_sas_token_for_streaming.sh

# Runtime CUDA toolkit provisioner. Invoked by the cuda-toolkit-provisioner init
# container for models that require DeepGEMM (nvcc JIT), to install cuda-toolkit-12-9
# into a node hostPath for runtimes that JIT-compile with nvcc.
COPY docker/presets/models/tfs/cuda-provision.sh /workspace/cuda-provision.sh
RUN chmod +x /workspace/cuda-provision.sh

RUN VLLM_VERSION=$(grep 'vllm==' /workspace/requirements.txt | cut -d'=' -f3) && \
    curl --fail -o /workspace/vllm/multi-node-serving.sh \
    https://raw.githubusercontent.com/vllm-project/vllm/refs/tags/v${VLLM_VERSION}/examples/ray_serving/multi-node-serving.sh && \
    chmod +x /workspace/vllm/multi-node-serving.sh

# Chat template
ADD presets/workspace/inference/chat_templates /workspace/chat_templates

RUN ln -s /workspace/weights /workspace/tfs/weights && \
    ln -s /workspace/weights /workspace/vllm/weights

RUN echo $VERSION > /workspace/version.txt
