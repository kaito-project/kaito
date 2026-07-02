---
title: Distributed Cache
---

This document explains how to enable distributed caching for model weights and KV cache in KAITO using a pluggable cache provider (currently DACS).

## Overview

The distributed cache integration accelerates model loading and enables KV cache sharing across inference pods. It supports two independent caching concerns:

- **Model Weights** — Caches model files in a distributed NVMe cache, avoiding repeated downloads from blob storage. Uses filesystem interception (LD_PRELOAD) so existing model loading code works unchanged.
- **KV Cache** — Shares attention KV cache between inference pods for prefill/decode disaggregation or prompt cache sharing.

Each concern is independently configurable with its own provider and mode.

## Prerequisites

1. **KAITO** installed with the `distributedCache` feature gate enabled
2. **DACS cache operator** deployed in the cluster (manages Cache CRs in `dacs-cache-system` namespace)
3. **DACS CSI driver + mutating webhook** installed (handles library injection into labeled pods)
4. **Azure Workload Identity** configured on the KAITO nodes (for DefaultAzureCredential access to blob storage)

## Installation

### 1. Install DACS

Install the DACS distributed cache operator, CSI driver, and mutating webhook. The webhook automatically injects cache libraries into pods labeled with `dacs.azure.com/inject: "true"`.

```bash
helm install dacs-cache oci://<registry>/charts/dacs-cache \
  --namespace dacs-cache-system --create-namespace \
  --set cache.nodeSelectorKey=dacs.azure.com/cache-node \
  --set-string cache.nodeSelectorValue=enabled
```

### 2. Configure KAITO

Enable the distributed cache feature gate and configure the DACS provider in your Helm values:

```yaml
featureGates:
  distributedCache: true

cache:
  providers:
    dacs:
      enabled: true
      discoveryEndpoint: ""  # Auto-discovered from Cache CR status if empty
      kvCacheEnabled: true
      kvConnectorProtocol: "tcp"
      blobEndpoint: "https://<your-account>.blob.core.windows.net"  # For prewarm Jobs
      blobContainer: "kaito-models"
      blobPrefix: "kaito-models"
      prewarmImage: ""  # Set if using prewarm Jobs
```

Install or upgrade KAITO:

```bash
helm upgrade --install kaito charts/kaito/workspace -f values.yaml
```

## Workspace Configuration

Add a `cache` section to your Workspace spec:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: phi-4-cached
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: phi-4
inference:
  preset:
    name: "microsoft/phi-4"
cache:
  modelWeights:
    provider: dacs
    mode: Opportunistic
  kvCache:
    provider: dacs
    mode: Opportunistic
```

### Cache Modes

Each concern supports three modes:

| Mode | Behavior |
|------|----------|
| `Required` | Block pod deployment until cache infrastructure is ready. Workspace enters a waiting state if cache is unavailable. |
| `Opportunistic` | Use cache if available; proceed without it if unavailable. This is the recommended default. |
| `Disabled` | Do not interact with the cache for this concern. |

You can configure each concern independently. For example, use `Required` for model weights (to guarantee fast startup) but `Opportunistic` for KV cache (which is an optimization, not a requirement):

```yaml
cache:
  modelWeights:
    provider: dacs
    mode: Required
  kvCache:
    provider: dacs
    mode: Opportunistic
