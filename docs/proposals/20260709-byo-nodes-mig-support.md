---
title: NVIDIA Multi-Instance GPU (MIG) Support for BYO Nodes
authors:
  - "@zhehli688"
  - "@robert-cronin"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2026-07-10
last-updated: 2026-07-15
status: draft
---

# Title

NVIDIA Multi-Instance GPU (MIG) Support for BYO Nodes

## Summary

KAITO currently allocates full GPUs to model workloads. This proposal adds support for NVIDIA MIG, allowing users to run inference workloads on GPU partitions instead of full GPUs. A user with a MIG-partitioned node writes:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: phi-4-mini-mig
spec:
  resource:
    labelSelector:
      matchLabels:
        kaito.sh/mig-enabled: "true"
    partition:
      mode: mig
      profile: "1g.10gb"
  inference:
    preset:
      name: "phi-4-mini"
```

KAITO validates the model fits in the partition, requests the correct `nvidia.com/mig-1g.10gb` extended resource, and forces tensor-parallel-size to 1. Multiple Workspaces can share MIG slices on the same node.

The initial scope targets BYO (bring-your-own) nodes with mixed strategy. Single strategy support follows via ConfigMap overrides and auto-detection.

## Glossary

- **MIG**: Multi-Instance GPU. NVIDIA feature that partitions a physical GPU into isolated instances with dedicated compute and memory.
- **MIG profile**: A partition configuration described as `<compute_slices>g.<memory>gb` (e.g., `1g.10gb`, `3g.40gb`).
- **Single strategy**: NVIDIA device plugin mode where all MIG partitions on a node must be the same size. Partitions are exposed as regular `nvidia.com/gpu` resources.
- **Mixed strategy**: NVIDIA device plugin mode where different partition sizes can coexist. Each profile gets its own extended resource (e.g., `nvidia.com/mig-1g.10gb`).
- **GFD**: GPU Feature Discovery. NVIDIA component that labels nodes with GPU attributes.
- **NAP**: Node auto-provisioning (via Karpenter).
- **BYO**: Bring-your-own nodes.
- **TP**: Tensor parallelism. Distributing a model across multiple GPUs — not feasible on MIG slices.
- **MPS**: Multi-Process Service. NVIDIA's software-level GPU sharing (logical partitioning, no memory isolation). MIG provides hardware-level isolation.
- **InferenceSet**: A KAITO CRD ([proposal](20250918-introduce_inferenceset_autoscaling.md)) that manages multiple Workspace replicas as a single object with autoscaling support.

## Motivation

Organizations running large GPU nodes (A100, H100) for big models often have spare GPU capacity. A node with 8× A100 GPUs running a large model on 4 GPUs leaves 4 GPUs idle. Smaller LLMs like phi-4-mini only need ~4GB of VRAM — dedicating a full A100 (80GB) to each wastes ~$3/hr per unused GPU. MIG allows partitioning those spare GPUs into smaller slices, so multiple small models can run on capacity that would otherwise sit unused.

On Azure, single-GPU VMs like `Standard_NC24ads_A100_v4` (1× A100-80GB) or `Standard_NC40ads_H100_v5` (1× H100-94GB) can be MIG-partitioned into 7 slices, each serving an independent model — turning one VM into a cost-effective multi-model inference node.

MIG is most valuable when:
- You already have large-GPU nodes and want to reclaim spare capacity for small models
- You need hardware-level isolation between co-located inference workloads (stronger than MPS, which only provides software-level sharing)
- Smaller GPU instances (T4, A10) are unavailable in your region or don't meet your requirements

Refs: [Issue #1744](https://github.com/kaito-project/kaito/issues/1744)

### User Stories

**Platform engineer reclaiming spare GPU capacity:**
As a platform engineer managing an AKS cluster with A100 nodes, I want to partition idle GPUs into MIG slices so my ML engineers can deploy small models without wasting full A100s.

**ML engineer deploying a small model:**
As an ML engineer, I want to deploy phi-4-mini on a MIG partition by adding a `partition` block (`mode: mig`, `profile: "1g.10gb"`) to my Workspace spec, without needing to understand NVIDIA device plugin internals.

**Team running multiple small models cost-effectively:**
As a team lead, I want to run 7 independent phi-4-mini instances on a single A100-80GB using `1g.10gb` MIG slices, reclaiming GPU capacity that would otherwise sit idle.

### Goals

1. Allow users to run inference workloads on MIG partitions.
2. Support running multiple KAITO workspaces on MIG partitions of the same node.
3. Handle both NVIDIA MIG strategies (single and mixed).
4. Prevent tensor parallelism on MIG slices (hardware-isolated, limited CUDA IPC between slices).

### Non-Goals

1. Managing MIG partition lifecycle (creating/destroying partitions on the node). We recommend the [NVIDIA GPU Operator's MIG manager](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-mig.html) for declarative partition setup.
2. NAP integration (auto-provisioning MIG-enabled nodes).
3. MIG support for tuning workloads.
4. Bin-packing multiple models into a single pod.
5. Cluster-wide MIG capacity accounting (see Risks and Mitigations).

## NVIDIA MIG Strategies

NVIDIA provides two strategies for exposing MIG devices in Kubernetes. The choice is made when deploying the NVIDIA device plugin (e.g., `helm install --set migStrategy=mixed`).

### Mixed Strategy

- Different partition sizes can coexist on the same GPU (e.g., one `3g.40gb` + one `4g.40gb` on an A100-80GB).
- Each profile is exposed as a distinct extended resource: `nvidia.com/mig-1g.10gb`, `nvidia.com/mig-3g.40gb`, etc.
- Pods must request the specific MIG resource type they need.

### Single Strategy

- All MIG partitions on a node must be the same size.
- Partitions are exposed as regular `nvidia.com/gpu` resources (same as non-MIG GPUs).

### Key Differences

| Aspect | Mixed | Single |
|--------|-------|--------|
| Resource type | `nvidia.com/mig-<profile>` | `nvidia.com/gpu` |
| Partition sizes | Can differ per GPU | Must be uniform per node |
| Pod must know it's MIG? | Yes (requests specific profile) | No (looks like regular GPU) |
| Partial allocation | Yes (request 1 of N partitions) | Yes (request 1 of N "GPUs") |


## Proposal

### API Changes

GPU partitioning is exposed through a generic `Partition` field on `ResourceSpec`. A
`mode` discriminator selects the partitioning technology; today only NVIDIA MIG
(`mode: "mig"`) is implemented, but the shape leaves room for other partitioning
technologies without another breaking API change.

```go
type ResourceSpec struct {
    // ... existing fields ...

    // Partition specifies GPU partitioning for the workload.
    // Requires enableMIG feature gate and BYO nodes (instanceType must be empty).
    // +optional
    Partition *PartitionSpec `json:"partition,omitempty"`
}

