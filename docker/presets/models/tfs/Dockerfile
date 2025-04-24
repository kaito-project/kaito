FROM python:3.12-slim AS base

ARG MODEL_TYPE
ARG VERSION

# Set the working directory
WORKDIR /workspace

COPY kaito/presets/workspace/dependencies/requirements.txt /workspace/requirements.txt

RUN pip install --no-cache-dir -r /workspace/requirements.txt

# Required for torch.compile
RUN apt-get update -y && apt-get install --no-install-recommends gcc perl -y && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# 1. Huggingface transformers
COPY kaito/presets/workspace/inference/${MODEL_TYPE}/inference_api.py \
    kaito/presets/workspace/tuning/${MODEL_TYPE}/cli.py \
    kaito/presets/workspace/tuning/${MODEL_TYPE}/fine_tuning.py \
    kaito/presets/workspace/tuning/${MODEL_TYPE}/parser.py \
    kaito/presets/workspace/tuning/${MODEL_TYPE}/dataset.py \
    kaito/presets/workspace/tuning/${MODEL_TYPE}/metrics/metrics_server.py \
    /workspace/tfs/

# 2. vLLM
COPY kaito/presets/workspace/inference/vllm/inference_api.py /workspace/vllm/inference_api.py

# Chat template
ADD kaito/presets/workspace/inference/chat_templates /workspace/chat_templates

RUN echo $VERSION > /workspace/version.txt

FROM base AS model

ARG WEIGHTS_PATH

# Model weights
COPY ${WEIGHTS_PATH} /workspace/weights

RUN ln -s /workspace/weights /workspace/tfs/weights && \
    ln -s /workspace/weights /workspace/vllm/weights
