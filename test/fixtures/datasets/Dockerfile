FROM busybox:latest

ARG DATASET_PATH=test/fixtures/datasets/dataset1

RUN mkdir -p /data

COPY ${DATASET_PATH}/dataset.parquet /data/