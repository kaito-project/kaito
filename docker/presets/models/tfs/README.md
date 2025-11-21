# TFS Models Docker Preset

This directory contains the Dockerfile for building base images for TFS models.

It also contains the `build_oci_artifact.sh` script, which is used by GitHub Actions to package Hugging Face models as OCI artifacts using [AIKit](https://github.com/kaito-project/aikit).

## build_oci_artifact.sh

This script automates the process of:
1. Parsing a Hugging Face model URL to extract the repository ID and revision.
2. Building an OCI artifact using `docker buildx build` with the `aikit` syntax.
3. Pushing the artifact to an Azure Container Registry (ACR) using `oras cp`.

### Usage

```bash
./build_oci_artifact.sh <model_version_url> <image_name> <registry> <image_tag> [hf_token]
```

### Arguments

- `model_version_url`: The full URL to the Hugging Face model (e.g., `https://huggingface.co/org/repo/tree/revision`).
- `image_name`: The name of the output image (e.g., `kaito-falcon-7b`).
- `registry`: The target registry (e.g., `myregistry.azurecr.io`).
- `image_tag`: The tag for the output image (e.g., `0.0.1`).
- `hf_token`: (Optional) Hugging Face token for accessing private models.
