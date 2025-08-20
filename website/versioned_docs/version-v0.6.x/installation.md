---
title: Installation
---

KAITO (Kubernetes AI Toolchain Operator) can be installed on any Kubernetes cluster using Helm. This guide covers the basic installation of the KAITO workspace controller.

## Prerequisites

Before you begin, ensure you have the following tools installed:

- An existing Kubernetes cluster (can be hosted on any cloud provider) with NVIDIA GPU nodes
  - For cloud provider-specific guides, see:
    - [Azure Setup using AKS](azure)
    - [AWS Setup using EKS](aws)
- [Helm](https://helm.sh) to install the operator
- [kubectl](https://kubernetes.io/docs/tasks/tools/) to interact with your Kubernetes cluster

## Install KAITO Workspace Controller

Install the KAITO workspace controller using Helm:

```bash
export KAITO_WORKSPACE_VERSION=0.6.0
export CLUSTER_NAME=kaito

helm install kaito-workspace \
  https://github.com/kaito-project/kaito/raw/gh-pages/charts/kaito/workspace-$KAITO_WORKSPACE_VERSION.tgz \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --wait
```

### Verify KAITO Installation

Check that the KAITO workspace controller is running:

```bash
kubectl get pods -n kaito-workspace
kubectl describe deploy kaito-workspace -n kaito-workspace
```

You should see the workspace controller pod in a `Running` state.

## Setup GPU Nodes

You need to create GPU nodes in order to run a Workspace with KAITO. There are two options:

- **Bring your own GPU (BYO) nodes**: Create your own GPU nodes to run KAITO deployments on. 
- **Auto-provisioning**: Set up automatic GPU node provisioning for your cloud provider. 

### Option 1: Bring your own GPU nodes

:::tip
This is recommended if you just want to try KAITO with the least amount of setup required or if you only plan to use 1-2 GPU nodes. If your cloud provider is not listed in the [Auto-provisioning section](#option-2-auto-provision-gpu-nodes), you must create your own GPU nodes. If it is, you can still manually create the nodes if you don't want to set up auto-provisioning.
:::

Create your GPU nodes and label them for quick access. In these docs, we will use the label `accelerator=nvidia` but any label can work.

List your GPU nodes and verify that they are all present and ready.

```bash
kubectl get nodes -l accelerator=nvidia
```

The output should look similar to this. Note the names of the nodes as they will be specified when deploying a Workspace.

```
NAME                                  STATUS   ROLES    AGE     VERSION
gpunp-26695285-vmss000000             Ready    <none>   2d21h   v1.31.9
gpunp-26695285-vmss000001             Ready    <none>   2d21h   v1.31.9
```

### Option 2: Auto-provision GPU nodes

:::tip
This is recommended for production environments and any non-trivial use cases, or if you plan to use a number of nodes.
:::

The following cloud providers support auto-provisioning GPU nodes in addition to BYO nodes.

- [Azure (AKS)](azure#setup-auto-provisioning) - Set up GPU auto-provisioning with Azure GPU Provisioner
- [AWS (EKS)](aws#setup-auto-provisioning) - Set up GPU auto-provisioning with Karpenter

## Next Steps

Once KAITO is installed, you can:

- Follow the [Quick Start](quick-start) guide to deploy your first model
- Try auto-provisioning with cloud provider if you have not done so
- Explore the available [model presets](presets)
