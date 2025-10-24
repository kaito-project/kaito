# Deploy KAITO on AKS using Terraform

This is a sample of how to deploy an Open Source KAITO on a new Azure Kubernetes Service (AKS) using Terraform. This sample will deploy the following resources:

- Azure Kubernetes Service (AKS)
- Azure Container Registry (ACR) with short lived, repo scoped token
- Azure Managed Identity with Federated Credential and Role Assignment for GPU Provisioner
- Install the KAITO GPU Provisioner Helm Chart
- Install the KAITO Workspace Helm Chart
- Kubernetes Secret for the ACR token

## Prerequisites

- Terraform 1.9.7 or later
- Azure CLI 2.65.0 or later
- kubectl 1.30.5 or later
- Helm 3.16.2 or later

## Setup

To deploy this sample, you will to use the Azure CLI to login to your Azure account and set the subscription you want to use, then use the Terraform CLI to provision the Azure resources and execute the Helm installations for the KAITO operators.

Login to your Azure account and set the subscription you want to use.

```bash
az login
az account set -s <subscription-id>
```

Export the subscription ID for Terraform to use.

```bash
export ARM_SUBSCRIPTION_ID=$(az account show --query id -o tsv)
```

Initialize the Terraform providers.

```bash
terraform init
```

## Deploy

Before you deploy, review the following variables in the [variables.tf](./variables.tf) file which are available for customization:

- `location` - The Azure region to deploy the resources. Be sure you have the necessary quota in the region.
- `kaito_gpu_provisioner_version` - The version of the KAITO GPU Provisioner.
- `kaito_workspace_version` - The version of the KAITO Workspace.
- `registry_repository_name` - The name of the output image when running a sample fine-tuning job.
- `deploy_kaito_ragengine` - Whether to deploy the KAITO RAG Engine. Defaults to `true`.
- `kaito_ragengine_version` - The version of the KAITO RAG Engine.
- `features` - A list of KAITO features to enable. For example, to enable the Gateway API Inference Extension. Defaults to `["gatewayAPIInferenceExtension"]`.

Run the Terraform apply command and enter `yes` when prompted to deploy the Azure resources.

```bash
terraform apply
```

## Verify

Log into the AKS cluster.

```bash
az aks get-credentials -g $(terraform output -raw rg_name) -n $(terraform output -raw aks_name)
```

Verify installation of the KAITO operators.

```bash
helm list -n gpu-provisioner
helm list -n kaito-workspace
```

Check status of the KAITO pods.

```bash
kubectl get po -n gpu-provisioner
kubectl get po -n kaito-workspace
```

## Use

KAITO is now installed on the AKS cluster but no workspaces have been created. To use the KAITO workspaces, please refer to the YAML manifests found in the [examples](../examples/) directory or KAITO [docs](../docs/).

## Cleanup

Run the Terraform destroy command and enter `yes` when prompted to delete the Azure resources.

```bash
terraform destroy
```
