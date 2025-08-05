---
title: Multi-Node Inference
---

This document explains how to configure and use multi-node distributed inference in KAITO for large models that require more GPU resources than a single node can provide.

## Overview

Multi-node inference allows you to deploy large AI models across multiple nodes (servers) when the model is too large to fit on a single node. KAITO supports different parallelism strategies depending on your model's requirements:

| Strategy | Use Case | Supported |
|----------|----------|-----------|
| **Single GPU** | Small models that fit on one GPU | ✅ |
| **Single-Node Multi-GPU** | Models that need multiple GPUs but fit on one node | ✅ |
| **Multi-Node Multi-GPU** | Very large models (400B+ parameters) requiring multiple nodes | ✅ |

## When to Use Multi-Node Inference

Consider multi-node inference when:

- Your model has 400B+ parameters and cannot fit on a single node
- You need to serve models like very large language models that exceed single-node memory capacity
- You have specific performance requirements that benefit from distributed processing

:::note
Multi-node inference introduces additional complexity and network overhead. Only use it when your model truly requires more resources than a single node can provide.
:::

## Supported Models

The following preset models support multi-node distributed inference:

- **Llama3**: `llama-3.3-70b-instruct`
- **DeepSeek**: `deepseek-r1-0528`, `deepseek-v3-0324` 

Check the [presets documentation](./presets.md) for the complete list and their specific requirements.

## Configuration

### Basic Multi-Node Setup

To deploy a model across multiple nodes, specify the node count in your Workspace configuration:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-large-model
resource:
  count: 2                    # Number of nodes to use
  instanceType: "Standard_NC80adis_H100_v5"
  labelSelector:
    matchLabels:
      apps: large-model
inference:
  preset:
    name: "llama-3.3-70b-instruct"
```

### Pre-Provisioned Nodes

If you're using pre-provisioned GPU nodes, specify them explicitly:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-large-model
resource:
  count: 2
  preferredNodes:
    - gpu-node-1
    - gpu-node-2
  labelSelector:
    matchLabels:
      apps: large-model
inference:
  preset:
    name: "llama-3.3-70b-instruct"
```

:::warning
Pre-provisioned nodes must have the same matching labels as specified in the `resource` spec, and each node must report available GPU resources.
:::

### Custom vLLM Parameters for Multi-Node

You can customize vLLM runtime parameters for distributed inference using a ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: distributed-inference-config
data:
  inference_config.yaml: |
    vllm:
      gpu-memory-utilization: 0.95
      max-model-len: 131072
---
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-large-model
resource:
  count: 2
  instanceType: "Standard_NC80adis_H100_v5"
  labelSelector:
    matchLabels:
      apps: large-model
inference:
  preset:
    name: "llama-3.3-70b-instruct"
  config: "distributed-inference-config"
