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

## Understanding Auto-Provisioning on Azure

KAITO can use the [Azure GPU Provisioner](https://github.com/Azure/gpu-provisioner) to automatically provision GPU nodes. This controller:

- Creates new GPU nodes when workspaces require specific instance types
- Supports various Azure GPU SKUs (Standard_NC series, etc.)
- Manages node lifecycle based on workload demands
- Integrates with Azure's managed identity system for secure access

### When to Use Auto-Provisioning

Choose auto-provisioning when:
- You want KAITO to manage GPU node creation automatically
- Your workloads have varying GPU requirements
- You prefer to specify exact Azure instance types in your workspaces

:::note
Alternative: If you already have GPU nodes or manage them separately, use the [preferred nodes approach](quick-start#option-1-using-preferred-nodes-existing-gpu-nodes) instead.
:::

## Set Up Auto-Provisioning

### Step 1: Create and configure an AKS Cluster

If you don't have an AKS cluster yet, you can create one using the Azure CLI:

```bash
export RESOURCE_GROUP="kaito-rg"
export CLUSTER_NAME="kaito-cluster"
export LOCATION="eastus"
az group create --name $RESOURCE_GROUP --location $LOCATION
az aks create --resource-group $RESOURCE_GROUP --name $CLUSTER_NAME --enable-oidc-issuer --enable-workload-identity --enable-managed-identity --generate-ssh-keys
```

Connect to the cluster:
```bash
az aks get-credentials --resource-group $RESOURCE_GROUP --name $CLUSTER_NAME
```

### Step 2: Create Managed Identity

Create a managed identity for the GPU provisioner with the necessary permissions:

```bash
export SUBSCRIPTION=$(az account show --query id -o tsv)
export IDENTITY_NAME="kaitoprovisioner"

# Create the managed identity
az identity create --name $IDENTITY_NAME -g $RESOURCE_GROUP

# Get the principal ID for role assignment
export IDENTITY_PRINCIPAL_ID=$(az identity show --name $IDENTITY_NAME -g $RESOURCE_GROUP --subscription $SUBSCRIPTION --query 'principalId' -o tsv)

# Assign Contributor role to the cluster
az role assignment create \
  --assignee $IDENTITY_PRINCIPAL_ID \
  --scope /subscriptions/$SUBSCRIPTION/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.ContainerService/managedClusters/$CLUSTER_NAME \
  --role "Contributor"
```

### Step 3: Install GPU Provisioner

Install the Azure GPU Provisioner using Helm:

```bash
export GPU_PROVISIONER_VERSION=0.3.5

# Download and configure Helm values
curl -sO https://raw.githubusercontent.com/Azure/gpu-provisioner/main/hack/deploy/configure-helm-values.sh
chmod +x ./configure-helm-values.sh && ./configure-helm-values.sh $CLUSTER_NAME $RESOURCE_GROUP $IDENTITY_NAME

# Install GPU provisioner
helm install gpu-provisioner \
  --values gpu-provisioner-values.yaml \
  --set settings.azure.clusterName=$CLUSTER_NAME \
  --wait \
  https://github.com/Azure/gpu-provisioner/raw/gh-pages/charts/gpu-provisioner-$GPU_PROVISIONER_VERSION.tgz \
  --namespace gpu-provisioner \
  --create-namespace
```

### Step 4: Create Federated Credential

Create the federated identity credential to allow the GPU provisioner to access Azure resources:

```bash
export AKS_OIDC_ISSUER=$(az aks show -n $CLUSTER_NAME -g $RESOURCE_GROUP --subscription $SUBSCRIPTION --query "oidcIssuerProfile.issuerUrl" -o tsv)

az identity federated-credential create \
  --name kaito-federatedcredential \
  --identity-name $IDENTITY_NAME \
  -g $RESOURCE_GROUP \
  --issuer $AKS_OIDC_ISSUER \
  --subject system:serviceaccount:"gpu-provisioner:gpu-provisioner" \
  --audience api://AzureADTokenExchange \
  --subscription $SUBSCRIPTION
```

## Verify Setup

Check that the GPU provisioner is running correctly:

```bash
# Check Helm installations
helm list -n gpu-provisioner
helm list -n kaito-workspace

# Check GPU provisioner status
kubectl describe deploy gpu-provisioner -n gpu-provisioner
kubectl get pods -n gpu-provisioner
```

The GPU provisioner pod should be in a `Running` state. If it's failing, check the logs:

```bash
kubectl logs --selector=app.kubernetes.io/name=gpu-provisioner -n gpu-provisioner
```

## Using Auto-Provisioning

Once set up, you can create workspaces that automatically provision GPU nodes:

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  instanceType: "Standard_NC6s_v3"  # Will trigger node creation
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

For the complete list and specifications, see the [Azure GPU-optimized VM sizes documentation](https://learn.microsoft.com/en-us/azure/virtual-machines/sizes-gpu).

## Clean Up

To remove the auto-provisioning setup:

```bash
# Uninstall GPU provisioner
helm uninstall gpu-provisioner -n gpu-provisioner

# Delete the managed identity (optional)
az identity delete --name $IDENTITY_NAME -g $RESOURCE_GROUP
```
