---
title: Multi-Instance GPU (MIG)
---

[NVIDIA Multi-Instance GPU (MIG)](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/) partitions a single physical GPU (A100, H100, H200, B200, …) into up to seven hardware-isolated instances, each with its own dedicated compute and memory. KAITO can schedule an inference workload onto one MIG slice, so several small models can share one large GPU instead of each occupying a whole card.

## Limitations

| Limitation | Reason |
| --- | --- |
| **BYO nodes only** — no node auto-provisioning (NAP/Karpenter) | KAITO does not create or partition MIG nodes; requires `disableNodeAutoProvisioning=true`. |
| **No tensor parallelism** — the model must fit in **one** slice | MIG slices are hardware-isolated with limited cross-slice CUDA IPC; a model requiring more than 1 GPU is rejected. |
| **No tuning** — inference only | Tuning workloads are not supported on MIG partitions. |
| **`partition.profile` is immutable** once set | Changing the slice size requires recreating the workspace. |
| **KAITO does not manage the partition layout** | Create/destroy MIG instances with the [NVIDIA GPU Operator MIG manager](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-mig.html). |
| **CPU KV-cache offload is disabled** on MIG pods | A MIG slice's small memory footprint plus host-RAM offload sizing can OOM a shared node; offload is turned off automatically. |

## Prerequisites

1. **A MIG-capable GPU node** (A100 / H100 / H200 / B200) in your cluster. Supported GPUs are listed in [NVIDIA Multi-Instance GPU User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/latest/supported-gpus.html).
2. [**The NVIDIA GPU Operator**](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-mig.html#enabling-mig-during-installation) installed, with the MIG manager enabled and a device-plugin **strategy** chosen (`mixed` or `single`).
3. [**The GPU partitioned** into your desired profile(s)](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-mig.html#configuring-mig-profiles), e.g. via the GPU Operator's `nvidia.com/mig.config` node label.
4. **KAITO installed** with `enableMIG=true` and `disableNodeAutoProvisioning=true`, and with the bundled NFD/GFD disabled (`gpu-feature-discovery.nfd.enabled=false`, `gpu-feature-discovery.gfd.enabled=false`) since the GPU Operator already provides them:
    ```bash
    helm install workspace ./charts/kaito/workspace \
        --namespace kaito-workspace --create-namespace \
        --set featureGates.enableMIG=true \
        --set featureGates.disableNodeAutoProvisioning=true \
        --set gpu-feature-discovery.nfd.enabled=false \
        --set gpu-feature-discovery.gfd.enabled=false
    ```
5. **The node labeled** so your workspace can target it, e.g.:
   ```bash
   kubectl label node <node-name> kaito-inferenceset=phi-4-mini
   ```

## NVIDIA MIG strategies

The strategy is chosen when you deploy the NVIDIA device plugin (GPU Operator Helm value `mig.strategy`). KAITO detects a MIG node from the GPU Operator labels (`nvidia.com/mig.config != "all-disabled"` **and** `nvidia.com/mig.config.state == "success"`) and supports both strategies.

| Aspect | Mixed | Single |
| --- | --- | --- |
| Resource requested | `nvidia.com/mig-<profile>` | `nvidia.com/gpu` |
| Partition sizes per GPU | Can differ (e.g. `2g.24gb` + `3g.47gb`) | Must be uniform per node |
| `partition` in the spec | **Required** (`mode: mig` + profile) — selects the slice | **Not needed** — any slice is a generic GPU |

## Configuration reference

Add a `partition` block under `resource` (Workspace) or `template.resource` (InferenceSet):

```yaml
resource:
  labelSelector:
    matchLabels:
      kaito.sh/mig-enabled: "true"   # target your MIG node(s)
  partition:
    mode: mig                        # only "mig" is supported today
    profile: "2g.24gb"               # mixed strategy only; omit the partition block for single strategy
```

- `mode` — the partitioning technology. Currently only `mig` (NVIDIA Multi-Instance GPU) is supported; the field is a discriminator that leaves room for other technologies later.
- `profile` — the MIG profile in `<compute>g.<memory>gb` form (e.g. `1g.10gb`, `2g.24gb`, `3g.47gb`, `7g.80gb`). Must be a [known NVIDIA profile](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/latest/supported-mig-profiles.html) and large enough for the model.
- `instanceType` — must be **empty** (BYO). Node auto-provisioning is disabled.
- `labelSelector` — targets the MIG node(s); required so the pod lands on a partitioned node.

**How KAITO maps the request:**

- **Mixed** — you set `resource.partition` (`mode: mig`, a profile), and KAITO requests the matching per-profile resource (`nvidia.com/mig-2g.24gb`). This is what lets you pick a specific slice size on a heterogeneously partitioned GPU.
- **Single** — all slices look like plain `nvidia.com/gpu`, so **no** partition block is needed; KAITO still detects the node is MIG (from labels) and sizes against a slice.

## Examples
### Mixed strategy

The GPU is partitioned into differently sized slices, and you select `2g.24gb` explicitly. KAITO requests `nvidia.com/mig-2g.24gb`.

```yaml
apiVersion: kaito.sh/v1beta1
kind: InferenceSet
metadata:
  name: phi-4-mini
  namespace: default
spec:
  replicas: 2
  labelSelector:
    matchLabels:
      kaito-inferenceset: phi-4-mini
  template:
    inference:
      preset:
        accessMode: public
        name: "microsoft/Phi-4-mini-instruct"
    resource:
      partition:
        mode: mig
        profile: "2g.24gb"
```

A single-replica `Workspace` equivalent:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: phi-4-mini-mig
resource:
  labelSelector:
    matchLabels:
      kaito-workspace: phi-4-mini
  partition:
    mode: mig
    profile: "2g.24gb"
inference:
  preset:
    name: "microsoft/Phi-4-mini-instruct"
```

### Single strategy

All slices on the node are the same size and exposed as `nvidia.com/gpu`, so **no `partition` block is required** — KAITO detects MIG from the node labels and requests a generic GPU (which the device plugin maps to one slice).

```yaml
apiVersion: kaito.sh/v1beta1
kind: InferenceSet
metadata:
  name: phi-4-mini
  namespace: default
spec:
  replicas: 2
  labelSelector:
    matchLabels:
      kaito-inferenceset: phi-4-mini
  template:
    inference:
      preset:
        accessMode: public
        name: "microsoft/Phi-4-mini-instruct"
```

### Verify MIG is working
Under MIG mode every inference pod should see exactly one MIG device, and the `MIG-...` UUIDs must differ between pods. To verify, run the following script:

   ```bash
   for p in $(kubectl get pods -l inferenceset.kaito.sh/created-by=phi-4-mini -o name); do
     echo "== $p =="
     kubectl exec "$p" -- nvidia-smi -L
   done
   ```

Expected outputs:
   ```text
   == pod/phi-4-mini-btgq9-0 ==
   GPU 0: NVIDIA H100 NVL (UUID: GPU-47310961-...)
     MIG 2g.24gb  Device 0: (UUID: MIG-1bb00402-...)
   == pod/phi-4-mini-cnm42-0 ==
   GPU 0: NVIDIA H100 NVL (UUID: GPU-47310961-...)
     MIG 2g.24gb  Device 0: (UUID: MIG-064e07a2-...) # different slice ✔
   ```

## See also
- [InferenceSet](./inference.md)
- [NVIDIA MIG User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/)
- [NVIDIA GPU Operator — MIG support](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-mig.html)
