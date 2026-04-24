---
title: MultiRoleInference for Prefill/Decode Disaggregated Inference
authors:
  - "@andyzhangx"
reviewers:
  - "@Fei-Guo"
  - "@rambohe-ch"
  - "@zhuangqh"
creation-date: 2026-04-24
last-updated: 2026-04-24
status: provisional
see-also:
  - "[Planned] Migrate EPP to llm-d inference scheduler"
  - "/docs/proposals/20250704-keda-scaler-for-inference-workloads.md"
---
# MultiRoleInference for Prefill/Decode Disaggregated Inference

## Summary

This proposal introduces a `MultiRoleInference` CRD for prefill/decode (P/D) disaggregated inference in KAITO using [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler) ([architecture](https://github.com/llm-d/llm-d-inference-scheduler/blob/main/docs/architecture.md)) as the routing layer, built on the Kubernetes-native Gateway API stack. The MultiRoleInference controller creates separate child InferenceSets for prefill and decode roles, a shared InferencePool, and orchestrates KV cache transfer between them via NixlConnector.

## Motivation

Large language models (LLMs) like DeepSeek-V3 benefit significantly from separating the prefill (prompt processing) and decode (token generation) phases onto different GPU pools. Prefill is compute-bound and benefits from high parallelism, while decode is memory-bound and benefits from optimized KV cache utilization. Running both on the same GPU pool leads to suboptimal resource utilization.

### Goals

- Define a `MultiRoleInference` CRD that allows users to specify prefill and decode roles with independent replica counts, instance types, and configurations
- Automatically create and manage child InferenceSets for each role
- Integrate with llm-d inference scheduler for P/D-aware request routing via Gateway API
- Support multi-GPU Ray cluster topologies (head pod routing via `apps.kubernetes.io/pod-index: "0"`)
- Enable independent KEDA autoscaling for prefill and decode roles

### Non-Goals/Future Work

- Custom scheduling algorithms beyond llm-d's built-in plugins
- Cross-cluster P/D disaggregation
- Non-vLLM inference engines

## Proposal

### Request Flow

```
Client
  │  POST /v1/chat/completions
  │  {"model": "deepseek-v32", "messages": [...]}
  ▼
Gateway (Envoy-based: Istio Gateway or Envoy Gateway)
  │
  ▼
BBR (ext-proc)                          ◄── Extract model name from body
  │  Inject header: X-Gateway-Model-Name: deepseek-v32
  ▼
HTTPRoute                               ◄── Match header → route to InferencePool
  │  backendRefs: deepseek-v32
  ▼
llm-d EPP (ext-proc)                    ◄── P/D disaggregation scheduling
  │
  │  1. disagg-profile-handler decides: prefill needed?
  │  2. by-label-selector filters decode pods
  │  3. scorer ranks candidates (prefix-cache + load-aware)
  │  4. picker selects best decode pod
  │  5. disagg-headers-handler sets x-prefiller-host-port header (prefill-pod-ip:8000)
  │
  └──► decode workspace (inference-role=decode)
         routing sidecar (port 8080) receives request
         ├── if x-prefiller-host-port header present:
         │     contacts prefill pod → KV transfer via NixlConnector → decode
         └── if no header: prefill + decode locally
```

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                     MultiRoleInference CR                        │
│  name: deepseek-v32                                              │
│  roles: [prefill x2, decode x3]                                  │
└──────────────────────────┬───────────────────────────────────────┘
                           │ controller reconcile
                           │
       ┌───────────────────┼───────────────────────────┐
       │                   │                           │
       ▼                   ▼                           ▼
┌──────────────┐   ┌──────────────┐   ┌────────────────────────────┐
│ InferenceSet │   │ InferenceSet │   │ InferencePool              │
│ deepseek-v32 │   │ deepseek-v32 │   │ deepseek-v32               │
│ -prefill     │   │ -decode      │   │                            │
│ replicas: 2  │   │ replicas: 3  │   │ selector:                  │
│              │   │              │   │   apps: deepseek-v32       │
│ workspaces:  │   │ workspaces:  │   │                            │
│  ws-0        │   │  ws-0        │   │ ┌────────────────────────┐ │
│  ws-1        │   │  ws-1        │   │ │ llm-d EPP              │ │
│              │   │  ws-2        │   │ │ disagg-profile-handler │ │
│ ws labels:   │   │ ws labels:   │   │ │ prefill-filter         │ │
│  apps:       │   │  apps:       │   │ │ decode-filter          │ │
│   deepseek-  │   │   deepseek-  │   │ └────────────────────────┘ │
│   v32        │   │   v32        │   │                            │
│  inference-  │   │  inference-  │   │                            │
│   role:      │   │   role:      │   │                            │
│   prefill    │   │   decode     │   │                            │
└──────────────┘   └──────────────┘   └────────────────────────────┘
                                                ▲
                                                │ ext-proc
                                      ┌─────────┴──────────┐
                                      │  Gateway (Envoy)    │
                                      │  + BBR              │
                                      │  + HTTPRoute        │
                                      └────────────────────┘
```

## MultiRoleInference CRD

### User-Facing CR

```yaml
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  # The InferencePool resource name matches the MRI name.
  # The HelmRelease that deploys this pool is named "deepseek-v32-inferencepool".
  name: deepseek-v32
  namespace: default
spec:
  labelSelector:
    matchLabels:
      apps: deepseek-v32
  model:
    name: deepseek-ai/DeepSeek-V3.2
    modelAccessSecret: hf-token
  # Optional: custom EPP plugins. If not set, controller auto-generates P/D config.
  eppPluginsConfigRef:
    name: deepseek-v32-epp-plugins
  roles:
    - type: prefill
      replicas: 2
      instanceType: Standard_NC24ads_A100_v4
      runtimeConfigRef: prefill-params   # optional ConfigMap for role-specific vLLM args
    - type: decode
      replicas: 3
      instanceType: Standard_NC24ads_A100_v4
      runtimeConfigRef: decode-params    # optional ConfigMap for role-specific vLLM args
```

**Design rationale for the simplified CRD:**

- **`model` at top level** — the model is the core shared resource across all roles. Elevating it from `inference.preset.name` makes the intent immediately clear: "one model, multiple roles". This avoids the indirection through `InferenceSpec.Preset.PresetMeta.Name` which is verbose and inherits Workspace-era complexity.
- **`instanceType` per role** — P/D disaggregation often uses different GPU SKUs (e.g., large memory for prefill, smaller for decode). Placing `instanceType` directly on the role keeps the most common tuning knob visible at the top level.
- **`runtimeConfigRef` per role** — each role may need different vLLM runtime arguments (e.g., prefill: `--max-num-batched-tokens`, decode: `--kv-cache-dtype`). Named `runtimeConfigRef` instead of generic `config` to clearly indicate it references vLLM/runtime parameters.
- **`modelAccessSecret` on model** — the secret is model-scoped (HuggingFace token), not role-scoped. Placing it next to the model name is the natural location.

### API Types

```go
type MultiRoleInferenceRoleType string

const (
    MultiRoleInferenceRolePrefill MultiRoleInferenceRoleType = "prefill"
    MultiRoleInferenceRoleDecode  MultiRoleInferenceRoleType = "decode"
)

// MultiRoleInferenceModelSpec defines the shared model configuration.
// The model is the core shared resource — all roles serve the same model
// with different runtime configurations.
type MultiRoleInferenceModelSpec struct {
    // Name is the model identifier (e.g., HuggingFace model ID).
    // This maps to the preset name used when generating child InferenceSets.
    // +required
    Name string `json:"name"`

    // ModelAccessSecret references a Secret containing credentials for model download
    // (e.g., HuggingFace token). Applied to all child workloads.
    // +optional
    ModelAccessSecret string `json:"modelAccessSecret,omitempty"`
}

type MultiRoleInferenceRoleSpec struct {
    // Type is the role type. Supported values: prefill, decode.
    // +kubebuilder:validation:Enum=prefill;decode
    Type MultiRoleInferenceRoleType `json:"type"`

    // Replicas is the number of workspaces (InferenceSet replicas) for this role.
    // Each replica maps to one Workspace → one StatefulSet.
    // +kubebuilder:default=1
    // +kubebuilder:validation:Minimum=1
    Replicas int32 `json:"replicas"`

    // InstanceType specifies the GPU node SKU for this role.
    // Different roles may use different instance types (e.g., larger GPU for prefill).
    // +required
    InstanceType string `json:"instanceType"`

    // RuntimeConfigRef references a ConfigMap with role-specific vLLM runtime arguments.
    // These override or extend the default inference parameters for this role.
    // +optional
    RuntimeConfigRef string `json:"runtimeConfigRef,omitempty"`
}

type MultiRoleInferenceSpec struct {
    // LabelSelector is propagated to generated child workloads (InferenceSets, Workspaces).
    // +required
    LabelSelector *metav1.LabelSelector `json:"labelSelector"`

    // Model defines the shared model configuration across all roles.
    // +required
    Model MultiRoleInferenceModelSpec `json:"model"`

    // EPPPluginsConfigRef references a ConfigMap containing custom EPP plugins configuration.
    // If not set, the controller auto-generates a standard P/D disaggregation plugin config
    // with prefill-filter, decode-filter, precise-prefix-cache-scorer, and load-aware-scorer.
    // +optional
    EPPPluginsConfigRef *corev1.LocalObjectReference `json:"eppPluginsConfigRef,omitempty"`

    // Roles defines the role topology of this inference service.
    // Exactly two roles are required: one prefill and one decode.
    // +kubebuilder:validation:MinItems=2
    // +kubebuilder:validation:MaxItems=2
    // +kubebuilder:validation:XValidation:rule="self.exists(r, r.type == 'prefill') && self.exists(r, r.type == 'decode')",message="exactly one prefill and one decode role required"
    Roles []MultiRoleInferenceRoleSpec `json:"roles"`
}

type MultiRoleInferenceStatus struct {
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model.name"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MultiRoleInference struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec              MultiRoleInferenceSpec   `json:"spec"`
    Status            MultiRoleInferenceStatus `json:"status,omitempty"`
}
```

## Controller-Generated Resources

The MultiRoleInference controller reconciles one CR into the following 6 types of resources:

### 1. Prefill InferenceSet

The controller creates **one** prefill InferenceSet with `spec.replicas` set from `roles[prefill].replicas`. For the example MRI with `prefill.replicas: 2`, the generated InferenceSet has `spec.replicas: 2` (2 prefill workspaces):

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: deepseek-v32-prefill
  namespace: default
  labels:
    kaito.sh/parent: deepseek-v32
    kaito.sh/role: prefill
  ownerReferences:
    - apiVersion: kaito.sh/v1alpha1
      kind: MultiRoleInference
      name: deepseek-v32
      controller: true
      blockOwnerDeletion: true
spec:
  replicas: 2
  labelSelector:
    matchLabels:
      apps: deepseek-v32
  template:
    metadata:
      labels:
        apps: deepseek-v32
        inference-role: prefill
        kaito.sh/parent: deepseek-v32
    resource:
      instanceType: Standard_NC24ads_A100_v4
    inference:
      preset:
        name: deepseek-ai/DeepSeek-V3.2
        presetOptions:
          modelAccessSecret: hf-token
      config: prefill-params    # from MRI roles[prefill].runtimeConfigRef
```

The `runtimeConfigRef` field references a ConfigMap with role-specific vLLM arguments. Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prefill-params
data:
  inference_config.yaml: |
    max_probe_steps: 6
    vllm:
      tensor-parallel-size: 1
      max_model_len: 1024
      gpu-memory-utilization: 0.95
      kv-transfer-config: '{"kv_connector":"NixlConnector","kv_role":"kv_both","kv_load_failure_policy":"fail"}'
```

### 2. Decode InferenceSet with Sidecar Container

The controller creates **one** decode InferenceSet with `spec.replicas` set from `roles[decode].replicas`. For the example MRI with `decode.replicas: 3`, the generated InferenceSet has `spec.replicas: 3` (3 decode workspaces). Each decode workspace has `inference-role: decode` label and decode vLLM config. **Critically, decode workspaces require a sidecar container** for P/D coordination.

#### Why Decode Pods Need a Sidecar

In the llm-d P/D architecture ([disaggregation docs](https://github.com/llm-d/llm-d-inference-scheduler/blob/main/docs/disaggregation.md)), all requests are routed to the **decode worker first**. The decode worker's sidecar is responsible for:

1. Receiving EPP metadata (selected decode workspace + optional prefill workspace via `x-prefiller-host-port` header)
2. If prefill is disaggregated → forwarding the prefill request to the selected prefill worker and waiting for KV cache parameters
3. Sending the decode request to the local vLLM engine with `remote_prefill=true` and the KV cache block IDs
4. Returning the final response through the inference gateway

> **Note**: No sidecar or coordination logic is needed on prefill workspaces. Prefill workspaces are stateless workers that process prompts and produce KV cache.

#### P/D Request Sequence

```
1. Client → Inference Gateway (Envoy + EPP)
2. EPP runs disagg-profile-handler:
   a. Decode stage: select a decode pod (always runs first)
   b. Prefill stage: PD decider evaluates prompt length + prefix cache hit
      - High cache hit or short prompt → skip prefill, decode handles everything
      - Low cache hit + long prompt → select a prefill pod
3. Request lands on Decode Worker Sidecar
   a. If x-prefiller-host-port header exists:
      - Sidecar → Prefill Worker: send prompt (max_tokens=1)
      - Prefill Worker: run prefill, produce KV cache
      - Prefill Worker → Sidecar: return KV cache parameters (prefill ID + memory block IDs)
      - Sidecar → local vLLM: decode with remote_prefill=true
      - local vLLM → Prefill Worker: read KV cache via NixlConnector
      - local vLLM: run decode, generate tokens
   b. If no prefill header → run both prefill + decode locally
4. Decode Worker → Sidecar → Gateway → Client
```

#### Generated Decode InferenceSet

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: deepseek-v32-decode
  namespace: default
  labels:
    kaito.sh/parent: deepseek-v32
    kaito.sh/role: decode
  ownerReferences:
    - apiVersion: kaito.sh/v1alpha1
      kind: MultiRoleInference
      name: deepseek-v32
      controller: true
      blockOwnerDeletion: true
spec:
  replicas: 3
  labelSelector:
    matchLabels:
      apps: deepseek-v32
  template:
    metadata:
      labels:
        apps: deepseek-v32
        inference-role: decode
        kaito.sh/parent: deepseek-v32
    resource:
      instanceType: Standard_NC24ads_A100_v4
    inference:
      preset:
        name: deepseek-ai/DeepSeek-V3.2
        presetOptions:
          modelAccessSecret: hf-token
      config: decode-params     # from MRI roles[decode].runtimeConfigRef
```

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: decode-params
data:
  inference_config.yaml: |
    max_probe_steps: 6
    vllm:
      tensor-parallel-size: 1
      max_model_len: 1024
      gpu-memory-utilization: 0.95
      kv-transfer-config: '{"kv_connector":"NixlConnector","kv_role":"kv_both","kv_load_failure_policy":"fail"}'
```

#### Sidecar Injection

The Workspace controller creates decode StatefulSets with the routing sidecar container included. When the Workspace controller detects the `inference-role: decode` label during reconciliation, it adds the [llm-d routing sidecar](https://github.com/llm-d/llm-d-routing-sidecar) as part of the StatefulSet's container spec alongside the main vLLM container. Prefill workspaces (labeled `inference-role: prefill`) are created without the sidecar. The sidecar handles P/D coordination:

```yaml
# Injected into decode pod spec by the controller
containers:
  - name: vllm                          # main inference container (existing)
    # ...
  - name: llm-d-routing-sidecar         # sidecar (injected by controller)
    image: mcr.microsoft.com/oss/v2/llm-d/llm-d-routing-sidecar:v0.7.0
    ports:
      - containerPort: 8080             # sidecar listens for incoming requests
        name: sidecar
    env:
      - name: BACKEND_URL
        value: "http://localhost:5000"  # local vLLM engine
      - name: POD_IP
        valueFrom:
          fieldRef:
            fieldPath: status.podIP
```

The sidecar sits in front of the vLLM engine on decode workspaces:
- Incoming requests hit the sidecar (port 8080)
- Sidecar orchestrates prefill (if needed) and then forwards to local vLLM (port 5000)
- The InferencePool `targetPorts` should point to the sidecar port (8080) for decode workspaces

> **Multi-node Ray cluster**: Since the sidecar is part of the StatefulSet pod template, all decode pods (head + workers) will have the sidecar container. Only the head pod (index 0) receives traffic from the EPP, so only its sidecar is actively working. Worker pod sidecars remain idle.

#### Sidecar Injection Mechanism: Workspace Controller Label-Based Injection

Four approaches were evaluated for injecting the routing sidecar into decode pods:

1. **MRI controller patches StatefulSet directly** — rejected due to controller competition (MRI controller and Workspace controller both reconciling the same StatefulSet, causing override loops)
2. **InferenceSet API `additionalContainers` field** — rejected; the sidecar configuration is fully deterministic (fixed image, port, env vars), so exposing it in the API adds unnecessary complexity without user value
3. **Mutating webhook** — rejected due to operational overhead (extra component to deploy/maintain, TLS certs, availability risk) and poor observability (sidecar not visible in pod spec until admission)
4. **Workspace controller includes sidecar in decode StatefulSet** ✅ — the Workspace controller checks the `inference-role` label during reconciliation. When it detects `inference-role: decode`, it includes the routing sidecar as part of the StatefulSet's container spec when creating/updating the StatefulSet. The sidecar is a first-class container in the decode StatefulSet, not a post-hoc injection. The MRI controller only needs to set the label on child InferenceSets — it does not touch the sidecar directly. Unlike #1, there is no controller competition because the Workspace controller is the sole owner of the StatefulSet. Unlike #2, the user never sees or configures it. Unlike #3, no extra infrastructure is needed.

**Selected approach (#4)**: The Workspace controller includes the sidecar container in decode StatefulSets based on the `inference-role: decode` label. The MRI controller's responsibility is limited to setting the correct label when creating child InferenceSets. The sidecar configuration is deterministic and pinned in `consts.go` (similar to how EPP image is managed in [PR #1975](https://github.com/kaito-project/kaito/pull/1975)):

```go
const (
    // Routing sidecar for P/D disaggregation on decode workspaces
    RoutingSidecarImage = "mcr.microsoft.com/oss/v2/llm-d/llm-d-routing-sidecar"
    RoutingSidecarTag   = "v0.7.0"
    RoutingSidecarPort  = 8080
)
```

The injected sidecar container:
- **Image**: pinned version in `consts.go`, updated by KAITO releases
- **Port 8080**: receives requests from EPP, proxies to local vLLM on port 5000
- **BACKEND_URL**: always `http://localhost:5000` (local vLLM engine)
- **POD_IP**: from `status.podIP` fieldRef

No InferenceSet API changes required. Users do not need to know about or configure the sidecar.

### InferencePool and EPP Ownership

In the standard (non-disaggregated) flow, each InferenceSet creates its own InferencePool and EPP via `ensureGatewayAPIInferenceExtension()`. With MultiRoleInference, this changes:

| | Standard InferenceSet | MultiRoleInference |
|---|---|---|
| **Mapping** | 1 InferenceSet → 1 InferencePool → 1 EPP | 1 MRI → N child InferenceSets → **1 shared InferencePool** → 1 EPP |
| **InferencePool created by** | InferenceSet controller | MultiRoleInference controller |
| **EPP sees** | All workspaces from 1 InferenceSet | All prefill + decode workspaces (filtered by `by-label-selector` plugin) |

Child InferenceSets must **skip** the GWIE logic to avoid creating redundant InferencePool/EPP resources. Standalone InferenceSets (not created by MultiRoleInference) continue to create their own InferencePool/EPP as before — this is an additive change, not a breaking one:

```go
// In InferenceSet controller's ensureGatewayAPIInferenceExtension()
func (c *InferenceSetReconciler) ensureGatewayAPIInferenceExtension(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) error {
    // Skip GWIE for child InferenceSets managed by MultiRoleInference.
    // Use OwnerReferences (controller-managed) instead of labels (easily user-modifiable)
    // to prevent accidental GWIE bypass on standalone InferenceSets.
    for _, owner := range iObj.OwnerReferences {
        if owner.Controller != nil && *owner.Controller &&
            owner.Kind == "MultiRoleInference" &&
            owner.APIVersion == "kaito.sh/v1alpha1" {
            return nil
        }
    }
    // ... existing logic for standalone InferenceSets (unchanged) ...
}
```

### 3. InferencePool

One InferencePool per MultiRoleInference, selecting ALL prefill + decode workspaces. This resource is **rendered by the GWIE Helm chart** (deployed via the OCIRepository + HelmRelease in step 5), not created directly by the controller:

```yaml
apiVersion: inference.networking.k8s.io/v1
kind: InferencePool
metadata:
  # The InferencePool resource name matches the MRI name.
  # The HelmRelease that deploys this pool is named "deepseek-v32-inferencepool".
  name: deepseek-v32
  namespace: default
spec:
  targetPorts:
    - number: 8080
  selector:
    matchLabels:
      apps: deepseek-v32
      apps.kubernetes.io/pod-index: "0"
```

#### Multi-GPU / Ray Cluster Routing

When a model uses tensor parallelism (e.g., 8-way TP), each workspace creates a **Ray cluster** — a StatefulSet where pod index 0 is the head (runs the vLLM API server) and pods 1..N are workers (GPU compute only). For example, `prefill.replicas: 2` with 8-way TP produces:

- Workspace 0: pod-0 (head, vLLM API) + pod-1..7 (workers) = 8 pods
- Workspace 1: pod-0 (head, vLLM API) + pod-1..7 (workers) = 8 pods
- **16 total pods, but only 2 accept requests**

The EPP must only route to head pods. Kubernetes StatefulSet pods have a built-in label `apps.kubernetes.io/pod-index`, so the InferencePool selector includes `apps.kubernetes.io/pod-index: "0"` to match only head pods. This works for both single-GPU (1 pod per workspace, always index 0) and multi-GPU Ray cluster topologies.

In P/D mode, **all requests go to decode pods first** (through the routing sidecar on port 8080). The sidecar handles prefill orchestration internally — prefill pods are not accessed via the InferencePool. The `targetPorts: [{number: 8080}]` ensures the Gateway routes to the decode sidecar, which then:
- Contacts the selected prefill pod directly (via `x-prefiller-host-port` header set by `disagg-headers-handler`, e.g., `prefill-pod-ip:8000`) on the vLLM serving port (8000, the default vLLM port — this is the prefill pod's container port, not the InferencePool targetPort which is 8080 for the decode sidecar)
- Falls back to local prefill+decode if not disaggregated

### 4. EPP Plugin ConfigMap (auto-generated if not provided)

When `eppPluginsConfigRef` is not set, the controller generates a default P/D disaggregation config:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: deepseek-v32-epp-plugins
  namespace: default
  ownerReferences:
    - kind: MultiRoleInference
      name: deepseek-v32
data:
  config.yaml: |
    apiVersion: inference.networking.k8s.io/v1
    kind: EndpointPickerConfig
    featureGates:
      - prepareDataPlugins
    plugins:
      # Required for P/D: sets x-prefiller-host-port header so decode sidecar
      # knows which prefill pod to contact.
      - type: disagg-headers-handler
      # PD decider: decides whether to disaggregate based on prefix cache hit ratio.
      # NOTE: disagg-profile-handler + P/D requires a PrefixCachePlugin in both profiles.
      - type: prefix-based-pd-decider
        parameters:
          nonCachedTokens: 4
      - type: disagg-profile-handler
        parameters:
          deciders:
            prefill: prefix-based-pd-decider
      # Precise prefix cache scorer: tracks real-time KV cache state across vLLM pods.
      # Required by disagg-profile-handler for accurate P/D decisions.
      - type: precise-prefix-cache-scorer
        parameters:
          tokenProcessorConfig:
            blockSize: 64            # must match vLLM block size
          indexerConfig:
            kvBlockIndexConfig:
              enableMetrics: true
            tokenizersPoolConfig:
              modelName: deepseek-ai/DeepSeek-V3.2
      # Pod role filters: use by-label-selector with KAITO's inference-role label
      # (not llm-d's built-in prefill-filter/decode-filter which look for llm-d.ai/role).
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
      - type: load-aware-scorer
        parameters:
          threshold: 10
      - type: max-score-picker
    schedulingProfiles:
      - name: prefill
        plugins:
          - pluginRef: prefill-filter
          - pluginRef: precise-prefix-cache-scorer
            weight: 50
          - pluginRef: load-aware-scorer
            weight: 10
          - pluginRef: max-score-picker
      - name: decode
        plugins:
          - pluginRef: decode-filter
          - pluginRef: precise-prefix-cache-scorer
            weight: 50
          - pluginRef: load-aware-scorer
            weight: 10
          - pluginRef: max-score-picker
```

### 5. OCI Repository + HelmRelease (InferencePool chart with llm-d EPP) — ✅ Done ([PR #1975](https://github.com/kaito-project/kaito/pull/1975))

Already implemented in the existing InferenceSet controller. PR #1975 migrated the default EPP from GWIE to llm-d inference scheduler. The controller reuses the existing GWIE InferencePool chart, overriding the EPP image to llm-d:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: deepseek-v32-inferencepool
  namespace: default
  ownerReferences:
    - kind: MultiRoleInference
      name: deepseek-v32
spec:
  url: oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool
  ref:
    tag: v1.3.1
  interval: 10m
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: deepseek-v32-inferencepool
  namespace: default
  ownerReferences:
    - kind: MultiRoleInference
      name: deepseek-v32
spec:
  chartRef:
    kind: OCIRepository
    name: deepseek-v32-inferencepool
  interval: 10m
  values:
    inferenceExtension:
      runtime: llm-d
      image:
        hub: mcr.microsoft.com/oss/v2/llm-d
        name: llm-d-inference-scheduler
        tag: v0.7.1
        pullPolicy: IfNotPresent
      # Inject custom P/D plugin config from the auto-generated ConfigMap
      # The controller copies the content from deepseek-v32-epp-plugins ConfigMap's
      # config.yaml into this HelmRelease value.
      pluginsConfigFile: "config.yaml"
      pluginsCustomConfig:
        config.yaml: |
          # content from deepseek-v32-epp-plugins ConfigMap key "config.yaml"
          ...
    inferencePool:
      targetPorts:
        - number: 8080
      selector:
        matchLabels:
          apps: deepseek-v32
```

### 6. DestinationRule (TLS bypass for EPP)

> **Note**: The DestinationRule is a temporary workaround. It will be removed once [kaito-project/kaito#1983](https://github.com/kaito-project/kaito/pull/1983) lands, which disables EPP secure serving (`--secure-serving=false`) so that the Gateway → EPP connection no longer requires TLS bypass.

```yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: deepseek-v32-inferencepool-epp
  namespace: default
  ownerReferences:
    - kind: MultiRoleInference
      name: deepseek-v32
spec:
  host: deepseek-v32-inferencepool-epp
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
```

## EPP Plugin Chain: How P/D Routing Works

### Plugin Execution Flow

```
Incoming Request
      │
      ▼
┌─────────────────────────┐
│ disagg-headers-handler  │  Step 0: Set P/D coordination headers
│                         │  (x-prefiller-host-port for decode sidecar)
└──────────┬──────────────┘
           │
           ▼
┌─────────────────────────┐
│ disagg-profile-handler  │  Step 1: Decide prefill or decode
│                         │
│  Uses prefix-based-     │  - New prompt with uncached tokens → "prefill" profile
│  pd-decider to check    │  - KV cache already available      → "decode" profile
│  if KV cache exists     │
└──────────┬──────────────┘
           │
           │  profile = "prefill" or "decode"
           ▼
┌─────────────────────────┐
│ by-label-selector       │  Step 2: Filter pods by role
│                         │
│  prefill profile →      │  - Only pods with inference-role=prefill
│    prefill-filter       │
│  decode profile →       │  - Only pods with inference-role=decode
│    decode-filter        │
└──────────┬──────────────┘
           │
           │  filtered pod list
           ▼
┌─────────────────────────┐
│ scorer plugins          │  Step 3: Rank candidate pods
│                         │
│  Both profiles:         │  - precise-prefix-cache-scorer (weight: 50)
│    prefix-cache +       │    tracks real-time KV cache state
│    load-aware +         │  - load-aware-scorer (weight: 10)
│    max-score-picker     │  - max-score-picker (pick best)
└──────────┬──────────────┘
           │
           │  selected decode pod
           ▼
     Envoy forwards request to selected decode pod (sidecar port 8080)
```

> **Why `by-label-selector` instead of llm-d's built-in `prefill-filter`/`decode-filter`?**
> llm-d's built-in filters look for the `llm-d.ai/role` label. KAITO uses `inference-role` as the pod label convention. Using `by-label-selector` with `matchLabels: {inference-role: prefill/decode}` achieves the same filtering without requiring pods to carry llm-d-specific labels.

### KV Cache Transfer Between Prefill and Decode

The current P/D disaggregation design uses [NixlConnector](https://github.com/ai-dynamo/nixl) as the default KV cache transfer mechanism. NixlConnector enables high-performance KV cache transfer between prefill and decode workspaces via RDMA (when available) or TCP fallback. The `kv-transfer-config` is **controller-managed**: users do not need to include it in the `runtimeConfigRef` ConfigMap. During reconciliation, the controller merges the role-specific vLLM configuration from `runtimeConfigRef` and ensures the effective config for both prefill and decode workspaces includes the required KV transfer settings (`kv_connector=NixlConnector`, `kv_role=kv_both`), overriding any conflicting user-provided values. The earlier ConfigMap examples show the final merged config for clarity, not what users need to provide.

```
Prefill Pod                              Decode Pod
┌──────────────────────┐                ┌──────────────────────┐
│ vLLM                 │                │ vLLM                 │
│                      │                │                      │
│ Role: kv_both        │  NixlConnector │ Role: kv_both        │
│                      │ ──────────────>│                      │
│ 1. Process prompt    │  KV cache      │ 4. Receive KV cache  │
│ 2. Build KV cache    │  transfer      │ 5. Generate tokens   │
│ 3. Send KV to decode │  (RDMA/TCP)    │ 6. Return response   │
└──────────────────────┘                └──────────────────────┘
```

## KEDA Autoscaling Integration

> **Note**: This section covers the autoscaling design for the [keda-kaito-scaler](https://github.com/kaito-project/keda-kaito-scaler) project. Implementation details will be discussed in that project's design process.

Each child InferenceSet is a standard InferenceSet with `/scale` subresource. Basic ScaledObject-based scaling works without changes, but **MRI-level annotation-based scaling requires a keda-kaito-scaler update** to support per-role annotations (e.g., `prefill-min-replicas`, `decode-min-replicas`).

### Recommended Approach: Annotation-Based Auto-Provision (Per InferenceSet)

The MultiRoleInference controller propagates KEDA annotations to child InferenceSets. This is the recommended approach because:
- Users configure scaling in a single place (the MRI resource) without needing to know child InferenceSet names
- Leverages the existing keda-kaito-scaler architecture — requires scaler update to support per-role annotations (e.g., `prefill-min-replicas`, `decode-min-replicas`)
- Prefill and decode get independent scaling metrics and thresholds

> **Note**: Users can also create ScaledObject resources targeting child InferenceSets directly for full KEDA flexibility, but the annotation-based approach is the primary supported path.

```yaml
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: deepseek-v32
  annotations:
    # Prefill scaling config
    scaledobject.kaito.sh/prefill-auto-provision: "true"
    scaledobject.kaito.sh/prefill-metricName: "vllm:num_requests_waiting"
    scaledobject.kaito.sh/prefill-threshold: "10"
    scaledobject.kaito.sh/prefill-min-replicas: "1"   # requires keda-kaito-scaler update for P/D support
    scaledobject.kaito.sh/prefill-max-replicas: "4"
    # Decode scaling config
    scaledobject.kaito.sh/decode-auto-provision: "true"
    scaledobject.kaito.sh/decode-metricName: "vllm:gpu_cache_usage_perc"
    scaledobject.kaito.sh/decode-threshold: "80"
    scaledobject.kaito.sh/decode-min-replicas: "2"    # requires keda-kaito-scaler update for P/D support
    scaledobject.kaito.sh/decode-max-replicas: "6"
spec:
  roles:
    - type: prefill
      replicas: 2
      instanceType: Standard_NC24ads_A100_v4
    - type: decode
      replicas: 3
      instanceType: Standard_NC24ads_A100_v4
```

Controller translates to per-InferenceSet annotations:

```yaml
# Generated: deepseek-v32-prefill
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: deepseek-v32-prefill
  annotations:
    scaledobject.kaito.sh/auto-provision: "true"
    scaledobject.kaito.sh/metricName: "vllm:num_requests_waiting"
    scaledobject.kaito.sh/threshold: "10"
    scaledobject.kaito.sh/max-replicas: "4"
# ...

# Generated: deepseek-v32-decode
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: deepseek-v32-decode
  annotations:
    scaledobject.kaito.sh/auto-provision: "true"
    scaledobject.kaito.sh/metricName: "vllm:gpu_cache_usage_perc"
    scaledobject.kaito.sh/threshold: "80"
    scaledobject.kaito.sh/max-replicas: "6"
# ...
```

keda-kaito-scaler sees standard InferenceSet annotations → creates ScaledObject → KEDA scales `spec.replicas` (workspace count) via `/scale` subresource.

### Scaling Diagram

```
                    ┌──────────────────────────────────┐
                    │     MultiRoleInference            │
                    │                                    │
                    │  prefill.replicas: 2               │
                    │  decode.replicas: 3                │
                    └──────────┬───────────────────────┘
                               │ controller creates
              ┌────────────────┴────────────────┐
              ▼                                 ▼
     ┌──────────────────┐            ┌──────────────────┐
     │ InferenceSet     │            │ InferenceSet     │
     │ deepseek-v32-    │            │ deepseek-v32-    │
     │ prefill          │            │ decode           │
     │                  │            │                  │
     │ spec.replicas: 2 │◄── KEDA   │ spec.replicas: 3 │◄── KEDA
     │ (/scale)         │   scales  │ (/scale)         │   scales
     │                  │            │                  │
     │ workspace-0      │            │ workspace-0      │
     │ workspace-1      │            │ workspace-1      │
     │                  │            │ workspace-2      │
     └──────────────────┘            └──────────────────┘
```

## User Experience

### Deploy

```bash
# 1. Prerequisites: Gateway API implementation (Istio Gateway or Envoy Gateway), BBR (same as standard KAITO GWIE setup)

# 2. Create MultiRoleInference
kubectl apply -f - <<EOF
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  # The InferencePool resource name matches the MRI name.
  # The HelmRelease that deploys this pool is named "deepseek-v32-inferencepool".
  name: deepseek-v32
  namespace: default
spec:
  labelSelector:
    matchLabels:
      apps: deepseek-v32
  model:
    name: deepseek-ai/DeepSeek-V3.2
    modelAccessSecret: hf-token
  roles:
    - type: prefill
      replicas: 2
      instanceType: Standard_NC24ads_A100_v4
    - type: decode
      replicas: 3
      instanceType: Standard_NC24ads_A100_v4
EOF

# 3. Create HTTPRoute for the model
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: deepseek-v32
spec:
  parentRefs:
    - name: inference-gateway
  rules:
    - matches:
        - headers:
            - name: X-Gateway-Model-Name
              value: deepseek-v32
      backendRefs:
        - group: inference.networking.k8s.io
          kind: InferencePool
          name: deepseek-v32
          port: 8080
EOF
```

### Monitor

```bash
# Check MultiRoleInference status
kubectl get mri deepseek-v32
# NAME            READY   AGE
# deepseek-v32    True    10m

# Check child InferenceSets
kubectl get is -l kaito.sh/parent=deepseek-v32
# NAME                      REPLICAS   READYREPLICAS   AGE
# deepseek-v32-prefill      2          2               10m
# deepseek-v32-decode       3          3               10m

# Check InferencePool and EPP
kubectl get inferencepool deepseek-v32
kubectl get pod -l inferencepool=deepseek-v32-epp

# Verify workspace labels
kubectl get pods -l apps=deepseek-v32 --show-labels
# NAME                                    LABELS
# deepseek-v32-prefill-ws-0               apps=deepseek-v32,inference-role=prefill
# deepseek-v32-prefill-ws-1               apps=deepseek-v32,inference-role=prefill
# deepseek-v32-decode-ws-0                apps=deepseek-v32,inference-role=decode
# deepseek-v32-decode-ws-1                apps=deepseek-v32,inference-role=decode
# deepseek-v32-decode-ws-2                apps=deepseek-v32,inference-role=decode
```

### Test

```bash
# Send request through Gateway
curl -s http://<gateway-ip>/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-v32",
    "messages": [{"role": "user", "content": "Explain KV cache in transformers"}]
  }'
```

## Implementation Checklist

| Phase | Step | Description | Status / Dependencies |
|-------|------|-------------|----------------------|
| **Phase 1: Core** | 1 | MultiRoleInference CRD types (prefill + decode roles) | TODO |
| | 2 | Controller: create prefill/decode child InferenceSets with `inference-role` label and `kaito.sh/parent` label | TODO |
| | 3 | Controller: inject default vLLM NixlConnector kv-transfer-config (`kv_both`) into child InferenceSet config | TODO |
| | 4 | Controller: create InferencePool (selector: `apps.kubernetes.io/pod-index: "0"` for Ray cluster support) | TODO |
| | 5 | Controller: auto-generate P/D EPP plugin ConfigMap (`disagg-profile-handler` + `by-label-selector`) | TODO |
| | 6 | Controller: create OCI Repository + HelmRelease (llm-d EPP image) | ✅ Done ([PR #1975](https://github.com/kaito-project/kaito/pull/1975)) — currently in InferenceSet controller; will be moved to MRI controller |
| | 7 | Controller: create DestinationRule (TLS bypass) — **temporary, remove after [kaito#1983](https://github.com/kaito-project/kaito/pull/1983)** | TODO (skip if #1983 merges first) |
| | 8 | Workspace controller: include llm-d routing sidecar in decode StatefulSet spec when `inference-role: decode` label is present | TODO |
| | 9 | Controller: status aggregation from child InferenceSets + InferencePool → MRI status | TODO |
| | 10 | Webhook: validation + defaulting | TODO |
| **Phase 2: Advanced** | 11 | Support custom `eppPluginsConfigRef` for user-defined EPP plugins | TODO |
| | 12 | Support MRI `roles[].replicas` sync: controller watches MRI spec changes and updates child InferenceSet `spec.replicas` | TODO |
| | 13 | E2E tests | TODO |
| **Phase 3: Autoscaling** ([keda-kaito-scaler](https://github.com/kaito-project/keda-kaito-scaler)) | 14 | Controller: propagate KEDA annotations from MRI to child InferenceSets | TODO |
| | 15 | keda-kaito-scaler: understand role-specific metrics (prefill vs decode) | TODO |

## References

- [llm-d Inference Scheduler Architecture](https://github.com/llm-d/llm-d-inference-scheduler/blob/main/docs/architecture.md)
- [MultiRoleInference proposal (PR #1846)](https://github.com/kaito-project/kaito/pull/1846)
- llm-d EPP migration proposal (planned — not yet submitted)
- [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler)
- [llm-d disagg-profile-handler](https://github.com/llm-d/llm-d-inference-scheduler/tree/main/pkg/plugins)
- [keda-kaito-scaler](https://github.com/kaito-project/keda-kaito-scaler)
- [vLLM disaggregated prefill](https://docs.vllm.ai/en/latest/features/disagg_prefill.html)
- [Gateway API Inference Extension](https://github.com/kubernetes-sigs/gateway-api-inference-extension)
- [GWIE to llm-d migration plan](https://github.com/kubernetes-sigs/gateway-api-inference-extension/issues/2430)
