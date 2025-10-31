---
title: Cloud Provider Agnostic Scheduling
authors:
  - "@chewong"
  - "@jont828"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2025-09-03
last-updated: 2025-10-01
status: draft
see-also:
---

## Goals

Today, KAITO fast-fails on potential OOM errors by ensuring the instance type's GPU count and memory obtained from cloud SKU maps is larger than the preset mode's total GPU memory requirement. This approach has several limitations:

- It requires maintaining a SKU map and updating it as new instance types are released or deprecated, which is labor-intensive and error-prone.
- The SKU map is cloud-provider specific, currently supporting only Azure and AWS.
- It blocks validation BYO node scenarios where users bring their own nodes with GPUs. In order for BYO node validation to work, the existing nodes still have to be an instance type existing in the SKU map, and the user must manually specify it in the `instanceType` field as well. Howevere, KAITO should work with any node as long as it has the required GPU resources and without requiring the user to explicitly specify an instance type.
- It also requires Karpenter to be installed beforehand.

The goal of this proposal is to allow KAITO to validate the GPU requirements for nodes without using a SKU map or a specified instance type, which enables accurate validation with BYO nodes from any cloud provider. This proposal is tightly coupled with the [BYO scenario redesign](20250820-byo-nodes.md) and unblocks it by removing the dependency on instance types for BYO nodes. Instead, the GPU requirement validation will be done using provider-neutral runtime node attributes discovered via node labels sourced from NVIDIA GPU Feature Discovery (GFD) which provides information such as GPU product, count, and memory. These nodes will still need to be homogeneous in terms of GPU product and configuration to simplify scheduling and the GPU requirement estimations. 

BYO and NAP will be also be mutually exclusive in order to simplify the scheduling logic. BYO will now be supported by disabling NAP, and the instance type is no longer required while NAP scenarios will continue to rely on `workspace.resource.instanceType` for provisioning decisions.

## Non-Goals

