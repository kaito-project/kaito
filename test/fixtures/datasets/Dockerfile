FROM busybox:latest

ARG DATASET_PATH=docker/datasets/dataset1

RUN mkdir -p /data

COPY ${DATASET_PATH}/dataset.parquet /data/