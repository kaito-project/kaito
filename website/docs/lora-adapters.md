---
title: LoRA Adapters
---

This guide walks through the end-to-end workflow for using [LoRA](https://arxiv.org/abs/2106.09685) (Low-Rank Adaptation) adapters with KAITO — from fine-tuning a base model to deploying inference with one or more adapters attached.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Step 1: Fine-Tune a Model with QLoRA](#step-1-fine-tune-a-model-with-qlora)
- [Step 2: Deploy Inference with Adapters](#step-2-deploy-inference-with-adapters)
- [Step 3: Test the Inference Endpoint](#step-3-test-the-inference-endpoint)
- [Using Multiple Adapters](#using-multiple-adapters)
- [Adapter Configuration Reference](#adapter-configuration-reference)
- [Troubleshooting](#troubleshooting)

---

## Overview

LoRA adapters allow you to customize a pre-trained model's behavior without modifying the base weights. This is useful for:

- **Domain adaptation** — Specialize a general model for medical, legal, or code tasks
- **Cost efficiency** — Train only a small number of parameters (typically < 1% of the model)
- **Hot-swapping** — Attach different adapters to the same base model for different use cases

KAITO supports LoRA adapters in two ways:

1. **Fine-tuning** — Use a `Workspace` with `tuning.method: qlora` (or `lora`) to produce adapter weights
2. **Inference** — Use `inference.adapters[]` to attach one or more pre-built adapter images to a base model

---

## Prerequisites

- KAITO operator installed ([installation guide](https://kaito-project.github.io/kaito/docs/installation))
- `kubectl` configured for your cluster
- A container registry (e.g., Azure Container Registry) for storing adapter images
- (For fine-tuning) A training dataset accessible via URL or PersistentVolumeClaim
- (Optional, for curl examples) [`jq`](https://stedolan.github.io/jq/) installed for JSON parsing on the command line

---

## Step 1: Fine-Tune a Model with QLoRA

KAITO supports QLoRA (Quantized LoRA) and standard LoRA fine-tuning through the `Workspace` CRD. The tuning job trains adapter weights and pushes them as a container image to your registry. For full details on fine-tuning configuration (custom hyperparameters, ConfigMaps, etc.), see the [Tuning Guide](https://kaito-project.github.io/kaito/docs/tuning).

### Create the Tuning Workspace

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-tuning-phi-3
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      app: tuning-phi-3
tuning:
  preset:
    name: phi-3-mini-128k-instruct
  method: qlora
  input:
    urls:
      - "https://huggingface.co/datasets/philschmid/dolly-15k-oai-style/resolve/main/data/train-00000-of-00001-54e3756291ca09c6.parquet?download=true"
  output:
    image: "<YOUR_ACR>.azurecr.io/phi-3-adapter:0.0.1"
    imagePushSecret: <YOUR_ACR_SECRET>
```

```sh
kubectl apply -f workspace-tuning.yaml
```

### Monitor the Tuning Job

```sh
# Check workspace status
kubectl get workspace workspace-tuning-phi-3

# Watch training logs
kubectl logs -l app=tuning-phi-3 -f
```

When the workspace `STATE` becomes `Succeeded`, the adapter weights have been saved to the configured output destination.

---

## Step 2: Deploy Inference with Adapters

Once you have an adapter image (from fine-tuning or a pre-built image), attach it to a base model workspace.

> **Runtime note:** The `strength` field and multiple adapter support require the **Transformers** runtime. If your cluster uses the **vLLM** runtime (default when the vLLM feature gate is enabled), vLLM will reject Workspaces that set `strength`. To use adapter strength, add the annotation `kaito.sh/runtime: transformers` to your Workspace metadata. If using vLLM, omit the `strength` field entirely.

**Example with Transformers runtime (supports `strength`):**

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-3-adapter
  annotations:
    kaito.sh/runtime: transformers
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: phi-3-adapter
inference:
  preset:
    name: phi-3-mini-128k-instruct
  adapters:
    - source:
        name: "my-lora-adapter"
        image: "<YOUR_ACR>.azurecr.io/phi-3-adapter:0.0.1"
      strength: "1.0"
```

**Example with vLLM runtime (omit `strength`):**

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-3-adapter
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: phi-3-adapter
inference:
  preset:
    name: phi-3-mini-128k-instruct
  adapters:
    - source:
        name: "my-lora-adapter"
        image: "<YOUR_ACR>.azurecr.io/phi-3-adapter:0.0.1"
```

```sh
kubectl apply -f workspace-inference-adapter.yaml
```

Key fields:

| Field | Description |
|-------|-------------|
| `adapters[].source.name` | A unique name for the adapter |
| `adapters[].source.image` | Container image containing the adapter weights |
| `adapters[].strength` | Adapter influence (0.0–1.0). **Transformers runtime only** — omit for vLLM |

---

## Step 3: Test the Inference Endpoint

```sh
# Get the service endpoint
export CLUSTERIP=$(kubectl get svc workspace-phi-3-adapter -o jsonpath="{.spec.clusterIPs[0]}")

# List available models
kubectl run -it --rm --restart=Never curl --image=curlimages/curl:8.7.1 -- \
  curl -s http://$CLUSTERIP/v1/models | jq

# Send an inference request
kubectl run -it --rm --restart=Never curl --image=curlimages/curl:8.7.1 -- \
  curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-3-mini-128k-instruct",
    "messages": [{"role": "user", "content": "Summarize the key points of contract law."}],
    "max_tokens": 200,
    "temperature": 0.7
  }'
```

---

## Using Multiple Adapters

You can attach multiple LoRA adapters to the same base model. Use the `strength` field to control each adapter's influence.

> **Important:** Multiple adapters with `strength` require the **Transformers** runtime. Add `metadata.annotations: { kaito.sh/runtime: transformers }` to your Workspace. If using vLLM, omit the `strength` field.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-falcon-multi-adapter
  annotations:
    kaito.sh/runtime: transformers
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: falcon-7b-adapter
inference:
  preset:
    name: "falcon-7b"
  adapters:
    - source:
        name: "legal-adapter"
        image: "<YOUR_ACR>.azurecr.io/falcon-legal:0.0.1"
      strength: "0.8"
    - source:
        name: "summarization-adapter"
        image: "<YOUR_ACR>.azurecr.io/falcon-summarize:0.0.1"
      strength: "0.5"
```

> **Note:** When using multiple adapters, keep the total combined strength reasonable. Very high combined strengths may degrade output quality.

---

## Adapter Configuration Reference

### Supported Base Models

LoRA adapters can be used with any KAITO preset model that supports adapter injection. Refer to the [supported models list](https://kaito-project.github.io/kaito/docs/presets) for details.

### Adapter Image Format

The adapter container image should contain the LoRA weight files (typically `adapter_model.safetensors` and `adapter_config.json`) produced by PEFT/Hugging Face training. When using KAITO's built-in tuning (Step 1), this image is created automatically.

### Runtime Compatibility

| Feature | Transformers | vLLM |
|---------|-------------|------|
| Basic adapter loading | ✅ | ✅ |
| `strength` field | ✅ | ❌ Rejected |
| Multiple adapters | ✅ | Limited |

To select the Transformers runtime, add `kaito.sh/runtime: transformers` to your Workspace annotations.

### Fine-Tuning Methods

| Method | Description | GPU Memory |
|--------|-------------|------------|
| `qlora` | QLoRA — 4-bit quantized base model + LoRA adapters (recommended) | Lower — fits on smaller GPUs |
| `lora` | Standard LoRA — full-precision base model + LoRA adapters | Higher — requires more VRAM |

> **Recommendation:** Use `qlora` unless you have a specific reason to use full-precision LoRA, as it significantly reduces GPU memory requirements with minimal quality loss.

### GPU Instance Type Guidelines

| Instance Type | GPU | VRAM | Recommended For |
|--------------|-----|------|------------------|
| `Standard_NC24ads_A100_v4` | A100 | 80 GB | Most fine-tuning and inference workloads |
| `Standard_NV36ads_A10_v5` | A10 | 24 GB | QLoRA tuning with smaller models (< 7B params) |

> Choose your instance type based on the model size and quantization method. Larger models and full LoRA require more VRAM.

### Tuning Output Options

| Output Type | Config Field | Description |
|------------|--------------|-------------|
| Container image | `output.image` + `output.imagePushSecret` | Pushes adapter weights as a container image to your registry |
| PVC volume | `output.volumeSource` | Saves adapter weights to a PersistentVolumeClaim |

---

## Troubleshooting

### Adapter image pull errors

```
Failed to pull image "<YOUR_ACR>.azurecr.io/adapter:0.0.1": unauthorized
```

**Fix:** Ensure your cluster has an `imagePullSecret` configured for your container registry, or that the node's managed identity has `AcrPull` permissions.

### Workspace validation fails with `strength` field

```
admission webhook rejected: strength is not supported with vLLM runtime
```

**Fix:** Either:
- Add `metadata.annotations: { kaito.sh/runtime: transformers }` to use the Transformers runtime, or
- Remove the `strength` field from all adapter entries to use vLLM

### Out of memory during fine-tuning

```
CUDA out of memory
```

**Fix:** Try:
- Use a GPU instance with more VRAM (e.g., `Standard_NC24ads_A100_v4`)
- Reduce `per_device_train_batch_size` in your ConfigMap
- Ensure `load_in_4bit: true` is set in `QuantizationConfig` (use `qlora` method)
- Switch from `lora` to `qlora` method to reduce memory requirements

### Adapter not loading during inference

If the model responds as if no adapter is attached:
- Verify the adapter image contains valid PEFT weight files
- Check `strength` is not set to `"0.0"` (Transformers runtime only)
- Inspect pod logs: `kubectl logs -l apps=<your-label>`

### Tuning job not completing

```sh
# Check workspace status
kubectl get workspace <name> -o yaml

# Check pod events
kubectl describe pod -l app=<your-tuning-label>
```

Common causes: insufficient GPU memory, dataset download failures, or registry push authentication issues.

---

## Further Reading

- [KAITO Inference Presets](https://kaito-project.github.io/kaito/docs/presets)
- [KAITO Tuning Guide](https://kaito-project.github.io/kaito/docs/tuning)
- [KAITO Inference Guide](https://kaito-project.github.io/kaito/docs/inference)
- [LoRA Paper](https://arxiv.org/abs/2106.09685)
- [QLoRA Paper](https://arxiv.org/abs/2305.14314)
- [PEFT Library](https://github.com/huggingface/peft)
