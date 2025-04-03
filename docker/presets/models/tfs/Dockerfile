ARG CUDA_VERSION=12.4.1
ARG BASE_IMAGE=nvidia/cuda:${CUDA_VERSION}-devel-ubuntu22.04
FROM ${BASE_IMAGE} AS base

FROM nvidia/cuda:12.4.1-devel-ubuntu22.04

ARG WEIGHTS_PATH
ARG MODEL_TYPE
ARG VERSION

ARG PYTHON_VERSION=3.12

# Set the working directory
WORKDIR /workspace
ENV DEBIAN_FRONTEND=noninteractive
ENV PATH="/root/.local/bin:$PATH"

# Install Python
RUN echo 'tzdata tzdata/Areas select America' | debconf-set-selections && \
    echo 'tzdata tzdata/Zones/America select Los_Angeles' | debconf-set-selections && \
    apt-get -qq update && \
    apt-get -qq install -y --no-install-recommends \
    software-properties-common && \
    add-apt-repository -y ppa:deadsnakes/ppa && \
    apt-get -qq update && \
    apt-get -qq install -y python${PYTHON_VERSION} python${PYTHON_VERSION}-dev python${PYTHON_VERSION}-venv && \
    update-alternatives --install /usr/bin/python3 python3 /usr/bin/python${PYTHON_VERSION} 1 && \
    update-alternatives --set python3 /usr/bin/python${PYTHON_VERSION}

# Install other dependencies
RUN apt-get -qq update && \
    apt-get -qq install -y --no-install-recommends \
    curl \
    libibverbs-dev \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Model weights
COPY ${WEIGHTS_PATH} /workspace/weights

COPY kaito/presets/workspace/dependencies/requirements.txt /workspace/requirements.txt

RUN --mount=type=cache,target=/root/.cache/uv \
    curl -LsSf https://astral.sh/uv/install.sh | sh && \
    uv pip install --system -r /workspace/requirements.txt

# Required for torch.compile
RUN apt-get update -y && apt-get install gcc -y && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# 1. Huggingface transformers
# COPY presets/workspace/inference/${MODEL_TYPE}/inference_api.py \
#     presets/workspace/tuning/${MODEL_TYPE}/cli.py \
#     presets/workspace/tuning/${MODEL_TYPE}/fine_tuning.py \
#     presets/workspace/tuning/${MODEL_TYPE}/parser.py \
#     presets/workspace/tuning/${MODEL_TYPE}/dataset.py \
#     presets/workspace/tuning/${MODEL_TYPE}/metrics/metrics_server.py \
#     /workspace/tfs/

# 2. vLLM
COPY kaito/presets/workspace/inference/vllm/inference_api.py /workspace/vllm/inference_api.py
# Convenient script to run multi-node serving
RUN curl https://raw.githubusercontent.com/vllm-project/vllm/refs/heads/v0.8.1/examples/online_serving/multi-node-serving.sh -o /workspace/vllm/multi-node-serving.sh && \
    chmod +x /workspace/vllm/multi-node-serving.sh

# Chat template
ADD kaito/presets/workspace/inference/chat_templates /workspace/chat_templates

RUN echo $VERSION > /workspace/version.txt && \
    # ln -s /workspace/weights /workspace/tfs/weights && \
    ln -s /workspace/weights /workspace/vllm/weights
