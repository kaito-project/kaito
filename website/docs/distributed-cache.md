---
title: Distributed Cache
---

This document explains how to enable distributed caching for model weights and KV cache in KAITO using a pluggable cache provider.

KAITO ships the cache **framework** and a built-in `noop` reference provider only — no functional cache backend is bundled. To integrate a real cache, implement the `cache.Provider` interface in a Go package and register it with the framework (see [Implementing a provider](#implementing-a-provider)).

## Overview

The distributed cache integration accelerates model loading and enables KV cache sharing across inference pods. It supports two independent caching concerns:

- **Model Weights** — Caches model files in a distributed cache, avoiding repeated downloads from blob storage. Providers hook the model load path, so existing model loading code works unchanged.
- **KV Cache** — Shares attention KV cache between inference pods for prefill/decode disaggregation or prompt cache sharing.

Each concern is independently configurable with its own provider and mode.

## Prerequisites

1. **KAITO** built with a cache provider linked in and the `distributedCache` feature gate enabled
2. Any cluster-side infrastructure the provider requires (see the provider's own documentation)

## Installation

### 1. Link in and configure a provider

The built-in `noop` provider performs no caching. To enable real caching, build KAITO with a provider package linked in (see [Implementing a provider](#implementing-a-provider)) and add its configuration under `cache.providers.<name>` per the provider's documentation. The provider registers itself with the framework at startup; its `Name()` is the value you reference from the Workspace spec.

### 2. Configure KAITO

Enable the distributed cache feature gate and add your provider's configuration in your Helm values:

```yaml
featureGates:
  distributedCache: true

cache:
  providers: {}   # add your provider's configuration here
```

Install or upgrade KAITO:

```bash
helm upgrade --install kaito charts/kaito/workspace -f values.yaml
```

### Implementing a provider

A cache provider implements the `cache.Provider` interface and registers itself with KAITO's provider registry. To add one:

1. Create a package (e.g. `pkg/cache/<name>/`) with a type that satisfies `cache.Provider` (see `pkg/cache/noop/provider.go` for a minimal reference).
2. Register it by calling `cache.Register(...)` in `cmd/workspace/main.go` (as is done for the `noop` provider), gating it on the `distributedCache` feature gate and any provider-specific configuration.
3. Optionally register conformance expectations via `init()` so the provider is exercised by the registry-driven conformance and e2e suites.
4. Optionally implement `cache.PodApplicabilityChecker` if the cache only engages under specific conditions (see [Provider-declared applicability](#provider-declared-applicability)).

Because registration happens at compile time, "installing" a provider means building KAITO with that provider package linked in — there is no runtime plugin mechanism.

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
  modelCache:
    provider: <provider-name>
    mode: Opportunistic
  kvCache:
    provider: <provider-name>
    mode: Opportunistic
```

### Cache Modes

Each concern supports three modes:

| Mode | Behavior |
|------|----------|
| `Required` | Block pod deployment until cache infrastructure is ready. Workspace enters a waiting state while cache is unavailable. |
| `Opportunistic` | Use cache if available; proceed without it if unavailable. This is the recommended default. |
| `Disabled` | Do not interact with the cache for this concern. |

You can configure each concern independently. For example, use `Required` for model weights (to guarantee fast startup) but `Opportunistic` for KV cache (which is an optimization, not a requirement):

```yaml
cache:
  modelCache:
    provider: <provider-name>
    mode: Required
  kvCache:
    provider: <provider-name>
    mode: Opportunistic
```

### Cleanup on Delete

`modelCache.cleanupOnDelete` is reserved for future use:

```yaml
cache:
  modelCache:
    provider: <provider-name>
    mode: Opportunistic
    cleanupOnDelete: true   # reserved — currently has no effect
```

:::note Not yet implemented
Setting `cleanupOnDelete: true` currently has **no effect**. Cached model chunks are not explicitly evicted when a workspace is deleted — they are reclaimed by the cache provider's own TTL/eviction policy. The field is accepted today so specs remain forward-compatible once invalidation-on-delete is implemented.
:::

## How It Works

### Model Weights Caching

When model weight caching is enabled, KAITO renders the inference workload, asks the selected provider whether it applies (see [Provider-declared applicability](#provider-declared-applicability)), and if so applies the provider's pod mutations (labels, env vars, volumes, volume mounts, init containers). Reads then go through the provider's cache layer — cache hits are served from the cache, misses fall through to blob storage and are cached for subsequent requests.

### Provider-declared applicability

The framework does **not** hardcode when a cache applies. A provider may implement the optional `cache.PodApplicabilityChecker` interface and inspect the fully-rendered workload (the `StatefulSet`) to decide whether its cache can actually engage. This avoids silent no-op injection — and, in `Required` mode, avoids blocking on a cache that could never take effect. Providers that don't implement it are always considered applicable.

Applicability is entirely provider-specific. For example, a streaming provider that hooks a model streamer's read path (e.g. `--load-format=runai_streamer` with an `az://` model path) would return `false` for a workload using the default download path, since nothing would consume its cache library; a mount-based provider (FUSE/PVC) might apply unconditionally.

### Cache Readiness Conditions

KAITO reports cache status through the `ModelCacheReady` and `KVCacheReady` workspace conditions:

```bash
kubectl get workspace <name> -o jsonpath='{.status.conditions[?(@.type=="ModelCacheReady")]}'
```

:::note What "ready" means
These conditions mean two things are both true: (1) the provider reports that its cache **backend** infrastructure is available and reachable, **and** (2) KAITO has actually **injected** the provider's cache mutations (labels/env/volumes) into the rendered inference pod. A condition is only reported `True` once both hold; if the backend is ready but the provider is not applicable to the workload (see [Provider-declared applicability](#provider-declared-applicability)) or nothing was injected into the pod, the condition is reported `False`.

They do **not** guarantee that this workspace's specific model weights have already been warmed into the cache. As a result, a workspace can show `ModelCacheReady=True` while the first model load is still a cache **miss**. This is expected and harmless: a miss transparently falls through to blob storage and populates the cache, so subsequent loads are served from the cache. In `Required` mode, deployment is unblocked once the cache backend is ready, not once the model is fully warmed.
:::

### KV Cache Sharing

When KV caching is enabled, KAITO applies the provider's KV-cache mutations to the pod (for example, a KV connector configuration and any labels/volumes the provider needs). The specifics are provider-defined and returned through the provider's `PodMutations` for the KV cache concern.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  Inference Pod                                            │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Main Container (vLLM)                                │ │
│  │ reads model weights via the provider's cache client  │ │
│  └──────────────────────────┬──────────────────────────┘ │
│                             │                             │
└─────────────────────────────┼─────────────────────────────┘
                              │ (reads via provider)
                              ▼
              ┌───────────────────────────────┐
              │  Cache Provider backend        │
              │  Fast path: cached blocks      │
              └───────────────┬───────────────┘
                              │ (cache miss)
                              ▼
              ┌───────────────────────────────┐
              │  Blob Storage                  │
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

If your Workspace is stuck with condition `ModelCacheReady=False`:

1. Confirm the provider is installed and registered, and check the provider's own backend/readiness resources.
2. If using `Opportunistic` mode, the workspace will proceed without cache. Investigate why the cache infrastructure isn't ready.
3. Check the condition's `message`: the backend may be ready but the condition can still be `False` because the cache was not injected into the inference pod — e.g. the provider is not applicable to this workload (see [Provider-declared applicability](#provider-declared-applicability)) or the pod has not been rendered yet. Inspect the StatefulSet's pod template for the provider's expected labels/env.

### Model loading errors

If the inference pod fails to load the model:

1. Verify the provider injected its expected env vars/volumes into the pod.
2. Confirm the source (blob) storage endpoint is accessible with the pod's credentials.
3. Check the provider's own logs for injection or connection errors.

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
| `cache.providers` | Per-provider configuration map (`cache.providers.<name>.*`). Provider-specific; see the provider's docs. | `{}` |
