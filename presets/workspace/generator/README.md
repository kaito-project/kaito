# Tools and documentation

This directory contains tools and documentation for calculating GPU SKU requirements and model deployment specifications, generating model architecture list supported by specific vLLM version.

## Files

- **`model-sku-calculation.md`**: Comprehensive guide for calculating SKU requirements, VRAM consumption, and maximum token lengths based on model configurations
- **`preset_generator.py`**: Python utility for generating Kaito model preset configurations by analyzing Hugging Face models

## Usage

### Preset Generator Tool

```bash
python3 preset_generator.py <model_repo> [--token=YOUR_HF_TOKEN] [--debug]
```

**Example:**
```bash
$ python3 preset_generator.py microsoft/Phi-4-mini-instruct
attn_type: GQA
name: phi-4-mini-instruct
type: tfs
version: 0.0.1
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 58Gi
model_file_size_gb: 8
bytes_per_token: 131072
model_token_limit: 131072
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

1. Reads the `base` image tag from `presets/workspace/models/supported_models.yaml`
2. Runs `list_supported_llm_archs.py` inside the corresponding `kaito-base` Docker image:
   ```sh
   docker run --rm --entrypoint python3 \
     mcr.microsoft.com/aks/kaito/kaito-base:<tag> \
     /workspace/vllm/list_supported_llm_archs.py
   ```
3. Writes the output directly to `presets/workspace/models/vllm_model_arch_list.txt`

**Prerequisites:** `yq` and `docker` must be available in `PATH`.

> The base image tag is kept in sync with the `base` entry in
> `supported_models.yaml`, so bumping that tag and re-running the target is
> all that is needed when upgrading vLLM.

## Prerequisites

- Python 3.x
- Install dependencies: `pip install -r requirements.txt`
- Optional: Hugging Face token for private models

## Integration

These tools support the KAITO model deployment pipeline by providing accurate resource estimation for:
- GPU memory requirements
- Optimal node counts
- Token length limits
- SKU selection guidance
