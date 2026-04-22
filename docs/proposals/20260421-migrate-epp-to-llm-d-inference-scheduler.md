---
title: Migrate Default EPP to llm-d Inference Scheduler
authors:
  - "@andyzhangx"
reviewers:
  - "@Fei-Guo"
  - "@rambohe-ch"
  - "@zhuangqh"
creation-date: 2026-04-21
last-updated: 2026-04-21
status: provisional
see-also:
  - "/docs/proposals/20250918-introduce_inferenceset_autoscaling.md"
---

# Migrate Default EPP to llm-d Inference Scheduler

## Summary

This proposal migrates KAITO's default Endpoint Picker (EPP) from the upstream [Gateway API Inference Extension (GWIE)](https://github.com/kubernetes-sigs/gateway-api-inference-extension) EPP to the [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler). The llm-d inference scheduler consolidates the GWIE EPP with advanced scheduling plugins — including KV cache-aware routing, prefix cache matching, and prefill/decode disaggregation — under a single project, per the [GWIE to llm-d migration plan](https://github.com/kubernetes-sigs/gateway-api-inference-extension/issues/2430).

This change only replaces the EPP container image in the InferencePool Helm release. The InferencePool chart, CRDs, and all other KAITO components remain unchanged. There is no breaking change for existing users.

## Motivation

