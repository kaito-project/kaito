# syntax=docker/dockerfile:1
FROM vllm/vllm-openai-cpu:v0.25.1-x86_64

ARG VERSION

WORKDIR /workspace

COPY presets/workspace/inference/vllm/inference_api.py \
    presets/workspace/inference/vllm/multi-node-health-check.py \
    presets/workspace/inference/vllm/list_supported_llm_archs.py \
    presets/workspace/inference/vllm/benchmark_entrypoint.py \
    presets/workspace/inference/vllm/benchmark_control.py \
    presets/workspace/inference/vllm/rate_limit.py \
    presets/workspace/inference/vllm/export_sas_token_for_streaming.sh \
    /workspace/vllm/
# Temporary upstream workarounds imported by inference_api.py (delete with the
# module once the referenced fixes land).
COPY presets/workspace/inference/vllm/_workarounds/ /workspace/vllm/_workarounds/

ADD presets/workspace/inference/chat_templates /workspace/chat_templates

RUN chmod +x /workspace/vllm/export_sas_token_for_streaming.sh && \
    mkdir -p /workspace/weights && \
    ln -sf /workspace/weights /workspace/vllm/weights && \
    echo $VERSION > /workspace/version.txt

ENV VLLM_TARGET_DEVICE=cpu
