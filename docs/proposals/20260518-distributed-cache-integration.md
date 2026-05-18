---
title: Distributed Cache Integration
authors:
  - "@croomes"
reviewers:
  - "@Fei-Guo"
  - "@chewong"
creation-date: 2026-05-18
last-updated: 2026-05-18
status: provisional
see-also:
  - "/docs/proposals/20250609-model-as-oci-artifacts.md"
  - "/docs/proposals/20250325-distributed-inference.md"
---

# Distributed Cache Integration

## Table of Contents

- [Distributed Cache Integration](#distributed-cache-integration)
  - [Table of Contents](#table-of-contents)
  - [Glossary](#glossary)
  - [Summary](#summary)
  - [Motivation](#motivation)
    - [Goals](#goals)
    - [Non-Goals/Future Work](#non-goalsfuture-work)
  - [Proposal](#proposal)
    - [User Stories](#user-stories)
      - [Story 1 — Model Weight Cache Acceleration](#story-1--model-weight-cache-acceleration)
      - [Story 2 — KV Cache for Prefix Reuse](#story-2--kv-cache-for-prefix-reuse)
      - [Story 3 — Unified Provider for Both Concerns](#story-3--unified-provider-for-both-concerns)
      - [Story 4 — Mixed Providers](#story-4--mixed-providers)
      - [Story 5 — Graceful Degradation](#story-5--graceful-degradation)
    - [Architecture](#architecture)
    - [API Changes](#api-changes)
      - [CacheSpec (New Top-Level Workspace Field)](#cachespec-new-top-level-workspace-field)
      - [Cache Provider Interface](#cache-provider-interface)
      - [PodMutations](#podmutations)
    - [Implementation Details/Notes/Constraints](#implementation-detailsnotesconstraints)
      - [Installation Model](#installation-model)
      - [Phase 1: Foundation](#phase-1-foundation)
      - [Phase 2: Cache Controller](#phase-2-cache-controller)
      - [Phase 3: Workspace Integration](#phase-3-workspace-integration)
      - [Phase 4: Model Lifecycle Hooks](#phase-4-model-lifecycle-hooks)
      - [Phase 5: Observability](#phase-5-observability)
    - [Client Integration Patterns](#client-integration-patterns)
    - [Risks and Mitigations](#risks-and-mitigations)
  - [Alternatives](#alternatives)
  - [Upgrade Strategy](#upgrade-strategy)
  - [Additional Details](#additional-details)
    - [Test Plan](#test-plan)
  - [Implementation History](#implementation-history)

## Glossary

- **Cache Provider**: An implementation of KAITO's cache interface that manages a specific caching backend (e.g., a distributed NVMe cache, a FUSE-based dataset cache, or an in-cluster object store proxy).
- **Cache Controller**: A new KAITO controller that bridges workspace semantics with cache infrastructure, managing cache lifecycle and readiness signaling.
- **PodMutations**: The set of changes (environment variables, volumes, volume mounts, init containers) that a cache provider injects into model pods to enable cache access.
- **Prewarm**: The process of populating a cache with model weights before inference pods start serving, reducing cold-start latency.

## Summary

This proposal adds a pluggable distributed caching framework to KAITO that accelerates both model loading and inference for AI workloads. It addresses two complementary caching concerns:

1. **Model weight caching** — Caches static model files on fast local storage (NVMe, ramdisk) or in a distributed cache layer, reducing cold-start load times from minutes to seconds.
2. **KV caching** — Caches attention key/value tensors across requests, enabling prefix reuse, faster time-to-first-token, and disaggregated prefill/decode architectures.

The design introduces a provider interface that allows different cache implementations to be plugged into KAITO independently for each concern. A single workspace can use different providers for model weight caching and KV caching, or a unified provider that handles both. A lightweight Cache Controller manages lifecycle and bridges workspace intent with cache infrastructure. Examples of cache backends that could implement this interface include [Fluid](https://github.com/fluid-cloudnative/fluid) (CNCF Incubating, Kubernetes-native dataset caching), distributed NVMe cache services, and KV cache stores like [FlexKV](https://github.com/taco-project/FlexKV).

## Motivation

Model loading and inference latency are significant operational pain points for KAITO users:

- **Cold-start times**: Loading a 70B parameter model from cloud storage can take 5-10 minutes. For autoscaled inference workloads, this creates unacceptable latency during scale-out events.
- **Storage egress costs**: Repeatedly fetching multi-GB model weights from cloud storage incurs significant egress charges, especially across availability zones.
- **Scale-out penalty**: When new nodes join the cluster, models must be re-downloaded from remote storage, creating a "thundering herd" effect during scaling events.
- **Redundant computation**: Without KV caching, common prompt prefixes (system prompts, few-shot examples) are recomputed on every request, wasting GPU cycles and increasing time-to-first-token.
- **Disaggregated inference**: Prefill/decode disaggregation requires a shared KV cache layer to transfer attention state between prefill and decode pods.

A distributed cache layer addresses both concerns: model weight caching for fast startup, and KV caching for efficient inference.

### Goals

- Enable KAITO workspaces to transparently benefit from distributed caching (both model weights and KV) without requiring users to understand cache backend internals.
- Provide a declarative per-workspace cache configuration with independent control over model weight caching and KV caching.
- Support per-concern provider selection — different backends can be used for model weights vs. KV caching, or a single backend can serve both.
- Define a provider interface that allows different cache backends to be plugged in, each managing its own infrastructure lifecycle.
- Inject cache client configuration into model pods without modifying model images or inference code.
- Degrade gracefully when a cache provider is not installed or the cache is unavailable.

### Non-Goals/Future Work

- **Implementing a cache backend** — KAITO provides the integration framework; actual caching is delegated to external operators (e.g., [Fluid](https://github.com/fluid-cloudnative/fluid), distributed NVMe services, KV stores).
- **Multi-tenant cache isolation** — Initial implementation assumes a single shared cache cluster per KAITO installation. Per-tenant cache partitioning is deferred.
- **Cache rebalancing coordination** — Provider-specific rebalancing will be transparent to KAITO initially. Deeper integration (scale-event signaling, drain-gate awareness) is a future enhancement.
- **Modifying KAITO model images** — Model images may need cache-specific client libraries. Image changes are tracked separately from this proposal.
- **KV cache eviction policy tuning** — TTL, growth factors, and write modes are provider-specific configuration managed via Helm values, not per-workspace settings.

## Proposal

### User Stories

#### Story 1 — Model Weight Cache Acceleration

As a KAITO user deploying a large preset model (e.g., Llama 3.3 70B), I want model loading to be accelerated by a distributed cache so that my inference pods reach serving state in seconds rather than minutes on subsequent deployments.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: llama-70b-inference
spec:
  resource:
    count: 2
    instanceType: "Standard_NC96ads_A100_v4"
  inference:
    preset:
      name: llama-3.3-70b-instruct
  cache:
    modelWeights:
      provider: "my-nvme-cache"
      mode: Opportunistic
      prewarmOnDeploy: true
```

#### Story 2 — KV Cache for Prefix Reuse

As a platform engineer running a high-throughput chat service, I want attention KV tensors from common system prompts to be cached and shared across requests, reducing time-to-first-token for repeated prefixes.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: chat-service
spec:
  resource:
    count: 4
    instanceType: "Standard_NC96ads_A100_v4"
  inference:
    preset:
      name: llama-3.3-70b-instruct
  cache:
    kvCache:
      provider: "my-kv-store"
      mode: Required
```

#### Story 3 — Unified Provider for Both Concerns

As a KAITO operator using a cache backend that supports both model weights and KV caching, I want to configure a single provider for both, sharing infrastructure and reducing operational complexity.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: fully-cached
spec:
  resource:
    count: 4
    instanceType: "Standard_NC96ads_A100_v4"
  inference:
    preset:
      name: deepseek-v3
  cache:
    modelWeights:
      provider: "unified-cache"
      mode: Required
      prewarmOnDeploy: true
    kvCache:
      provider: "unified-cache"
      mode: Opportunistic
```

#### Story 4 — Mixed Providers

As a platform engineer, I want to use Fluid for model weight caching (FUSE mount) and a separate KV store for attention caching, combining the best tool for each job.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: mixed-providers
spec:
  resource:
    count: 2
    instanceType: "Standard_NC96ads_A100_v4"
  inference:
    preset:
      name: llama-3.3-70b-instruct
  cache:
    modelWeights:
      provider: "fluid"
      mode: Opportunistic
    kvCache:
      provider: "flexkv"
      mode: Required
```

#### Story 5 — Graceful Degradation

As a KAITO operator, I want workspaces to proceed with model deployment even if the cache is temporarily unavailable (e.g., during maintenance), falling back to direct cloud storage access without user intervention.

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: flexible-deployment
spec:
  resource:
    count: 1
    instanceType: "Standard_NC24ads_A100_v4"
  inference:
    preset:
      name: phi-4
  cache:
    modelWeights:
      provider: "my-cache"
      mode: Opportunistic  # use if available, proceed without if not
```

### Architecture

```
┌───────────────────────────────────────────────────────────────────────┐
│  KAITO Cluster                                                        │
│                                                                       │
│  ┌──────────────────────┐         ┌────────────────────────────────┐  │
│  │  KAITO Workspace     │         │  Cache Backend Operator        │  │
│  │  Controller          │         │  (e.g., Fluid, NVMe cache)     │  │
│  └──────────┬───────────┘         └───────────────┬────────────────┘  │
│             │                                     │                   │
│             │   ┌────────────────────────┐        │                   │
│             └───│  Cache Controller      │────────┘                   │
│                 │  (pkg/cache/)          │                            │
│                 └───────────┬────────────┘                            │
│                             │                                         │
│              ┌──────────────┼──────────────┐                          │
│              │              │              │                          │
│              ▼              ▼              ▼                          │
│   ┌──────────────┐  ┌───────────┐  ┌──────────────┐                   │
│   │ Cache CRs    │  │ Discovery │  │ Prewarm      │                   │
│   │ (lifecycle)  │  │ Resources │  │ Jobs         │                   │
│   └──────────────┘  └───────────┘  └──────────────┘                   │
│              │                                                        │
│              ▼                                                        │
│   ┌──────────────────────────────────────────┐                        │
│   │  Cache Server Pods                       │                        │
│   │  (managed by backend operator)           │                        │
│   └──────────────────────────────────────────┘                        │
│                                                                       │
│   ┌──────────────────────────────────────────┐                        │
│   │  Model Pods (inference/tuning)           │                        │
│   │  +Injected PodMutations (env/vol/mounts) │                        │
│   └──────────────────────────────────────────┘                        │
└───────────────────────────────────────────────────────────────────────┘
```

**Component Interactions:**

1. The **Cache Controller** watches node topology and manages cache backend resources (e.g., CRDs, PVCs) via the provider interface.
2. The **Cache Backend Operator(s)** (external) reconcile backend-specific resources, deploying cache infrastructure on eligible nodes.
3. The **Workspace Controller** resolves providers independently for each concern (model weights, KV cache), queries each for readiness, obtains `PodMutations`, and merges them into model pods.
4. **Model pods** use the injected configuration (env vars, FUSE mounts, volumes, or KV connector config) to access cache infrastructure.

### API Changes

#### CacheSpec (New Top-Level Workspace Field)

```go
// CacheSpec configures distributed caching for model workloads.
// Cache is a top-level Workspace field, applicable to both inference and tuning.
// Each concern (model weights, KV cache) is configured independently with its
// own provider and mode, allowing different backends per concern.
type CacheSpec struct {
    // ModelWeights configures caching of static model weight files.
    // +optional
    ModelWeights *ModelWeightsCacheConfig `json:"modelWeights,omitempty"`

    // KVCache configures caching of attention key/value tensors.
    // +optional
    KVCache *KVCacheConfig `json:"kvCache,omitempty"`
}

// ModelWeightsCacheConfig controls how model weight files are cached.
type ModelWeightsCacheConfig struct {
    // Provider selects the cache implementation for model weights.
    // +kubebuilder:validation:MinLength=1
    Provider CacheProvider `json:"provider"`

    // Mode controls cache behavior.
    // Required: block until cache is ready.
    // Opportunistic: use cache if available, proceed without.
    // Disabled: no cache interaction.
    // +kubebuilder:default:="Opportunistic"
    // +kubebuilder:validation:Enum=Required;Opportunistic;Disabled
    Mode CacheMode `json:"mode,omitempty"`

    // PrewarmOnDeploy triggers cache population before model serving starts.
    // +optional
    PrewarmOnDeploy bool `json:"prewarmOnDeploy,omitempty"`

    // CleanupOnDelete invalidates cached model data when workspace is deleted.
    // +optional
    CleanupOnDelete bool `json:"cleanupOnDelete,omitempty"`
}

// KVCacheConfig controls how attention KV tensors are cached.
type KVCacheConfig struct {
    // Provider selects the cache implementation for KV tensors.
    // +kubebuilder:validation:MinLength=1
    Provider CacheProvider `json:"provider"`

    // Mode controls cache behavior.
    // Required: block until KV cache service is ready.
    // Opportunistic: use KV cache if available, proceed without.
    // Disabled: no KV cache interaction.
    // +kubebuilder:default:="Opportunistic"
    // +kubebuilder:validation:Enum=Required;Opportunistic;Disabled
    Mode CacheMode `json:"mode,omitempty"`
}

type CacheProvider string

type CacheMode string

const (
    CacheModeRequired      CacheMode = "Required"
    CacheModeOpportunistic CacheMode = "Opportunistic"
    CacheModeDisabled      CacheMode = "Disabled"
)
```

The `Workspace` struct gains a new optional field:

```go
type Workspace struct {
    // ...existing fields...
    Resource  ResourceSpec   `json:"resource"`
    Inference *InferenceSpec `json:"inference,omitempty"`
    Tuning    *TuningSpec    `json:"tuning,omitempty"`

    // Cache configures distributed caching for this workspace's workloads.
    // Applies to both inference and tuning when specified.
    // +optional
    Cache *CacheSpec `json:"cache,omitempty"`
}
```

#### Cache Provider Interface

```go
// Provider defines the interface that cache implementations must satisfy.
type Provider interface {
    // Name returns the provider identifier (e.g., "fluid", "nvme-cache").
    Name() string

    // IsAvailable reports whether the cache infrastructure is installed
    // and the provider can operate (e.g., CRD exists, operator running).
    IsAvailable(ctx context.Context) (bool, error)

    // IsReady reports whether the cache is warmed and ready to serve.
    IsReady(ctx context.Context) (bool, string, error)

    // PodMutations returns the pod-level changes needed to enable cache
    // access for a given workspace.
    PodMutations(ctx context.Context, workspace *kaitov1beta1.Workspace) (*PodMutations, error)

    // Prewarm triggers cache population for the model associated with
    // the given workspace. Returns immediately; warming is asynchronous.
    Prewarm(ctx context.Context, workspace *kaitov1beta1.Workspace) error

    // Cleanup invalidates cached data associated with a workspace.
    Cleanup(ctx context.Context, workspace *kaitov1beta1.Workspace) error
}
```

#### PodMutations

```go
// PodMutations describes all pod-level changes needed to enable cache access.
// Supports both env-var-based (e.g., storage interception libraries) and
// mount-based (e.g., FUSE, PVC) cache integrations.
type PodMutations struct {
    // EnvVars to inject into model containers.
    EnvVars []corev1.EnvVar
    // Volumes to add to the pod spec.
    Volumes []corev1.Volume
    // VolumeMounts to add to model containers.
    VolumeMounts []corev1.VolumeMount
    // InitContainers to prepend to the pod.
    InitContainers []corev1.Container
}
```

### Implementation Details/Notes/Constraints

#### Installation Model

Cache providers are installed as **conditional Helm subchart dependencies** within KAITO's workspace chart. This follows the same pattern used for existing optional components (Flux2, gpu-feature-discovery, local-csi-driver).

Each cache provider contributes:
1. **A Helm subchart** — listed in `Chart.yaml` with a `condition` field gating installation
2. **CRDs** — installed via the subchart's `crds/` directory for reliable ordering (CRDs install before templates)
3. **Operator deployment** — the provider's controller/operator deployed into a dedicated namespace
4. **Provider-specific values** — exposed under a provider key in `values.yaml`

Example `Chart.yaml` dependency entry for a cache provider:
```yaml
dependencies:
  # ...existing dependencies...
  - name: my-cache-provider
    version: "1.x.x"
    repository: "oci://registry.example.com/charts"
    condition: cache.providers.myCacheProvider.enabled
```

Example `values.yaml` structure:
```yaml
cache:
  enabled: false
  defaultProvider: ""           # which provider workspaces use by default
  providers:
    myCacheProvider:
      enabled: false
      namespace: "cache-system"
      # Provider-specific configuration
      nodeSelector:
        key: "node-type"
        value: "gpu-nvme"
      serverSizeInGB: 800
      scaling:
        minServers: 1
        maxServers: 32
```

This model ensures:
- **Single `helm install`** — users get KAITO + cache infrastructure in one deployment
- **CRD ordering** — provider CRDs are available before KAITO's controllers attempt to use them
- **Conditional install** — cache providers are only deployed when explicitly enabled
- **Version pinning** — chart version in `Chart.yaml` pins the compatible provider version
- **Independence** — providers can also be installed standalone (e.g., for pre-existing clusters) and KAITO will discover them at runtime via `IsAvailable()`

For clusters where the cache backend is already deployed externally (not via KAITO's Helm chart), KAITO's Cache Controller discovers it at runtime through the provider's `IsAvailable()` check. The subchart dependency is not required in this case — only the feature gate and provider configuration need to be set.

#### Phase 1: Foundation

- Add `FeatureFlagDistributedCache` constant (`pkg/utils/consts/consts.go`) and register it in `pkg/featuregates/featuregates.go` (default: `false`).
- Create `pkg/cache/` package with provider interface, `PodMutations` type, and provider registry.
- Implement no-op provider (`pkg/cache/noop/`) for testing and disabled mode.
- Add `cache` configuration section to `values.yaml`:
  ```yaml
  cache:
    enabled: false
    providers:
      myProvider:
        enabled: false
        namespace: ""          # namespace where backend is deployed
        nodeSelector: {}       # label selector for cache-eligible nodes
        # Provider-specific settings here
  ```
- Add startup provider discovery check (validate configured providers are registered and available).

#### Phase 2: Cache Controller

Create `pkg/cache/controller.go` with the Cache Controller:

- **Provider Discovery**: At startup, resolve the configured provider via the registry. If unavailable, enter degraded state (log warning, emit event) without crashing.
- **Node Watching**: Watch eligible nodes (Ready, schedulable, matching configured label selector) to inform providers about cache topology.
- **Provider Lifecycle**: Call `IsAvailable()` to verify backend readiness. Providers manage their own backend-specific resources (CRDs, PVCs, StatefulSets) internally.
- **Status Monitoring**: Expose a `CacheReady` condition derived from `provider.IsReady()`.
- **RBAC**: Add rules for provider-specific resources (configurable), core API (nodes, namespaces, events), and batch API (jobs for prewarm/cleanup).

Register the controller in `cmd/workspace/main.go` behind the feature gate.

#### Phase 3: Workspace Integration

- Add `Cache *CacheSpec` to the `Workspace` struct in `api/v1beta1/workspace_types.go`.
- Modify workspace controller reconciliation to process each concern independently:
  - For `modelWeights` (if configured):
    - Resolve provider via registry (`cache.Get(workspace.Cache.ModelWeights.Provider)`)
    - Check `IsAvailable()` and `IsReady()`
    - Apply mode: `Required` blocks, `Opportunistic` proceeds, `Disabled` skips
    - Call `PodMutations()` and collect results
  - For `kvCache` (if configured):
    - Resolve provider via registry (`cache.Get(workspace.Cache.KVCache.Provider)`)
    - Check `IsAvailable()` and `IsReady()`
    - Apply mode independently from model weights
    - Call `PodMutations()` and collect results
  - Merge all PodMutations (deduplicate env vars if same provider used for both)
  - Inject merged mutations into model pod specs
- Add validation webhook rules:
  - Each sub-config's `mode` must be a valid enum value
  - Each sub-config's `provider` must be a registered provider
  - `prewarmOnDeploy` is a no-op if model weights `mode` is `Disabled`
- Add conditions to `WorkspaceStatus`: `ModelWeightsCacheReady`, `KVCacheReady`.

#### Phase 4: Model Lifecycle Hooks

- **Prewarm**: On Workspace creation with `prewarmOnDeploy: true`, call `provider.Prewarm()` to spawn a deterministic-named idempotent Job that reads model weights through the cache (warming it). In `Required` mode, block serving until prewarm completes (with timeout). In `Opportunistic` mode, prewarm is best-effort.
- **Cleanup**: On Workspace deletion with `cleanupOnDelete: true`, add a finalizer that calls `provider.Cleanup()` to invalidate cached model data. Cleanup is bounded by a timeout to prevent blocking deletion indefinitely.

#### Phase 5: Observability

- Define a standard metrics interface for providers to expose cache performance (hit/miss rate, latency, eviction counts).
- Surface cache performance in Workspace status annotations or events.
- Documentation: user guide for enabling caching, provider implementation guide, architecture diagram, troubleshooting.

### Client Integration Patterns

Cache providers integrate with model pods through one or more of the following patterns, all expressed as `PodMutations`:

**Pattern 1: Environment Variable Injection (Storage Interception)**

Providers that use client-side library interception inject environment variables that configure the interception layer. Model images must include the provider's client library. Example env vars for model weight caching:
```
CACHE_ENABLED=true
CACHE_DISCOVERY_ENDPOINT=http://cache-discovery.<namespace>.svc.cluster.local:<port>
```

**Pattern 2: FUSE Volume Mounts**

Providers like [Fluid](https://github.com/fluid-cloudnative/fluid) expose cached data as FUSE-mounted volumes. The provider adds Volumes and VolumeMounts to the pod spec, making cached model files available at a filesystem path:
```yaml
volumes:
  - name: model-cache
    persistentVolumeClaim:
      claimName: model-dataset
volumeMounts:
  - name: model-cache
    mountPath: /models
```

**Pattern 3: Init Container Warm-up**

Some providers use init containers to pre-fetch model data into a shared volume (e.g., emptyDir backed by NVMe) before the main model container starts:
```yaml
initContainers:
  - name: cache-warmup
    image: provider/warmup-agent:latest
    volumeMounts:
      - name: model-data
        mountPath: /cache
```

**Pattern 4: Inference Engine KV Connector Configuration**

KV cache providers inject configuration that tells the inference engine (e.g., vLLM) how to connect to the external KV store. This is typically done via environment variables or command-line arguments:
```
VLLM_KV_TRANSFER_CONFIG={"kv_connector":"MyKVConnector","locator_nodes":"cache-discovery.<namespace>.svc.cluster.local:9065","protocol":"rdma"}
```

The inference engine's KV connector handles put/get operations for attention tensors transparently during prefill and decode.

**Combining Patterns:** The `PodMutations` struct supports all patterns simultaneously. When both model weight and KV cache providers are configured, their mutations are merged. If the same provider serves both concerns, it deduplicates shared configuration (e.g., a single discovery endpoint env var used by both the storage interception library and the KV connector).

### Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Cache provider not installed when feature gate enabled | Controller cannot verify cache readiness | Provider discovery check at startup + graceful degradation to `Unavailable` state; clear error events |
| Cache unavailable in `Required` mode blocks workspace indefinitely | Model deployment stuck | Configurable timeout with clear condition messaging; recommend `Opportunistic` mode for non-critical workloads |
| Provider-specific client library not in model image | Cache configured but model cannot use it | Document image requirements per provider; emit warning event if cache is enabled but provider reports incompatibility |
| Cache-eligible nodes not labelled correctly | Cache pods cannot schedule | Pre-flight check on node labels; emit event listing expected vs. found labels |
| Provider API incompatibility after upgrade | PodMutations generation fails | Pin provider versions in config; use unstructured client for CRD-based providers for forward compatibility |
| Cache rebalancing causes transient misses during scale events | Elevated latency during topology changes | Transparent to initial integration; future: surface `DegradedWarmup` status via provider |

## Alternatives

### Option 1: Pure Helm Subchart (No KAITO Controller)

Deploy the cache backend entirely via Helm subchart with static configuration. KAITO injects config but does not manage cache lifecycle.

**Pros:** Simplest implementation; no new controller code.
**Cons:** No dynamic adaptation to topology; no per-workspace mode control; no prewarm/cleanup lifecycle; poor observability into cache state.

### Option 2: Embed Cache Logic in KAITO

Implement cache server management directly in KAITO (StatefulSet, ConfigMaps, discovery service) without using an external operator.

**Pros:** Full control; no external dependency.
**Cons:** Duplicates complex distributed systems logic; maintenance burden; misses upstream bug fixes and features; violates single-responsibility.

### Option 3: Sidecar-Based Cache Client

Inject a sidecar container running the cache client instead of relying on in-process library interception or FUSE mounts.

**Pros:** No model image modifications needed.
**Cons:** Adds latency (IPC vs. in-process); resource overhead; more complex pod lifecycle; may not align with all provider designs.

### Option 4 (Chosen): Provider Interface + External Operator

KAITO defines a provider interface; concrete providers manage their own backend-specific resources. KAITO injects client configuration into pods via `PodMutations`.

**Pros:** Clear separation of concerns; minimal code in KAITO; each provider leverages its battle-tested operator; automatic benefit from upstream improvements; extensible to new backends.
**Cons:** Runtime dependency on external operator; cross-namespace coordination; requires provider discovery logic.

## Upgrade Strategy

- **New installations**: Enable via `cache.enabled=true` in Helm values. Feature gate `FeatureFlagDistributedCache` must also be set to `true`. Specify `cache.provider` to select the backend.
- **Existing installations**: No breaking changes. `Cache` field on Workspace is optional; existing workspaces without it behave identically to today.
- **Provider upgrades**: Each provider manages its own versioning. KAITO pins compatible provider versions in documentation and validates at startup.
- **Feature gate promotion**: Once stable, promote `FeatureFlagDistributedCache` to default-on (beta), then remove the gate (GA).
- **Provider interface stability**: The `Provider` interface is internal to KAITO initially. Once external providers are supported, version the interface with a compatibility guarantee.

## Additional Details

### Test Plan

| Category | Scope | Approach |
|----------|-------|----------|
| Unit tests | Provider interface, CacheSpec validation, PodMutations generation | Standard Go table-driven tests with mock provider |
| Unit tests | Cache controller reconciliation logic | envtest with mock provider |
| Integration tests | End-to-end cache lifecycle (create workspace → cache ready → pods injected) | envtest with fake provider CRD; verify PodMutations applied |
| Integration tests | Graceful degradation (provider absent, cache not ready) | envtest without provider; verify workspace proceeds in Opportunistic mode |
| E2E tests | Full stack with a real cache backend | Cluster with NVMe SKU; deploy cache provider + KAITO; verify model loads from cache |
| E2E tests | Mode behavior (Required blocks, Opportunistic proceeds) | Same cluster; test both modes with cache available/unavailable |

### Files to Create/Modify

**New Files:**
- `pkg/cache/provider.go` — Provider interface and PodMutations type
- `pkg/cache/registry.go` — Provider registry
- `pkg/cache/controller.go` — Cache controller
- `pkg/cache/controller_test.go` — Tests
- `pkg/cache/noop/provider.go` — No-op provider for testing

**Modified Files:**
- `pkg/utils/consts/consts.go` — Add `FeatureFlagDistributedCache`
- `pkg/featuregates/featuregates.go` — Register feature gate
- `charts/kaito/workspace/Chart.yaml` — Add cache provider subchart dependency (conditional)
- `charts/kaito/workspace/values.yaml` — Add cache config section
- `api/v1beta1/workspace_types.go` — Add CacheSpec field
- `api/v1beta1/workspace_validation.go` — Validate cache fields
- `pkg/workspace/controllers/workspace_controller.go` — Cache integration logic
- `pkg/workspace/manifests/manifests.go` — PodMutations injection
- `cmd/workspace/main.go` — Register cache controller

## Implementation History

- [ ] 05/18/2026: Initial proposal drafted
- [ ] MM/DD/YYYY: First round of feedback from maintainers
- [ ] MM/DD/YYYY: Open proposal PR
- [ ] MM/DD/YYYY: Proposal accepted
- [ ] MM/DD/YYYY: Phase 1 implementation PR (foundation + provider interface)
- [ ] MM/DD/YYYY: Phase 2 implementation PR (cache controller)
- [ ] MM/DD/YYYY: Phase 3 implementation PR (workspace integration)
