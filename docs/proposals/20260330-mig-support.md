---
title: NVIDIA Multi-Instance GPU (MIG) Support for BYO Nodes
authors:
  - "@robert-cronin"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2026-03-30
last-updated: 2026-03-30
status: draft
see-also:
  - docs/proposals/20250820-byo-nodes.md
  - docs/proposals/20250902-cloud-provider-agnostic-scheduling.md
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
    mig:
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
As an ML engineer, I want to deploy phi-4-mini on a MIG partition by adding `mig.profile: "1g.10gb"` to my Workspace spec, without needing to understand NVIDIA device plugin internals.

**Team running multiple small models cost-effectively:**
As a team lead, I want to run 7 independent phi-4-mini instances on a single A100-80GB using `1g.10gb` MIG slices, reclaiming GPU capacity that would otherwise sit idle.

### Goals

1. Allow users to run inference workloads on MIG partitions.
2. Validate at admission time that the model fits in the requested MIG partition.
3. Support running multiple KAITO workspaces on MIG partitions of the same node.
4. Handle both NVIDIA MIG strategies (single and mixed).
5. Prevent tensor parallelism on MIG slices (hardware-isolated, limited CUDA IPC between slices).

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
- GFD labels: `nvidia.com/mig.strategy=mixed`, `nvidia.com/gpu.product=A100-SXM4-80GB-MIG-1g.10gb`.

### Single Strategy

- All MIG partitions on a node must be the same size.
- Partitions are exposed as regular `nvidia.com/gpu` resources (same as non-MIG GPUs).
- GFD labels: `nvidia.com/mig.strategy=single`, `nvidia.com/gpu.memory=10240` (per-slice), `nvidia.com/gpu.count=7`.

### Key Differences

| Aspect | Mixed | Single |
|--------|-------|--------|
| Resource type | `nvidia.com/mig-<profile>` | `nvidia.com/gpu` |
| Partition sizes | Can differ per GPU | Must be uniform per node |
| Pod must know it's MIG? | Yes (requests specific profile) | No (looks like regular GPU) |
| Partial allocation | Yes (request 1 of N partitions) | Yes (request 1 of N "GPUs") |

Real-world data from GitHub issues across NVIDIA, KServe, and KubeRay repos shows that **mixed strategy with `1g.10gb` and `3g.40gb` profiles are the most commonly used** configurations. Mixed strategy is also the approach used by users running multiple different-sized models on the same GPU.

## Proposal

### Mixed Strategy Support

#### API Changes

Add `MIG` field to `ResourceSpec`:

```go
type ResourceSpec struct {
    // ... existing fields ...

    // MIG specifies NVIDIA Multi-Instance GPU configuration.
    // Requires enableMIG feature gate and BYO nodes (instanceType must be empty).
    // +optional
    MIG *MIGSpec `json:"mig,omitempty"`
}

type MIGSpec struct {
    // Profile is the MIG partition profile name (e.g., "1g.10gb").
    // Validated against known NVIDIA MIG profiles.
    Profile string `json:"profile"`
}
```

> **Design decision: no `Count` field.** MIG slices are hardware-isolated with only limited CUDA IPC between them, so tensor parallelism across slices is not feasible. Each Workspace pod always requests exactly 1 MIG slice. For running multiple instances of the same model, use multiple Workspace CRs or an InferenceSet (see Multi-replica with InferenceSet below).

#### Validation

When `resource.mig` is set:

1. Feature gate `enableMIG` must be true.
2. `instanceType` must be empty (BYO only). The webhook validates this per-Workspace rather than requiring a global feature gate.
3. Profile must match the MIG profile format (`<digits>g.<digits>gb`) and correspond to a known NVIDIA MIG profile. Long-term, consider relaxing the whitelist to regex-only validation so new GPU generations don't require a KAITO release.
4. Model must fit in a single MIG partition: `model_size × 1.02 < partition_memory × 0.84`. The 1.02× covers model-loading overhead (CUDA context, cuBLAS workspace). The 0.84 is KAITO's conservative `gpu-memory-utilization` setting (vLLM default is 0.90).
5. Model must not require tensor parallelism (`GPUCountRequirement` must be `"1"` or `DisableTensorParallelism` must be true).
6. MIG spec is immutable on update.
7. MIG is rejected for tuning workloads.

