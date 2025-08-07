---
title: Installation
---

The following guidance assumes **Azure Kubernetes Service (AKS)** is used to host the Kubernetes cluster. If you want to use Elastic Kubernetes Service (EKS) instead, please follow the installation guide [here](./aws.md).

Before you begin, ensure you have the following tools installed:

- [Azure CLI](https://learn.microsoft.com/cli/azure/install-azure-cli) to provision Azure resources
- [Helm](https://helm.sh) to install this operator
- [kubectl](https://kubernetes.io/docs/tasks/tools/) to view Kubernetes resources
- [git](https://git-scm.com/downloads) to clone this repo locally
- [yq](https://github.com/mikefarah/yq) to process yaml files
- [jq](https://jqlang.github.io/jq/download) to process JSON files

**Important Note**:
Ensure you use a release branch of the repository for a stable version of the installation.

If you do not already have an AKS cluster, run the following Azure CLI commands to create one:

```bash
export RESOURCE_GROUP="myResourceGroup"
export MY_CLUSTER="myCluster"
export LOCATION="eastus"
az group create --name $RESOURCE_GROUP --location $LOCATION
az aks create --resource-group $RESOURCE_GROUP --name $MY_CLUSTER --enable-oidc-issuer --enable-workload-identity --enable-managed-identity --generate-ssh-keys
```

Connect to the AKS cluster.

```bash
az aks get-credentials --resource-group $RESOURCE_GROUP --name $MY_CLUSTER
```

If you do not have `kubectl` installed locally, you can install using the following Azure CLI command.

```bash
az aks install-cli
```

## Install workspace controller

> Be sure you've cloned this repo and connected to your AKS cluster before attempting to install the Helm charts.

Install the Workspace controller.

```bash
export KAITO_WORKSPACE_VERSION=0.5.1

helm install kaito-workspace  --set clusterName=$MY_CLUSTER --wait \
https://github.com/kaito-project/kaito/raw/gh-pages/charts/kaito/workspace-$KAITO_WORKSPACE_VERSION.tgz --namespace kaito-workspace --create-namespace
```

Note that if you have installed another node provisioning controller that supports Karpenter-core APIs, the following steps for installing `gpu-provisioner` can be skipped.

## Install gpu-provisioner controller

#### Enable Workload Identity and OIDC Issuer features
The *gpu-provisioner* controller requires the [workload identity](https://learn.microsoft.com/azure/aks/workload-identity-overview?tabs=dotnet) feature to acquire the access token to the AKS cluster.

> Run the following commands only if your AKS cluster does not already have the Workload Identity and OIDC issuer features enabled.

```bash
export RESOURCE_GROUP="myResourceGroup"
export MY_CLUSTER="myCluster"
az aks update -g $RESOURCE_GROUP -n $MY_CLUSTER --enable-oidc-issuer --enable-workload-identity --enable-managed-identity
```

#### Create an identity and assign permissions
The identity `kaitoprovisioner` is created for the *gpu-provisioner* controller. It is assigned Contributor role for the managed cluster resource to allow changing `$MY_CLUSTER` (e.g., provisioning new nodes in it).
```bash
export SUBSCRIPTION=$(az account show --query id -o tsv)
export IDENTITY_NAME="kaitoprovisioner"
az identity create --name $IDENTITY_NAME -g $RESOURCE_GROUP
export IDENTITY_PRINCIPAL_ID=$(az identity show --name $IDENTITY_NAME -g $RESOURCE_GROUP --subscription $SUBSCRIPTION --query 'principalId' -o tsv)
az role assignment create --assignee $IDENTITY_PRINCIPAL_ID --scope /subscriptions/$SUBSCRIPTION/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.ContainerService/managedClusters/$MY_CLUSTER  --role "Contributor"
```

#### Install helm charts

**Important Note**:
For kaito 0.4.2 and above, please use gpu-provisioner 0.3.2 or higher. For versions below kaito 0.4.2, please use gpu-provisioner 0.2.1.

Install the Node provisioner controller.
```bash
# get additional values for helm chart install
export GPU_PROVISIONER_VERSION=0.3.5

curl -sO https://raw.githubusercontent.com/Azure/gpu-provisioner/main/hack/deploy/configure-helm-values.sh
chmod +x ./configure-helm-values.sh && ./configure-helm-values.sh $MY_CLUSTER $RESOURCE_GROUP $IDENTITY_NAME

helm install gpu-provisioner --values gpu-provisioner-values.yaml --set settings.azure.clusterName=$MY_CLUSTER --wait \
https://github.com/Azure/gpu-provisioner/raw/gh-pages/charts/gpu-provisioner-$GPU_PROVISIONER_VERSION.tgz --namespace gpu-provisioner --create-namespace
```

#### Create the federated credential
The federated identity credential between the managed identity `kaitoprovisioner` and the service account used by the *gpu-provisioner* controller is created.
```bash
export AKS_OIDC_ISSUER=$(az aks show -n $MY_CLUSTER -g $RESOURCE_GROUP --subscription $SUBSCRIPTION --query "oidcIssuerProfile.issuerUrl" -o tsv)
az identity federated-credential create --name kaito-federatedcredential --identity-name $IDENTITY_NAME -g $RESOURCE_GROUP --issuer $AKS_OIDC_ISSUER --subject system:serviceaccount:"gpu-provisioner:gpu-provisioner" --audience api://AzureADTokenExchange --subscription $SUBSCRIPTION
```
Then the *gpu-provisioner* can access the managed cluster using a trust token with the same permissions of the `kaitoprovisioner` identity.
Note that before finishing this step, the *gpu-provisioner* controller pod will constantly fail with the following message in the log:
```
panic: Configure azure client fails. Please ensure federatedcredential has been created for identity XXXX.
```
The pod will reach running state once the federated credential is created.

## Verify installation
You can run the following commands to verify the installation of the controllers were successful.

Check status of the Helm chart installations.

```bash
helm list -n kaito-workspace
helm list -n gpu-provisioner
```

Check status of the `workspace`.

```bash
kubectl describe deploy kaito-workspace -n kaito-workspace
```

Check status of the `gpu-provisioner`.

```bash
kubectl describe deploy gpu-provisioner -n gpu-provisioner
```

## Troubleshooting
If you see that the `gpu-provisioner` deployment is not running after some time, it's possible that some values incorrect in your `values.ovveride.yaml`.

Run the following command to check `gpu-provisioner` pod logs for additional details.

```bash
kubectl logs --selector=app.kubernetes.io\/name=gpu-provisioner -n gpu-provisioner
```

## Clean up

```bash
helm uninstall gpu-provisioner -n gpu-provisioner
helm uninstall kaito-workspace -n kaito-workspace
```
