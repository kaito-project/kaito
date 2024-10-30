FROM python:3.10-slim@sha256:684b1aaf96a7942b3c3af438d162e0baa3510aa7af25ad76d238e0c746bdec79

ARG WEIGHTS_PATH
ARG MODEL_TYPE
ARG VERSION

# Set the working directory
WORKDIR /workspace

COPY kaito/presets/inference/${MODEL_TYPE}/requirements.txt /workspace/tfs/inference-requirements.txt
COPY kaito/presets/tuning/${MODEL_TYPE}/requirements.txt /workspace/tfs/tuning-requirements.txt
COPY kaito/presets/inference/vllm/requirements.txt /workspace/vllm/inference-requirements.txt

RUN pip install --no-cache-dir -r /workspace/tfs/inference-requirements.txt && \
    pip install --no-cache-dir -r /workspace/tfs/tuning-requirements.txt && \
    pip install --no-cache-dir -r /workspace/vllm/inference-requirements.txt

# 1. Huggingface transformers
COPY kaito/presets/inference/${MODEL_TYPE}/inference_api.py \
    kaito/presets/tuning/${MODEL_TYPE}/cli.py \
    kaito/presets/tuning/${MODEL_TYPE}/fine_tuning.py \
    kaito/presets/tuning/${MODEL_TYPE}/parser.py \
    kaito/presets/tuning/${MODEL_TYPE}/dataset.py \
    kaito/presets/tuning/${MODEL_TYPE}/metrics/metrics_server.py \
    /workspace/tfs/

# 2. vLLM
COPY kaito/presets/inference/vllm/inference_api.py /workspace/vllm/inference_api.py

# Chat template
RUN apt update && apt install -y git && \
    rm /var/lib/apt/lists/* -r
RUN git clone https://github.com/chujiezheng/chat_templates /tmp/chat_templates && \
    cd /tmp/chat_templates && \
    git reset --hard 670a2eb && \
    cp -r ./chat_templates/ /workspace/ && \
    rm -rf /tmp/chat_templates

# Model weights
COPY ${WEIGHTS_PATH} /workspace/weights
RUN echo $VERSION > /workspace/version.txt && \
    ln -s /workspace/weights /workspace/tfs/weights && \
    ln -s /workspace/weights /workspace/vllm/weights
