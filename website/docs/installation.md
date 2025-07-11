---
title: Installation
---

KAITO (Kubernetes AI Toolchain Operator) can be installed on any Kubernetes cluster using Helm. This guide covers the basic installation of the KAITO workspace controller.

## Prerequisites

Before you begin, ensure you have the following tools installed:

- An existing Kubernetes cluster (can be hosted on any cloud provider) with NVIDIA GPU nodes
  - For cloud provider-specific guides, see:
    - [Azure AKS Setup](azure)
    - [AWS EKS Setup](aws)
- [Helm](https://helm.sh) to install the operator
- [kubectl](https://kubernetes.io/docs/tasks/tools/) to interact with your Kubernetes cluster

## Install KAITO Workspace Controller

Install the KAITO workspace controller using Helm:

```bash
export KAITO_WORKSPACE_VERSION=0.5.1
export CLUSTER_NAME=kaito

helm install kaito-workspace \
  https://github.com/kaito-project/kaito/raw/gh-pages/charts/kaito/workspace-$KAITO_WORKSPACE_VERSION.tgz \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --wait
```

### Verify Installation

Check that the KAITO workspace controller is running:

```bash
kubectl get pods -n kaito-workspace
kubectl describe deploy kaito-workspace -n kaito-workspace
```

You should see the workspace controller pod in a `Running` state.

## Cloud Provider Setup

For auto-provisioning capabilities (automatic GPU node provisioning), refer to the cloud provider-specific documentation:

- [Azure (AKS)](azure) - Set up GPU auto-provisioning with Azure GPU Provisioner
- [AWS (EKS)](aws) - Set up GPU auto-provisioning with Karpenter

:::note
Auto-provisioning is optional. You can use KAITO with existing GPU nodes by following the preferred nodes approach in the [Quick Start](quick-start) guide.
:::

## Next Steps

Once KAITO is installed, you can:

- Follow the [Quick Start](quick-start) guide to deploy your first model
- Set up auto-provisioning for your cloud provider (see [Cloud Provider Setup](#cloud-provider-setup))
- Explore the available [model presets](presets)