```

Key parameters for multi-node inference:
- `tensor-parallel-size`: Automatically set by KAITO based on the number of GPUs per node
- `pipeline-parallel-size`: Automatically set by KAITO based on the number of nodes
- `gpu-memory-utilization`: Fraction of GPU memory to use (0.0-1.0) - user configurable
- `max-model-len`: Maximum sequence length - user configurable

:::note
The `tensor-parallel-size` and `pipeline-parallel-size` parameters are automatically managed by KAITO based on your cluster configuration and do not need to be specified in the ConfigMap.
:::

## Architecture

### Single-Node Multi-GPU

When using multiple GPUs on a single node, KAITO uses **tensor parallelism** to split the model across GPUs within that node:

```
┌─────────────────────────────────────┐
│             Node 1                  │
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐    │
│  │GPU 1│ │GPU 2│ │GPU 3│ │GPU 4│    │
│  └─────┘ └─────┘ └─────┘ └─────┘    │
│           Model Split               │
└─────────────────────────────────────┘
```

### Multi-Node Multi-GPU

For multi-node deployments, KAITO combines **pipeline parallelism** between nodes and **tensor parallelism** within each node:

```
┌─────────────────────┐    ┌─────────────────────┐
│       Node 1        │    │       Node 2        │
│  ┌─────┐┌─────┐     │    │  ┌─────┐ ┌─────┐    │
│  │GPU 1││GPU 2│     │◄──►│  │GPU 3│ │GPU 4│    │
│  └─────┘└─────┘     │    │  └─────┘ └─────┘    │
│   Layers 1-N/2      │    │   Layers N/2+1-N    │
└─────────────────────┘    └─────────────────────┘
```

## Resource Validation

KAITO automatically validates that your configuration provides sufficient resources:

- **GPU Count**: `(GPUs per instance) × (workspace.resource.count) ≥ (Required GPUs for model)`
- **Memory**: `(GPU memory) × (Total GPUs) ≥ (Required model memory)`

If validation fails, you'll receive an error message when creating or updating the workspace.

:::tip Resource Optimization
KAITO may use fewer nodes than specified in `workspace.resource.count` if the model can fit efficiently on fewer nodes. This optimizes GPU utilization and reduces network overhead, but be mindful of the costs when provisioning many nodes.
:::

## Service Architecture

Multi-node inference uses Kubernetes StatefulSets to ensure stable pod identity and coordination:

- **Leader Pod** (index 0): Coordinates the distributed inference and serves the API
- **Worker Pods** (index 1+): Join the Ray cluster and participate in model serving

The service endpoint points to the leader pod, which handles all incoming requests and coordinates with worker pods.

:::note Future Enhancement
KAITO will support [LeaderWorkerSet](https://lws.sigs.k8s.io/docs/overview/) in the future to provide better management of leader-worker topologies and improved fault tolerance for multi-node deployments.
:::

## Health Monitoring

Multi-node deployments use specialized health checks:

- **Liveness Probes**: Monitor Ray cluster health and detect dead actors
- **Readiness Probes**: Check service availability via the leader pod's `/health` endpoint

If worker pods fail, the leader will restart to reinitialize the entire cluster, ensuring all pods are synchronized.

## Best Practices

1. **Resource Planning**: Carefully plan your GPU and memory requirements before deployment
2. **Network Bandwidth**: Ensure sufficient network bandwidth between nodes for optimal performance
3. **Monitoring**: Monitor both individual node health and overall cluster performance
4. **Cost Management**: Be aware that multi-node deployments can be expensive; only use when necessary

## Troubleshooting

### Service Unavailable After Deployment

If the service becomes unavailable:

1. Check if all pods are running: `kubectl get pods -l app=<your-app-label>`
2. Verify Ray cluster health in leader pod logs
3. Ensure network connectivity between nodes
4. Check resource allocation and GPU availability

### Worker Pod Failures

Worker pod failures will trigger leader pod restart to reinitialize the cluster:

1. Monitor pod restart events
2. Check for resource constraints (memory, GPU)
3. Verify node-to-node network connectivity
4. Review pod logs for Ray cluster connection issues

### Performance Issues

If you experience poor performance:

1. Monitor network latency between nodes
2. Check GPU utilization across all nodes
3. Review memory usage and potential bottlenecks
4. Consider adjusting parallelism parameters

## API Usage

Multi-node inference services expose the same API as single-node deployments. The vLLM runtime provides OpenAI-compatible endpoints:

```bash
# Get the cluster IP of your service
kubectl get services

# Check service health
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- \
  curl http://<CLUSTER-IP>:80/health

# Generate text using chat completions API
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- \
  curl -X POST http://<CLUSTER-IP>:80/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama-3.3-70b-instruct",
    "messages": [{"role": "user", "content": "Your prompt here"}],
    "max_tokens": 100
  }'
```

For detailed API specifications, see the [inference documentation](./inference.md#inference-api).

## Limitations

- **Custom Models**: Multi-node inference is currently only supported for preset models
- **Fault Tolerance**: The system requires leader restart when worker pods fail
- **Network Dependency**: Performance heavily depends on inter-node network quality
- **Complexity**: Debugging and monitoring are more complex than single-node deployments

## Related Documentation

- [Inference](./inference.md) - General inference documentation
- [Presets](./presets.md) - Supported models and their requirements
- [Custom Model](./custom-model.md) - Using custom models (single-node only)