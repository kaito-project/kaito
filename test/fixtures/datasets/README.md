# E2E Fine-Tuning Dataset Files

## Overview

The dataset files are used for conducting end-to-end (E2E) testing for fine-tuning. The Dockerfile builds an image incorporating the [dolly-15k-oai-style](https://huggingface.co/datasets/philschmid/dolly-15k-oai-style) and [kubernetes-reformatted-remove-outliers](https://huggingface.co/datasets/ishaansehgal99/kubernetes-reformatted-remove-outliers) datasets, which are then used within init containers specifically for fine-tuning. 

## Files

- **Dockerfile**: Builds the Docker image for the E2E tests.

- **dataset.parquet**: The datasets themselves, downloaded from [dolly-15k-oai-style](https://huggingface.co/datasets/philschmid/dolly-15k-oai-style) and [kubernetes-reformatted-remove-outliers](https://huggingface.co/datasets/ishaansehgal99/kubernetes-reformatted-remove-outliers)


## Usage

Build the Docker images with the following command:

```bash

make docker-build-dataset
 