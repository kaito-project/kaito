---
title: MultiRoleInference for Prefill/Decode Disaggregation in Kaito
authors:
  - "@andyzhangx"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2026-03-13
last-updated: 2026-03-13
status: provisional
---

# Title

MultiRoleInference for prefill/decode disaggregation in Kaito

## Summary

As large language model serving evolves, prefill/decode (PD) disaggregation has become a practical deployment pattern for improving latency and GPU utilization. Prefill typically drives high GPU compute utilization and tends to demand a larger share of active memory bandwidth during prompt processing, while decode is more sensitive to sustained GPU memory residency and KV cache footprint over longer-running token generation. Running both stages in the same backend can lead to GPU and memory resource contention and reduced efficiency.

In this proposal, we introduce a new `MultiRoleInference` Custom Resource Definition (CRD) and controller in Kaito. `MultiRoleInference` is designed to describe and manage a logical inference service composed of multiple coordinated roles, including:

- `router`
- `prefill`
- `decode`

The new API allows users to define shared inference settings once and then configure each role independently with its own replica count, instance type, and optional role-specific config. The controller is responsible for generating and reconciling the underlying resources required for the full topology, including backend workloads, internal Services, and the external router Service.

This proposal aims to provide a first-class API for PD-disaggregated inference in Kaito without overloading the existing `InferenceSet` abstraction.

## Motivation

LLM inference service is a core and widely-used capability in Kaito. As model sizes and request concurrency continue to increase, there is growing demand for more advanced serving topologies beyond a single monolithic inference workload.

From the technical perspective, PD disaggregation is a natural fit for modern vLLM serving:

- **Prefill** handles prompt ingestion and initial KV construction. It is often compute-heavy and short-lived.
- **Decode** consumes KV cache and generates tokens incrementally. It is often latency-sensitive and benefits from independent scaling.
- **Router** provides a unified endpoint and coordinates traffic between prefill and decode backends.

Although it is possible to approximate this topology by manually managing multiple `InferenceSet` resources, that model has several limitations:

- users must manually create and coordinate multiple resources
- pairing between prefill and decode backends is implicit and error-prone
- router deployment and routing configuration are not first-class concepts
- replica management for each role is scattered across resources
- operational visibility is fragmented

To solve these issues, Kaito should provide a dedicated multi-role inference API that models the PD-disaggregated topology directly.

### Goals
- Introduce a new `MultiRoleInference` CRD and controller for PD-disaggregated inference including following built-in roles:
  - `router`
  - `prefill`
  - `decode`
- Allow each role to have its own replica count
- Allow `prefill` and `decode` roles to use different GPU instance types and role-specific ConfigMaps

### Non-Goals/Future Work

- Introduce autoscaling in the first version

## Proposal

This proposal introduces a new `MultiRoleInference` CRD and controller in Kaito.

A `MultiRoleInference` resource represents one logical inference service composed of multiple coordinated roles. The controller reconciles the resource into the required workloads and networking resources for PD-disaggregated serving.

### new MultiRoleInference CRD API Change

- `MultiRoleInference` Custom Resource (CR) example:

```yaml
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: deepseek-v32
spec:
  labelSelector:
    matchLabels:
      apps: deepseek-v32
  inference:
    preset:
      name: deepseek-ai/DeepSeek-V3.2
      presetOptions:
        modelAccessSecret: hf-token # optinal, reference to secret name
  roles:
    - name: router
      replicas: 1
    - name: prefill
      replicas: 2
      instanceType: Standard_NC24ads_A100_v4
      config: pd-params # optinal, reference to ConfigMap name
    - name: decode
      replicas: 2
      instanceType: Standard_NC24ads_A100_v4
      config: pd-params # optinal, reference to ConfigMap name
```

This API separates the topology into three layers:

- **shared logical identity** via `metadata.name`
- **shared inference model settings** via `spec.inference`
- **role-specific deployment settings** via `spec.roles`

To support PD-disaggregation while continuing to reuse the existing `InferenceSet`-based backend implementation.

These fields are mainly intended for controller-generated backend workspaces created by `MultiRoleInference`. In this design, `MultiRoleInference` is the user-facing API for PD-disaggregated inference, while the generated `InferenceSet` resources serve as internal child resources for `prefill` and `decode` backends.

We choose `InferenceSet` as the child resource because it already integrates well with KEDA autoscaling, which makes it straightforward to extend `MultiRoleInference` in a later phase to support autoscaling for both prefill and decode workloads.

### Why a New CRD

The existing `InferenceSet` resource is suitable for a single inference workload, but PD disaggregation is inherently a multi-role topology. Extending `InferenceSet` to model `router`, `prefill`, and `decode` together would blur its semantics and make the API harder to reason about.

A dedicated `MultiRoleInference` CRD provides:

- explicit topology modeling
- clear ownership for generated child resources
- centralized reconciliation and status reporting
- a simpler user experience for PD-disaggregated serving

