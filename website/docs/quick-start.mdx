---
title: Quick Start
---

After installing KAITO, you can quickly deploy a phi-4-mini-instruct inference service to get started.

## Prerequisites

- A Kubernetes cluster with KAITO installed (see [Installation](installation))
- `kubectl` configured to access your cluster
- **GPU nodes available in your cluster** - You have two options:
  - **Bring your own GPU (BYO) nodes**: If you already have GPU nodes in your cluster, you can use the preferred nodes approach (shown below)
  - **Auto-provisioning**: Set up automatic GPU node provisioning for your cloud provider (see [these steps](installation#option-2-auto-provision-gpu-nodes))

## Deploy Your First Model

Your model can be deployed either on your own GPU nodes, or KAITO can auto-provision nodes for it to run on.

### Option 1: Bring your own GPU nodes

Let's start by deploying a phi-4-mini-instruct model on your existing GPU nodes.

First get the nodes.

```bash
kubectl get nodes -l accelerator=nvidia
```

The output should look similar to this, showing all your GPU nodes.

```
NAME                                  STATUS   ROLES    AGE     VERSION
gpunp-26695285-vmss000000             Ready    <none>   2d21h   v1.31.9
gpunp-26695285-vmss000001             Ready    <none>   2d21h   v1.31.9
```

The GPU nodes will need a label in order for a KAITO Workspace to select it. We'll use the label `apps=llm-inference` for this example. Label the nodes you want to use. 

```bash
kubectl label node gpunp-26695285-vmss000001 apps=llm-inference
kubectl label node gpunp-26695285-vmss000000 apps=llm-inference
```

Create a YAML file named `phi-4-workspace.yaml` with the following content. The label selector here must match the labels you set on the nodes.

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  preferredNodes:
    - gpunp-26695285-vmss000000
    - gpunp-26695285-vmss000001
  labelSelector:
    matchLabels:
      apps: llm-inference
inference:
  preset:
    name: phi-4-mini-instruct
```

Apply your configuration to your cluster:

```bash
kubectl apply -f phi-4-workspace.yaml
```

### Option 2: Auto-provision GPU nodes

The following cloud providers support auto-provisioning GPU nodes in addition to BYO nodes.

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

<Tabs groupId="provider">
<TabItem value="azure" label="Azure" default>

:::info
If you have not already, follow the steps [here](azure#setup-auto-provisioning) to install the `gpu-provisioner` Helm chart.

Ensure that the chart is ready and pods are running with [these steps](azure#verify-setup) otherwise the GPU nodes will not be provisioned.
:::

Create a YAML file named `phi-4-workspace.yaml` with the following content. The `instanceType` field will specify what nodes will be auto-provisioned instead of only matching existing nodes in the BYO case.

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  instanceType: "Standard_NC6s_v3" # Will trigger node creation
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

</TabItem>
<TabItem value="aws" label="AWS" default>

:::info
If you have not already, follow the steps [here](aws#setup-auto-provisioning) to install the `Karpenter` Helm chart.

Ensure that the chart is ready and pods are running, otherwise the GPU nodes will not be provisioned.
:::

Create a YAML file named `phi-4-workspace.yaml` with the following content. The `instanceType` field will specify what nodes will be auto-provisioned instead of only matching existing nodes in the BYO case.

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  instanceType: "g5.4xlarge" # Will trigger node creation
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

</TabItem>
</Tabs>

Apply your configuration to your cluster:

```bash
kubectl apply -f phi-4-workspace.yaml
```

## Monitor Deployment

Track the workspace status to see when the model has been deployed successfully:

```bash
kubectl get workspace workspace-phi-4-mini
```

When the `WORKSPACESUCCEEDED` column becomes `True`, the model has been deployed successfully:

```bash
NAME                   INSTANCE                   RESOURCEREADY   INFERENCEREADY   JOBSTARTED   WORKSPACESUCCEEDED   AGE
workspace-phi-4-mini   Standard_NC24ads_A100_v4   True            True                          True                 4h15m
```

:::note
The `INSTANCE` column will default to `Standard_NC24ads_A100_v4` if you have not set up auto-provisioning. If you have auto-provisioning configured, it will show the specific instance type used.
:::

## Test the Model

Find the inference service's cluster IP and test it using a temporary curl pod:

```bash
# Get the service endpoint
kubectl get svc workspace-phi-4-mini
export CLUSTERIP=$(kubectl get svc workspace-phi-4-mini -o jsonpath="{.spec.clusterIPs[0]}")

# List available models
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -s http://$CLUSTERIP/v1/models | jq
```

You should see output similar to:

```json
{
  "object": "list",
  "data": [
    {
      "id": "phi-4-mini-instruct",
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
    "model": "phi-4-mini-instruct",
    "messages": [{"role": "user", "content": "What is kubernetes?"}],
    "max_tokens": 50,
    "temperature": 0
  }'
```

## Next Steps

🎉 Congratulations! You've successfully deployed and tested your first model with KAITO.

**What's Next:**

- **Explore More Models**: Check out the full range of supported models in the [presets documentation](https://github.com/kaito-project/kaito/tree/main/presets)
- **Advanced Configurations**: Learn about [workspace configurations](https://github.com/kaito-project/kaito/blob/main/api/v1alpha1/workspace_types.go)
- **Custom Models**: See how to [contribute new models](https://github.com/kaito-project/kaito/blob/main/docs/How-to-add-new-models.md)

**Additional Resources:**

- [Fine-tuning Guide](https://github.com/kaito-project/kaito/tree/main/examples/fine-tuning) - Customize models for your specific use cases
- [RAG Examples](https://github.com/kaito-project/kaito/tree/main/examples/RAG) - Retrieval-Augmented Generation patterns
- [Troubleshooting Guide](https://github.com/kaito-project/kaito/blob/main/docs/troubleshooting.md) - Common issues and solutions
