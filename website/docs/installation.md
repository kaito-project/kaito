---
title: Installation
---

KAITO (Kubernetes AI Toolchain Operator) can be installed on any Kubernetes cluster using Helm. This guide covers the basic installation of the KAITO workspace controller.

## Prerequisites

Before you begin, ensure you have the following tools installed:

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
  --wait \
  --take-ownership
```

#### Using Nightly Builds (for testing purpose)
<details>
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
  --wait \
  --take-ownership
```
</details>

### Verify KAITO Installation

Check that the KAITO workspace controller is running:

```bash
kubectl get pods -n kaito-workspace
kubectl describe deploy kaito-workspace -n kaito-workspace
```

You should see the workspace controller pod in a `Running` state.

## Setup GPU Nodes

The inference workload created by KAITO needs to be run in GPU nodes. There are two **mutually exclusive** options to setup GPU nodes, you must choose one approach or the other:

- **Auto-provisioning**: Set up automatic GPU node provisioning. This is recommended for GPU nodes provisioned by cloud providers. 
- **Bring your own GPU (BYO) nodes**: Create and configure your own GPU nodes. This is recommended for GPUs installed in local or on-prem clusters.

### Option 1: Auto-provision GPU nodes

The following cloud providers support auto-provisioning GPU nodes.

- Azure (AKS) - [Set up GPU auto-provisioning with Azure GPU Provisioner or Azure Karpenter](azure.md#setup-auto-provisioning).
- AWS (EKS) - [Set up GPU auto-provisioning with AWS Karpenter](aws.md#setup-auto-provisioning).

KAITO controller will create new GPU nodes in the cluster when there are insufficient GPUs to run the workspace.

:::tip
You can still use this option when you plan to use existing GPU nodes in the cluster as long as:
- The instance type of the node is known by KAITO (i.e., listed in [here](https://github.com/kaito-project/kaito/blob/main/pkg/sku/azure_sku_handler.go) or [here](https://github.com/kaito-project/kaito/blob/main/pkg/sku/aws_sku_handler.go)). 
- Follow the [instruction](faq#how-do-i-use-existing-gpus-in-the-cluster-for-my-inference-workload) to add proper labels and make sure the instance type is consistent between the node and the workspace.

When KATIO controller ensures existing GPUs are sufficient to run the workspace, no extra GPU nodes will be created.
:::

### Option 2: Bring your own GPU nodes

When using this option, you must install/update KAITO with Node Auto Provisioning feature disabled:

```bash
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set featureGates.disableNodeAutoProvisioning=true \
  --wait \
  --take-ownership
```
Follow this [instruction](faq#how-do-i-use-existing-gpus-in-the-cluster-for-my-inference-workload) to make sure the
BYO nodes are correctly labeled manually. 

:::note
For BYO nodes, KATIO controller relies on Node Feature Discovery and GPU Feature Discovery daemonsets to populate proper node labels for the GPU hardware. These two daemonsets are not needed for instance types that KAITO know since KATIO controller is able to extract the GPU topology and hardware specification from the instanc type. If KAITO does not know the instance type, even though the node is provisoned by cloud vendor, BYO option has to be chosen. 
:::


## Next Steps

Once KAITO is installed, you can:

- Follow the [Quick Start](quick-start) guide to deploy your first model