We are not changing the validating webhook or NAP semantics; we only change where capacity data for existing nodes comes from (provider-neutral node attributes rather than SKU maps). We are not adding a dynamic VRAM estimator (done by #1178), packaging or installing any specific labeler, extending to non-NVIDIA accelerators, or integrating DRA/ResourceSlice yet. SKU maps remain for NAP provisioning decisions.

## Assumptions

- NVIDIA GPUs are used for inference workloads.
- In BYO cases, users pre-provision some nodes with GPUs and GPU drivers installed (via GPU operator or other means).
- Inference presets define the total GPU memory requirement per inference replica (e.g. 8GB for phi-4-mini-instruct).
- No bin-packing of multiple replicas on the same node; each replica may span multiple nodes for distributed inference if needed.

## NVIDIA GPU Feature Discovery (GFD)

One way to source provider‑neutral GPU attributes is NVIDIA [GPU Feature Discovery](https://github.com/NVIDIA/k8s-device-plugin/tree/main/docs/gpu-feature-discovery) (GFD), which publishes node labels such as `nvidia.com/gpu.count`, `nvidia.com/gpu.memory` (in MiB), and `nvidia.com/gpu.product`. GFD builds on [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) (NFD) and targets NVIDIA GPUs; it aligns with the direction in issue #1222.

## Implementation

### Dependency Installation

We can install the `nvidia-device-plugin` and the `gpu-feature-discovery` Helm charts as a dependencies defined in `Chart.yaml`. Each chart is conditionally installed based on the feature gate `disableNodeAutoProvisioning` and a new feature gate `nvidiaDevicePlugin.enabled` (default true) to allow users to disable the installation. This also replaces the existing [DaemonSet](https://github.com/kaito-project/kaito/blob/main/charts/kaito/workspace/templates/nvidia-device-plugin-ds.yaml) that installs the NVIDIA device plugin manually and allows it to be disabled with a flag.


The `gpu-feature-discovery` Helm chart is a subchart of `nvidia-device-plugin` and will not work without NVIDIA device plugin present. We install it as a separate dependency to allow the user to install the GFD chart but not the NVIDIA device plugin in case they already have the device plugin installed via other means, i.e. a DaemonSet. 

```yaml
...
dependencies:
  - name: nvidia-device-plugin # Replaces existing DaemonSet that installs this manually.
    version: 0.17.2
    repository: https://nvidia.github.io/k8s-device-plugin
    condition: nvidiaDevicePlugin.enabled 
  - name: gpu-feature-discovery
    version: 0.17.2
    repository: https://nvidia.github.io/k8s-device-plugin
    condition: featureGates.disableNodeAutoProvisioning # Only install GFD when NAP is disabled.
...
```

### Proposed Node Selection Algorithm

BYO and NAP will be mutually exclusive to simplify the logic. This means BYO will be toggled with the disable NAP feature gate in the controller and the Helm chart values. This means we can assume that if NAP is enabled, all matching nodes are auto-provisioned and when NAP is disabled, all matching nodes are BYO.

In NAP scenarios, the KAITO controller provisions NodeClaims to create enough nodes of the same instanceType to satisfy the total GPU memory requirement (per-replica memory from the preset times replicas), and removes excess NodeClaims if needed. For instance, a preset requiring 8GB per replica with 2 replicas (total 16GB) on an 80GB instanceType would provision 2 nodes - one per replica, without bin-packing - despite one node sufficing for the memory.

For BYO scenarios (where `workspace.resource.instanceType` is empty), the proposed algorithm must address two key aspects:
- **Homogeneous placement**: All pods from the same Workspace must run on nodes with the same GPU product (e.g. all A100s or all H100s), consistent with NAP behavior and best practices.
- **Node capacity check**: Selected nodes must provide sufficient total GPU memory for the preset (per-replica memory times replicas); otherwise, the validating webhook rejects the Workspace creation or update.

The proposed algorithm is as follows:

#### NAP Scenario:

The `workspace.resource.instanceType` is no longer defaulted to `Standard_NC24ads_A100_v4`. It must be explicitly set and the webhook will fail if empty. The existing logic remains unchanged.

#### NAP Disabled:

- Workspace Validating Webhook:
  1. Confirm `workspace.resource.instanceType` is empty; fail if absent.
  2. List nodes matching the label selector; fail if there is no matching node.
  3. Verify uniformity: All nodes must have identical `nvidia.com/gpu.product`, `nvidia.com/gpu.count` and `nvidia.com/gpu.memory`; fail if not. This ensures homogeneous placement, i.e. all nodes have the same GPU type and configuration to simplify scheduling and the GPU requirement estimations.
  4. Calculate total GPU memory per node: `nvidia.com/gpu.count` * `nvidia.com/gpu.memory`.
  5. If the preset model does not support multi-node distributed inference (e.g. phi-4-mini-instruct), fail if the total GPU memory per node from 4) is less than the preset's total GPU memory requirement. The reason is that without multi-node distributed inference, each inference replica must fit entirely on a single node.
 
- Workspace Controller Reconciliation:
  1. Skip all NodeClaim-related operations if `workspace.resource.instanceType` is empty.
  2. Create/update underlying Deployment/LeaderWorkerSet with nodeSelector from `workspace.resource.labelSelector`.

>[!IMPORTANT]
> We do not enforce a minimum number of nodes during Workspace validation as long as at least one node matches the labelSelector (including `nvidia.com/gpu.product`), it provides the GPU count and memory hints for capacity computation.
>
> In NAP, KAITO handles provisioning or deleting the delta node amount to meet the desired total nodes based on `workspace.inference.replicas * workspace.status.perReplicaNodeCount`. In BYO, users are responsible for provisioning sufficient nodes; if not enough are available, pods may remain in Pending state, and KAITO will not remediate.

## Work Items

- [ ] ⁠Deprecate `.resource.preferredNodes` in Workspace CRD
- [ ] Install nvidia-device-plugin and gpu-feature-discovery via Helm chart dependencies, removing existing DaemonSet hard coded in the Workspace Helm chart
- [ ] Implement the proposed node selection algorithm in the KAITO Workspace controller
- [ ] Convert `.resource.instanceType` to an optional field with no default value