---
title: Quick Start
---

After installing KAITO, you can quickly deploy a phi-3.5-mini-instruct inference service to get started.

## Prerequisites

- A Kubernetes cluster with KAITO installed (see [Installation](installation))
- `kubectl` configured to access your cluster

## Deploy Your First Model

Let's start by deploying a phi-3.5-mini-instruct model using a workspace configuration:

```yaml title="phi-3.5-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-3-5-mini
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: phi-3-5
inference:
  preset:
    name: phi-3.5-mini-instruct
```

Apply this configuration to your cluster:

```bash
kubectl apply -f phi-3.5-workspace.yaml
```

## Monitor Deployment

Track the workspace status to see when the model has been deployed successfully:

```bash
kubectl get workspace workspace-phi-3-5-mini
```

When the `WORKSPACESUCCEEDED` column becomes `True`, the model has been deployed successfully:

```bash
NAME                     INSTANCE                   RESOURCEREADY   INFERENCEREADY   JOBSTARTED   WORKSPACESUCCEEDED   AGE
workspace-phi-3-5-mini   Standard_NC24ads_A100_v4   True            True                          True                 4h15m
```

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
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-3.5-mini-instruct",
    "messages": [{"role": "user", "content": "What is kubernetes?"}],
    "max_tokens": 50,
    "temperature": 0
  }'
```

## Next Steps

🎉 Congratulations! You've successfully deployed and tested your first model with KAITO.

- **Learn More**: Explore the full range of supported models in the [presets documentation](https://github.com/kaito-project/kaito/tree/main/presets)
- **Advanced Usage**: Learn about [workspace configurations](https://github.com/kaito-project/kaito/blob/main/api/v1alpha1/workspace_types.go)
- **Contributing**: See how to [contribute new models](https://github.com/kaito-project/kaito/blob/main/docs/How-to-add-new-models.md)
