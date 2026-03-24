---
name: kaito-inference
description: Help users deploy LLM models to Kubernetes using the KAITO kubectl plugin. Use this skill whenever the user mentions deploying an LLM, AI model, or language model to Kubernetes, or asks about KAITO, kaito workspaces, GPU inference on k8s, or wants to run models like Llama, Phi, Mistral, DeepSeek, Falcon, Qwen, or Gemma on a Kubernetes cluster. Also trigger when the user mentions "kubectl kaito", model serving on k8s, or wants to set up an inference endpoint in Kubernetes — even if they don't say "KAITO" explicitly.
---

# KAITO Inference Skill

Help users deploy LLM models to Kubernetes using the `kubectl kaito` plugin. The goal is to ask the right questions, recommend a model and configuration, and produce a ready-to-run `kubectl kaito deploy` command.

## What is KAITO?

KAITO (Kubernetes AI Toolchain Operator) automates AI model inference on Kubernetes. The `kubectl kaito` plugin simplifies this by turning a few flags into a complete GPU-provisioned deployment. Users don't need to write YAML — the plugin generates the Workspace CRD automatically.

## Workflow

When a user wants to deploy a model, walk through these steps:

### 0. Check if the kubectl kaito plugin is installed

Before anything else, verify the plugin is available:

```bash
kubectl kaito --help
```

If the command fails (not found), stop and ask the user to install it first:

```bash
# Via Krew (recommended)
kubectl krew install kaito

# Or download from GitHub releases
# https://github.com/kaito-project/kaito-kubectl-plugin/releases
```

Do not proceed with deploy commands until the plugin is confirmed installed.

### 1. Understand what they need

Ask (or infer from context) these key things:

- **What model?** A specific model name, or a use case to recommend one for (e.g., "I need a small code assistant" → suggest `qwen2.5-coder-7b` or `phi-4-mini`).
- **Preset or HuggingFace?** KAITO supports official preset models (e.g., `phi-4`, `llama-3.1-8b-instruct`) and any HuggingFace model via its model card ID (e.g., `Qwen/Qwen3-4B-Instruct-2507`).
- **GPU instance type?** If the user knows their cloud environment, help pick the right SKU. Otherwise, suggest based on model size.

If the user already specified a model and enough context, skip the questions and generate the command directly.

### 2. Check the supported preset models

Use the `kubectl kaito models` command to list and inspect available preset models:

```bash
# List all supported preset models
kubectl kaito models list

# List with detailed info (instance types, GPU memory, node counts)
kubectl kaito models list --detailed

# Get full details for a specific model
kubectl kaito models describe <model-name>

# JSON output for parsing
kubectl kaito models list --output json
```

Use this to:
- Validate whether the user's requested model is a supported preset
- Look up recommended instance types, GPU memory, and node counts
- Suggest models that match the user's requirements

### 2b. Search HuggingFace Hub if model is not a preset

If the user asks for a model that doesn't exist in the KAITO preset list, search for it on HuggingFace Hub using the API:

```bash
curl -s "https://huggingface.co/api/models?search=<query>&filter=text-generation&sort=downloads&direction=-1&limit=5"
```

This returns JSON with matching models. Key fields to use:

- `id` — the full model ID to pass to `--model` (e.g., `Qwen/Qwen3-4B-Instruct-2507`)
- `downloads` — popularity indicator
- `tags` — check for relevant tags like `text-generation`, `conversational`
- `pipeline_tag` — the model's task type
- `siblings` — file list (look for `config.json` to estimate size)

**Workflow when model is not a preset:**

1. Search HuggingFace with the user's query (model name, family, or use case keywords)
2. Present the top results to the user — show the model ID, download count, and any relevant tags
3. Let the user pick, or recommend the best match
4. Generate the deploy command using the full HuggingFace model ID (e.g., `org/model-name`)
5. Remind the user they need a `--model-access-secret` if the model is gated/private

**To check model details** (size, config, gating):

```bash
curl -s "https://huggingface.co/api/models/<org>/<model-name>"
```

Look at:
- `gated` field — if truthy, the model requires a HuggingFace token (needs `--model-access-secret`)
- `safetensors.total` — total parameter count in bytes, useful for GPU sizing
- `cardData.license` — license info to mention to the user

