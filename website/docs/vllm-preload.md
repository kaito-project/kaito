---
title: vLLM Weight Preload
---

This document explains how to keep a model's weights on the GPU across restarts of the vLLM inference container, so a restart doesn't reload them from disk.

## Overview

When the vLLM container restarts (a crash, a failed liveness probe, an OOM), vLLM normally reads the weights from disk again and reruns quantization. For large models that can take minutes.

With the `kaito.sh/enable-vllm-preload` annotation, KAITO runs vLLM preload (`vllm preload`, vLLM 0.31+) next to the server:

- A native sidecar (`vllm-preload`) runs `vllm preload` with the same model args as the server. It loads the weights once and keeps them on the GPUs.
- The server starts with `--load-format ipc_cache` and maps the sidecar's weights over CUDA IPC instead of loading them.
- If the sidecar isn't reachable or its weights don't match the server's config, vLLM falls back to loading from disk.

The sidecar starts before the server and is ready once every GPU holds its shard, so the first start takes about as long as before. Later restarts of the server skip the weight load.

## Usage

Add the annotation to a `Workspace`:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
  annotations:
    kaito.sh/enable-vllm-preload: "true"
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

The annotation only accepts `"true"` or `"false"`.

## How the GPUs are shared

The daemon and the server have to run on the same GPUs. The sidecar holds the pod's GPU request, writes the UUIDs of its GPUs to a shared `emptyDir`, and pins CUDA to them. The server has no GPU request of its own: it sees the node's GPUs (`NVIDIA_VISIBLE_DEVICES=all`, like the LMCache sidecar) and pins itself to the same UUIDs in the same order. The pod still requests the same number of GPUs as before.

## Requirements and Limitations

- vLLM runtime on GPU nodes, single-node workloads only. Multi-node, MIG partitions and streamed weights (`az://`) are skipped, and the workload runs as before.
- Sleep mode must not be used, because the weights live in the sidecar's memory.
- If the sidecar itself restarts while the server is running, it loads a second copy of the weights. The server keeps serving from the first copy until it restarts.
