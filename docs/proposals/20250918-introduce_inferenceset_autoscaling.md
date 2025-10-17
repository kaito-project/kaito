---
title: Introduce a new InferenceSet CRD and Controller for scaling inference workloads automatically
authors:
  - "@andyzhangx"
reviewers:
  - "@Fei-Guo"
  - "@rambohe-ch"
  - "@zhuangqh"
creation-date: 2025-09-18
last-updated: 2025-09-18
status: provisional
see-also:
---

# Title

Introduce a new `InferenceSet` CRD and Controller for scaling inference workloads automatically

## Summary

As the volume of pending inference requests grows, scaling additional inference instances becomes essential to avoid blocking inference requests. Conversely, if the number of pending inference requests decreases, it is advisable to contemplate reducing inference instances to enhance GPU resource utilization.

We hope to provide an auto-scaler feature for scaling inference workloads automatically in terms of changes of custom metrics from inference pods, and this auto scaler doesn't depend on other components(this means Kaito is a self-contained component without dependencies).

Due to some technical issues (as explained in next `Motivation` section), we want to introduce a new `InferenceSet` CRD and Controller for running inference workloads and offering autoscaling capability. `InferenceSet` would be the recommended API for kaito users to scale inference workloads automatically. Kaito users can continue to utilize the current `Workspace` Custom Resource to execute inference workloads without autoscaling functionality. There is no breaking change in this proposal.

## Motivation