// PartitionMode identifies the GPU partitioning technology.
// +kubebuilder:validation:Enum=mig
type PartitionMode string

const (
    // PartitionModeMIG partitions the GPU using NVIDIA Multi-Instance GPU (MIG).
    PartitionModeMIG PartitionMode = "mig"
)

type PartitionSpec struct {
    // Mode selects the partitioning technology. Currently only "mig".
    // +kubebuilder:validation:Enum=mig
    Mode PartitionMode `json:"mode"`
    // Profile is the partition profile, interpreted per Mode. For MIG this is a
    // profile name like "1g.10gb", "3g.40gb".
    Profile string `json:"profile"`
}
```

> **Design decision: generic `Partition`, not a MIG-specific field.** Naming the field
> after one vendor's technology would force another breaking API change to add a second.
> The `mode` discriminator keeps the API stable — validation and the controller dispatch
> on it, and the NVIDIA-specific implementation (the `mig` package, `IsMIG`,
> `GetMIGGPUConfig`) lives behind `mode: "mig"`.

> **Design decision: no `Count` field.** MIG slices are hardware-isolated with only limited CUDA IPC between them, so tensor parallelism across slices is not feasible. Each Workspace pod always requests exactly 1 partition. For running multiple instances of the same model, use multiple Workspace CRs or an InferenceSet (see Multi-replica with InferenceSet below).

`InferenceSetResourceSpec` currently exposes only `InstanceType` (marked `+required`), because InferenceSet was designed for the NAP path. Partitioning requires BYO nodes, so two changes are needed in **both `v1alpha1` and `v1beta1`**:

```go
type InferenceSetResourceSpec struct {
    // InstanceType specifies the GPU node SKU.
    // Now optional: must be empty (BYO) when Partition is set.
    // +optional
    InstanceType string `json:"instanceType,omitempty"`

    // Partition specifies GPU partitioning for each replica. Mirrors Workspace
    // ResourceSpec.Partition; propagated verbatim to each child Workspace.
    // +optional
    Partition *PartitionSpec `json:"partition,omitempty"`
}
```

The controller propagation at inferenceset_controller.go gains one line so each child Workspace inherits the partition spec:

```go
workspaceObj.Resource = kaitov1beta1.ResourceSpec{
    InstanceType:  iObj.Spec.Template.Resource.InstanceType,
    LabelSelector: iObj.Spec.Selector,
    Partition:     iObj.Spec.Template.Resource.Partition, // new
}
```

> **Making `InstanceType` optional is a broader change** than MIG alone — it also unblocks non-MIG BYO InferenceSets (deploying replicas onto pre-existing nodes via label selector). This is intentional and complements the BYO Workspace work. Existing InferenceSet CRs that set `instanceType` are unaffected.


### Validation

When `resource.partition` is set (validation dispatches on `mode`; only `mig` is implemented today):

1. Feature gate `enableMIG` must be true.
2. `instanceType` must be empty (BYO only). The webhook validates this per-Workspace rather than requiring a global feature gate.
3. Profile must match the MIG profile format (`<digits>g.<digits>gb`) and correspond to a known NVIDIA MIG profile. Long-term, consider relaxing the whitelist to regex-only validation so new GPU generations don't require a KAITO release.
4. MIG spec is immutable on update.
5. MIG is rejected for tuning workloads.

Model-to-partition fit is only coarsely enforced in the webhook: a lightweight
admission-time check rejects a preset whose raw weight size (`TotalSafeTensorFileSize`)
exceeds the slice's advertised memory, giving fast feedback on obviously oversized
models. The authoritative, overhead-aware sizing is still performed by the node
estimator, which sizes GPUs from model memory against the slice's usable memory.

### Estimator

- MIG workloads always return nodeCount=1. Multi-node MIG is technically possible but out of scope.
- Memory-fit check: the model plus runtime overhead must fit the slice's usable memory. The estimator reuses its non-MIG single-device overhead model rather than a single fudge factor:
  - `modelSize = TotalSafeTensorFileSize × 1.02` (weight expansion once loaded by vLLM)
  - `overhead = 2.3 GiB base (CUDA context, NCCL, small-model activations/CUDA graphs) + KV cache (maxModelLen × BytesPerToken) + 0.05 × modelSize`
  - fits when `modelSize + overhead ≤ slice_memory × 0.84` (the `0.84` mirrors vLLM's `--gpu-memory-utilization`).

### Pod Spec Generation

- Resource requests/limits use `nvidia.com/mig-<profile>: 1` instead of `nvidia.com/gpu` on the mixed/spec-driven path. Under single strategy the request stays `nvidia.com/gpu`: the profile is a spec property (`resource.partition.profile`), and single-strategy workloads have no spec profile, so they keep requesting the generic resource.
- Tensor parallelism is forced to 1.
- **CPU KV-cache offload is disabled** (`kaito-kv-cache-cpu-memory-utilization=0`). LMCache sizes its CPU offload buffer from *host* RAM (cgroup-unaware); on a shared MIG node, several pods each reserving a large fraction of host RAM overcommits the node and triggers OOM kills. Offload is therefore turned off for any MIG workload.
- MIG-specific toleration added for `nvidia.com/mig-<profile>` (precautionary — the NVIDIA device plugin does not add MIG taints by default, but cluster operators may). Only added on the mixed/spec-driven path where a per-profile taint key exists.

### MIG Mode Detection
`GetGPUConfigFromNodeLabels` detects a MIG node from the NVIDIA GPU Operator mig-manager labels: `nvidia.com/mig.config != "all-disabled"` **and** `nvidia.com/mig.config.state == "success"`. This signal works for **both** strategies and is robust to the transient states during repartitioning.

When detected, KAITO sets `IsMIG=true` on the GPU config, which (a) disables CPU KV-cache offload and (b) sizes memory against a slice. It deliberately does **not** derive a MIG *profile* from node labels: under single strategy the slices are requested via the generic `nvidia.com/gpu` resource, so no per-profile name is needed, and a physically partitioned GPU can host multiple different profiles. The requested profile stays a spec property (`resource.partition.profile`) used only on the mixed path. Consequently `GPUConfig` carries only `IsMIG`, not a profile field.

### get_max_gpu_memory_utilization Changes

KAITO's `get_max_gpu_memory_utilization()` in `inference_api.py` uses `pynvml` which reports parent GPU memory (80GB) rather than MIG slice memory (10GB). The runtime allocation via `torch.cuda.mem_get_info()` is correct, but the initialization path needs fixing


### Example CRs
#### Mixed strategy
Under mixed strategy a GPU can contain different profiles and we need to specify which profile to deploy our model on:
```yaml
apiVersion: kaito.sh/v1beta1
kind: InferenceSet
metadata:
  name: phi-4-mini-mig
