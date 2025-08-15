---
title: AWS Setup
---

This guide covers setting up auto-provisioning capabilities for KAITO on Amazon Elastic Kubernetes Service (EKS). Auto-provisioning allows KAITO to automatically create GPU nodes when needed for your AI workloads.

## Prerequisites

- An EKS cluster with KAITO workspace controller installed (see [Installation](installation))
- [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) for managing AWS resources
- [eksctl](https://eksctl.io/installation/) for EKS cluster management
- [kubectl](https://kubernetes.io/docs/tasks/tools/) configured to access your EKS cluster

## Understanding Auto-Provisioning on AWS

KAITO can use [Karpenter](https://karpenter.sh/) to automatically provision GPU nodes. This controller:

- Creates new GPU nodes when workspaces require specific instance types
- Supports various AWS GPU instances (g4, g5, p3, p4 series, etc.)
- Manages node lifecycle based on workload demands
- Integrates with AWS IAM for secure access

:::note
Alternative: If you already have GPU nodes or manage them separately, use the preferred nodes approach in the [Quick Start](quick-start) instead.
:::

## Set Up Auto-Provisioning

### Create EKS Cluster and install Karpenter

Follow the instructions [here](https://karpenter.sh/docs/getting-started/getting-started-with-karpenter/) to create an EKS cluster and install Karpenter.

Then update the KAITO workspace controller Helm chart values for AWS:

```bash
helm update kaito-workspace --namespace kaito-workspace --set cloudProviderName=aws
```

### Using Auto-Provisioning

Once Karpenter is set up, you can create workspaces that automatically provision GPU nodes:

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

Apply the workspace:

```bash
kubectl apply -f phi-4-workspace.yaml
```

## Supported AWS GPU Instance Types

The GPU provisioner supports various AWS GPU SKUs, see [supported options here](https://github.com/kaito-project/kaito/blob/main/pkg/sku/aws_sku_handler.go).

For the complete list and specifications, see the [AWS GPU instance documentation](https://docs.aws.amazon.com/dlami/latest/devguide/gpu.html).

## Clean Up

See the Karpenter documentation for instructions on how to clean up your EKS cluster and remove Karpenter [here](https://karpenter.sh/docs/getting-started/getting-started-with-karpenter/#9-delete-the-cluster).
