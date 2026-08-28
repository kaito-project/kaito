# Tools and documentation

This directory contains the Go preset generator and documentation for calculating GPU SKU requirements and model deployment specifications.

## Files

- **`model-sku-calculation.md`**: Comprehensive guide for calculating SKU requirements, VRAM consumption, and maximum token lengths based on model configurations
- **`generator.go`**: Generates KAITO model parameters from Hugging Face metadata
- **`update_model_catalog/`**: Updates the generated model catalog

## Usage

### Preset Generator Tool

```bash
go run ./cmd/preset-generator <model_repo>
```

**Example:**
```bash
$ go run ./cmd/preset-generator microsoft/Phi-4-mini-instruct
attn_type: GQA
name: phi-4-mini-instruct
architectures:
- Phi3ForCausalLM
version: https://huggingface.co/microsoft/Phi-4-mini-instruct
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 87Gi
model_file_size_gb: 7.15
bytes_per_token: 131072
model_token_limit: 131072
reasoning_parser: ""
tool_call_parser: ""
vllm:
  model_name: phi-4-mini-instruct
  model_run_params:
    load_format: auto
    config_format: auto
    tokenizer_mode: auto
  disallow_lora: false
```

**Output:**
- A YAML configuration block for the Kaito preset, including:
  - Storage requirements
  - Compute parameters (bytes per token, model token limit)
  - VLLM parameters

### Documentation

See [`model-sku-calculation.md`](./model-sku-calculation.md) for:
- Detailed VRAM calculation formulas
- Node count estimation methods  
- Maximum token length calculations
- GPU memory optimization strategies

### Generate supported vLLM model architecture list

The supported architecture list is stored as a plain-text file
(`presets/workspace/models/vllm_model_arch_list.txt`, one name per line) and
embedded at compile time into the Go binary via `//go:embed`.

To regenerate it, run:

```sh
make generate-vllm-arch-list
```

This invokes `hack/generate_vllm_arch_list.sh`, which:

1. Reads the `base` image tag from `presets/workspace/models/base_images.yaml`
2. Runs `list_supported_llm_archs.py` inside the corresponding `kaito-base` Docker image:
   ```sh
   docker run --rm --entrypoint python3 \
     mcr.microsoft.com/aks/kaito/kaito-base:<tag> \
     /workspace/vllm/list_supported_llm_archs.py
   ```
3. Writes the output directly to `presets/workspace/models/vllm_model_arch_list.txt`

**Prerequisites:** `yq` and `docker` must be available in `PATH`.

> The base image tag is kept in sync with the `base` entry in
> `base_images.yaml`, so bumping that tag and re-running the target is
> all that is needed when upgrading vLLM.

## Prerequisites

- Go
- Optional: `HF_TOKEN` for private models

## Integration

These tools support the KAITO model deployment pipeline by providing accurate resource estimation for:
- GPU memory requirements
- Optimal node counts
- Token length limits
- SKU selection guidance
