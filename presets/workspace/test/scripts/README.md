# Manifest Generation Script

This script generates Kubernetes manifests for different model deployments based on templates and configurations.

## Overview

The script (`generate_manifests.py`) combines:
- Base templates from `presets/workspace/test/manifests/inference-tmpl/manifest.yaml`
- Model configurations from `.github/e2e-preset-configs.json`
- Predefined manifests from `presets/workspace/test/manifests/<model_name>/<model_name>.yaml`
to generate deployment/statefulset and service manifests for each model.

## Usage

```bash
# Print manifests to stdout
python generate_manifests.py <model_name> <runtime> [--repo REPO] [--tag TAG]
```

### Parameters:
- `model_name`: Name of the model (e.g., "mistral-7b", "falcon-40b")
- `runtime`: Runtime to use ("hf" for Hugging Face or "vllm" for vLLM)
- `--repo`: (Optional) Repository name to replace REPO in the template
- `--tag`: (Optional) Tag to replace TAG in the template

### Example:
```bash
# Generate manifest for Mistral 7B with vLLM runtime
python generate_manifests.py mistral-7b vllm

# Generate manifest for Falcon 40B with Hugging Face runtime and custom repo/tag
python generate_manifests.py falcon-40b hf --repo myregistry.azurecr.io --tag v1.0.0
```

## Configuration Files

### Template (presets/workspace/test/manifests/inference-tmpl/manifest.yaml)
Contains base templates for:
- Deployment
- Service
- ConfigMap

Placeholders:
- MODEL_NAME
- RUNTIME_COMMAND
- GPU_COUNT
- NODE_POOL
- NODE_COUNT
- REPO (can be overridden with --repo flag)
- TAG (can be overridden with --tag flag)

### Model Configurations
Located at `.github/e2e-preset-configs.json`
- Contains configurations for all supported models
- Each model configuration includes:
  - Basic info (name, node count, VM size, etc.)
  - Node pool specification
  - Runtime-specific configurations

## Example Configuration

```json
{
  "name": "mistral-7b",
  "node-count": 1,
  "node-vm-size": "Standard_NC6s_v3",
  "node-osdisk-size": 100,
  "OSS": true,
  "loads_adapter": false,
  "node_pool": "mistral7b",
  "runtimes": {
    "hf": {
      "command": "accelerate launch --num_processes 1 --num_machines 1 --machine_rank 0 --gpu_ids all /workspace/tfs/inference_api.py --pipeline text-generation --torch_dtype bfloat16",
      "gpu_count": 1
    },
    "vllm": {
      "command": "python3 /workspace/vllm/inference_api.py --served-model-name test --dtype float16 --chat-template /workspace/chat_templates/mistral-instruct.jinja",
      "gpu_count": 1
    }
  }
}
```
