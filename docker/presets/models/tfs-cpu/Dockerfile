# syntax=docker/dockerfile:1
FROM python:3.12-slim-bookworm

ARG MODEL_TYPE
ARG VERSION

WORKDIR /workspace

RUN pip3 install --no-cache-dir --upgrade pip && \
    apt-get update -y && apt-get upgrade -y && \
    if dpkg -s libssl3t64 >/dev/null 2>&1; then \
        apt-get install --only-upgrade --no-install-recommends -y openssl libssl3t64 openssl-provider-legacy; \
    elif dpkg -s libssl3 >/dev/null 2>&1; then \
        apt-get install --only-upgrade --no-install-recommends -y openssl libssl3; \
    else \
        apt-get install --only-upgrade --no-install-recommends -y openssl; \
    fi && \
    apt-get install --no-install-recommends -y gcc g++ libc-dev curl && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

COPY presets/workspace/dependencies/requirements-cpu.txt /workspace/requirements-cpu.txt
RUN --mount=type=cache,target=/root/.cache/pip \
    pip3 install --no-cache-dir -r /workspace/requirements-cpu.txt

COPY presets/workspace/inference/${MODEL_TYPE}/inference_api.py /workspace/tfs/inference_api.py
ADD presets/workspace/inference/chat_templates /workspace/chat_templates

RUN mkdir -p /workspace/weights && \
    ln -s /workspace/weights /workspace/tfs/weights && \
    echo $VERSION > /workspace/version.txt
