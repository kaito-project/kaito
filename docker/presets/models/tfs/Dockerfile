FROM python:3.12-slim AS base

ARG MODEL_TYPE
ARG VERSION

# Set the working directory
WORKDIR /workspace

COPY presets/workspace/dependencies/requirements.txt /workspace/requirements.txt

RUN pip install --no-cache-dir -q -r /workspace/requirements.txt

# Required for torch.compile
RUN apt-get update -y && apt-get install --no-install-recommends gcc perl -y && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# 1. Huggingface transformers
COPY presets/workspace/inference/${MODEL_TYPE}/inference_api.py \
    presets/workspace/tuning/${MODEL_TYPE}/cli.py \
    presets/workspace/tuning/${MODEL_TYPE}/fine_tuning.py \
    presets/workspace/tuning/${MODEL_TYPE}/parser.py \
    presets/workspace/tuning/${MODEL_TYPE}/dataset.py \
    presets/workspace/tuning/${MODEL_TYPE}/metrics/metrics_server.py \
    /workspace/tfs/

# 2. vLLM
COPY presets/workspace/inference/vllm/inference_api.py /workspace/vllm/inference_api.py

# Chat template
ADD presets/workspace/inference/chat_templates /workspace/chat_templates

RUN echo $VERSION > /workspace/version.txt