```

## How It Works

### Model Weights Caching

When model weight caching is enabled, KAITO applies the following to inference pods:

1. **Pod label** `dacs.azure.com/inject: "true"` — Triggers the DACS mutating webhook
2. **KAITO_MODEL_PATH** env var — Set to the local path where the model appears (e.g., `/mnt/models/kaito-models/microsoft/phi-4/main`)

The DACS webhook (triggered by the label) injects:
- `LD_PRELOAD` with `libStorageIntercept.so` (hostPath from CSI driver)
- StorageIntercept configuration via a projected ConfigMap volume
- Python client libraries (`PYTHONPATH`, `DACS_LIB_PATH`)

The inference runtime (vLLM) reads from `KAITO_MODEL_PATH` as if it were a local filesystem. StorageIntercept transparently fetches data from the DACS cache (NVMe-backed) or falls through to blob storage on cache miss.

### KV Cache Sharing

When KV caching is enabled, KAITO injects:

1. **Pod label** `dacs.azure.com/inject: "true"` — For KV connector library access
2. **VLLM_KV_TRANSFER_CONFIG** env var — Configures vLLM's KV transfer mechanism with:
   - Full connector class path: `dacs_client.connectors.vllm_connector.DacsKVConnector`
   - Discovery endpoint, protocol, and TTL settings

### Auto-Discovery

If `discoveryEndpoint` is left empty in the Helm configuration, KAITO automatically reads the endpoint from the Cache CR's `status.discoveryEndpoint` field. This enables zero-configuration when DACS is installed in the same cluster.

### Prewarm

If a `prewarmImage` is configured, KAITO can create Kubernetes Jobs that download model weights from Hugging Face and upload them to blob storage before the inference pod starts. Prewarm Jobs also receive the `dacs.azure.com/inject` label so the webhook injects cache client libraries.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  Inference Pod (labeled: dacs.azure.com/inject=true)   │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Main Container (vLLM)                                │ │
│  │ LD_PRELOAD=libStorageIntercept.so (injected by hook) │ │
│  │ reads /mnt/models/...                                │ │
│  └──────────────────────────┬──────────────────────────┘ │
│                             │                             │
└─────────────────────────────┼─────────────────────────────┘
                              │ (intercepted reads)
                              ▼
              ┌───────────────────────────────┐
              │  DACS Cache (NVMe nodes)    │
              │  Fast path: local/remote NVMe  │
              └───────────────┬───────────────┘
                              │ (cache miss)
                              ▼
              ┌───────────────────────────────┐
              │  Azure Blob Storage            │
              │  (source of truth)             │
              └───────────────────────────────┘
```

## AI Runway Interoperability

When used with [AI Runway](https://github.com/Azure/airunway), the cache configuration flows through the AI Runway controller:

1. AI Runway detects cache configuration in the InferencePool spec
2. AI Runway resolves provider-specific config and writes it to `status.cache.kvCache`
3. KAITO reads the resolved config and applies pod mutations

This allows AI Runway to manage fleet-level cache policies while KAITO handles per-pod injection.

## Troubleshooting

### Cache not ready

If your Workspace is stuck with condition `ModelWeightsCacheReady=False`:

1. Check the DACS Cache CR status:
   ```bash
   kubectl get caches -n dacs-cache-system -o yaml
   ```
2. Verify the cache server pods are running:
   ```bash
   kubectl get pods -n dacs-cache-system
   ```
3. If using `Opportunistic` mode, the workspace will proceed without cache. Switch to investigate why the cache infrastructure isn't ready.

### Model loading errors

If the inference pod fails to load the model:

1. Verify the injection label is on the pod:
   ```bash
   kubectl get pod <pod> --show-labels | grep dacs
   ```
2. Check that the webhook injected `LD_PRELOAD`:
   ```bash
   kubectl exec <pod> -- env | grep -E "LD_PRELOAD|KAITO_MODEL"
   ```
3. Confirm the blob storage endpoint is accessible with Workload Identity credentials.
4. Check DACS webhook logs for injection errors:
   ```bash
   kubectl logs -n dacs-cache-system -l app=dacs-webhook
   ```

### Feature gate not active

If `cache` spec is ignored, ensure the feature gate is enabled:

```bash
kubectl get deploy -n kaito-workspace -o yaml | grep feature-gates
```

The output should include `distributedCache=true`.

## Configuration Reference

| Helm Value | Description | Default |
|---|---|---|
| `featureGates.distributedCache` | Enable the distributed cache feature | `false` |
| `cache.providers.dacs.enabled` | Register the DACS provider | `false` |
| `cache.providers.dacs.discoveryEndpoint` | DACS cache server discovery URL (auto-discovered if empty) | `""` |
| `cache.providers.dacs.kvCacheEnabled` | Enable KV cache support | `true` |
| `cache.providers.dacs.kvConnectorProtocol` | KV connector transport (`tcp` or `rdma`) | `tcp` |
| `cache.providers.dacs.blobEndpoint` | Azure Blob Storage endpoint (for prewarm Jobs) | `""` |
| `cache.providers.dacs.blobContainer` | Blob container for model storage | `kaito-models` |
| `cache.providers.dacs.blobPrefix` | Path prefix within the container | `kaito-models` |
| `cache.providers.dacs.prewarmImage` | Image for prewarm Jobs | `""` |
