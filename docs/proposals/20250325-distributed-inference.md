---
title: Distributed Inference
authors:
  - chewong
reviewers:
  - Kaito contributor
creation-date: 2025-03-25
last-updated: 2025-05-01
status: provisional
---

# Title

Distributed Inference

## Summary
While KAITO excels with single-GPU models, the increasing size of state-of-the-art models necessitates multi-node distributed inference capabilities. Models with hundreds of billions of parameters often exceed the memory capacity of even the largest single nodes available today.

KAITO has support for multi-node inference via HuggingFace Transformers and Torch Elastic, previously used for deprecated models like Llama 2 13B/70B. However, no current preset models leverage this and the default vLLM runtime lacks multi-node support within KAITO since its adoption in January 2025 ([#823](https://github.com/kaito-project/kaito/pull/823)).

This proposal aims to bridge this gap by implementing multi-node distributed inference, primarily focusing on the vLLM runtime. The goal is to enable  deployment of very large preset models across multiple nodes, ensuring KAITO remains capable of serving cutting-edge models while maintaining a consistent user experience.

## What Is Distributed Inference Anyway?

Distributed inference enables models too large for a single GPU to run across multiple GPUs or nodes. Key strategies relevant to KAITO include:

1.  **Single-Node Multi-GPU (Tensor Parallelism):** Splits a model across multiple GPUs *within* a single node. Used when a model fits on one node but exceeds a single GPU's memory. Both HuggingFace Transformers and vLLM support this.
2.  **Multi-Node Multi-GPU (Pipeline and Tensor Parallelism):** Splits a model across multiple nodes, typically using pipeline parallelism between nodes and tensor parallelism within each node. Used for models exceeding a single node's capacity. HuggingFace Transformers supports this via Torch Elastic, but vLLM currently does not in KAITO.

KAITO's current support for these strategies:

| Strategy              | HuggingFace Transformers |        vLLM        |
| --------------------- | :----------------------: | :----------------: |
| Single GPU            |           Yes            |        Yes         |
| Single-Node Multi-GPU |           Yes            |        Yes         |
| Multi-Node Multi-GPU  |           Yes            | No (This proposal) |

### Goals

- Implement multi-node distributed inference for preset models using the vLLM inference runtime in KAITO.

### Non-Goals

- **Applying distributed inference to all preset models:** This proposal specifically targets very large models (e.g., 400B+ parameters) that necessitate multi-node deployment. Applying it to smaller models is not intended, as it could introduce unnecessary overhead and potentially degrade performance.
- **Supporting distributed inference for custom models:** Custom models currently use Kubernetes Deployments, which lack the stable pod identity required for the proposed multi-node coordination using StatefulSets. Extending support to custom models would require a separate effort.
- **Implementing distributed model tuning:** The scope of this proposal is limited to inference. Distributed tuning is not addressed.
- **Introducing new runtimes or deployment mechanisms:** This proposal focuses on enhancing the existing vLLM runtime and StatefulSet deployment strategy, rather than introducing alternatives like LeaderWorkerSet.

## Requirements

### Workspace API Changes

No API changes are needed for the workspace. The existing workspace API implicitly supports multi-node distributed inference, so users can continue using the same Workspace spec without any changes due to the following:

- The number of nodes is specified via `.resource.count` in the Workspace specification:

```yaml
...
resource:
  count: 2
  instanceType: "Standard_ND96asr_v4"
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

- [x] For models requiring more than one GPU, validate that `# GPUs/instance × workspace.resource.count ≥ Required # GPUs for a preset model`, and `GPU memory * # GPUs ≥ Required model memory`. This validation is performed in the API server when creating or updating a workspace. If the condition is not met, an error message will be returned to the user. This is already implemented in [`api/v1beta1/workspace_validation.go`](https://github.com/kaito-project/kaito/blob/1815428804593eaa94de0d6f78d82b53e85d0137/api/v1beta1/workspace_validation.go#L293-L380) and does not require any changes.

> [!WARNING]
> KAITO respects the user-defined `workspace.resource.count` and deploys the model across all specified nodes, even if the model could fit on fewer. Users should be aware that requesting more nodes than optimal for a model might introduce unnecessary communication overhead and potentially degrade performance, particularly without high-speed networking.

### vLLM Runtime Parameters

Flag additions to the vLLM base command is needed to support multi-node distributed inference. Per vLLM's [guidance](https://docs.vllm.ai/en/latest/serving/distributed_serving.html):

- [x] `--tensor-parallel-size`: Set to the number of GPUs per node to configure tensor parallelism. This parameter determines how the model’s tensors are split across GPUs within a single node.
- [ ] `--pipeline-parallel-size`: Set to the number of nodes to configure pipeline parallelism. This parameter determines how different model layers are sharded across nodes.

vLLM uses [Ray](https://www.ray.io/) as the default framework to manage its distributed inference runtime. vLLM has provided a convenient script called [`multi-node-serving.sh`](https://github.com/vllm-project/vllm/blob/main/examples/online_serving/multi-node-serving.sh) to start the Ray server. The same script can be used by worker pods to join the Ray cluster.

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

With a StatefulSet, the leader pod can be identified by its ordinal index (e.g., `kaito-vllm-0`), and the worker pods can be identified by their ordinal indices (e.g., `kaito-vllm-1`, `kaito-vllm-2`, etc.). The Ray cluster address can be constructed using the headless service of the StatefulSet.

```yaml
...
command:
  - /bin/sh
  - -c
  - |
    if [ "${POD_INDEX}" = "0" ]; then
      <leader command>
    else
      <worker command>
    fi
env:
  - name: POD_INDEX
    valueFrom:
      fieldRef:
        fieldPath: metadata.annotations['apps.kubernetes.io/pod-index']
...
```

### Liveness and Readiness Probes

Standard HTTP probes are insufficient for multi-node vLLM, as only the leader pod serves the `/health` endpoint at port 5000. Probes must be adapted to verify both Ray cluster integrity (all nodes) and the leader's vLLM server readiness.

- [ ] **Develop Health Check Script:** Create a script (`multi-node-health-check.sh`) that checks Ray cluster status (e.g., using `ray health-check`) and the vLLM server's `/health` endpoint regardless of the pod's role (leader or worker). This script should be placed in the container image and executed by the probes.
- [ ] **Configure Probes:** Update the StatefulSet to use this script via an `exec` probe for liveness and readiness checks.

Example probe configuration:

```yaml
livenessProbe:
  exec:
    command:
      - /bin/sh
      - -c
      - /workspace/vllm/multi-node-health-check.sh --host $(KAITO_LEADER_SERVICE_HOST) --ray_port 6379 --vllm_port 5000
  initialDelaySeconds: 60 # Allow time for distributed setup
  periodSeconds: 15
readinessProbe:
  exec:
    command:
      - /bin/sh
      - -c
      - /workspace/vllm/multi-node-health-check.sh --host $(KAITO_LEADER_SERVICE_HOST) --ray_port 6379 --vllm_port 5000
  initialDelaySeconds: 60 # Allow time for distributed setup
  periodSeconds: 15
```

### Container Image Update

The preset model container images require the following updates:

- [ ] **Include Essential Scripts:** Add the `multi-node-serving.sh` script (sourced from [vLLM examples](https://github.com/vllm-project/vllm/blob/main/examples/online_serving/multi-node-serving.sh)) and the custom `multi-node-health-check.sh` script to the Dockerfile. These scripts are necessary for initializing the Ray cluster and performing health checks in a multi-node environment.
- [ ] **Handle Large Model Weights:** For very large models (e.g., 400B+ parameters), embedding weights directly into the container image is impractical due to size constraints. Leverage existing mechanisms for runtime model weight downloading ([#982](https://github.com/kaito-project/kaito/issues/982)) or external weight caching ([#1023](https://github.com/kaito-project/kaito/issues/1023)) to manage these large artifacts effectively.
