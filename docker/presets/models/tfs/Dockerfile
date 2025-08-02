FROM python:3.12-slim AS dependencies

ARG MODEL_TYPE
ARG VERSION

# https://docs.ray.io/en/latest/cluster/usage-stats.html
ENV RAY_USAGE_STATS_ENABLED="1"

# Set the working directory
WORKDIR /workspace

# Required for torch.compile
RUN apt-get update -y && apt-get install --no-install-recommends curl gcc libc-dev perl -y && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

FROM dependencies AS base

COPY presets/workspace/dependencies/requirements.txt /workspace/requirements.txt

RUN pip install --no-cache-dir -q -r /workspace/requirements.txt

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
    /workspace/vllm/

RUN VLLM_VERSION=$(grep 'vllm==' /workspace/requirements.txt | cut -d'=' -f3) && \
    curl -o /workspace/vllm/multi-node-serving.sh \
    https://raw.githubusercontent.com/vllm-project/vllm/refs/tags/v${VLLM_VERSION}/examples/online_serving/multi-node-serving.sh && \
    chmod +x /workspace/vllm/multi-node-serving.sh

# Chat template
ADD presets/workspace/inference/chat_templates /workspace/chat_templates

RUN ln -s /workspace/weights /workspace/tfs/weights && \
    ln -s /workspace/weights /workspace/vllm/weights

RUN echo $VERSION > /workspace/version.txt
