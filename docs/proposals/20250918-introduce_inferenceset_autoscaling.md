---
title: Introduce a new InferenceSet CRD and Controller for scaling inference workloads automatically
authors:
  - "@andyzhangx"
reviewers:
  - "@Fei-Guo"
  - "@rambohe-ch"
  - "@zhuangqh"
creation-date: 2025-09-17
last-updated: 2025-09-17
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

LLM inference service is a baisc and widly-used feature in Kaito, and Kaito community interest in auto scaler for inference workloads continues to intensify, related issues: [#306](https://github.com/kaito-project/kaito/issues/306), [#1104](https://github.com/kaito-project/kaito/issues/1104).

From the technical perspective, It's a good idea to provide auto-scaler capability, becasue the auto-scaler of inference workloads dynamically adjusts the number of inference instances based on request volume--scaling up during traffic spikes to improve inference speed, and scaling down during low demand to minimize GPU resource waste.

And during the implementation of [KEP: 20250611-workspace-subresource-scale-api.md](./20250917-infefrenceset_autoscaling.md), we found that current Workspace CRD cannot satisfy `scalepath` requirement for the scale subresource, which means that to continue with scale API support in Workspace, we need to introduce a workspace API breaking change, you could find more details [here](https://docs.google.com/presentation/d/1qCpFWuWzvoi5yA2rAa7OhCAUHPRoAmR3-ODJ3SDIgI4/edit?slide=id.p#slide=id.p). 

To overcome these issues, we want to introduce a new `InferenceSet` CRD and Controller for scaling inference workloads automatically. If you want to run inference workloads with autoscaling capablity, you could create a `InferenceSet` CR, and kaito InferenceSet controller would create a series of kaito workspaces per replica number setting in `InferenceSet` CR, and autoscale per the inference workloads requests.

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
  name: llama2-7b
spec:
  replicas: 3 # number of workspace CR created by InferenceSet controller
  quota: 10 # optional, total GPU node count limit for InferenceSet
  selector:
    matchLabels:
      # workspace created by InferenceSet controller would use this label in resource.labelSelector
      apps: large-model
  template:
    resource:
      instanceType: "Standard_NC24ads_A100_v4"
    inference: # fields in inference are the same as in workspace.resource.inference
      preset:
        name: "llama2-7b"
        modelAccessSecret: "hf-token"
      adapters:
        ...
  updateStrategy:
    type: RollingUpdate
```

- related fields:
  - `spec.Replicas`
    number of workspace CR created by InferenceSet controller
  - `spec.quota`
    optional, total GPU node count limit for InferenceSet
  - `spec.selector.matchLabels`
    workspace created by InferenceSet controller would use this label in resource.labelSelector
  - `spec.updateStrategy.type`
    available values: `RollingUpdate`, `OnDelete` (same as updateStrategy of `StatefulSet`)

## Implementation Strategy

The implementation will be split into a few key steps:

### Step 1:
For kaito inference workloading based on small models, `worksapce` controller would create k8s deployment to run inference pods, we need to transform Deployment into StatefulSet within the workspace controller, that would ensure all Kaito inference workloads utilize StatefulSets. This standardization simplifies the process of introducing a new `InferenceSet` CRD and developing a new `InferenceSet` controller.

### Step 2:
Create new `InferenceSet` CRD and implement new `InferenceSet` controller, below are details:

the `InferenceSet` controller would create a few `Workspace` CRs per the `InferenceSet.spec.Replicas`, e.g. if `InferenceSet.spec.Replicas` equals to `3`, it would create 3 `Workspace` CRs naming like `{InferenceSet.metadata.name}-0`, `{InferenceSet.metadata.name}-1`, `{InferenceSet.metadata.name}-2` with label `infernecesetmember.kaito.sh:{InferenceSet.metadata.name}-0`, `infernecesetmember.kaito.sh:{InferenceSet.metadata.name}-1`,`infernecesetmember.kaito.sh:{InferenceSet.metadata.name}-2` respecitively. The other fields of `Workspace` CR are copied from `InferenceSet.template.resource` and `InferenceSet.template.inference`. Later on, `Workspace` controller would create a few statefulset and headless services for each `Workspace` CR.

### Step 3:
Address other functionalities, e.g. Update Strategy

## Implementation History
- [ ] 09/18/2025: Open proposal PR