**GPU sizing from parameter count:**
- ≤ 3B params → `Standard_NC6s_v3` (1× V100 16GB)
- 4B–7B params → `Standard_NC6s_v3` (16GB) or `Standard_NC24ads_A100_v4` (80GB) for faster inference
- 8B–14B params → `Standard_NC24ads_A100_v4` (1× A100 80GB)
- 30B–40B params → `Standard_NC48ads_A100_v4` (2× A100 160GB)
- 70B+ params → `Standard_NC96ads_A100_v4` (4× A100 320GB)

### 3. Generate the deploy command

Build the `kubectl kaito deploy` command with the appropriate flags.

**Minimal command (preset model):**
```bash
kubectl kaito deploy \
  --workspace-name <name> \
  --model <model-name> \
  --instance-type <gpu-sku>
```

**HuggingFace model (needs access secret):**
```bash
kubectl kaito deploy \
  --workspace-name <name> \
  --model <org/model-name> \
  --instance-type <gpu-sku> \
  --model-access-secret <secret-name>
```

**Full flag reference:**

| Flag | Purpose | When to use |
|------|---------|-------------|
| `--workspace-name` | Name for the Workspace resource | Always (required) |
| `--model` | Model name or HuggingFace ID | Always (required) |
| `--instance-type` | GPU VM SKU (e.g., `Standard_NC24ads_A100_v4`) | When auto-provisioning is on |
| `--count` | Number of GPU nodes (default: 1) | Large models needing multi-node |
| `--model-access-secret` | K8s secret with HuggingFace token | Private/gated HuggingFace models |
| `--inference-config` | ConfigMap name or path to YAML config | Custom vLLM/runtime params |
| `--adapters` | LoRA adapters to load | When using fine-tuned adapters |
| `--enable-load-balancer` | Create external LoadBalancer | When external access is needed |
| `--dry-run` | Show config without deploying | When user wants to preview |
| `--namespace` | Target namespace | When not using default namespace |

### 4. Explain what happens next

After giving the command, briefly tell the user:

- The plugin creates a `Workspace` custom resource
- KAITO operator provisions GPU nodes and deploys the model
- They can check progress with: `kubectl kaito status --workspace-name <name> --watch`
- Once ready, get the endpoint: `kubectl kaito get-endpoint --workspace-name <name>`
- They can chat interactively: `kubectl kaito chat --workspace-name <name>`

## Common instance types (Azure)

These are typical Azure GPU SKUs — adjust if the user is on a different cloud:

| SKU | GPUs | GPU Memory | Good for |
|-----|------|------------|----------|
| `Standard_NC6s_v3` | 1× V100 | 16 GB | Small models (≤7B params) |
| `Standard_NC24ads_A100_v4` | 1× A100 | 80 GB | Medium models (7B–14B) |
| `Standard_NC48ads_A100_v4` | 2× A100 | 160 GB | Large models (30B–40B) |
| `Standard_NC96ads_A100_v4` | 4× A100 | 320 GB | Very large models (70B+) |

## Inference configuration

If the user needs custom inference parameters (e.g., max sequence length, GPU memory utilization), help them create a config YAML:

```yaml
vllm:
  gpu-memory-utilization: 0.95
  max-model-len: 16384
  swap-space: 4
```

Then reference it in the deploy command: `--inference-config config.yaml`

The plugin automatically creates a ConfigMap named `{workspace-name}-inference-config`.

## Prerequisites reminder

If the user seems early in their setup, mention these requirements:
- Kubernetes cluster with GPU nodes (or auto-provisioning enabled)
- KAITO operator installed (`helm install kaito/workspace`)
- `kubectl-kaito` plugin installed (via `kubectl krew install kaito` or built from source)
- For HuggingFace models: a K8s secret with the HF token

Only mention prerequisites that seem relevant — don't dump the full list on someone who clearly already has things set up.

## Key principles

- Be concise: generate the command and explain only what's needed.
- Be practical: if the user gives enough info, skip questions and go straight to the command.
- Be helpful with model selection: if they describe a use case, recommend a specific model with reasoning.
- Always validate model names against the fetched supported models list when possible.
- Prefer `--dry-run` when the user is exploring or unsure — it's safe and shows what would happen.
