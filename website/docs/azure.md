---
title: Azure Setup
---

This guide covers setting up auto-provisioning capabilities for KAITO on Azure Kubernetes Service (AKS). Auto-provisioning allows KAITO to automatically create GPU nodes when needed for your AI workloads.

## Prerequisites

- An AKS cluster with KAITO workspace controller installed
  - See [Step 1](#step-1-create-and-configure-an-aks-cluster) to create an AKS cluster
  - See [Installation](installation) to install the KAITO workspace controller
- [Azure CLI](https://learn.microsoft.com/cli/azure/install-azure-cli) for managing Azure resources
- [kubectl](https://kubernetes.io/docs/tasks/tools/) configured to access your AKS cluster

Log in to Azure and select the subscription you want to use:

```bash
az login
az account set --subscription "<your-subscription-id>"
```

## Understanding Auto-Provisioning on Azure

KAITO uses [Azure Karpenter (karpenter-provider-azure)](https://github.com/Azure/karpenter-provider-azure) to automatically provision GPU nodes. This controller:

- Creates new GPU nodes when workspaces require specific instance types
- Supports various Azure GPU SKUs (Standard_NC series, etc.)
- Manages node lifecycle based on workload demands
- Integrates with Azure's managed identity system for secure access

:::warning Deprecation notice
The [Azure GPU Provisioner](https://github.com/Azure/gpu-provisioner) (`gpu-provisioner`) is **deprecated** and no longer recommended for new clusters. Use Azure Karpenter as described below. Existing `gpu-provisioner` installations continue to work, but new setups should adopt Karpenter (`nodeProvisioner=karpenter`).
:::

### When to Use Auto-Provisioning

Choose auto-provisioning when:
- You want KAITO to manage GPU node creation automatically
- Your workloads have varying GPU requirements
- You prefer to specify exact Azure instance types in your workspaces

:::note
Alternative: If you already have GPU nodes or manage them separately, use the [bring your own GPU nodes approach](./installation.md#option-2-bring-your-own-gpu-nodes) instead.
:::

## Set Up Auto-Provisioning

Setting up auto-provisioning has two parts:

1. **Create the AKS cluster and install Azure Karpenter** — this is standard self-hosted Karpenter setup. Follow the upstream [self-hosted Karpenter installation](https://github.com/Azure/karpenter-provider-azure#installation-self-hosted-karpenter) guide so you always track the latest supported procedure.
2. **Apply the KAITO-specific configuration** — point the KAITO workspace controller at Karpenter (Step 2 below).

:::tip Prefer `make` targets?
If you'd rather not run each command by hand, the KAITO repository ships `make` targets that automate Step 1. See [Alternative: set up with KAITO make targets](#alternative-set-up-with-kaito-make-targets).
:::

### Step 1: Create the AKS cluster and install Azure Karpenter

Follow the [self-hosted Karpenter installation](https://github.com/Azure/karpenter-provider-azure#installation-self-hosted-karpenter) guide to:

- create an AKS cluster with the OIDC issuer and workload identity enabled,
- create the Karpenter workload identity, federated credential, and role assignments, and
- install the Azure Karpenter controller with its Helm chart.

Refer to that guide for prerequisites and the full configuration details.

### Step 2: Configure the KAITO Workspace Controller to Use Karpenter

The KAITO workspace controller must run with `nodeProvisioner=karpenter`. Update your [existing installation](./installation.md) with `helm upgrade`, reusing the same `kaito/workspace` chart from the Helm repository you added during [Installation](./installation.md):

```bash
helm upgrade kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --reuse-values \
  --set nodeProvisioner=karpenter
```

Setting `nodeProvisioner=karpenter` does two things: it renders the `kaito-nodeclasses` ConfigMap (the built-in `AKSNodeClass` definitions), and it starts the controller with `--node-provisioner=karpenter`. The upgrade re-renders the ConfigMap and triggers a controller rollout; on startup the new pod reads that ConfigMap and creates the `AKSNodeClass` resources, so the node classes only appear after this step.

### Alternative: set up with KAITO make targets

Instead of running the Step 1 commands by hand, you can use the `make` targets shipped in the KAITO repository. They wrap the same upstream Azure Karpenter installation, so the result matches the official guide. Choose this path only if you're comfortable cloning the repo, and run all commands from the root of the KAITO repository.

Clone the repository and change into it:

```bash
git clone https://github.com/kaito-project/kaito.git
cd kaito
```

Set the shared variables used by the `make` targets:

```bash
export AZURE_RESOURCE_GROUP="kaito-rg"
export AZURE_CLUSTER_NAME="kaito-cluster"
export AZURE_LOCATION="eastus"
export KARPENTER_NAMESPACE=karpenter
export AZURE_SUBSCRIPTION_ID=$(az account show --query id -o tsv)
export TEST_SUITE=azkarpenter               # selects the Karpenter identities/roles
```

:::note
`make azure-karpenter-helm` requires `yq` v4.30+.
:::

Create the cluster, identities, and install Azure Karpenter:

```bash
# Create the resource group and an AKS cluster configured for Karpenter
# (Azure CNI Overlay + Cilium, OIDC issuer, and workload identity)
make create-rg
make create-aks-cluster-for-karpenter

# Create the Karpenter workload identity, federated credential, and role assignments
make generate-identities

# Install the Azure Karpenter controller
make azure-karpenter-helm
```

Then continue with [Step 2: Configure the KAITO Workspace Controller to Use Karpenter](#step-2-configure-the-kaito-workspace-controller-to-use-karpenter) above.

## Verify Setup

Check that Karpenter and the KAITO workspace controller are running correctly:

```bash
# Check Helm installations
helm list -n karpenter
helm list -n kaito-workspace

# Check Karpenter status
kubectl describe deploy karpenter -n karpenter
kubectl get pods -n karpenter
```

The Karpenter pod should be in a `Running` state. If it's failing, check the logs:

```bash
kubectl logs --selector=app.kubernetes.io/name=karpenter -n karpenter
```

## Using Auto-Provisioning

Once set up, you can create workspaces that automatically provision GPU nodes:

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  instanceType: "Standard_NC4as_T4_v3"  # Will trigger node creation
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

Then apply the workspace:

```bash
kubectl apply -f phi-4-workspace.yaml
```

## Supported Azure GPU Instance Types

The GPU provisioner supports various Azure GPU SKUs, see [supported options here](https://github.com/kaito-project/kaito/blob/main/pkg/sku/azure_sku_handler.go).

:::note
KAITO only supports NVIDIA GPUs with CUDA compute capability **>= 8.0** (Ampere and newer, such as A10, A100, H100, H200). Older architectures such as **NVIDIA T4** (Turing, 7.5), **NVIDIA V100** (Volta, 7.0), and **NVIDIA M60** (Maxwell, 5.2) are **not supported**. This means SKUs such as `Standard_NC*_T4_v3`, `Standard_NC*s_v3` (V100), and `Standard_NV*s_v3` (M60) cannot be used with KAITO.
:::

For the complete list and specifications, see the [Azure GPU-optimized VM sizes documentation](https://learn.microsoft.com/en-us/azure/virtual-machines/sizes-gpu).

## Clean Up

To remove the auto-provisioning setup:

```bash
# Uninstall Karpenter
helm uninstall karpenter -n karpenter

# Delete the managed identity (optional)
# The Karpenter workload identity created by `make generate-identities` is named azkarpenterIdentity
az identity delete --name azkarpenterIdentity -g $AZURE_RESOURCE_GROUP
```

## Select a Node Class (Optional)

When using Azure Karpenter, KAITO ships **two built-in `AKSNodeClass` definitions** and lets you choose which one a `Workspace` uses through an annotation.

These node classes are defined in the KAITO workspace Helm chart under `karpenterProviders.azure.nodeClasses`, rendered into the `kaito-nodeclasses` ConfigMap, and created by the controller at startup:

| NodeClass name | Image family | OS disk size | Default |
| --- | --- | --- | --- |
| `image-family-ubuntu` | `Ubuntu2204` | 300 GB | ✅ yes |
| `image-family-azure-linux` | `AzureLinux` | 300 GB | no |

The default node class is the entry labeled `karpenter.kaito.sh/default: "true"` (i.e. `image-family-ubuntu`). Any `Workspace` that does not select a node class uses it.

### Select a node class per Workspace

Set the `kaito.sh/node-class-name` annotation on a `Workspace` to the name of the built-in node class you want:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
  annotations:
    kaito.sh/node-class-name: image-family-azure-linux
resource:
  instanceType: "Standard_NC4as_T4_v3"
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

Notes:

- If the annotation is absent or empty, KAITO uses the default node class (`image-family-ubuntu`).
- The annotation value must match the name of an `AKSNodeClass` that exists in the cluster — one of the two built-in names above, or a custom node class you add.

### Add or customize node classes (advanced)

To add your own image families or disk sizes, edit `karpenterProviders.azure.nodeClasses` in the workspace Helm values. Each entry becomes an `AKSNodeClass`, and **exactly one** entry must be marked `default: true`:

```yaml
karpenterProviders:
  azure:
    nodeClasses:
      - name: image-family-ubuntu
        default: true
        spec:
          imageFamily: Ubuntu2204
          osDiskSizeGB: 300
      - name: image-family-azure-linux
        spec:
          imageFamily: AzureLinux
          osDiskSizeGB: 300
```

:::note
The legacy `--default-node-image-family` controller flag and the `kaito.sh/node-image-family` annotation apply only to the deprecated `gpu-provisioner` path and are not used by Azure Karpenter.
:::
