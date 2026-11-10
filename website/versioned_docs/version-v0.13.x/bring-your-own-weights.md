---
title: Bring Your Own Weights
---

This document explains how to serve a model from **model weights that already exist on the GPU node** instead of downloading them from HuggingFace (or streaming them from blob storage).

## Overview

By default, a KAITO inference workload obtains a model's weights at pod startup — either by downloading them from HuggingFace or by [streaming them from blob storage](./model-mirror-streaming.md). With **local weights**, you pre-place the weights on the node's disk and tell KAITO to load them directly.

When enabled via the `kaito.sh/use-local-weights` annotation, KAITO:

- Derives a node directory from the preset name and mounts it **read-only** into the inference container via a `hostPath` volume.
- Points vLLM at that directory with `--model <path>` and loads it with the Run:ai model streamer (`--load-format runai_streamer`).
- **Skips the HuggingFace download entirely** — no model-puller init container is added.

This removes the download from the cold-start critical path, which is useful for large models or air-gapped/offline clusters where HuggingFace is unreachable.

## Requirements and Limitations

- **Weights must already be on the node.** KAITO does not copy the weights — the directory must exist on every node the workspace can schedule to. Typically this is done by pre-populating the path on [bring-your-own GPU nodes](./kaito-on-byo-gpu-nodes.md).
- **The directory must contain a complete model repo** — the safetensors shards plus `config.json`, the tokenizer files, and any other files vLLM needs to load the model.
- **vLLM runtime and HuggingFace models only.**


## Weights Location on the Node

KAITO reads the weights from a directory derived from the HuggingFace model ID:

```
/opt/kaito/models/<sanitized-preset-name>
```

The HuggingFace model ID is sanitized into a single path segment: lowercased, with `/` and `\` replaced by `-`. For example:

| HuggingFace model ID | Node directory |
| --- | --- |
| `deepseek-ai/DeepSeek-V4-Flash` | `/opt/kaito/models/deepseek-ai-deepseek-v4-flash` |
| `google/gemma-4-31B-it` | `/opt/kaito/models/google-gemma-4-31b-it` |

That directory is mounted read-only into the inference container at the same path, and vLLM loads the model from it.

## Usage

Add the `kaito.sh/use-local-weights: "true"` annotation to a `InferenceSet` (or `Workspace`). Everything else — the preset name, instance type, and label selector — is configured as usual.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-deepseek-v4-flash
  annotations:
    kaito.sh/use-local-weights: "true"
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: deepseek-v4-flash
inference:
  preset:
    name: "deepseek-ai/DeepSeek-V4-Flash". # HuggingFace model ID
```

Before creating this workspace, make sure the weights exist on the node at
`/opt/kaito/models/deepseek-ai-deepseek-v4-flash`.

## Verifying

Once the pod is running, confirm that the weights are mounted from the node and that no download occurred:

```bash
# The inference pod has a read-only hostPath volume named "model-weights-hostpath".
kubectl get pod -l kaito.sh/workspace=workspace-deepseek-v4-flash \
  -o jsonpath='{.items[0].spec.volumes[?(@.name=="model-weights-hostpath")].hostPath.path}'
# -> /opt/kaito/models/deepseek-ai-deepseek-v4-flash

# The pod should NOT contain a model-puller init container, and the vLLM
# args should include --model=/opt/kaito/models/... and --load-format=runai_streamer.
kubectl logs -l kaito.sh/workspace=workspace-deepseek-v4-flash
```

## Related

- [Model Mirror and Streaming](./model-mirror-streaming.md) — download weights once into a shared PVC and stream them at startup.
- [KAITO on BYO GPU Nodes](./kaito-on-byo-gpu-nodes.md) — run KAITO on nodes you provision yourself.
