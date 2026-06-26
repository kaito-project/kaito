---
title: Distributed Cache Integration
authors:
  - "@croomes"
  - "@hasethuraman"
reviewers:
  - "@Fei-Guo"
  - "@chewong"
creation-date: 2026-06-09
last-updated: 2026-06-18
status: provisional
see-also:
  - "/docs/proposals/20250609-model-as-oci-artifacts.md"
  - "/docs/proposals/20250325-distributed-inference.md"
  - "/docs/proposals/20260520-model-mirror.md"
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
    - [Cache Scope](#cache-scope)
    - [API Changes](#api-changes)
     - [CacheSpec (Workspace / InferenceSet Field)](#cachespec-workspace--inferenceset-field)
     - [Cache Provider Interface](#cache-provider-interface)
     - [PodMutations](#podmutations)
    - [Implementation Details/Notes/Constraints](#implementation-detailsnotesconstraints)
      - [Installation Model](#installation-model)
      - [Phase 1: Foundation](#phase-1-foundation)
      - [Phase 2: Cache Controller](#phase-2-cache-controller)
      - [Phase 3: Workspace Integration](#phase-3-workspace-integration)
      - [Phase 4: ModelMirror Integration (Cache Warming)](#phase-4-modelmirror-integration-cache-warming)
      - [Phase 5: Observability](#phase-5-observability)
    - [Client Integration Patterns](#client-integration-patterns)
    - [Runtime Integration Details](#runtime-integration-details)
    - [Risks and Mitigations](#risks-and-mitigations)
  - [Alternatives](#alternatives)
  - [Upgrade Strategy](#upgrade-strategy)
  - [Additional Details](#additional-details)
    - [Test Plan](#test-plan)
  - [Implementation History](#implementation-history)

## Glossary

- **Cache Provider**: An implementation of KAITO's cache interface that manages a specific caching backend (e.g., a distributed NVMe cache, a FUSE-based dataset cache, or an in-cluster object store proxy).
- **Cache Controller**: A new KAITO controller that bridges workspace semantics with cache infrastructure, managing cache lifecycle and readiness signaling.
- **PodMutations**: The set of changes (environment variables, volumes, volume mounts, init containers, labels) that a cache provider injects into model pods to enable cache access.
- **Cache Warming**: The process of populating a cache with model weights, reducing cold-start latency. Occurs when ModelMirror downloader uses a cache-aware storage backend.

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
- **KV cache eviction policy tuning** — TTL, growth factors, and write modes are provider-specific configuration. Users can supply provider-specific settings via the `Config` ConfigMap field on `KVCacheSpec`; the provider validates and merges with its defaults from Helm values.

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
    modelCache:
      provider: "my-nvme-cache"
      mode: Opportunistic
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
    modelCache:
      provider: "unified-cache"
      mode: Required
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
    modelCache:
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
    modelCache:
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
│  │  Controller          │         │  (external)                    │  │
│  └──────────┬───────────┘         └───────────────┬────────────────┘  │
│             │                                     │ watches           │
│             │   ┌────────────────────────┐        │                   │
│             │   │  Cache Controller      │────────┘                   │
│             │   │  (pkg/cache/)          │                            │
│             │   └───────────┬────────────┘                            │
│             │               │ creates/reconciles                      │
│             │               ▼                                         │
│             │    ┌──────────────┐                                     │
│             │    │ Cache CRs    │                                     │
│             │    │ (provider-   │                                     │
│             │    │  specific)   │                                     │
│             │    └──────┬───────┘                                     │
│             │           │                                             │
│             │           ▼ (backend operator reconciles)               │
│             │    ┌──────────────────────────────────────────┐         │
│             │    │  Cache Server Pods                       │         │
│             │    │  (managed by backend operator)           │         │
│             │    └──────────────────────────────────────────┘         │
│             │                                                         │
│             │ creates/tracks                                          │
│             │                                                         │
│             ▼                                                         │
│   ┌──────────────────────────────────────────────────────────────┐    │
│   │  Model Pods (inference/tuning)                               │    │
│   │  +Injected PodMutations (env/vol/labels)                     │    │
│   └──────────────────────────────────────────────────────────────┘    │
└───────────────────────────────────────────────────────────────────────┘
```

**Component Interactions:**

1. The **Cache Backend Operator** (external, pre-installed) watches provider-specific Cache CRs and reconciles cache infrastructure (server pods, discovery service). Installed independently of KAITO.
2. The **Cache Controller** creates and reconciles the backend's Cache CR from provider configuration (Helm values), and monitors its status for readiness. Scope: cluster-level cache infrastructure only.
3. The **Workspace Controller** owns the per-workspace cache lifecycle: resolves providers per concern, queries readiness, gates inference pod creation on cache mode, and injects `PodMutations` into model pods.
4. **Cache warming** occurs during the ModelMirror download — either transparently via a cache-aware CSI driver (StorageClass), or via webhook injection into the download pod. No separate warming Jobs are required.
5. **Model pods** use injected configuration (env vars, volumes, labels, or KV connector config) to access cache infrastructure.


### Cache Scope

Cache infrastructure is inherently **model-scoped**: one set of cached weights serves all pods running the same model, regardless of how many Workspaces or InferenceSets reference it. This requires the cache configuration to be **shareable** across workloads without duplication.

**Options considered:**

| Option | API Location | Sharing scope | Pros | Cons |
|--------|-------------|---------------|------|------|
| 1. Workspace only | `Workspace.Cache` inline | Single workspace | Simple; no new CRD | Duplicated across InferenceSet replicas; no cross-workspace sharing |
| 2. InferenceSet + Workspace | `InferenceSetTemplate.Cache` inline, propagated to child Workspaces | All replicas of one InferenceSet | Single point per set; no new CRD; Workspace also supports standalone use | Cross-InferenceSet sharing requires duplicating config |
| 3. Cluster-scoped CRD | New cluster-scoped CRD, referenced by name | Any workload using the same model | Maximum sharing; one definition, many consumers | New CRD; reference resolution complexity |

**Chosen approach: Option 2 — `CacheSpec` on InferenceSet + Workspace, with InferenceSet propagation.**

`CacheSpec` is defined on both `InferenceSetTemplate` and `Workspace`. When an InferenceSet creates child Workspaces, it propagates `Cache` config to each child (the same way it propagates `Resource` and `Inference`). A standalone Workspace can also specify `Cache` directly.

**Inheritance chain:**

```
InferenceSet.spec.template.cache         ← defined once per model/set
     ↓ propagated to child Workspaces at creation time
Workspace.cache                          ← inherited from InferenceSet, or specified directly
```

See [Phase 3: Workspace Integration](#phase-3-workspace-integration) for propagation logic and resolution rules.

**Why not a cluster-scoped CRD?**
- Option 2 covers the dominant use case (N replicas of one model sharing one cache config) without introducing a new CRD.
- A cluster-scoped CRD can be introduced as a **non-breaking future addition** if cross-InferenceSet sharing becomes a common requirement.

**KV cache scope for disaggregated inference:** For `MultiRoleInference`, KV cache must be shared across prefill and decode roles (which are separate InferenceSets). Since the user creates the `MultiRoleInference` CR (not the InferenceSets directly), `MultiRoleInferenceSpec` will gain a `Cache *CacheSpec` field. The MultiRoleInference controller propagates the `kvCache` config to both role InferenceSets during `reconcileInferenceSet` (`pkg/controllers/multiroleinference/controller.go`), ensuring they connect to the same KV cache backend. This API addition to `MultiRoleInference` is deferred to when both the cache feature and MultiRoleInference reach beta, since MultiRoleInference is currently alpha and feature-gated.

### API Changes

#### CacheSpec (Workspace / InferenceSet Field)

`CacheSpec` appears on both `Workspace` and `InferenceSetTemplate`.

```go
// CacheSpec configures distributed caching for model workloads.
// Cache is a top-level Workspace field, applicable to both inference and tuning.
// Each concern (model weights, KV cache) is configured independently with its
// own provider and mode, allowing different backends per concern.
type CacheSpec struct {
    // ModelCache configures caching of model weight files.
    // +optional
    ModelCache *ModelCacheSpec `json:"modelCache,omitempty"`

    // KVCache configures caching of attention key/value tensors.
    // +optional
    KVCache *KVCacheSpec `json:"kvCache,omitempty"`
}

// ModelCacheSpec controls how model weight files are cached.
type ModelCacheSpec struct {
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

    // Config is the name of a ConfigMap in the same namespace containing
    // provider-specific model cache configuration. The provider validates
    // and merges this with its defaults from Helm values.
    // +optional
    Config string `json:"config,omitempty"`

    // CleanupOnDelete invalidates cached model data when workspace is deleted.
    // +optional
    CleanupOnDelete bool `json:"cleanupOnDelete,omitempty"`
}

// KVCacheSpec controls how attention KV tensors are cached.
type KVCacheSpec struct {
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

    // Config is the name of a ConfigMap in the same namespace containing
    // provider-specific KV cache configuration (e.g., cache size, TTL,
    // eviction policy). The provider validates and merges this with its
    // defaults from Helm values.
    // +optional
    Config string `json:"config,omitempty"`
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

The `InferenceSetTemplate` struct also gains a `Cache` field for propagation:

```go
type InferenceSetTemplate struct {
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Resource  InferenceSetResourceSpec   `json:"resource"`
    Inference kaitov1beta1.InferenceSpec `json:"inference"`

    // Cache configures distributed caching for all Workspaces created by this InferenceSet.
    // Propagated to child Workspaces at creation time.
    // +optional
    Cache *kaitov1beta1.CacheSpec `json:"cache,omitempty"`
}
```

The `MultiRoleInferenceSpec` struct also gains a `Cache` field (deferred to beta, see [Cache Scope](#cache-scope)):

```go
type MultiRoleInferenceSpec struct {
    // ...existing fields (Model, Roles, LabelSelector)...

    // Cache configures distributed caching for all roles in this MultiRoleInference.
    // The controller propagates kvCache config to both prefill and decode role
    // InferenceSets, ensuring they connect to the same KV cache backend.
    // +optional
    Cache *kaitov1beta1.CacheSpec `json:"cache,omitempty"`
}
```

#### Cache Provider Interface

```go
// CacheConcern identifies which cache concern a provider is being asked about.
type CacheConcern string

const (
    CacheConcernModelWeights CacheConcern = "ModelWeights"
    CacheConcernKVCache      CacheConcern = "KVCache"
)

// Provider defines the interface that cache implementations must satisfy.
type Provider interface {
    // Name returns the provider identifier (e.g., "fluid", "nvme-cache").
    Name() string

    // IsAvailable reports whether the cache infrastructure is installed
    // and the provider can operate (e.g., CRD exists, operator running).
    IsAvailable(ctx context.Context) (bool, error)

    // IsReady reports whether the cache is warmed and ready to serve.
    // Returns (ready, reason, error).
    IsReady(ctx context.Context) (bool, string, error)

    // PodMutations returns the pod-level changes needed for a specific cache
    // concern (ModelWeights or KVCache).
    PodMutations(ctx context.Context, concern CacheConcern, workspace *kaitov1beta1.Workspace, modelName, modelRevision string) (*PodMutations, error)

    // Cleanup invalidates cached data for the specified model.
    Cleanup(ctx context.Context, workspace *kaitov1beta1.Workspace, modelName string) error
}
```

**Note on provider lifecycle vs pod-mutation:** The `Provider` interface above covers the **pod-facing contract** — what changes to inject into model pods. Providers also have a **lifecycle/reconciliation responsibility**: creating and managing backend-specific resources (Cache CRs, ConfigMaps, discovery Services) that the pod-level mutations depend on. This lifecycle is driven by the Cache Controller (Phase 2) and is internal to each provider implementation, not expressed in this interface. See [Phase 2: Cache Controller](#phase-2-cache-controller) for details on resource ownership.
```

#### PodMutations

The Workspace controller calls `provider.PodMutations()` after resolving the effective cache config from the Workspace's `Cache` fields (see [Phase 3: Workspace Integration](#phase-3-workspace-integration) for the resolution flow). The returned `PodMutations` are merged into the model pod spec.

```go
// PodMutations describes all pod-level changes needed to enable cache access.
// Supports both env-var-based (e.g., storage interception libraries) and
// mount-based (e.g., FUSE, PVC) cache integrations.
type PodMutations struct {
    // Labels to add to the pod template metadata.
    // Used to trigger webhook-based injection (e.g., provider-specific CSI driver).
    Labels map[string]string
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
  providers:
    myCacheProvider:
      enabled: false
      namespace: "cache-system"
      # Provider-specific infrastructure configuration
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

For clusters where the cache backend is already deployed externally (not via KAITO's Helm chart), KAITO's Cache Controller discovers it at runtime through the provider's `IsAvailable()` check and reconciles provider-specific resources from configuration. The subchart dependency is not required in this case — only the feature gate and provider configuration need to be set.

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
- **Provider Lifecycle**: Once `IsAvailable()` confirms the backend is installed, reconcile provider-specific resources from Helm values configuration (e.g., Cache CRs, ConfigMaps, discovery Services). Providers manage their own backend-specific resources internally; the Cache Controller drives the lifecycle. This provisioning step is where providers create the resources that `PodMutations` later references — for example, a ConfigMap containing KV connector configuration that is then mounted into model pods via `PodMutations.Volumes`.
- **Readiness Monitoring**: Periodically call `provider.IsReady()` and expose a `CacheReady` condition. Workspace controllers query this before injecting PodMutations in Required mode.
- **RBAC**: Extend KAITO's ClusterRole with rules for provider-specific Cache CRs (get/list/watch/create/update), core API (nodes for topology, events for status). Provider-specific resource types are configurable per provider.

Register the controller in `cmd/workspace/main.go` behind the feature gate.

#### Phase 3: Workspace Integration

- Add `Cache *CacheSpec` to the `Workspace` struct in `api/v1beta1/workspace_types.go`.
- Add `Cache *CacheSpec` to `InferenceSetTemplate` in `api/v1alpha1/inferenceset_types.go`.
- Modify InferenceSet controller to propagate `Cache` to child Workspaces (at `inferenceset_controller.go:311+`):
  - At Workspace creation time, copy `template.cache` into `workspaceObj.Cache`.
  - The child Workspace carries the full cache config and is **self-contained** — it does not depend on the parent InferenceSet at runtime for cache resolution.
- Cache resolution rules (applied by the Workspace controller):
  1. If `workspace.Cache` has `ModelCache`/`KVCache` fields → use them.
  2. If no `Cache` on the Workspace and no parent InferenceSet → caching disabled (no-op).
- Modify workspace controller reconciliation to process each concern independently:
  - For `modelCache` (if configured):
    - Resolve provider via registry (`cache.Get(workspace.Cache.ModelCache.Provider)`)
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
- Add conditions to `WorkspaceStatus`: `ModelCacheReady`, `KVCacheReady`.

#### Phase 4: ModelMirror Integration (Cache Warming)

Cache warming is a side-effect of ModelMirror's model download, not a separate lifecycle phase. When a cache provider is installed, downloads write through to both persistent storage and the distributed cache simultaneously.

**CSI Driver Path (preferred):**

The cache provider exposes a CSI driver and registers a StorageClass. The workspace controller sets this StorageClass on the ModelMirror CR via `spec.storage.storageClassName`. All writes to the PVC flow through the CSI driver, which populates the distributed cache transparently.

**Webhook Path (alternative):**

For providers without a CSI driver, a mutating admission webhook intercepts ModelMirror download pods. The webhook matches pods using existing ModelMirror labels (e.g., `objectSelector` matching `kaito.sh/model-mirror` labels set by the ModelMirror controller). When matched, the webhook injects the cache interception layer (env vars, volumes, init containers) into the download pod. No changes to the ModelMirror API are required — the webhook targets labels the controller already sets.

**Mode interaction:**

- `Required`: Workspace controller gates inference pod creation on both ModelMirror reaching `Ready` AND `provider.IsReady()` confirming cache is warm.
- `Opportunistic`: Workspace controller gates only on ModelMirror `Ready`. If the cache is not yet warm, inference proceeds — the model streamer falls back to persistent storage and the cache warms lazily on first read.

**Cleanup:** On Workspace deletion with `cleanupOnDelete: true`, a finalizer calls `provider.Cleanup()` to invalidate cached model data. Cleanup is bounded by a timeout to prevent blocking deletion indefinitely.

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

**Pattern 5: Pod Label + Mutating Webhook**

Providers that use an external mutating admission webhook (e.g., a CSI driver injector) add labels to the pod template via `PodMutations.Labels`. The webhook watches for labelled pods and injects volumes, volume mounts, or sidecars at admission time:
```yaml
labels:
  cache-provider.example.com/inject: "true"
```
KAITO only applies the label; the webhook owns the injection logic. This keeps provider-specific mutation logic outside KAITO.

**Combining Patterns:** The `PodMutations` struct supports all patterns simultaneously. When both model weight and KV cache providers are configured, their mutations are merged. If the same provider serves both concerns, it deduplicates shared configuration (e.g., a single discovery endpoint env var used by both the storage interception library and the KV connector).

### Runtime Integration Details

This section specifies the **data-plane contracts** — what the model container actually communicates with at runtime, beyond the Kubernetes API / control-plane mechanics described above.

#### Model Weight Cache: Runtime Read Path

The in-pod consumer of cached model weights is the **[run:ai model streamer](https://github.com/run-ai/runai-model-streamer)** (not a HuggingFace download). When caching is enabled, the cache client layer sits between the streamer and blob storage:

```
┌─────────────────────────────────────────────────────────────────┐
│  vLLM pod                                                        │
│                                                                  │
│  run:ai model streamer (in-process)                              │
│       │                                                          │
│  Cache enabled:                                                  │
│       └──► Cache client layer                                    │
│                ├─ HIT ──► Distributed Cache                      │
│                └─ MISS ──► Blob Storage (transparent fallback)   │
│                                                                  │
│  Cache disabled (no provider / mode: Disabled):                  │
│       └──► Blob Storage directly (existing behavior)             │
└─────────────────────────────────────────────────────────────────┘
```

**Key points:**
- **Cache hit**: model loads in seconds (memory-speed, no network fetch from blob).
- **Cache miss**: handled transparently by the cache client layer — falls back to blob storage and lazily warms the cache on the read path. The streamer is unaware of the miss.
- **Cache disabled**: the streamer talks directly to blob storage, identical to today's behavior. No cache layer is involved.
- **Blob storage is abstracted, not bypassed** — it remains the backing origin for cache misses and initial population.

**Provider-injected configuration** (via `PodMutations` env vars):
```
RUNAI_STREAMER_CACHE_ENABLED=true
RUNAI_STREAMER_CACHE_ENDPOINT=http://cache-discovery.<ns>.svc.cluster.local:<port>
```

The streamer's cache-backend support was added in [runai-model-streamer#139](https://github.com/run-ai/runai-model-streamer/pull/139).

#### KV Cache: Runtime Integration

KV caching operates at the **API level** (not storage/filesystem level). It integrates with the inference engine's KV management layer. Two integration modes are supported:

**Mode 1: LMCache L2 Backend (primary, complementary to existing L1)**

KAITO's existing KV optimization uses [LMCache](https://docs.lmcache.ai/) for **L1** offloading (CPU memory / local NVMe). The distributed cache acts as an **L2 backend** behind LMCache:

```
┌──────────────────────────────────────────────────────────────┐
│  vLLM inference engine                                        │
│       │                                                       │
│       ├─ L1: LMCache (local CPU/NVMe offload) ← existing     │
│       │       │                                               │
│       │       └─ L2: Distributed KV Cache ← this proposal    │
│       │              (e.g., FlexKV, distributed KV stores)               │
│       │              Shared across pods/roles                  │
│       │                                                       │
│       └─ GPU HBM (hot KV, always present)                    │
└──────────────────────────────────────────────────────────────┘
```

**L1 and L2 are complementary, not exclusive.** LMCache's storage backend configuration is extended to register the distributed cache as a remote L2 store. Lookups flow: GPU HBM → L1 (local) → L2 (distributed) → recompute.

**Provider-injected configuration** (via `PodMutations` volumes):

The provider creates a runtime ConfigMap by merging admin defaults (from Helm values) with user-provided settings (from `KVCacheSpec.Config` ConfigMap, if specified). The result is mounted into the model pod:

```yaml
# ConfigMap: kv-cache-config (created by provider during lifecycle reconciliation)
# Merges Helm defaults + user's Config ConfigMap
data:
  lmcache_config.yaml: |
    storage_backend: "remote"
    remote_backend: "custom"
    custom_backend_module: "provider_lmcache_backend"
    custom_backend_config:
      endpoint: "cache-discovery.<ns>.svc.cluster.local:9065"
```

**Mode 2: vLLM KV Connector Replacement (advanced, provider-specific)**

For prefill/decode disaggregation (`MultiRoleInference`), the distributed cache acts as the **KV transfer layer** between prefill and decode pods, replacing or extending vLLM's built-in connector:

```
VLLM_KV_TRANSFER_CONFIG={"kv_connector":"ProviderConnector","locator_nodes":"cache-discovery.<ns>.svc.cluster.local:9065","protocol":"rdma"}
```

The connector implements vLLM's KVConnector interface, handling `put` (prefill writes KV) and `get` (decode reads KV) operations transparently.

**Note:** Mode 1 and Mode 2 are **mutually exclusive within a single pod** — vLLM has one KV path configured at startup. However, different InferenceSets can use different modes: for example, a single-role serving InferenceSet using Mode 1 (LMCache L2 for prefix reuse) alongside a MultiRoleInference using Mode 2 (connector replacement for P/D KV transfer). The provider's `PodMutations` for `CacheConcernKVCache` configures whichever mode is appropriate for the workload. If a provider replaces the entire vLLM KV connector (Mode 2), it should document whether LMCache's local CPU/NVMe offload (L1) remains in the data path or is bypassed, so users understand the resulting cache hierarchy.

#### Provider-Specific ConfigMap

Both model-weight and KV cache providers may author a **ConfigMap** containing structured runtime configuration too complex for individual env vars. The ConfigMap is delivered to pods via `PodMutations.Volumes` and `PodMutations.VolumeMounts` (as a ConfigMap-backed volume), or via `PodMutations.EnvVars` referencing individual keys. This uses the existing `PodMutations` fields — no additional mechanism is required.

This allows providers to deliver connector configs, endpoint discovery, TLS certs, or protocol parameters without polluting the env-var namespace.

### Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Cache provider not installed when feature gate enabled | Controller cannot verify cache readiness | Provider discovery check at startup + graceful degradation to `Unavailable` state; clear error events |
| Cache unavailable in `Required` mode blocks workspace indefinitely | Model deployment stuck | Configurable timeout with clear condition messaging; recommend `Opportunistic` mode for non-critical workloads |
| Provider-specific client library not in model image | Cache configured but model cannot use it | Document image requirements per provider; emit warning event if cache is enabled but provider reports incompatibility |
| Cache-eligible nodes not labelled correctly | Cache pods cannot schedule | Cache Controller's Node Watching labels eligible nodes automatically; emit event listing expected vs. found labels |
| Provider resource drift (manual edits to managed resources) | Cache Controller overwrites manual changes on next reconcile | Document which resources are managed; support annotation to opt out of reconciliation |
| Provider API incompatibility after upgrade | PodMutations generation fails | Pin provider versions in config; use unstructured client for CRD-based providers for forward compatibility |
| Cache rebalancing causes transient misses during scale events | Elevated latency during topology changes | Transparent to initial integration; future: surface `DegradedWarmup` status via provider |
| ModelMirror labels change in future releases | Webhook stops matching download pods | Pin expected labels in provider config; emit warning event if webhook receives no matching pods within expected timeframe |

## Alternatives

### Option 1: Pure Helm Subchart (No KAITO Controller)

Deploy the cache backend entirely via Helm subchart with static configuration. KAITO injects config but does not manage cache lifecycle.

**Pros:** Simplest implementation; no new controller code.
**Cons:** No dynamic adaptation to topology; no per-workspace mode control; no cleanup lifecycle; poor observability into cache state.

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

- **New installations**: Enable via `cache.enabled=true` in Helm values. Feature gate `FeatureFlagDistributedCache` must also be set to `true`. Add `cache` config to InferenceSet or Workspace specs to enable caching for workloads.
- **Existing installations**: No breaking changes. `Cache` field on Workspace and InferenceSetTemplate is optional; existing workspaces without it behave identically to today.
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

## Implementation History

- [ ] 05/18/2026: Initial proposal drafted
- [ ] MM/DD/YYYY: First round of feedback from maintainers
- [ ] MM/DD/YYYY: Open proposal PR
- [ ] MM/DD/YYYY: Proposal accepted
- [ ] MM/DD/YYYY: Phase 1 implementation PR (foundation + provider interface)
- [ ] MM/DD/YYYY: Phase 2 implementation PR (cache controller)
- [ ] MM/DD/YYYY: Phase 3 implementation PR (workspace integration)
