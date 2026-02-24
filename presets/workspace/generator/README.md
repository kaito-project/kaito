# Tools and documentation

This directory contains tools and documentation for calculating GPU SKU requirements and model deployment specifications, generating model architecture list supported by specific vLLM version.

## Files

- **`model-sku-calculation.md`**: Comprehensive guide for calculating SKU requirements, VRAM consumption, and maximum token lengths based on model configurations
- **`preset_generator.py`**: Python utility for generating Kaito model preset configurations by analyzing Hugging Face models
- **`gen_model_arch_list.sh`**: Bash script for generating model architecture list supported by specific vLLM version.

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

 - Run a vLLM inference workload in KAITO that is not supported by current vLLM version (e.g. `Alibaba-NLP/gte-multilingual-reranker-base`), and then get the supported model architecture list from the error message of inference pod logs
 - Save the model architecture list as a file, e.g. `model_arch_v0.14.1.txt`
   > Note: the file format is like `'AfmoeForCausalLM', 'ApertusForCausalLM', 'AquilaModel'` ...
 - Run following command to update the `vLLMModelArchMap` in `presets/workspace/models/vllm_model_arch_list.go`
   ```sh
   presets/workspace/generator/gen_model_arch_list.sh model_arch_v0.14.1.txt
   ```
   Then you will get the updated vLLM model architecture list supported by current vLLM version.

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
