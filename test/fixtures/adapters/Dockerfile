FROM busybox:latest

ARG ADAPTER_PATH=docker/adapters/adapter1

RUN mkdir -p /data

COPY ${ADAPTER_PATH}/adapter_config.json /data/
COPY ${ADAPTER_PATH}/adapter_model.safetensors /data/