### Architecture Overview

The intended runtime topology is:

- one `router` workload and Service for external traffic and request forwarding between prefill and decode workloads
- one `prefill` workload and internal Service
- one `decode` workload and internal Service

The request flow is:

1. client sends request to the router Service
2. router forwards prefill requests to the prefill backend Service
3. router forwards decode requests to the decode backend Service
4. the final response is returned through the router endpoint

Clients only interact with the router Service.

### Controller Behavior

The `MultiRoleInference` controller reconciles one `MultiRoleInference` resource into the following child resources:

- role-specific inference workloads for `prefill`
- role-specific inference workloads for `decode`
- a `vllm-router` Deployment for `router`
- one Service for each role
- status conditions summarizing role readiness

For the initial implementation, the controller may reuse existing `InferenceSet`-based inference logic for backend roles where practical. In that model:

- `prefill` and `decode` can be implemented internally as generated `InferenceSet` resources, with each role's `replicas` value mapped to the replica count of its corresponding backend `StatefulSet`
- `router` can be represented as a generated Deployment and Service
- generated resources are owned by the parent `MultiRoleInference`

This minimizes duplicated code and keeps the new controller focused on orchestration.

### Role Semantics

#### router

The `router` role serves as the public entry point of the PD-disaggregated inference topology using Kubernetes deployment. KAITO will use [vLLM Router](https://github.com/vllm-project/router) as the routing layer. vLLM Router is a lightweight and high-performance request proxy designed for large-scale vLLM deployments, with built-in support for advanced load balancing and prefill/decode disaggregation.

Responsibilities:

- accept client traffic
- route traffic to prefill and decode backends
- expose a stable Service endpoint
- operate independently from GPU-backed backend roles

Characteristics:

- normally CPU-based
- typically one replica, but can scale to multiple replicas
- does not require `instanceType`

#### prefill

The `prefill` role serves the prefill stage using InferenceSet in vLLM PD disaggregation.

Responsibilities:

- process prompts
- construct KV cache
- act as the KV producer in the vLLM connector model

Characteristics:

- GPU-backed
- supports independent replica count
- requires `instanceType`
- may use a role-specific ConfigMap

#### decode

The `decode` role serves the decode stage using InferenceSet in vLLM PD disaggregation.

Responsibilities:

- consume KV cache
- generate output tokens
- act as the KV consumer in the vLLM connector model

Characteristics:

- GPU-backed
- supports independent replica count
- requires `instanceType`
- may use a role-specific ConfigMap

### Router and Backend Wiring

The controller automatically wires the router to stable Kubernetes Services instead of pod IPs.

For a `MultiRoleInference` named `deepseek-v32` in namespace `<ns>`, the generated internal endpoints may be:

- prefill backend: `http://deepseek-v32-prefill-0.deepseek-v32-prefill-0-headless.<ns>.svc.cluster.local:5000`
- decode backend: `http://deepseek-v32-decode-0.deepseek-v32-decode-0-headless.<ns>.svc.cluster.local:5000`

The external router endpoint may be:

- router service: `http://deepseek-v32-router.<ns>.svc.cluster.local:10001`

Using Services instead of pod IPs provides:

- support for multiple backend pods
- stable discovery
- decoupling between router and pod lifecycle

### vLLM Runtime Configuration

The backend roles are translated into vLLM PD configuration by the controller and runtime wrapper.

Expected mapping:

- `prefill` . `NixlConnector` + `kv_producer`
- `decode` . `NixlConnector` + `kv_consumer`

The controller also injects the required runtime environment, such as backend pod IP exposure, so the vLLM backend can publish the correct endpoint information.

Low-level vLLM connector parameters are intentionally hidden in the first API version. The controller provides sane defaults and owns the wiring details.

### Multiple Backend Replicas

The API explicitly supports multiple backend replicas.

For example:

- `prefill.replicas: 2`
- `decode.replicas: 2`

In this case, MultiRoleInference controller would create two InferenceSets for prefill and two InferenceSets for decode, there would be two endpoints for prefill and two endpoints for decode in vllm-router configuration:

```
      - command:
        - vllm-router
        - --pd-disaggregation
        - --prefill
        - http://deepseek-v32-prefill-0.deepseek-v32-prefill-0-headless.<ns>.svc.cluster.local:5000
        - http://deepseek-v32-prefill-1.deepseek-v32-prefill-1-headless.<ns>.svc.cluster.local:5000
        - --decode
        - http://deepseek-v32-decode-0.deepseek-v32-decode-0-headless.<ns>.svc.cluster.local:5000
        - http://deepseek-v32-decode-1.deepseek-v32-decode-1-headless.<ns>.svc.cluster.local:5000
```

## related fields in `MultiRoleInference` Custom Resource(CR)

### `spec.labelSelector`

`spec.labelSelector` defines the labels that should be applied to generated workloads and used in their `resource.labelSelector` or equivalent scheduling logic.

Purpose:

- keep generated role workloads logically grouped
- support compatibility with existing Kaito resource placement behavior
- allow operators to select the same infrastructure scope for all generated child workloads

### `spec.inference`

`spec.inference` defines the shared inference configuration across roles.

For the initial version, it contains the model preset and optional preset options.

Example:

```yaml
inference:
  preset:
    name: deepseek-ai/DeepSeek-V3.2
    presetOptions:
      modelAccessSecret: hf-token
```

Purpose:

- define the model preset once
- avoid repeated model configuration in each role
- ensure prefill and decode use the same base inference settings

### `spec.roles`

`spec.roles` defines the role topology of the logical inference service.

Each element describes one role group.

#### `roles[].name`

Supported values in the initial version:

- `router`
- `prefill`
- `decode`

Role names must be unique within a resource.

#### `roles[].replicas`

`replicas` defines the desired replicas of deployment for the role.

Examples:

- one router replica of Kubernetes Deployment
- two replicas of prefill InferenceSet
- two replicas of decode InferenceSet

This field allows independent scaling per role.

#### `roles[].instanceType`

`instanceType` specifies the compute SKU for the role.

Expected usage:

- required for GPU-backed roles such as `prefill` and `decode`
- not required for `router`

#### `roles[].config`

`config` references a ConfigMap that contains role-specific inference arguments.

Expected usage:

- `prefill` may use a ConfigMap for prefill-oriented parameters
- `decode` may use a ConfigMap for decode-oriented parameters

This allows each backend role to specialize its runtime behavior while still sharing the same base model preset.

## MultiRoleInference API

```go
type MultiRoleInferenceRoleName string

const (
  MultiRoleInferenceRoleRouter  MultiRoleInferenceRoleName = "router"
  MultiRoleInferenceRolePrefill MultiRoleInferenceRoleName = "prefill"
  MultiRoleInferenceRoleDecode  MultiRoleInferenceRoleName = "decode"
)

type MultiRoleInferencePresetSpec struct {
  // Name is the inference preset name or model identifier.
  // +optional
  Name string `json:"name,omitempty"`

  // PresetOptions contains preset-specific options.
  // +optional
  PresetOptions map[string]string `json:"presetOptions,omitempty"`
}

type MultiRoleInferenceSharedInferenceSpec struct {
  // Preset defines the shared inference preset for all backend roles.
  // +optional
  Preset *MultiRoleInferencePresetSpec `json:"preset,omitempty"`
}

type MultiRoleInferenceRoleSpec struct {
  // Name is the role name. Supported values: router, prefill, decode.
  // +kubebuilder:validation:Enum=router;prefill;decode
  Name MultiRoleInferenceRoleName `json:"name"`

  // Replicas is the desired replica count for the role.
  // For router, this maps to the Deployment replica count.
  // For prefill/decode, this maps to the number of generated InferenceSet resources.
  // +kubebuilder:validation:Minimum=1
  // +optional
  Replicas *int32 `json:"replicas,omitempty"`

  // InstanceType specifies the compute SKU for GPU-backed roles.
  // +optional
  InstanceType string `json:"instanceType,omitempty"`

  // Config references a ConfigMap that contains role-specific inference arguments.
  // +optional
  Config string `json:"config,omitempty"`
}

type MultiRoleInferenceSpec struct {
  // LabelSelector is propagated to generated child workloads.
  // +optional
  LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

  // Inference defines the shared inference configuration across roles.
  // +optional
  Inference *MultiRoleInferenceSharedInferenceSpec `json:"inference,omitempty"`

  // Roles defines the full role topology of this inference service.
  // +optional
  Roles []MultiRoleInferenceRoleSpec `json:"roles,omitempty"`
}

type MultiRoleInferenceStatus struct {
  // Conditions represents the latest available observations.
  // +optional
  Conditions []metav1.Condition `json:"conditions,omitempty"`

  // RouterServiceName is the generated Service name for client access.
  // +optional
  RouterServiceName string `json:"routerServiceName,omitempty"`

  // ObservedGeneration records the latest reconciled generation.
  // +optional
  ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="RouterService",type="string",JSONPath=".status.routerServiceName"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MultiRoleInference struct {
  metav1.TypeMeta   `json:",inline"`
  metav1.ObjectMeta `json:"metadata,omitempty"`

  Spec   MultiRoleInferenceSpec   `json:"spec,omitempty"`
  Status MultiRoleInferenceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type MultiRoleInferenceList struct {
  metav1.TypeMeta `json:",inline"`
  metav1.ListMeta `json:"metadata,omitempty"`
  Items           []MultiRoleInference `json:"items"`
}
```

## Implementation History

- [x] 03/13/2026: Open proposal PR
- [ ] Onboard vllm-router to MCR image
- [ ] Add `MultiRoleInference` API types and CRD
- [ ] Add controller and generated child resources
- [ ] Add validation and defaulting
- [ ] Add tests and examples
