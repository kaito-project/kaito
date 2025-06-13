# Model Downloader and OCI Registry Pusher

This utility script downloads model files from Hugging Face Hub and pushes them to an OCI (Open Container Initiative) registry using ORAS (OCI Registry As Storage).

## Overview

The `download_and_push_model.py` script automates the process of:
1. Downloading model files from Hugging Face Hub
2. Pushing these files to an OCI-compatible registry

## Requirements

- Python 3.x
- [huggingface_hub](https://github.com/huggingface/huggingface_hub) Python package
- [ORAS CLI](https://oras.land/docs/installation) installed and available in PATH
- Optional: Hugging Face authentication token for accessing private models

## Usage

Example.

```bash
MODEL_NAME="mistral-7b-instruct" \
MODEL_VERSION="https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3" \
WEIGHTS_DIR="/path/to/temp/dir" \
HF_TOKEN="hf_your_token" \
./download_and_push_model.py my-registry.com/llm-models/mistral-7b-instruct:v0.3
```

### Environment Variables

- `MODEL_NAME` (required): Name identifier for the model
- `MODEL_VERSION` (required): URL or identifier of the Hugging Face model
  - Can be a direct URL like `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3`
  - Can include specific commits: `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/commit/e0bc86c23ce5aae1db576c8cca6f06f1f73af2db`
  - Can include branches: `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/tree/main`
- `WEIGHTS_DIR` (optional): Local directory to store downloaded model files (defaults to `/tmp/`)
- `HF_TOKEN` (optional): Hugging Face authentication token for private models

## Notes

- The script uses a custom media type `application/vnd.kaito.llm.v1` for OCI artifacts
- Hidden files in the model directory are excluded from being pushed