LLM inference service is a basic and widly-used feature in Kaito, and Kaito community interest in auto scaler for inference workloads continues to intensify, related issues: [#306](https://github.com/kaito-project/kaito/issues/306), [#1104](https://github.com/kaito-project/kaito/issues/1104).

From the technical perspective, It's a good idea to provide auto-scaler capability, because the auto-scaler of inference workloads dynamically adjusts the number of inference instances based on request volume--scaling up during traffic spikes to improve inference speed, and scaling down during low demand to minimize GPU resource waste.

To overcome these issues, we want to introduce a new `InferenceSet` CRD and Controller for scaling inference workloads automatically. If you want to run inference workloads with autoscaling capability, you could create a `InferenceSet` CR, and kaito InferenceSet controller would create a series of kaito workspaces per replica number setting in `InferenceSet` CR, and autoscale per the inference workloads requests.

This new `InferenceSet` CRD and controller are specifically designed for executing inference workloads with autoscaling capability. It is important to note that this proposal has no impact on fine-tuning and RAG features, and there is no breaking change on existing inference workload usage.

### Goals

- Introduce a new `InferenceSet` CRD and controller for scaling inference workloads automatically

### Non-Goals/Future Work

- Support inference workload autoscaling for Bring Your Own node scenario
- Support a customized auto-sacler for kaito, this part will be addressed in other proposal

## Proposal

### new InferenceSet CRD API Change

 - `InferenceSet` Custom Resource(CR) example:
```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: llama-3-1-8b
spec:
  replicas: 2 # number of workspace CR created by InferenceSet controller
  nodeCountLimit: 10 # optional, total GPU node count limit for InferenceSet
  labelSelector:
    matchLabels:
      # workspace created by InferenceSet controller would use this label in resource.labelSelector
      apps: large-model
  template:
    resource:
      instanceType: "Standard_NC24ads_A100_v4"
    inference: # fields in inference are the same as in workspace.inference
      preset:
        name: llama-3.1-8b-instruct
        presetOptions:
          modelAccessSecret: hf-token
      adapters:
        ...
      template: # pod template
        ...
  updateStrategy:
    type: RollingUpdate
```

### related fields in `InferenceSet` Custom Resource(CR)
  - `spec.Replicas`

    number of `workspace` CR created by InferenceSet controller
  - `spec.nodeCountLimit`

    (optional) total GPU node count limit for InferenceSet, this is used in autoscaling scenario, every workspace CR would consumes a few CPU nodes, the total CPU node should not exceed this nodeCountLimit otherwise there would be error during autoscaling
  - `spec.selector.matchLabels`

    workspace created by InferenceSet controller would use this label in resource.labelSelector
  - `spec.updateStrategy.type`

    allows you to configure and disable automated rolling updates for existing workspace CRs, available values: `RollingUpdate`(default), `OnDelete`, and `rollingUpdate` supports `maxUnavailable`.
     - following fields supports update:
       - `spec.Replicas`
       - `spec.nodeCountLimit`
       - `spec.template.adapters`

### InferenceSet API
```go
type InferenceSetResourceSpec struct {
	// InstanceType specifies the GPU node SKU.
	// +required
	InstanceType string `json:"instanceType,omitempty"`
}

// InferenceSetTemplate defines the template for creating InferenceSet instances.
type InferenceSetTemplate struct {
	Resource  InferenceSetResourceSpec   `json:"resource,omitempty"`
	Inference kaitov1beta1.InferenceSpec `json:"inference,omitempty"`
}

// InferenceSetSpec defines the desired state of InferenceSet
type InferenceSetSpec struct {
	// Template is the template used to create the InferenceSet.
	Template InferenceSetTemplate `json:"template"`
	// Replicas is the desired number of workspaces to be created.
	// +optional
	// +kubebuilder:default:=1
	Replicas int `json:"replicas,omitempty"`
	// NodeCountLimit is the maximum number of GPU nodes that can be created for the InferenceSet.
	// If not specified, there is no limit on the number of GPU nodes that can be created.
	// +optional
	NodeCountLimit int `json:"nodeCountLimit,omitempty"`
	// workspace created by InferenceSet controller would use this label in resource.labelSelector
	// +required
	Selector *metav1.LabelSelector `json:"labelSelector"`
	// UpdateStrategy indicates the strategy to use to replace existing workspaces with new ones.
	// +optional
	// +kubebuilder:default:={"type":"RollingUpdate","rollingUpdate":{"maxUnavailable":1}}
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	UpdateStrategy appsv1.StatefulSetUpdateStrategy `json:"updateStrategy,omitempty"`
}

// InferenceSetStatus defines the observed state of InferenceSet
type InferenceSetStatus struct {
	// Replicas is the total number of workspaces created by the InferenceSet.
	// +required
	Replicas int `json:"replicas"`
	// Selector is used to select the pods that provide metrics for making scaling action decisions.
	// This field must be set when HPA and VPA is used for scaling.
	Selector string `json:"selector,omitempty"`
	// Conditions report the current conditions of the InferenceSet.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// InferenceSet is the Schema for the InferenceSet API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=inferencesets,scope=Namespaced,categories=inferenceset,shortName={is,isets}
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description=""
// +kubebuilder:printcolumn:name="ReadyReplicas",type="integer",JSONPath=".status.readyReplicas",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type InferenceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InferenceSetSpec   `json:"spec,omitempty"`
	Status InferenceSetStatus `json:"status,omitempty"`
}

// InferenceSetList contains a list of InferenceSet
// +kubebuilder:object:root=true
type InferenceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InferenceSet `json:"items"`
}
```

## Implementation Strategy

The implementation will be split into a few key steps:

### Step 1:
Create new `InferenceSet` CRD and implement new `InferenceSet` controller, below are details:

the `InferenceSet` controller would create a few `Workspace` CRs per the `InferenceSet.spec.Replicas`, e.g. if `InferenceSet.spec.Replicas` equals to `3`, it would create 3 `Workspace` CRs naming like `{InferenceSet.metadata.name}-0`, `{InferenceSet.metadata.name}-1`, `{InferenceSet.metadata.name}-2` with label `infernecesetmember.kaito.sh:{InferenceSet.metadata.name}-0`, `infernecesetmember.kaito.sh:{InferenceSet.metadata.name}-1`,`infernecesetmember.kaito.sh:{InferenceSet.metadata.name}-2` respecitively. The other fields of `Workspace` CR are copied from `InferenceSet.template.resource` and `InferenceSet.template.inference`. Later on, `Workspace` controller would create a few statefulset and headless services for each `Workspace` CR.

### Step 2:
Address other functionalities, e.g. Update Strategy
  - `spec.updateStrategy.type`

    allows you to configure and disable automated rolling updates for existing workspace CRs, available values: `RollingUpdate`(default), `OnDelete`, and `rollingUpdate` supports `maxUnavailable`.
     - following fields supports update:
       - `spec.Replicas`
       - `spec.nodeCountLimit`
       - `spec.template.adapters`

## Implementation History
- [x] 09/18/2025: Open proposal PR
- [x] 09/25/2025: address comments and add InferenceSet API