spec:
  replicas: 2
  labelSelector:
    matchLabels:
      kaito-inferenceset: "phi-4-mini-mig"
  template:
    resource:
      partition:
        mode: mig
        profile: "2g.24gb"
    inference:
      preset:
        name: "phi-4-mini"
```

#### Single strategy
Same as deploying an InferenceSet without MIG because GPU slices appear as plain `nvidia.com/gpu` under single strategy:
```yaml
apiVersion: kaito.sh/v1beta1
kind: InferenceSet
metadata:
  name: phi-4-mini-mig
spec:
  replicas: 2
  labelSelector:
    matchLabels:
      kaito-inferenceset: "phi-4-mini-mig"
  template:
    inference:
      preset:
        name: "phi-4-mini"
```

## Alternatives Considered

### 1. Raw `resourceRequests` map instead of `PartitionSpec`

The [original issue #1744](https://github.com/kaito-project/kaito/issues/1744) proposed a generic map:

```yaml
resourceRequests:
  nvidia.com/mig-1g.10gb: 1
```

This is more Kubernetes-native and forward-compatible (works with any extended resource). However, it doesn't enable admission-time validation — KAITO can't parse memory from an opaque resource name to check model fit. The structured `PartitionSpec` enables profile validation, memory-fit checking, and TP rejection that a raw map cannot. If we later support non-NVIDIA accelerator partitioning, a generic `resourceRequests` map could complement `PartitionSpec`.

### 2. NVIDIA MPS instead of MIG

MPS (Multi-Process Service) is NVIDIA's software-level GPU sharing. It provides logical partitioning with SM percentage limits but no memory isolation, no memory bandwidth QoS, and no error isolation. MIG provides hardware-level isolation on all three dimensions. For multi-tenant inference where workloads must not interfere with each other, MIG is the right choice.


## Future Work

- **Dynamic Resource Allocation (DRA).** The Kubernetes DRA API ([KEP-4381](https://github.com/kubernetes/enhancements/issues/4381), beta in 1.32) is the long-term replacement for extended resources. `PartitionSpec` is an abstraction layer that can translate to DRA `ResourceClaims` when DRA matures.
- **Scheduling failure surfacing.** Propagate pod `FailedScheduling` events to Workspace status conditions.
- **B200/Blackwell and other GPU profiles.** B200 (180GB) MIG profiles are documented by NVIDIA. Additional MIG-capable GPUs (H20, GB200, RTX PRO 5000/6000) will be added as validated.