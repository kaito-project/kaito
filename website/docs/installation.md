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
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --wait
```

### Using Nightly Builds (Optional)

KAITO publishes nightly controller images to the GitHub Container Registry (GHCR). These images are built daily from the `main` branch and can be used for testing the latest features before a release.

:::caution
Nightly builds are **not recommended for production use**. They are built from the latest `main` branch and may contain untested or incomplete features.
:::

Each nightly image is tagged with:

- **`nightly-latest`** — always points to the most recent successful nightly build
- **`nightly-<sha>`** — pinned to a specific commit (12-character short SHA)

To install the workspace controller using the latest nightly image:

```bash
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set image.repository=ghcr.io/kaito-project/kaito/workspace \
  --set image.tag=nightly-latest \
  --set image.pullPolicy=Always \
  --wait
```

For nightly RAG engine controller images, see the [RAG installation guide](rag#using-nightly-builds-optional).

### Verify KAITO Installation

Check that the KAITO workspace controller is running:

```bash
kubectl get pods -n kaito-workspace
kubectl describe deploy kaito-workspace -n kaito-workspace
```

You should see the workspace controller pod in a `Running` state.

## Configure Node Image Family (Optional)

KAITO supports configuring the node image family for generated `NodeClaim` resources.

:::note Version requirement
Node Image Family is supported in KAITO **v0.9.0 and later**.
:::

- Supported values: `ubuntu`, `azurelinux`
- Controller startup parameter: `--default-node-image-family`
- Helm chart value: `defaultNodeImageFamily` (mapped to `--default-node-image-family`)
- Workspace annotation: `metadata.annotations["kaito.sh/node-image-family"]`

### Controller startup parameter

You can set the controller-level default with Helm:

```bash
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set defaultNodeImageFamily=azurelinux \
  --wait
```

Notes:

- If startup parameter `--default-node-image-family` is empty, KAITO uses `ubuntu`.
- If startup parameter `--default-node-image-family` is not one of `ubuntu` or `azurelinux`, the workspace controller fails to start.

### Per-workspace override via annotation

You can override the controller default on a specific `Workspace`:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
  annotations:
    kaito.sh/node-image-family: azurelinux
```

Notes:

- `kaito.sh/node-image-family` has higher priority than `--default-node-image-family`.
- If the annotation value is unsupported, Workspace creation is rejected by validation webhook.

## Setup GPU Nodes

You need to create GPU nodes in order to run a Workspace with KAITO. There are two **mutually exclusive** options:

- **Bring your own GPU (BYO) nodes**: Create your own GPU nodes to run KAITO deployments on. When using BYO nodes, Node Auto Provisioning (NAP) must be disabled.
- **Auto-provisioning**: Set up automatic GPU node provisioning for your cloud provider. This option cannot be used with BYO nodes.

:::warning Mutual Exclusivity
BYO nodes and Auto-provisioning are **mutually exclusive**. You must choose one approach or the other, not both. When using BYO nodes, ensure that Node Auto Provisioning is disabled in the Helm installation.
:::

### Option 1: Bring your own GPU nodes

:::tip
This is recommended if you just want to try KAITO with the least amount of setup required or if you only plan to use 1-2 GPU nodes. If your cloud provider is not listed in the [Auto-provisioning section](#option-2-auto-provision-gpu-nodes), you must create your own GPU nodes. If it is, you can still manually create the nodes if you don't want to set up auto-provisioning.
:::

When using BYO nodes, you must install KAITO with Node Auto Provisioning disabled:

```bash
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set featureGates.disableNodeAutoProvisioning=true \
  --wait
```

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

- [Azure (AKS)](azure.md#setup-auto-provisioning) - Set up GPU auto-provisioning with Azure GPU Provisioner
- [AWS (EKS)](aws.md#setup-auto-provisioning) - Set up GPU auto-provisioning with Karpenter


## Next Steps

Once KAITO is installed, you can:

- Follow the [Quick Start](quick-start) guide to deploy your first model
- Try auto-provisioning with cloud provider if you have not done so
- Explore the available [model presets](presets)
