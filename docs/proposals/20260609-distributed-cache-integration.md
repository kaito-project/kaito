---
title: Distributed Cache Integration
authors:
  - "@croomes"
  - "@hasethuraman"
reviewers:
  - "@Fei-Guo"
  - "@chewong"
creation-date: 2026-06-09
last-updated: 2026-09-27
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
     - [CacheClass](#cacheclass)
     - [CacheClaim](#cacheclaim)
     - [Workspace / InferenceSet Claim References](#workspace--inferenceset-claim-references)
     - [MultiRoleInference Claim References](#multiroleinference-claim-references)
     - [ModelMirror Claim Reference](#modelmirror-claim-reference)
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

- **Cache Provider**: An implementation compiled into KAITO that translates CacheClass and CacheClaim intent into a vendor-specific Cache CR and pod mutations. The vendor operator, not the KAITO provider, deploys and manages the cache infrastructure.
- **Cache Controller**: A new KAITO controller that bridges workspace semantics with cache infrastructure, managing cache lifecycle and readiness signaling.
- **CacheClass**: A cluster-scoped definition of a cache provider's supported capabilities and default configuration.
- **CacheClaim**: A namespaced request to attach to or provision cache infrastructure through a CacheClass.
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
- Provide declarative, reusable cache configuration through CacheClasses and namespaced CacheClaims.
- Support multi-tenant isolation by keeping CacheClaims, vendor Cache CRs, and workload references within the tenant namespace; only cluster-admin defaults in CacheClass are shared globally.
- Support per-concern provider selection — different backends can be used for model weights vs. KV caching, or a single backend can serve both.
- Define a provider interface that allows different cache backends to be plugged in, each managing its own infrastructure lifecycle.
- Inject cache client configuration into model pods without modifying model images or inference code.
- Degrade gracefully when a vendor CRD/operator is unavailable or the cache cannot be provisioned.

### Non-Goals/Future Work

- **Implementing a cache backend** — KAITO provides the integration framework; actual caching is delegated to external operators (e.g., [Fluid](https://github.com/fluid-cloudnative/fluid), distributed NVMe services, KV stores).
- **Cache rebalancing coordination** — Provider-specific rebalancing will be transparent to KAITO initially. Deeper integration (scale-event signaling, drain-gate awareness) is a future enhancement.
- **Modifying KAITO model images** — Model images may need cache-specific client libraries. Image changes are tracked separately from this proposal.
- **KV cache eviction policy tuning** — TTL, growth factors, and write modes are provider-specific configuration supplied through CacheClass defaults and CacheClaim overrides.

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
  cacheClaims:
    modelWeights:
      name: llama-model-cache
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
  cacheClaims:
    kvCache:
      name: chat-kv-cache
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
  cacheClaims:
    modelWeights:
      name: unified-model-cache
    kvCache:
      name: unified-kv-cache
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
  cacheClaims:
    modelWeights:
      name: fluid-model-cache
    kvCache:
      name: flexkv-cache
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
  cacheClaims:
    modelWeights:
      name: opportunistic-model-cache
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
2. The **Cache Controller** resolves each CacheClaim through its CacheClass and invokes the selected in-repo provider. The provider attaches to an existing namespaced vendor Cache CR or creates one, then records the binding in CacheClaim status.
3. The **Workspace Controller** resolves CacheClaim references per concern, gates inference pod creation on the claim mode and binding status, and injects `PodMutations` into model pods.
4. The **ModelMirror Controller** resolves its model-weight CacheClaim and applies provider mutations to the download path, warming the bound cache with that model. No separate warming Jobs are required.
5. **Model pods** use injected configuration (env vars, volumes, labels, or KV connector config) to access cache infrastructure.


### Cache Scope

Model-weight cache is typically **model-scoped**: within one tenant namespace, a Shared CacheClaim or Shared vendor Cache CR can serve all Workspaces and InferenceSets using that model. An Exclusive claim remains dedicated to its own binding.

`CacheClass` is cluster-scoped and contains provider capabilities and default configuration managed by a cluster administrator. `CacheClaim` is namespaced and represents one requested model-weight or KV-cache attachment. Multiple workload instances may reference the same claim. A Shared claim may bind infrastructure used by other claims, while an Exclusive claim receives infrastructure dedicated to that claim.

The namespace is the tenant boundary: claims and workload references never resolve across namespaces, and Shared bindings are shared only within that boundary. A claim can name an existing vendor Cache CR in its namespace through `spec.cacheName`, or leave it empty so the provider resolves or creates a CR according to the binding rules. The vendor operator provisions the cache components, and KAITO records the bound CR name in `status.cacheName`.

**Attachment options:** Cache attachment can be declared at three workload levels:

| Option | API Location | Propagation | Use case |
|--------|--------------|-------------|----------|
| 1. Workspace | `Workspace.spec.cacheClaims` | The model-weight claim is copied to a managed ModelMirror created for the Workspace | Standalone Workspace attachment |
| 2. InferenceSet | `InferenceSet.spec.template.cacheClaims` | Copied to every child Workspace; each model-weight claim is then copied to its managed ModelMirror | One declaration shared by all Workspace replicas |
| 3. MultiRoleInference | `MultiRoleInference.spec.cacheClaims` | Copied to the prefill and decode InferenceSets, then to their child Workspaces | Shared model-weight and KV cache across both roles |

```
InferenceSet.spec.template.cacheClaims
     ↓ propagated to each child Workspace
Workspace.spec.cacheClaims
     ↓ modelWeights propagated when creating a managed ModelMirror
ModelMirror.spec.cacheClaim
```

For disaggregated inference, the propagation begins one level higher:

```
MultiRoleInference.spec.cacheClaims
     ↓ propagated to prefill and decode InferenceSets
InferenceSet.spec.template.cacheClaims
     ↓ propagated to each child Workspace
Workspace.spec.cacheClaims
```

A standalone ModelMirror may also declare a model-weight CacheClaim directly. This binds the requested cache infrastructure and warms it while the model is downloaded, without requiring a Workspace or InferenceSet.

### API Changes

#### CacheClass

`CacheClass` is cluster-scoped. It selects an in-KAITO provider, advertises the concerns, access modes, and operating modes it supports, and supplies provider-specific defaults. Claim parameters override class parameters.

```go
// +kubebuilder:resource:scope=Cluster
type CacheClass struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec              CacheClassSpec `json:"spec,omitempty"`
}

type CacheClassSpec struct {
    // Provider selects a cache provider registered in KAITO.
    // +kubebuilder:validation:MinLength=1
    Provider CacheProvider `json:"provider"`

    // +kubebuilder:validation:MinItems=1
    // +kubebuilder:validation:UniqueItems=true
    Concerns []CacheConcern `json:"concerns"`

    // +kubebuilder:validation:MinItems=1
    // +kubebuilder:validation:UniqueItems=true
    AccessModes []CacheAccessMode `json:"accessModes"`

    // +kubebuilder:validation:MinItems=1
    // +kubebuilder:validation:UniqueItems=true
    Modes []CacheMode `json:"modes"`

    // Provider-specific defaults, validated by the selected provider.
    // +optional
    Parameters apiextensionsv1.JSON `json:"parameters,omitempty"`
}

type CacheProvider string
type CacheConcern string
type CacheAccessMode string
type CacheMode string
type CacheClaimPhase string
```

KAITO installation creates a default DACS CacheClass for model-weight caching:

```yaml
apiVersion: cache.kaito.sh/v1alpha1
kind: CacheClass
metadata:
  name: dacs
spec:
  provider: dacs
  concerns: [ModelWeights]
  accessModes: [Shared, Exclusive]
  modes: [Required, Opportunistic]
  parameters:
    protocol: runai-streamer
```

#### CacheClaim

`CacheClaim` is namespaced. `cacheClassName` selects the in-repo provider and defaults. `cacheName` optionally names an existing vendor Cache CR in the same namespace; when omitted, the provider resolves or creates a CR according to the Shared or Exclusive binding rules.

```go
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
type CacheClaim struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec              CacheClaimSpec   `json:"spec,omitempty"`
    Status            CacheClaimStatus `json:"status,omitempty"`
}

type CacheClaimSpec struct {
    // CacheClassName references a cluster-scoped CacheClass.
    CacheClassName string `json:"cacheClassName"`

    // CacheName identifies an existing vendor Cache CR in this namespace.
    // If omitted, the provider resolves or creates one per the binding rules.
    // +optional
    CacheName string `json:"cacheName,omitempty"`

    // +kubebuilder:validation:Enum=ModelWeights;KVCache
    Concern CacheConcern `json:"concern"`

    // +kubebuilder:validation:Enum=Shared;Exclusive
    AccessMode CacheAccessMode `json:"accessMode"`

    // +kubebuilder:default:="Opportunistic"
    // +kubebuilder:validation:Enum=Required;Opportunistic
    Mode CacheMode `json:"mode,omitempty"`

    // Provider-specific overrides merged over CacheClass parameters.
    // +optional
    Parameters apiextensionsv1.JSON `json:"parameters,omitempty"`

    // Used when provider-specific cache infrastructure must be created.
    // +optional
    NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

type CacheClaimStatus struct {
    // +kubebuilder:validation:Enum=Pending;Bound;Failed
    Phase CacheClaimPhase `json:"phase,omitempty"`

    // CacheName is the vendor Cache CR attached or created by the provider.
    CacheName string `json:"cacheName,omitempty"`

    // AccessMode records the mode accepted for this binding.
    AccessMode CacheAccessMode `json:"accessMode,omitempty"`

    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

```yaml
apiVersion: cache.kaito.sh/v1alpha1
kind: CacheClaim
metadata:
  name: llama-model-cache
  namespace: team-a
spec:
  cacheClassName: dacs
  cacheName: shared-dacs
  concern: ModelWeights
  accessMode: Shared
  mode: Opportunistic
  parameters:
    protocol: runai-streamer
  nodeSelector:
    accelerator: nvidia
status:
  phase: Bound
  cacheName: shared-dacs
```

For an Exclusive claim, omit `cacheName` to have the provider create its deterministic vendor Cache CR and trigger dedicated infrastructure provisioning:

```yaml
apiVersion: cache.kaito.sh/v1alpha1
kind: CacheClaim
metadata:
  name: dedicated-model-cache
  namespace: team-a
spec:
  cacheClassName: dacs
  concern: ModelWeights
  accessMode: Exclusive
  mode: Required
  nodeSelector:
    accelerator: nvidia
```

For a Shared claim, omitting `cacheName` does not always create a private CR. Automatic discovery is scoped to the selected provider and CacheClaim namespace. If that scope contains exactly one vendor Cache CR annotated `cache.kaito.sh/access-mode: Shared`, the provider binds that cache. If none exists, it creates the claim's deterministic CR. If multiple Shared caches exist, the claim fails with `AmbiguousSharedCache` and the user must set `cacheName`.

The Cache Controller validates that the requested concern, access mode, and mode are supported by the CacheClass before invoking the provider. A missing CacheClass leaves the claim unprocessed with an explanatory condition.

**Binding and reclaim rules:**

- The provider serializes CacheClaim reconciliation within each namespace. The first claim successfully bound to a vendor Cache CR establishes its access mode.
- When the first claim creates or adopts a vendor Cache CR, the provider writes `cache.kaito.sh/access-mode: Shared|Exclusive` on that CR. Later claims validate this annotation before binding.
- The intended invariant is one automatically discoverable Shared vendor Cache CR per provider per namespace. `AmbiguousSharedCache` guards externally introduced or explicitly named configurations that violate this invariant; it is not the normal omitted-name path.
- Additional Shared claims may bind when the cache is already Shared.
- An Exclusive claim fails with an `AccessModeConflict` condition when any claim is already bound to that cache.
- A Shared claim fails with an `AccessModeConflict` condition when an Exclusive claim is already bound to that cache.
- When `spec.cacheName` is omitted, the provider first checks for the deterministic name `kaito-<CacheClaim UID>` so retries reuse a previously created CR.
- Otherwise, a Shared claim reuses the selected provider's only Shared vendor Cache CR in the namespace, creates its deterministic CR when none exists, or fails with `AmbiguousSharedCache` when multiple candidates exist.
- When `spec.cacheName` is omitted on an Exclusive claim, the provider always uses `kaito-<CacheClaim UID>`.
- Deterministic get-or-create ensures a retry resolves the same vendor CR even if the prior status update failed.
- Deleting a CacheClaim releases its binding but does not delete the vendor Cache CR, including CRs created by KAITO. The CR and its cache components are retained until the tenant namespace is deleted.

#### Workspace / InferenceSet Claim References

Workspace and InferenceSet reference existing CacheClaims in their namespace. Separate fields make the two concerns unique by construction.

```go
type CacheClaimReferences struct {
    // +optional
    ModelWeights *corev1.LocalObjectReference `json:"modelWeights,omitempty"`

    // +optional
    KVCache *corev1.LocalObjectReference `json:"kvCache,omitempty"`
}

type Workspace struct {
    // ...existing fields...

    // CacheClaims references model-weight and KV CacheClaims in this namespace.
    // +optional
    CacheClaims *CacheClaimReferences `json:"cacheClaims,omitempty"`
}

type InferenceSetTemplate struct {
    // ...existing fields...

    // CacheClaims is propagated to every child Workspace.
    // +optional
    CacheClaims *kaitov1beta1.CacheClaimReferences `json:"cacheClaims,omitempty"`
}
```

For example, one InferenceSet declaration attaches every Workspace replica to the same claims:

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: llama
  namespace: team-a
spec:
  replicas: 3
  template:
    cacheClaims:
      modelWeights:
        name: llama-model-cache
      kvCache:
        name: llama-kv-cache
    inference:
      preset:
        name: llama-3.3-70b-instruct
    resource:
      instanceType: Standard_ND96isr_H100_v5
```

The InferenceSet controller copies `template.cacheClaims` to every child Workspace. The Workspace controller verifies that each referenced claim is in `Bound` phase and that its `spec.concern` matches the corresponding field. A `Required` claim blocks workload creation until bound and ready; an `Opportunistic` claim allows the workload to proceed without cache mutations.

#### MultiRoleInference Claim References

`MultiRoleInferenceSpec` gains the same claim references. Its controller copies them to both the prefill and decode InferenceSet templates so all role Workspaces resolve the same model-weight and KV claims.

```go
type MultiRoleInferenceSpec struct {
    // ...existing fields...

    // CacheClaims is propagated to the prefill and decode InferenceSets.
    // +optional
    CacheClaims *kaitov1beta1.CacheClaimReferences `json:"cacheClaims,omitempty"`
}
```

The KV claim must be shared by both roles because it is the transfer layer between prefill and decode. The model-weight claim is also propagated to both roles so their managed ModelMirrors warm and serve the same model through the selected cache binding.

#### ModelMirror Claim Reference

A managed ModelMirror may reference one model-weight CacheClaim in its namespace. The reference causes the provider to create or attach the claim's cache infrastructure and inject the write-path configuration into the download Job, warming that cache with the ModelMirror's model.

```go
type ModelMirrorSpec struct {
    // ...existing fields...

    // CacheClaim references a ModelWeights CacheClaim in this namespace.
    // Supported only for Managed ModelMirrors.
    // +optional
    CacheClaim *corev1.LocalObjectReference `json:"cacheClaim,omitempty"`
}
```

```yaml
apiVersion: kaito.sh/v1alpha1
kind: ModelMirror
metadata:
  name: llama-model
  namespace: team-a
spec:
  mode: Managed
  source:
    registry: huggingface
    modelID: meta-llama/Llama-3.3-70B-Instruct
  storage:
    size: 150Gi
  cacheClaim:
    name: llama-model-cache
```

The ModelMirror controller resolves the reference in the ModelMirror namespace and requires the claim's concern to be `ModelWeights`. `cacheClaim` is invalid for a Static ModelMirror because no download occurs. In Required mode, the controller waits for the claim to bind before starting the download. In Opportunistic mode, it may download directly when the claim is unavailable. When a Workspace controller creates a managed ModelMirror, it propagates `workspace.spec.cacheClaims.modelWeights` to `ModelMirror.spec.cacheClaim`.

#### Cache Provider Interface

```go
const (
    CacheConcernModelWeights CacheConcern = "ModelWeights"
    CacheConcernKVCache      CacheConcern = "KVCache"
)

type CacheBinding struct {
    CacheName string
}

type ModelMirrorMutations struct {
    StorageClassName *string
    PodMutations     PodMutations
}

// Provider defines the interface that cache implementations must satisfy.
type Provider interface {
    // Name returns the CacheClass provider identifier (e.g., "dacs").
    Name() string

    // IsAvailable reports whether the cache infrastructure is installed
    // and the provider can operate (e.g., CRD exists, operator running).
    IsAvailable(ctx context.Context) (bool, error)

    // EnsureCache attaches to the named vendor Cache CR or creates one in the
    // CacheClaim namespace.
    EnsureCache(ctx context.Context, class *cachev1alpha1.CacheClass, claim *cachev1alpha1.CacheClaim) (*CacheBinding, error)

    // IsReady reports whether the bound cache infrastructure is ready.
    // Returns (ready, reason, error).
    IsReady(ctx context.Context, claim *cachev1alpha1.CacheClaim) (bool, string, error)

    // PodMutations returns the pod-level changes needed for a bound claim.
    PodMutations(ctx context.Context, claim *cachev1alpha1.CacheClaim, workspace *kaitov1beta1.Workspace, modelName, modelRevision string) (*PodMutations, error)

    // ModelMirrorMutations returns the storage and download-pod changes needed
    // to warm a bound model-weight claim.
    ModelMirrorMutations(ctx context.Context, claim *cachev1alpha1.CacheClaim, mirror *kaitov1alpha1.ModelMirror) (*ModelMirrorMutations, error)

    // Release removes provider-side state for a deleted binding. It does not
    // delete the vendor Cache CR.
    Release(ctx context.Context, claim *cachev1alpha1.CacheClaim) error
}
```

**Note on provider lifecycle vs pod-mutation:** Provider implementations are compiled into KAITO. `EnsureCache` creates or resolves the vendor Cache CR, while the independently installed vendor operator owns the resulting cache components. After the Cache Controller records the CR binding in CacheClaim status, `PodMutations` provides the serving-pod integration and `ModelMirrorMutations` provides the warming-path integration. See [Phase 2: Cache Controller](#phase-2-cache-controller) for resource ownership.

#### PodMutations

The Workspace controller calls `provider.PodMutations()` after resolving a bound CacheClaim and its CacheClass. The returned `PodMutations` are merged into the model pod spec.

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

### Implementation Details/Notes/Constraints

#### Installation Model

The integration has two distinct layers:

1. **KAITO provider adapter** — ships as part of KAITO and is registered in the in-process provider registry. It understands the vendor CR schema, creates or reads vendor Cache CRs, and generates pod mutations.
2. **Vendor cache control plane** — the vendor's CRD and operator are installed independently of KAITO. The operator watches namespaced Cache CRs and provisions the cache infrastructure components for each CR.

For example, DACS installs its Cache CRD and operator independently. Creating a DACS Cache CR in a namespace causes the DACS operator to provision the cache components for that namespace; KAITO does not deploy those components directly.

The attachment flow is:

- **Existing cache in the claim namespace**: `CacheClaim.spec.cacheName` names an existing vendor Cache CR. The in-repo provider reads and validates that CR and its `cache.kaito.sh/access-mode` annotation, then records its name in `CacheClaim.status.cacheName`.
- **No cache name on a Shared claim**: The provider first reuses its deterministic CR when present. Otherwise, it binds the sole Shared vendor Cache CR for the selected provider in the namespace, creates `kaito-<CacheClaim UID>` when none exists, or reports `AmbiguousSharedCache` when multiple candidates require explicit selection.
- **No cache name on an Exclusive claim**: The provider uses `kaito-<CacheClaim UID>` to get or create a dedicated vendor Cache CR.

When creating the CR, the provider applies the merged provider, CacheClass, and CacheClaim configuration and annotates its access mode. The vendor operator then provisions the namespace's cache components, and KAITO records the resolved CR name in claim status.

KAITO installation creates its CacheClass defaults, including the DACS model-weight CacheClass. The matching vendor CRD and operator must already be installed before claims using that class can bind. `Provider.IsAvailable()` verifies those prerequisites and reports an unbound claim condition when they are missing.

#### Phase 1: Foundation

- Add `FeatureFlagDistributedCache` constant (`pkg/utils/consts/consts.go`) and register it in `pkg/featuregates/featuregates.go` (default: `false`).
- Create `pkg/cache/` package with provider interface, `PodMutations` type, and provider registry.
- Implement supported provider adapters, including DACS, in `pkg/cache/`; providers are compiled into KAITO rather than installed as runtime plugins.
- Add the cluster-scoped CacheClass and namespaced CacheClaim APIs under `cache.kaito.sh/v1alpha1`, including CacheClaim status and conditions.
- Implement no-op provider (`pkg/cache/noop/`) for testing.
- Install a default DACS CacheClass for model weights with the KAITO chart.
- Add startup validation for duplicate provider registrations.

#### Phase 2: Cache Controller

Create `pkg/cache/controller.go` with the Cache Controller:

- **Provider Discovery**: Resolve `CacheClaim.spec.cacheClassName` to `CacheClass.spec.provider`, then select the compiled-in provider from the registry. If the class or provider is unavailable, leave the claim unbound and set a clear condition without crashing.
- **Claim Validation**: Validate the requested concern, access mode, and mode against the CacheClass. Merge parameters in this order: CacheClaim overrides, CacheClass defaults, provider defaults.
- **Node Watching**: Watch eligible nodes (Ready, schedulable, matching the claim's node selector) to inform providers about cache topology.
- **Provider Lifecycle**: Serialize CacheClaim reconciliation within the namespace, then call `EnsureCache()`. An explicit `spec.cacheName` selects that vendor Cache CR. Without a name, a Shared claim reuses its deterministic CR, the selected provider's sole CR annotated Shared in that namespace, or creates its deterministic CR when none exists; multiple Shared candidates produce `AmbiguousSharedCache`. An Exclusive claim always uses its deterministic CR. The first successful binding records `cache.kaito.sh/access-mode` on the vendor CR, and incompatible later claims fail with `AccessModeConflict`.
- **Claim Status**: Record `Pending`, `Bound`, or `Failed`, the resolved cache name, accepted access mode, and detailed conditions in `CacheClaim.status`.
- **Readiness Monitoring**: Periodically call `provider.IsReady()` for bound claims and expose infrastructure readiness in claim conditions. Workspace and ModelMirror controllers query this before applying provider mutations.
- **RBAC**: Extend KAITO's ClusterRole with rules for provider-specific Cache CRs (get/list/watch/create/update), core API (nodes for topology, events for status). Provider-specific resource types are configurable per provider.

Register the controller in `cmd/workspace/main.go` behind the feature gate.

#### Phase 3: Workspace Integration

- Add `CacheClaims *CacheClaimReferences` to the `Workspace` struct in `api/v1beta1/workspace_types.go`.
- Add `CacheClaims *CacheClaimReferences` to `InferenceSetTemplate` in `api/v1alpha1/inferenceset_types.go`.
- Add `CacheClaims *CacheClaimReferences` to `MultiRoleInferenceSpec` in `api/v1alpha1/multiroleinference_types.go`.
- Modify the MultiRoleInference controller to propagate `cacheClaims` to both the prefill and decode InferenceSet templates.
- Modify the InferenceSet controller to propagate `template.cacheClaims` to every child Workspace (at `inferenceset_controller.go:311+`).
- When creating a managed ModelMirror, propagate the Workspace's `modelWeights` claim to `ModelMirror.spec.cacheClaim`.
- Cache resolution rules (applied by the Workspace controller):
  1. Resolve each reference to a CacheClaim in the Workspace namespace.
  2. Verify the claim is `Bound` and its concern matches the reference field.
  3. Resolve the provider through the claim's CacheClass.
  4. If a reference is absent, caching for that concern is disabled (no-op).
- Modify workspace controller reconciliation to process each concern independently:
  - For `modelWeights`, resolve the referenced claim, apply its Required or Opportunistic mode, and collect `PodMutations()`.
  - For `kvCache`, independently resolve the referenced claim, apply its mode, and collect `PodMutations()`.
  - Merge all PodMutations (deduplicate env vars if same provider used for both)
  - Inject merged mutations into model pod specs
- Add validation webhook rules:
  - Each claim reference must use the matching concern.
  - The same claim cannot be used for incompatible concerns.
- Add conditions to `WorkspaceStatus`: `ModelCacheReady`, `KVCacheReady`.

#### Phase 4: ModelMirror Integration (Cache Warming)

Cache warming is a side-effect of a managed ModelMirror's model download, not a separate lifecycle phase. `ModelMirror.spec.cacheClaim` explicitly selects the model-weight claim to create or attach and warm. The ModelMirror controller resolves the bound claim and calls `provider.ModelMirrorMutations()` before creating the PVC and download Job.

- Validate that the claim exists in the ModelMirror namespace, has concern `ModelWeights`, and is backed by an available CacheClass.
- Reject `cacheClaim` on Static ModelMirrors.
- Surface binding and warming progress through ModelMirror conditions, including provider errors. Set `CacheWarm=True` only after the provider-mutated download completes successfully.
- If provider-selected storage conflicts with an explicitly configured `spec.storage.storageClassName`, report the conflict instead of silently overriding either value.

**CSI Driver Path (preferred):**

The cache provider exposes a CSI driver and registers a StorageClass. `ModelMirrorMutations.StorageClassName` selects that class for the ModelMirror PVC. All writes to the PVC flow through the CSI driver, which populates the distributed cache transparently.

**Webhook Path (alternative):**

For providers without a CSI driver, `ModelMirrorMutations.PodMutations` adds the provider's labels, environment variables, volumes, or init containers to the download Job. A provider webhook may use those labels to inject its cache interception layer.

**Mode interaction:**

- `Required`: The ModelMirror controller waits for the claim to bind and become ready before starting the download. The Workspace controller gates inference pod creation on both ModelMirror reaching `Ready` and its `CacheWarm=True` condition.
- `Opportunistic`: If the claim is unavailable, ModelMirror downloads to persistent storage without cache mutations. Inference may proceed when ModelMirror is `Ready`, and the cache can warm lazily on first read.

**Reclaim:** CacheClaim deletion releases its logical binding but never deletes the vendor Cache CR. This applies to both pre-existing CRs and CRs created by KAITO. The namespaced CR and its cache components are retained for reuse and are deleted with the tenant namespace.

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
│  Cache disabled (no CacheClaim reference):                       │
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

The provider creates a runtime ConfigMap by merging provider defaults, CacheClass parameters, and CacheClaim overrides. The result is mounted into the model pod:

```yaml
# ConfigMap: kv-cache-config (created by provider during lifecycle reconciliation)
# Merges provider defaults + CacheClass defaults + CacheClaim overrides
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
| CacheClass or its provider is unavailable | CacheClaim cannot bind | Leave the claim unbound and report a clear condition; `Opportunistic` workloads may proceed without cache mutations |
| Vendor CRD or operator is unavailable | KAITO cannot create or observe the namespaced vendor Cache CR | `Provider.IsAvailable()` checks vendor prerequisites and reports a claim condition without attempting fallback infrastructure |
| Concurrent Shared and Exclusive claims target one cache | Access-mode policy becomes nondeterministic | Provider serializes reconciliation within the namespace; first successful binding wins and incompatible claims fail with `AccessModeConflict` |
| Cache unavailable in `Required` mode blocks workspace indefinitely | Model deployment stuck | Configurable timeout with clear condition messaging; recommend `Opportunistic` mode for non-critical workloads |
| Provider-specific client library not in model image | Cache configured but model cannot use it | Document image requirements per provider; emit warning event if cache is enabled but provider reports incompatibility |
| Cache-eligible nodes not labelled correctly | Cache pods cannot schedule | Cache Controller's Node Watching labels eligible nodes automatically; emit event listing expected vs. found labels |
| Provider resource drift (manual edits to managed resources) | Cache Controller overwrites manual changes on next reconcile | Document which resources are managed; support annotation to opt out of reconciliation |
| Provider API incompatibility after upgrade | PodMutations generation fails | Pin provider versions in config; use unstructured client for CRD-based providers for forward compatibility |
| Cache rebalancing causes transient misses during scale events | Elevated latency during topology changes | Transparent to initial integration; future: surface `DegradedWarmup` status via provider |
| ModelMirror cache mutation conflicts with explicit storage configuration | Download cannot safely use both paths | Report a ModelMirror condition and require the user to select one compatible storage path |

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

KAITO defines a provider interface with vendor adapters compiled into the repository. An adapter creates or resolves the vendor's namespaced Cache CR and injects client configuration through `PodMutations`; the external vendor operator reconciles that CR into cache infrastructure.

**Pros:** Clear separation of concerns; KAITO owns attachment semantics while each vendor operator owns its infrastructure; automatic benefit from vendor improvements; extensible through additional in-repo adapters.
**Cons:** Runtime dependency on the vendor CRD and operator; each supported vendor requires a KAITO adapter and compatible RBAC.

## Upgrade Strategy

- **New installations**: Install the vendor CRD and operator independently, enable `FeatureFlagDistributedCache`, and install KAITO's default CacheClasses. Create CacheClaims and reference them from InferenceSet, Workspace, or managed ModelMirror specs.
- **Existing installations**: No breaking changes. CacheClaim references on Workspace, InferenceSetTemplate, and ModelMirror are optional; existing resources without references behave identically to today.
- **Provider upgrades**: KAITO provider adapters version with KAITO. Vendor CRDs and operators version independently; KAITO documents compatible versions and validates availability at runtime.
- **Feature gate promotion**: Once stable, promote `FeatureFlagDistributedCache` to default-on (beta), then remove the gate (GA).
- **Provider interface stability**: The `Provider` interface remains internal to KAITO; new vendors are added as in-repo adapters.

## Additional Details

### Test Plan

| Category | Scope | Approach |
|----------|-------|----------|
| Unit tests | Provider interface, CacheClass/CacheClaim validation, parameter merging, PodMutations generation | Standard Go table-driven tests with mock provider |
| Unit tests | CacheClaim reconciliation and status transitions | envtest with mock provider |
| Unit tests | First-come access-mode arbitration | Reconcile concurrent Shared/Exclusive combinations in one namespace; verify only compatible claims reach `Bound` |
| Unit tests | Shared cache discovery | Verify omitted `cacheName` behavior for zero, one, and multiple annotated Shared CRs, including deterministic retry preference |
| Unit tests | MultiRoleInference → InferenceSet → Workspace → ModelMirror claim propagation | Table-driven controller tests covering absent, model-weight-only, KV-only, and dual-concern references |
| Integration tests | Existing and new namespace cache attachment | envtest with a fake vendor CRD; verify attachment to a named CR and creation of a CR in the CacheClaim namespace |
| Integration tests | Deterministic create retry and reclaim | Fail the first status write; verify retry reuses `kaito-<CacheClaim UID>`, and claim deletion retains the vendor CR |
| Integration tests | Multi-tenant namespace isolation | Create identical claim and vendor CR names in two namespaces; verify each workload resolves only its namespace's binding |
| Integration tests | End-to-end cache lifecycle (create class and claim → claim bound → workspace pods injected) | envtest with fake provider CRD; verify status and PodMutations |
| Integration tests | ModelMirror create/attach and warming | envtest with a fake provider; verify claim resolution, storage/download mutations, and conditions |
| Integration tests | Graceful degradation (class/provider absent, claim not ready) | envtest without provider; verify workspace proceeds in Opportunistic mode |
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