#### Pod Spec Generation

- Resource requests/limits use `nvidia.com/mig-<profile>: 1` instead of `nvidia.com/gpu`.
- Tensor parallelism is forced to 1.
- MIG-specific toleration added for `nvidia.com/mig-<profile>` (precautionary — the NVIDIA device plugin does not add MIG taints by default, but cluster operators may).

#### Estimator

- MIG workloads always return nodeCount=1. Multi-node MIG is technically possible but out of scope.
- Memory check: model must fit in a single slice.

#### vLLM MIG Compatibility

vLLM has a known issue parsing MIG device UUIDs in mixed strategy mode ([vLLM #6551](https://github.com/vllm-project/vllm/issues/6551), [#17047](https://github.com/vllm-project/vllm/issues/17047)). When the NVIDIA device plugin sets `CUDA_VISIBLE_DEVICES` to a MIG UUID (e.g., `MIG-52b4...`), vLLM's initialization crashes attempting to parse it as an integer.

Additionally, KAITO's `get_max_gpu_memory_utilization()` in `inference_api.py` uses `pynvml` which reports parent GPU memory (80GB) rather than MIG slice memory (10GB). The runtime allocation via `torch.cuda.mem_get_info()` is correct, but the initialization path needs fixing.

**Workarounds required in Step 1:**
- Patch KAITO's Python entrypoint to handle MIG UUID format before vLLM initialization
- Replace `pynvml` calls with `torch.cuda.mem_get_info()` for memory detection

### Multi-Workspace on a Single Node

One of MIG's primary benefits is GPU sharing. In KAITO, each Workspace creates its own StatefulSet. Multiple Workspaces can target the same node via `labelSelector` since KAITO does not set pod anti-affinity. Kubernetes resource accounting handles co-location natively — two Workspaces each requesting `nvidia.com/mig-1g.10gb: 1` get separate MIG partitions on the same node.

#### Multi-replica with InferenceSet

For running multiple replicas of the same model on MIG, creating N separate Workspace CRs is tedious. The InferenceSet CRD is a better fit:

```yaml
# PROPOSED — not yet implemented. Requires Step 3 (InferenceSet integration).
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: phi-4-mini-mig
spec:
  replicas: 4
  labelSelector:
    matchLabels:
      kaito.sh/mig-enabled: "true"
  template:
    resource:
      mig:
        profile: "1g.10gb"
    inference:
      preset:
        name: "phi-4-mini"
```

### Single Strategy Support

With single strategy, MIG partitions appear as regular `nvidia.com/gpu` resources. KAITO's BYO path doesn't know it's dealing with MIG slices, causing incorrect `tensor-parallel-size`, `max-model-len`, and GPU resource count calculations.

#### Step 2a: ConfigMap overrides

The Workspace API already supports user-provided inference ConfigMaps via `inference.config`. The vLLM Python entrypoint reads ConfigMap values and applies them as CLI overrides (argparse last-wins). The controller-side must also respect these overrides when computing `max-model-len` and GPU resource request count (~40-50 lines of Go). This is a general-purpose improvement that benefits non-MIG users too.

#### Step 2b: Auto-detection from GFD labels

GFD exposes `nvidia.com/mig.strategy=single` as a node label. In `GetGPUConfigFromNodeLabels`, KAITO can detect this and automatically set `IsMIG=true`, force TP=1, and parse the profile from the `nvidia.com/gpu.product` label (e.g., `A100-SXM4-80GB-MIG-1g.10gb`). This eliminates the ConfigMap requirement for single strategy and provides admission-time validation.

#### Single strategy user workflow (after Steps 2a+2b)

```yaml
# No MIG spec needed — KAITO auto-detects single strategy from node labels
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: phi-4-mini-single-strategy
spec:
  resource:
    labelSelector:
      matchLabels:
        kaito.sh/mig-enabled: "true"
  inference:
    preset:
      name: "phi-4-mini"
```

## Compatible Models

Models that work on MIG partitions must have `GPUCountRequirement: "1"`. KAITO automatically forces TP=1 for MIG workloads. Preset models that fit on a `1g.10gb` partition (~8.4 GiB usable, i.e. 10 GiB × 0.84):

| Model | SafeTensor Size | MIG on 1g.10gb |
|-------|----------------|----------------|
| phi-4-mini | ~7.15 GiB | Yes (TP auto-forced to 1) |
| phi-3-mini-4k-instruct | ~7.12 GiB | Yes (TP auto-forced to 1) |
| mistral-7b | ~13.5 GiB | No — needs larger partition (e.g., `2g.20gb`) |
| falcon-7b | ~13.5 GiB | No — needs larger partition |

**Note on quantized models:** Users running quantized models (AWQ, GPTQ) via BYO templates can fit larger models on small MIG slices (e.g., 4-bit Mistral-7B at ~3.5 GiB fits on `1g.5gb`). KAITO's preset validation uses full-precision sizes; quantization-aware validation is future work.

**Performance:** A `1g.10gb` slice provides ~1/8th of the A100's HBM bandwidth (~255 GB/s). For phi-4-mini at FP16, this yields ~34 tokens/sec — well above human reading speed (~6 tokens/sec). With INT4 quantization, throughput exceeds 100 tokens/sec per slice.

## Alternatives Considered

### 1. Raw `resourceRequests` map instead of `MIGSpec`

The [original issue #1744](https://github.com/kaito-project/kaito/issues/1744) proposed a generic map:

```yaml
resourceRequests:
  nvidia.com/mig-1g.10gb: 1
```

This is more Kubernetes-native and forward-compatible (works with any extended resource). However, it doesn't enable admission-time validation — KAITO can't parse memory from an opaque resource name to check model fit. The structured `MIGSpec` enables profile validation, memory-fit checking, and TP rejection that a raw map cannot. If we later support non-NVIDIA accelerator partitioning, a generic `resourceRequests` map could complement `MIGSpec`.

### 2. Full auto-detection (no `MIGSpec` at all)

KAITO could infer everything from node labels: detect MIG from `nvidia.com/mig.strategy` or `nvidia.com/mig-*` allocatable resources, pick the smallest profile where the model fits, and auto-set TP=1. The user would write a normal BYO Workspace with no MIG-specific fields.

This is the simplest UX but sacrifices explicit user intent — the user can't choose a specific profile, and validation happens at reconcile time against real node state rather than at admission. We chose explicit `MIGSpec` for mixed strategy (user declares intent, gets early validation) and plan auto-detection for single strategy (where the node genuinely looks like regular GPUs).

### 3. NVIDIA MPS instead of MIG

MPS (Multi-Process Service) is NVIDIA's software-level GPU sharing. It provides logical partitioning with SM percentage limits but no memory isolation, no memory bandwidth QoS, and no error isolation. MIG provides hardware-level isolation on all three dimensions. For multi-tenant inference where workloads must not interfere with each other, MIG is the right choice.

## Implementation Strategy

### Step 1: Mixed Strategy Support

- Add `MIGSpec` to Workspace CRD (single `profile` field, always requests 1 slice)
- Webhook validation: feature gate, BYO-only, profile validation, memory fit, TP rejection, tuning rejection
- Pod spec generation with `nvidia.com/mig-<profile>: 1` resource requests/limits
- Estimator: MIG always returns nodeCount=1
- MIG spec immutable on update
- Fix vLLM MIG UUID handling in KAITO's Python entrypoint
- Fix `get_max_gpu_memory_utilization()` to use `torch.cuda.mem_get_info()`
- Unit + e2e tests (e2e gated behind `MIGRequired` label)

### Step 2: Single Strategy Support

- **2a:** Allow user-provided inference ConfigMap to override `tensor-parallel-size`, `max-model-len`, `gpu-memory-utilization` (~40-50 lines of Go)
- **2b:** Auto-detect `nvidia.com/mig.strategy=single` in `GetGPUConfigFromNodeLabels` — set `IsMIG=true`, force TP=1, parse profile from `nvidia.com/gpu.product` label
- Surface `FailedScheduling` pod events in Workspace status conditions

### Step 3: InferenceSet Integration

- Add `MIG` field to `InferenceSetResourceSpec`
- Multi-replica MIG via `InferenceSet.spec.replicas` instead of N separate Workspace CRs

### Step 4: Documentation and Examples

- MIG user guide with Azure-specific prerequisites (GPU Operator install with `--gpu-driver none`, MIG manager config, node labeling)
- Examples for both strategies
- Model compatibility matrix with performance expectations
- Multi-workspace-per-node walkthrough

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| **No cluster-wide MIG capacity accounting** — excess Workspaces result in Pending pods | Users confused by silent Pending state | Step 2: surface `FailedScheduling` events in Workspace status. Document limitation. |
| **Hardcoded profile whitelist** — new GPUs require a KAITO release | Blocks adoption of new hardware | Consider regex-based escape hatch in Step 1 (Open Question #1). |
| **vLLM MIG UUID crash** — mixed strategy MIG UUIDs cause vLLM startup failure | MIG workloads broken without workaround | Fix in Step 1: patch KAITO's Python entrypoint. Track upstream vLLM fix. |
| **Memory validation margin** — formula `model × 1.02 < partition × 0.84` may be tight on small slices | Models pass validation but OOM at runtime | The 0.84 factor reserves 16% for KV cache and runtime. Monitor real-world results and adjust if needed. |
| **Node drain kills all co-located Workspaces** — 7 Workspaces on 7 slices all go NotReady simultaneously | High blast radius on node maintenance | Document the blast radius. Consider PDBs in future work. |

## Open Questions

1. Should the profile whitelist be replaced with regex-only validation (`^\d+g\.\d+gb$`) to avoid blocking new GPU generations? The scheduler already rejects invalid resource names.
2. Should KAITO surface `FailedScheduling` pod events in Workspace status conditions in Step 1 or Step 2?
3. The proposal specifies per-Workspace BYO validation (`instanceType == ""`). The current code checks a global `disableNodeAutoProvisioning` feature gate. Should we keep the per-Workspace approach (more flexible, allows mixed MIG + NAP clusters) or the global gate (simpler)?

## Future Work

- **Quantization-aware validation.** Teach the memory-fit check about quantized model sizes so preset models with AWQ/GPTQ can be validated against small MIG partitions.
- **Dynamic Resource Allocation (DRA).** The Kubernetes DRA API ([KEP-4381](https://github.com/kubernetes/enhancements/issues/4381), beta in 1.32) is the long-term replacement for extended resources. `MIGSpec` is an abstraction layer that can translate to DRA `ResourceClaims` when DRA matures.
- **Scheduling failure surfacing.** Propagate pod `FailedScheduling` events to Workspace status conditions.
- **B200/Blackwell and other GPU profiles.** B200 (180GB) MIG profiles are documented by NVIDIA. Additional MIG-capable GPUs (H20, GB200, RTX PRO 5000/6000) will be added as validated.

## Implementation History

- [x] 2026/03/11: Open proposal PR
- [ ] MM/DD/YYYY: Complete Step 1 (mixed strategy + vLLM fixes)
- [ ] MM/DD/YYYY: Complete Step 2 (single strategy: ConfigMap + auto-detection)
- [ ] MM/DD/YYYY: Complete Step 3 (InferenceSet integration)
- [ ] MM/DD/YYYY: Complete Step 4 (documentation)