The GWIE project is migrating its EPP implementation and plugin ecosystem to `llm-d-inference-scheduler` ([tracking issue](https://github.com/kubernetes-sigs/gateway-api-inference-extension/issues/2430)). The key motivations are:

1. **Upstream migration**: The GWIE EPP codebase and plugins are moving to `llm-d-inference-scheduler` to accelerate development and avoid confusion about where to develop new plugins.
2. **Advanced scheduling plugins**: llm-d extends the GWIE EPP with plugins not available in the upstream GWIE EPP:
   - **Precise prefix cache scorer**: Token-level KV cache matching via KV events (vs. hash-based estimation)
   - **Prefill/Decode (P/D) disaggregation**: Separate prefill and decode phases to different pods for better GPU utilization
   - **Label-based pod filtering**: Route requests to pods matching specific labels (e.g., GPU type, inference role)
3. **Full backward compatibility**: llm-d uses the same `EndpointPickerConfig` API and is a drop-in replacement for the GWIE EPP. The default `default-plugins.yaml` ConfigMap created by the InferencePool chart works without modification.

### Goals

- Replace the default EPP image from `gateway-api-inference-extension` to `llm-d-inference-scheduler`
- Maintain full backward compatibility — no changes required for existing InferenceSet users
- Enable access to llm-d-specific advanced scheduling plugins via custom configuration

### Non-Goals/Future Work

- Changing the InferencePool Helm chart source (remains from GWIE `oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool`)
- Migrating BBR (Body-Based Routing) — BBR is an independent component unaffected by this change
- Implementing llm-d routing sidecar integration for KV event-based precise prefix cache scoring

## Proposal

### Architecture

The InferencePool Helm chart remains from GWIE. Only the EPP container image is overridden to use llm-d:

```
┌─────────────────────────────────────────────────────┐
│                   KAITO Controller                   │
│  (InferenceSet controller creates Flux resources)    │
└──────────────────────┬──────────────────────────────┘
                       │
          ┌────────────┴────────────┐
          ▼                         ▼
┌──────────────────┐    ┌────────────────────────────────┐
│  OCIRepository    │    │     HelmRelease                 │
│  (GWIE chart)     │    │  (EPP image override to llm-d)  │
│                   │    │                                  │
│  oci://registry.  │    │  image:                          │
│  k8s.io/gateway-  │    │    hub: mcr.microsoft.com/      │
│  api-inference-   │    │         oss/v2/llm-d             │
│  extension/charts │    │    name: llm-d-inference-        │
│  /inferencepool   │    │          scheduler               │
│                   │    │    tag: v0.7.1                    │
│  Tag: v1.3.1      │    │                                  │
└──────────────────┘    └────────────────────────────────┘
```

### Default Behavior (Zero Config)

After the migration, **no additional configuration is needed** for basic usage. The InferencePool chart creates a `default-plugins.yaml` ConfigMap with three default scorers, e.g.

```bash
# kubectl get configmap phi-4-mini-inferencepool-epp -o yaml
```

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: phi-4-mini-inferencepool-epp
  namespace: default
  labels:
    app.kubernetes.io/managed-by: Helm
    helm.toolkit.fluxcd.io/name: phi-4-mini-inferencepool
    helm.toolkit.fluxcd.io/namespace: default
data:
  default-plugins.yaml: |
    apiVersion: inference.networking.x-k8s.io/v1alpha1
    kind: EndpointPickerConfig
    plugins:
      - type: queue-scorer
      - type: kv-cache-utilization-scorer
      - type: prefix-cache-scorer
    schedulingProfiles:
      - name: default
        plugins:
          - pluginRef: queue-scorer
            weight: 2
          - pluginRef: kv-cache-utilization-scorer
            weight: 2
          - pluginRef: prefix-cache-scorer
            weight: 3
```

The llm-d EPP binary is fully compatible with this config format (same `EndpointPickerConfig` API).

### Default Plugin Metrics Compatibility

All three default scorers work out of the box with KAITO's vLLM inference server:

| Plugin | Required Metric | vLLM Metric Name | Available on Port 5000 |
|--------|----------------|-------------------|----------------------|
| `queue-scorer` | Request queue depth | `vllm:num_requests_waiting` + `vllm:num_requests_running` | ✅ Yes |
| `kv-cache-utilization-scorer` | KV cache usage | `vllm:kv_cache_usage_perc` | ✅ Yes |
| `prefix-cache-scorer` | None (EPP-internal hash) | N/A | ✅ N/A |

The EPP scrapes metrics from the InferencePool's `targetPorts` (port 5000, the same port as KAITO's vLLM API endpoint). KAITO's vLLM exposes the `/metrics` endpoint on port 5000 with all required Prometheus metrics.

### Advanced: Custom EPP Plugin Configuration

For advanced plugins like `precise-prefix-cache-scorer` or P/D disaggregation, users provide a custom `EndpointPickerConfig` via a **ConfigMap reference** in the InferenceSet spec. This keeps the InferenceSet clean — the plugin configuration lives in a separate ConfigMap.

#### API Changes

Add an optional `eppPluginsConfigRef` field to InferenceSetSpec:

```go
type InferenceSetSpec struct {
    // ...existing fields...

    // EPPPluginsConfigRef references a ConfigMap containing custom EPP plugins configuration.
    // The ConfigMap must contain a key "config.yaml" with the EndpointPickerConfig content.
    // If not specified, the default llm-d plugins (queue-scorer, kv-cache-utilization-scorer,
    // prefix-cache-scorer) are used.
    // +optional
    EPPPluginsConfigRef *corev1.LocalObjectReference `json:"eppPluginsConfigRef,omitempty"`
}
```

#### User Experience

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: phi-4-mini
spec:
  eppPluginsConfigRef:
    name: phi-4-mini-epp-plugins   # references a ConfigMap in the same namespace
  # ...other fields unchanged...
```

#### Controller Behavior

When `eppPluginsConfigRef` is set:

1. Controller reads the referenced ConfigMap's `config.yaml` key
2. Injects the content into the InferencePool HelmRelease values:
   ```yaml
   inferenceExtension:
     pluginsConfigFile: "custom-plugins.yaml"
     pluginsCustomConfig:
       custom-plugins.yaml: <content from ConfigMap>
   ```
3. Flux reconciles the HelmRelease → EPP deployment picks up the custom config
4. Controller watches the referenced ConfigMap for changes and triggers re-reconciliation

#### Example: Precise Prefix Cache Scorer

Token-level KV cache matching using KV events from the routing sidecar:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: phi-4-mini-epp-plugins
data:
  config.yaml: |
    apiVersion: inference.networking.x-k8s.io/v1alpha1
    kind: EndpointPickerConfig
    plugins:
      - type: single-profile-handler
      - type: decode-filter
      - type: precise-prefix-cache-scorer
        parameters:
          tokenProcessorConfig:
            blockSize: 64                 # must match vLLM block size
            hashSeed: "42"                # must match vLLM PYTHONHASHSEED env var
          indexerConfig:
            kvBlockIndexConfig:
              enableMetrics: true
      - type: kv-cache-utilization-scorer
      - type: queue-scorer
      - type: max-score-picker
    schedulingProfiles:
      - name: default
        plugins:
          - pluginRef: decode-filter
          - pluginRef: precise-prefix-cache-scorer
            weight: 2.0
          - pluginRef: kv-cache-utilization-scorer
            weight: 1.0
          - pluginRef: queue-scorer
            weight: 1.0
          - pluginRef: max-score-picker
```

> **Note**: `precise-prefix-cache-scorer` requires the `llm-d-routing-sidecar` to provide KV events via UDS socket. The sidecar must be deployed alongside vLLM pods.

#### Example: Prefill/Decode (P/D) Disaggregation

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: pd-disagg-epp-plugins
data:
  config.yaml: |
    apiVersion: inference.networking.x-k8s.io/v1alpha1
    kind: EndpointPickerConfig
    featureGates:
      - prepareDataPlugins
    plugins:
      - type: prefix-based-pd-decider
        parameters:
          nonCachedTokens: 4
      - type: disagg-profile-handler
        parameters:
          deciders:
            prefill: prefix-based-pd-decider
      - type: by-label-selector
        name: prefill-filter
        parameters:
          matchLabels:
            inference-role: prefill
      - type: by-label-selector
        name: decode-filter
        parameters:
          matchLabels:
            inference-role: decode
      - type: precise-prefix-cache-scorer
      - type: max-score-picker
    schedulingProfiles:
      - name: prefill
        plugins:
          - pluginRef: prefill-filter
          - pluginRef: precise-prefix-cache-scorer
            weight: 50
      - name: decode
        plugins:
          - pluginRef: decode-filter
          - pluginRef: max-score-picker
```

> **Note**: P/D disaggregation requires prefill and decode pods deployed separately with appropriate labels (`inference-role: prefill` / `inference-role: decode`).

### Feature Matrix

| Feature | Extra Config Needed? | Notes |
|---------|---------------------|-------|
| Basic inference routing (queue/kv-cache/prefix-cache) | ❌ No | Default plugins work out of the box |
| Precise prefix cache matching | ✅ Yes | ConfigMap with `eppPluginsConfigRef` |
| P/D disaggregated scheduling | ✅ Yes | ConfigMap + separate prefill/decode pods |
| Label-based pod filtering | ✅ Yes | ConfigMap with `eppPluginsConfigRef` |
| BBR multi-model routing | ❌ No | Independent component, unchanged |

### Request Flow

#### Single Model

```
Client Request → Gateway → HTTPRoute → DestinationRule → EPP (llm-d) → Best Pod
```

- **HTTPRoute**: Routes request to the correct InferencePool
- **DestinationRule**: TLS policy for Gateway → EPP connection (skip self-signed cert verification)
- **EPP (llm-d)**: Selects optimal pod based on queue depth, KV cache utilization, and prefix cache scoring

#### Multi-Model (with BBR)

```
Client Request → Gateway → BBR (body→header) → HTTPRoute (header match) → EPP → Best Pod
```

BBR extracts the model name from the request body and injects the `X-Gateway-Model-Name` header. HTTPRoute then routes to the correct InferencePool based on the header value.

BBR is **completely unaffected** by this EPP migration — it is an independent component with its own Helm chart.

### Why DestinationRule Is Needed

EPP runs with `--secure-serving=true` by default, generating a self-signed TLS certificate. Istio's sidecar proxy doesn't trust self-signed certs, so without the DestinationRule, the Gateway → EPP ext-proc connection fails with TLS errors. One DestinationRule is needed per InferencePool/EPP service.

> **Namespace note:** The DestinationRule must be created in the **same namespace** as the EPP service (i.e., the InferenceSet namespace). In Istio, DestinationRules are namespace-scoped and only visible to clients in the same namespace by default. Since KAITO deploys the Gateway/Envoy and EPP in the same namespace, this works out of the box. If a custom deployment places the Gateway in a different namespace, the DestinationRule must either be created in the Gateway's namespace or exported via `exportTo: ["*"]`.

```yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: <inferencepool-name>-epp
  namespace: <inferenceset-namespace>  # Must be in the same namespace as the EPP service
spec:
  host: <inferencepool-name>-epp
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
```

## Implementation Strategy

### Step 1: EPP Image Migration (This Proposal)

- Replace the default EPP image to use llm-d inference scheduler
- Update documentation
- **PR**: [kaito-project/kaito#1975](https://github.com/kaito-project/kaito/pull/1975)

### Step 2: MCR Image Publishing

- Publish `llm-d-inference-scheduler` to MCR (`mcr.microsoft.com/oss/v2/llm-d/llm-d-inference-scheduler`)
- Publish `llm-d-routing-sidecar` to MCR (`mcr.microsoft.com/oss/v2/llm-d/llm-d-routing-sidecar`)
- **PR**: [Azure/dalec-build-defs#6817](https://github.com/Azure/dalec-build-defs/pull/6817)

### Step 3: Advanced Plugin Documentation (Future)

- Document how to configure llm-d-specific plugins (precise prefix cache, P/D disaggregation)
- Add E2E tests for advanced plugin configurations

## References

- [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler)
- [GWIE to llm-d migration plan](https://github.com/kubernetes-sigs/gateway-api-inference-extension/issues/2430)
- [KAITO GWIE documentation](https://kaito-project.github.io/kaito/docs/gateway-api-inference-extension)
- [llm-d architecture docs](https://github.com/llm-d/llm-d-inference-scheduler/blob/main/docs/architecture.md)
- [Migration guide (detailed)](https://github.com/llm-d/llm-d/blob/main/docs/getting-started.md)

## Implementation History

- 2026-04-21: Open proposal PR
