# Model Downloader and Dockerfile Generator

This script automates the process of downloading machine learning models from Hugging Face and generating a Dockerfile to build an image containing these models.

## Description

The script performs two main functions:

1.  **Model Download**: It downloads a specified model from Hugging Face into a designated directory. It uses a Docker container with `huggingface_hub` to perform the download, which helps in managing dependencies and ensuring a consistent download environment.
2.  **Dockerfile Generation**: It generates a new Dockerfile based on a template (`Dockerfile` located in the same directory as the script). This generated Dockerfile includes instructions to copy the downloaded model files into the Docker image. It optimizes the COPY instructions by separating large files from smaller ones to improve Docker layer caching.

## Prerequisites

-   Docker must be installed and running on your system.

## Usage

To use the script, you need to set the following environment variables before execution:

-   `MODEL_NAME`: (Required) A user-friendly name for the model. This name will be used in the generated Dockerfile name (e.g., `Dockerfile_<MODEL_NAME>`).
-   `MODEL_VERSION`: (Required) The Hugging Face model identifier URL. This can be a URL to the model repository, a specific commit, branch, or tag.
    -   Examples:
        -   `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3` (defaults to `main` branch)
        -   `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/commit/e0bc86c23ce5aae1db576c8cca6f06f1f73af2db`
        -   `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/tree/my-branch`
        -   `https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/tag/v0.1`
-   `WEIGHTS_DIR`: (Optional) The directory where the model weights will be downloaded. Defaults to `/tmp/`.
-   `HF_TOKEN`: (Optional) Your Hugging Face access token. Required for downloading private or gated models.

### Running the script:

```bash
export MODEL_NAME="Mistral-7B-Instruct-v0.3"
export MODEL_VERSION="https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3"
export WEIGHTS_DIR="./my_models" # Optional
# export HF_TOKEN="your_hugging_face_token" # Optional, if needed

python3 download_and_gen_dockerfile.py
```

## Output

-   Model files downloaded into the specified `WEIGHTS_DIR`.
-   A new Dockerfile named `Dockerfile_<MODEL_NAME>` in the current directory where the script was executed.

## Example

If you run the script with:

```bash
export MODEL_NAME="MyMistral"
export MODEL_VERSION="https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3/commit/e0bc86c23ce5aae1db576c8cca6f06f1f73af2db"
export WEIGHTS_DIR="/data/models/mistral"

python3 download_and_gen_dockerfile.py
```

The script will:
1. Download `mistralai/Mistral-7B-Instruct-v0.3` at commit `e0bc86c...` to `/data/models/mistral/`.
2. Generate a Dockerfile named `Dockerfile_MyMistral` in your current directory, which includes instructions to copy the model files from `/data/models/mistral/` into the image.

You can then use this `Dockerfile_MyMistral` to build your Docker image:
```bash
docker buildx build -t my-custom-model-image \
    --build-context modeldir=/data/models/mistral \
    --build-arg VERSION=0.1 \
    --build-arg MODEL_TYPE=text-generation \
    -f Dockerfile_MyMistral .
```
