---
title: Quick Start
---

After installing KAITO, you can quickly deploy a phi-3.5-mini-instruct inference service to get started.

## Prerequisites

- A Kubernetes cluster with KAITO installed (see [Installation](installation))
- `kubectl` configured to access your cluster
- **GPU nodes available in your cluster** - You have two options:
  - **Existing GPU nodes**: If you already have GPU nodes in your cluster, you can use the preferred nodes approach (shown below)
  - **Auto-provisioning**: Set up automatic GPU node provisioning for your cloud provider ([Azure](azure) | [AWS](aws))

## Deploy Your First Model

Let's start by deploying a phi-3.5-mini-instruct model on your existing GPU nodes.

First get the nodes.

```bash
kubectl get nodes -l accelerator=nvidia
```

The output should look similar to this, showing all your GPU nodes.

```
NAME                                  STATUS   ROLES    AGE     VERSION
gpunp-26695285-vmss000000             Ready    <none>   2d21h   v1.31.9
```

Now label your GPU nodes to match the label selector. We'll use the label `apps=llm-inference` for this example.

```bash
kubectl label nodes gpunp-26695285-vmss000000 apps=llm-inference
```

Create a YAML file named `phi-3.5-workspace.yaml` with the following content:


```yaml title="phi-3.5-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-3-5-mini
resource:
  preferredNodes:
    - gpunp-26695285-vmss000000
  labelSelector:
    matchLabels:
      apps: llm-inference
inference:
  preset:
    name: phi-3.5-mini-instruct
```

Apply your configuration to your cluster:

```bash
kubectl apply -f phi-3.5-workspace.yaml
```

## Monitor Deployment

Track the workspace status to see when the model has been deployed successfully:

```bash
kubectl get workspace workspace-phi-3-5-mini
```

When the `WORKSPACEREADY` column becomes `True`, the model has been deployed successfully:

```bash
NAME                     INSTANCE                   RESOURCEREADY   INFERENCEREADY   JOBSTARTED   WORKSPACESUCCEEDED   AGE
workspace-phi-3-5-mini   Standard_NC24ads_A100_v4   True            True                          True                 4h15m
```

:::note
The `INSTANCE` column will default to `Standard_NC24ads_A100_v4` if you have not set up auto-provisioning. If you have auto-provisioning configured, it will show the specific instance type used.
:::

## Test the Model

Find the inference service's cluster IP and test it using a temporary curl pod:

```bash
# Get the service endpoint
kubectl get svc workspace-phi-3-5-mini
export CLUSTERIP=$(kubectl get svc workspace-phi-3-5-mini -o jsonpath="{.spec.clusterIPs[0]}")

# List available models
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -s http://$CLUSTERIP/v1/models | jq
```

You should see output similar to:

```json
{
  "object": "list",
  "data": [
    {
      "id": "phi-3.5-mini-instruct",
      "object": "model",
      "created": 1733370094,
      "owned_by": "vllm",
      "root": "/workspace/vllm/weights",
      "parent": null,
      "max_model_len": 16384
    }
  ]
}
```

## Make an Inference Call

Now make an inference call using the model:

```bash
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/v1/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-3.5-mini-instruct",
    "prompt": "What is kubernetes?",
    "max_tokens": 50,
    "temperature": 0
  }'
```

## Next Steps

🎉 Congratulations! You've successfully deployed and tested your first model with KAITO.

**What's Next:**

- **Explore More Models**: Check out the full range of supported models in the [presets documentation](https://github.com/kaito-project/kaito/tree/main/presets)
- **Set Up Auto-Provisioning**: Configure automatic GPU node provisioning for your cloud provider:
  - [Azure Setup](azure) - Azure GPU Provisioner
  - [AWS Setup](aws) - Karpenter
- **Advanced Configurations**: Learn about [workspace configurations](https://github.com/kaito-project/kaito/blob/main/api/v1alpha1/workspace_types.go)
- **Custom Models**: See how to [contribute new models](https://github.com/kaito-project/kaito/blob/main/docs/How-to-add-new-models.md)

**Additional Resources:**

- [Fine-tuning Guide](https://github.com/kaito-project/kaito/tree/main/examples/fine-tuning) - Customize models for your specific use cases
- [RAG Examples](https://github.com/kaito-project/kaito/tree/main/examples/RAG) - Retrieval-Augmented Generation patterns
- [Troubleshooting Guide](https://github.com/kaito-project/kaito/blob/main/docs/troubleshooting.md) - Common issues and solutions
