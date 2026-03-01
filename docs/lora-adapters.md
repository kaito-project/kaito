# Using LoRA Adapters with KAITO

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

1. **Fine-tuning** — Use a `Workspace` with `tuning.method: qlora` to produce adapter weights
2. **Inference** — Use `inference.adapters[]` to attach one or more pre-built adapter images to a base model

---

## Prerequisites

- KAITO operator installed ([installation guide](https://kaito-project.github.io/kaito/docs/installation))
- `kubectl` configured for your cluster
- A container registry (e.g., Azure Container Registry) for storing adapter images
- (For fine-tuning) A training dataset accessible via URL or PersistentVolumeClaim

---

## Step 1: Fine-Tune a Model with QLoRA

KAITO supports QLoRA (Quantized LoRA) fine-tuning through the `Workspace` CRD. The tuning job trains adapter weights and pushes them as a container image to your registry.

### 1a. Create a Tuning Configuration (Optional)

For advanced control over hyperparameters, create a `ConfigMap`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-lora-config
data:
  training_config.yaml: |
    training_config:
      ModelConfig:
        torch_dtype: "bfloat16"
        local_files_only: true
        device_map: "auto"

      QuantizationConfig:
        load_in_4bit: true
        bnb_4bit_quant_type: "nf4"
        bnb_4bit_compute_dtype: "bfloat16"
        bnb_4bit_use_double_quant: true

      LoraConfig:
        r: 8                    # Low-rank dimension
        lora_alpha: 8           # Scaling factor
        lora_dropout: 0.0
        target_modules:         # Layers to apply adapters to
          - "q_proj"
          - "k_proj"
          - "v_proj"
          - "o_proj"

      TrainingArguments:
        output_dir: "/mnt/results"
        save_strategy: "epoch"
        per_device_train_batch_size: 2

      DataCollator:
        mlm: true

      DatasetConfig:
        shuffle_dataset: true
        train_test_split: 1
```

```sh
kubectl apply -f my-lora-config.yaml
```

### 1b. Create the Tuning Workspace

**Option A: Training data from a URL**

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

**Option B: Training data from a PersistentVolumeClaim**

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-tuning-phi-3
resource:
  instanceType: "Standard_NC6s_v3"
  labelSelector:
    matchLabels:
      app: tuning-phi-3
tuning:
  preset:
    name: phi-3-mini-128k-instruct
  method: qlora
  input:
    volumeSource:
      persistentVolumeClaim:
        claimName: pvc-training-data
  output:
    volumeSource:
      persistentVolumeClaim:
        claimName: pvc-adapter-output
```

```sh
kubectl apply -f workspace-tuning.yaml
```

### 1c. Monitor the Tuning Job

```sh
# Check workspace status
kubectl get workspace workspace-tuning-phi-3

# Watch training logs
kubectl logs -l app=tuning-phi-3 -f
```

When the workspace `STATE` becomes `Ready` and `JOBSTARTED` / `WORKSPACESUCCEEDED` are `True`, the adapter weights have been saved to the configured output destination.

---

## Step 2: Deploy Inference with Adapters

Once you have an adapter image (from fine-tuning or a pre-built image), attach it to a base model workspace:

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
      strength: "1.0"
```

```sh
kubectl apply -f workspace-inference-adapter.yaml
```

Key fields:

| Field | Description |
|-------|-------------|
| `adapters[].source.name` | A unique name for the adapter |
| `adapters[].source.image` | Container image containing the adapter weights |
| `adapters[].strength` | Adapter influence (0.0 = disabled, 1.0 = full strength) |

---

## Step 3: Test the Inference Endpoint

```sh
# Get the service endpoint
export CLUSTERIP=$(kubectl get svc workspace-phi-3-adapter -o jsonpath="{.spec.clusterIPs[0]}")

# List available models
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- \
  curl -s http://$CLUSTERIP/v1/models | jq

# Send an inference request
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- \
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

You can attach multiple LoRA adapters to the same base model. Use the `strength` field to control each adapter's influence:

```yaml
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

### Fine-Tuning Methods

| Method | Description |
|--------|-------------|
| `qlora` | QLoRA — 4-bit quantized base model + LoRA adapters (recommended, lower memory) |

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

### Out of memory during fine-tuning

```
CUDA out of memory
```

**Fix:** Try:
- Use a GPU instance with more VRAM (e.g., `Standard_NC24ads_A100_v4`)
- Reduce `per_device_train_batch_size` in your ConfigMap
- Ensure `load_in_4bit: true` is set in `QuantizationConfig`

### Adapter not loading during inference

If the model responds as if no adapter is attached:
- Verify the adapter image contains valid PEFT weight files
- Check `strength` is not set to `"0.0"`
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
