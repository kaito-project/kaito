---
title: Multi-node Distributed Inference
authors:
  - chewong
reviewers:
  - Kaito contributor
creation-date: 2025-03-25
last-updated: 2025-03-25
status: provisional
---

# Title

Multi-node Distributed Inference

## Summary

The majority of preset models supported by Kaito are designed to operate on a single GPU, with the current inference runtimes optimized for single-node, single-GPU configurations. However, as models with larger parameter counts, such as Llama 70B, are integrated into Kaito, multi-GPU, multi-node distributed inference becomes necessary since they won't fit into all the available GPUs on a single node.

Kaito currently supports multi-GPU distributed inference through [Torch Elastic](https://pytorch.org/docs/stable/elastic/quickstart.html) and [HuggingFace Transformers](https://huggingface.co/docs/transformers/en/index) for two models: Llama 13B and Llama 70B. Llama 70B may require two nodes with a total of 8 GPUs (e.g., when using an instance type with 4 GPUs per node), although it can also be deployed on a single node with 8 GPUs, such as Azure’s `Standard_ND96asr_v4`. However, Kaito’s multi-node serving implementation lacks support for [GPUDirect RDMA](https://docs.nvidia.com/cuda/gpudirect-rdma/), a technology that enables direct memory access from the GPU to the network interface card (NIC) without CPU involvement. This capability reduces serving latency and increases token throughput by eliminating CPU-related bottlenecks during data transfers between GPUs across nodes, which is critical for distributed inference.

Additionally, vLLM became the default inference runtime for preset models for Kaito in January 2025 (refer to https://github.com/kaito-project/kaito/pull/823) but Kaito hasn't supported multi-GPU, multi-node distributed inference support for it yet.  This limitation requires attention to ensure compatibility with larger models in the future.

This proposal aims to future-proof Kaito’s capabilities by establishing multi-GPU, multi-node distributed inference as a fully supported feature for both HuggingFace Transformers and vLLM inference runtimes. The objective is to maintain a consistent user experience, enabling seamless deployment of large preset models across multiple nodes and GPUs with minimal changes to existing workflows.

### Goals

- Implement support for multi-GPU, multi-node distributed inference for preset models, with or without GPUDirect RDMA over high-speed networks.

### Non-Goals

- Implement multi-node distributed inference on **all** preset models. Smaller models may not require it, and some lack support for distributed inference configurations.
- Enforce specific high-speed network technology. Cloud providers vary in what technology they offer, so the solution should be flexible enough to different network types.
- Extend multi-node distributed inference to custom models. This proposal targets preset models only. Custom models default to `Deployments` rather than `StatefulSets` as the model deployment mechanism, and `Deployments` lack the [pod identity](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-identity) required for multi-node coordination. A separate proposal is needed to address this.
- Address distributed model tuning. Few preset models support fine-tuning in Kaito, and most do not require multi-node configurations. This proposal focuses on inference only.
- Introduce a new inference runtime. The existing runtimes (HuggingFace Transformers and vLLM) will be improved to support multi-GPU, multi-node distributed inference. A separate proposal is needed to address the addition of new runtimes.

## Requirements

### Driver and Device Plugin Installation

Similar to the [NVIDIA device plugin](https://github.com/kaito-project/kaito/blob/main/charts/kaito/workspace/templates/nvidia-device-plugin-ds.yaml) included in Kaito’s Helm chart, specific drivers and device plugins must be installed on the host to enable GPUDirect RDMA over high-speed networks, such as [InfiniBand (IB)](https://www.nvidia.com/en-us/networking/products/infiniband/) or [Elastic Fabric Adapter (EFA)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/efa.html). The installation process varies by cloud provider, but the general steps are as follows:

- Install the necessary driver to enable RDMA on the host, if not already provided in the worker node image by the cloud provider.
- Deploy the device plugin to allow pods to request and access high-speed network resources, avoiding the need for privileged containers. This step is required because most cloud providers do not include the device plugin by default during cluster creation.
- Specify the resource name and count in the PodSpec resource section (e.g., `rdma/ib: 8` or `vpc.amazonaws.com/efa: 32`).

Kaito supports Azure and AWS as primary cloud providers. The installation steps are outlined below:

| Cloud Provider | Installation Steps                                                                                                                                                                                                    |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Azure          | - Install [DOCA driver](https://catalog.ngc.nvidia.com/orgs/nvidia/teams/mellanox/containers/doca-driver) to enable [RDMA over IB](https://azure.github.io/aks-rdma-infiniband/)<br>- Install the RDMA device plugin. |
| AWS            | - Install the [EFA device plugin](https://github.com/aws/eks-charts/tree/master/stable/aws-efa-k8s-device-plugin)                                                                                                     |

### Workspace API Changes

No major API changes are needed for the workspace. The existing workspace API implicitly supports multi-GPU, multi-node distributed inference, so users can continue using the same Workspace spec without any changes due to the following:

- The number of nodes is specified via `.resource.count` in the Workspace specification:

```yaml
...
resource:
  instanceType: "Standard_ND96asr_v4"
  count: 2
...
```

For pre-provisioned nodes, users may define a list in the existing API `.resource.preferredNodes` to ensure inference deployment to specific nodes:

```yaml
...
resource:
  count: 2
  preferredNodes:
    - my-favorite-node-1
    - my-favorite-node-2
...
```

- The number of GPUs required is predefined for preset models, eliminating the need for users to specify this in the Workspace spec. The following table lists GPU requirements for Kaito's preset models as of March 2025:

| Model                        | Required GPUs |
| ---------------------------- | ------------- |
| deepseek-r1-distill-llama-8b | 1             |
| deepseek-r1-distill-qwen-14b | 1             |
| falcon-40b-instruct          | 2             |
| falcon-40b                   | 2             |
| falcon-7b-instruct           | 1             |
| falcon-7b                    | 1             |
| llama-2-13b-chat             | 2             |
| llama-2-13b                  | 2             |
| llama-2-70b-chat             | 8             |
| llama-2-70b                  | 8             |
| llama-2-7b-chat              | 1             |
| llama-2-7b                   | 1             |
| mistral-7b-instruct          | 1             |
| mistral-7b                   | 1             |
| phi-2                        | 1             |
| phi-3-medium-128k-instruct   | 1             |
| phi-3-medium-4k-instruct     | 1             |
| phi-3-mini-128k-instruct     | 1             |
| phi-3-mini-4k-instruct       | 1             |
| phi-3.5-mini-instruct        | 1             |
| qwen2.5-coder-7b-instruct    | 1             |
| phi-3.5-mini-instruct        | 1             |
| qwen2.5-coder-7b-instruct    | 1             |

### Workspace API Validation

- [x] For models requiring more than one GPU, validate that `# GPUs/instance × workspace.resource.count ≥ Required # GPUs for a preset model`. This validation is performed in the Kaito API server when creating or updating a workspace. If the condition is not met, an error message will be returned to the user. This is already implemented in [`api/v1beta1/workspace_validation.go`](https://github.com/kaito-project/kaito/blob/1815428804593eaa94de0d6f78d82b53e85d0137/api/v1beta1/workspace_validation.go#L293-L380) and does not require any changes.
- [ ] Verify that nodes provide access to high-speed network resources. If not, the default network interface will be used, which may not support GPUDirect RDMA and may lead to performance degradation.
- [ ] Introduce a feature gate named `ForceGPUDirectRDMA` to force the use of high-speed network resources for multi-GPU, multi-node distributed inference. When enabled, this feature gate requires Kaito to verify the availability of high-speed network resources (e.g., RDMA-capable hardware) during the deployment of large models that require multi-GPU, multi-node distributed inference. If such resources are available, Kaito will configure the workload to utilize them; if not, it will return an error message to the user and halt deployment. This feature gate guarantees that user's workloads leverage high-speed networking for optimal performance in distributed inference scenarios. By default, `ForceGPUDirectRDMA` is disabled to accommodate users who do not require this enforcement. It can be activated within the Kaito controller manager’s deployment specification:

```yaml
...
spec:
  containers:
    - name: manager
      args:
        - --feature-gates=ForceGPUDirectRDMA=true
...
```

> If the feature gate is disabled, Kaito will perform best-effort validation of the availability of high-speed network resources. If they are not available, Kaito will proceed with the deployment using the default network interface, which may not support GPUDirect RDMA. This approach allows users to deploy workloads even if high-speed network resources are not available, but it may result in suboptimal performance.

### Inference Runtime Parameters

#### vLLM

Flag additions to the vLLM base command is needed to support multi-GPU, multi-node distributed inference:

- `--tensor-parallel-size`: Set to the number of GPUs per node to configure tensor parallelism. This parameter determines how the model’s tensors are split across GPUs within a single node.
- `--pipeline-parallel-size`: Set to the number of nodes to configure pipeline parallelism. This parameter determines how different model layers are shared across nodes.

Per vLLM's [distributed inference and serving guidence](https://docs.vllm.ai/en/latest/serving/distributed_serving.html), it uses [Ray](https://www.ray.io/) as the default framework to manage its distributed inference runtime. The vLLM base image at `vllm/vllm-openai:latest` has provided a convenient script called [`multi-node-serving.sh`](https://github.com/vllm-project/vllm/blob/main/examples/online_serving/multi-node-serving.sh) to start the Ray server. The same script can be used by worker pods to join the Ray cluster.

Leader:

```bash
...
command:
  - /bin/sh
  - -c
  - |
    # --ray_cluster_size is the number of nodes in the cluster
    /workspace/vllm/multi-node-serving.sh leader --ray_cluster_size=2 --ray_port=6379
    # 8 GPUs per node, 2 nodes
    python3 /workspace/vllm/inference_api.py --tensor-parallel-size=8 --pipeline-parallel-size=2 --served-model-name=super-huge-model --kaito-config-file=/mnt/config/inference_config.yaml
...
```

Workers:

```bash
...
command:
  - /bin/sh
  - -c
  - |
    # --ray_address points to the cluster IP of the headless service of the leader pod
    /workspace/vllm/multi-node-serving.sh worker --ray_address=http://10.1.2.3:6379
...
```

#### HuggingFace Transformer

- Already supports distributed coordination with [Torch Elastic](https://pytorch.org/docs/stable/elastic/quickstart.html).


### Pod Template Modifications

To enable multi-node distributed inference, the pod template within the StatefulSet requires the following updates:

- [ ] **RDMA Resource Allocation**: Include RDMA resources in the pod template specification to allow the device plugin to assign them to pods. The resource name and count vary by cloud provider and GPU configuration. For example, AWS EFA requires `vpc.amazonaws.com/efa: 32` (supporting 32 EFA interfaces for 8 GPUs), while Azure RDMA over InfiniBand uses `rdma/ib: 8`.
- [ ] **IPC_LOCK Capability**: Add the `IPC_LOCK` capability to the container’s security context. This is essential for RDMA functionality, as it allows the container to lock memory pages in RAM, preventing swapping to disk and ensuring low-latency memory access critical for high-performance workloads.
- [ ] **Shared Memory Mount**: Mount `/dev/shm` in the pod specification. This provides shared memory for inter-process communication and synchronization, a requirement for vLLM to operate effectively in a distributed setup.

### Base Image Updates

The base image for `docker/presets/models/tfs/Dockerfile` must be modified to support multi-GPU, multi-node distributed inference:

- [ ] **Switch to NVIDIA CUDA Base**: Replace the current base image with [`nvidia/cuda`](https://hub.docker.com/r/nvidia/cuda), which includes pre-installed CUDA and NCCL libraries necessary for multi-GPU and multi-node operations.
- [ ] **Install RDMA Support**: Add `libibverbs-dev` via `apt-get install`. This library enables userspace processes to leverage RDMA “verbs” as defined by the InfiniBand Architecture and RDMA Protocol specifications
- [ ] **Add Python**: Install Python using `apt-get`, as it is not included in the `nvidia/cuda` base image. Python is required to execute the vLLM multi-node serving script effectively.

## Glossary

| Term           | Definition                                                                                                                                                                                   | Description                                                                                                                                                                                              |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| DOCA           | Data Center-on-a-Chip Architecture                                                                                                                                                           | A software framework from NVIDIA that provides a unified programming model for data processing and networking on NVIDIA hardware. It includes support for RDMA over InfiniBand.                          |
| EFA            | Elastic Fabric Adapter                                                                                                                                                                       | A network interface for AWS that provides low-latency, high-throughput networking and is optimized for HPC and machine learning workloads.                                                               |
| GPUDirect RDMA | A feature of NVIDIA GPUs that enables direct memory access from the GPU to the network interface card (NIC) without CPU involvement, reducing latency and boosting throughput significantly. | Critical for high-performance distributed inference as it eliminates CPU bottlenecks when transferring data between GPUs across nodes, significantly improving communication speed for large AI models.  |
| IB             | InfiniBand                                                                                                                                                                                   | A high-speed network technology often used in data centers and supercomputers, providing low latency and high throughput.                                                                                |
| NIC            | Network Interface Card                                                                                                                                                                       | A hardware component that connects a computer to a network, enabling communication with other devices.                                                                                                   |
| RDMA           | Remote Direct Memory Access                                                                                                                                                                  | A technology that allows direct memory access from the memory of one computer to another without involving either computer's operating system, reducing latency and CPU overhead for network operations. |


# History

- [x] 2023-03-24: Open proposal PR.
