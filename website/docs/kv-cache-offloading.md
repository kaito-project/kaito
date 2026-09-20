---
title: KV Cache Offloading
---

KV cache offloading extends vLLM's GPU KV cache with host memory, which can improve prefix reuse and reduce GPU KV-cache pressure for workloads with repeated prompt prefixes.. [LMCache](https://lmcache.ai/) is a KV-cache storage and transfer layer for LLM inference engines. KAITO runs an LMCache server in the inference container and connects vLLM to it through `LMCacheMPConnector`, allowing reusable KV blocks to be stored in CPU memory and loaded back into GPU memory when needed.


## Current Limitations

KV cache offloading is automatically disabled for:

- Multi-node pipeline-parallel inference
- Data-parallel deployments where each GPU runs an independent model replica
- MIG partitions
- Qwen family and Nemotron-H hybrid architectures that require additional LMCache configuration

KAITO currently supports KV cache offloading to host memory only. Offloading to secondary storage, such as local NVMe disks or remote object storage, is not supported.

Prefill/decode disaggregation uses NIXL for direct KV transfer and does not start the local LMCache server. See [Prefill/Decode Disaggregation](./prefill-decode-disaggregation.md).

## Configure Offloading

KV cache offloading is enabled by default for supported vLLM preset models. Its default CPU-memory utilization is `0.5`.

To choose a different value, create a ConfigMap with `kv_cache_cpu_memory_utilization` at the top level of `inference_config.yaml`, then reference that ConfigMap from the InferenceSet template:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: inference-params
  namespace: default
data:
  inference_config.yaml: |
    kv_cache_cpu_memory_utilization: 0.25
---
apiVersion: kaito.sh/v1beta1
kind: InferenceSet
metadata:
  name: gemma-4-12b
  namespace: default
spec:
  replicas: 1
  labelSelector:
    matchLabels:
      apps: gemma-4-12b
  template:
    resource:
      instanceType: Standard_NC24ads_A100_v4
    inference:
      preset:
        name: google/gemma-4-12B-it
      config: inference-params
```

Set the value to `0` to disable KV cache offloading:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: inference-params
  namespace: default
data:
  inference_config.yaml: |
    kv_cache_cpu_memory_utilization: 0
```

For a standalone Workspace, use the same ConfigMap name under `inference.config`.

## Memory Sizing

For each inference pod, KAITO calculates the LMCache L1 capacity as:

```text
L1 capacity = available host RAM × kv_cache_cpu_memory_utilization ÷ tensor parallel size
```

`available host RAM` is measured when the inference process starts. For example, with 400 GiB available RAM, utilization `0.5`, and tensor parallel size `2`, the L1 capacity is 100 GiB.

KAITO starts LMCache with lazy allocation and an initial L1 allocation of 1 GiB. LMCache then expands the pinned-memory pool in the background until it reaches the calculated capacity. Lazy allocation reduces synchronous startup work, but it does not reduce the eventual host-memory allocation.

Ensure the node has enough RAM for the final L1 capacity plus model loading, vLLM, and operating-system overhead. Kubernetes memory limits, if configured, must also accommodate the final allocation.

:::note
`kv_cache_cpu_memory_utilization` controls KV cache storage in host memory. It is different from vLLM's `cpu-offload-gb`, which offloads model weights.
:::

## Verify Offloading

Find the inference pod:

```bash
kubectl get pods -l kaito.sh/workspace=<workspace-name>
```

Check the logs for LMCache startup and allocation:

```bash
kubectl logs <pod-name> | grep -E "Offload KV cache|LMCache MP server|LazyMemoryAllocator"
```

Expected output includes the initial and final L1 sizes, followed by background expansion progress:

```text
Offload KV cache to LMCache MP server: 1.00 GB initial, 100.00 GB final
LMCache MP server is ready on 127.0.0.1:5555
LazyMemoryAllocator: Expanded ... pinned memory ... (100.0%)
```

During inference, LMCache logs store and retrieve operations. vLLM's `/metrics` endpoint also reports external prefix-cache activity, including `vllm:external_prefix_cache_hit_rate` when supported by the runtime version.

## Related Resources

- [Serving with InferenceSet](./inference.md)
- [LMCache documentation](https://docs.lmcache.ai/)
