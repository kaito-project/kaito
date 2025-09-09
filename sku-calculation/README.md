# SKU Calculation Tools

This directory contains tools and documentation for calculating GPU SKU requirements and model deployment specifications.

## Files

- **`model-sku-calculation.md`**: Comprehensive guide for calculating SKU requirements, VRAM consumption, and maximum token lengths based on model configurations
- **`calculate_model_weight_and_bytes_per_token.py`**: Python utility for analyzing Hugging Face models and calculating memory requirements

## Usage

### Model Analysis Tool

```bash
python3 calculate_model_weight_and_bytes_per_token.py <model_repo> [--token=YOUR_HF_TOKEN]
```

**Example:**
```bash
python3 calculate_model_weight_and_bytes_per_token.py deepseek-ai/deepseek-r1-0528
python3 calculate_model_weight_and_bytes_per_token.py meta-llama/Llama-3.3-70B-Instruct --token=hf_your_token_here
```

**Output:**
- Model weight size in GB and GiB
- Bytes per token calculation for KV-Cache estimation
- Configuration analysis from the model's config.json

### Documentation

See [`model-sku-calculation.md`](./model-sku-calculation.md) for:
- Detailed VRAM calculation formulas
- Node count estimation methods  
- Maximum token length calculations
- GPU memory optimization strategies

## Prerequisites

- Python 3.x
- `requests` library: `pip install requests`
- Optional: Hugging Face token for private models

## Integration

These tools support the KAITO model deployment pipeline by providing accurate resource estimation for:
- GPU memory requirements
- Optimal node counts
- Token length limits
- SKU selection guidance
