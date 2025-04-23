# Bring Your Own (BYO) GPU Nodes

This guide walks you through deploying Kaito on a Kubernetes cluster with self-provisioned GPU nodes.

## Prerequisites

If you are following the demo as is then you would need access to an Azure account.

### Tools

- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli)
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)
- [Helm](https://helm.sh/docs/intro/install/)
- [jq](https://jqlang.github.io/jq/download/)

## Set up a Kubernetes cluster with GPU nodes

> [!NOTE]
> If you already have a Kubernetes cluster, you can skip this section.

For the sake of this guide, we will create an Azure Kubernetes Service (AKS) cluster.

### Environment variables

Make necessary changes to the following environment variables and copy paste them into your terminal:

```bash
export LOCATION="southcentralus"
export RESOURCE_GROUP="kaito-rg"
export AKS_RG="${RESOURCE_GROUP}-aks"
export CLUSTER_NAME="kaito"
export AKS_WORKER_USER_NAME="azuser"
export SSH_KEY=~/.ssh/id_rsa.pub
export GPU_NODE_SIZE="Standard_NC24ads_A100_v4"
export GPU_NODE_COUNT=1
export GPU_NODE_POOL_NAME="gpunodes"
```

### Create a resource group

Run the following command to create a resource group:

```bash
az group create \
    --name "${RESOURCE_GROUP}" \
    --location "${LOCATION}"
```

### Create an Azure Kubernetes Service (AKS) cluster

Run the following command to create an AKS cluster:

```bash
az aks create \
    --resource-group "${RESOURCE_GROUP}" \
    --node-resource-group "${AKS_RG}" \
    --name "${CLUSTER_NAME}" \
    --enable-oidc-issuer \
    --enable-workload-identity \
    --enable-managed-identity \
    --node-count 1 \
    --location "${LOCATION}" \
    --ssh-key-value "${SSH_KEY}" \
    --admin-username "${AKS_WORKER_USER_NAME}" \
    --os-sku Ubuntu
```

### Add GPU nodes

Run the following commands to add or update the `aks-preview` extension:

> [!IMPORTANT]
> This is needed to enable the `--skip-gpu-driver-install` flag, you can read more about it [here](https://learn.microsoft.com/en-us/azure/aks/gpu-cluster?tabs=add-ubuntu-gpu-node-pool#skip-gpu-driver-installation-preview).

```bash
az extension add --name aks-preview
az extension update --name aks-preview
```

Run the following command to add GPU node to the AKS cluster:

```bash
az aks nodepool add \
    --name "${GPU_NODE_POOL_NAME}" \
    --resource-group "${RESOURCE_GROUP}" \
    --cluster-name "${CLUSTER_NAME}" \
    --node-count "${GPU_NODE_COUNT}" \
    --node-vm-size "${GPU_NODE_SIZE}" \
    --skip-gpu-driver-install
```

### Download kubeconfig

Run the following command to download the kubeconfig file:

```bash
az aks get-credentials \
    --resource-group "${RESOURCE_GROUP}" \
    --name "${CLUSTER_NAME}"
```

## Prepare the Kubernetes cluster for GPU workloads

### Install the NVIDIA GPU operator

> [!NOTE]
> If you have already set up your Kubernetes cluster with Nvidia's GPU operator, you can skip the GPU operator installation.

Run the following commands to create a namespace for the GPU operator:

```bash
kubectl create ns gpu-operator
kubectl label --overwrite ns gpu-operator pod-security.kubernetes.io/enforce=privileged
```

Run the following commands to install the GPU operator:

```bash
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
helm repo update

helm install \
    --wait \
    --generate-name \
    -n gpu-operator \
    --create-namespace \
    nvidia/gpu-operator
```

Ensure that the GPU operator is installed by running the following command:

```bash
kubectl -n gpu-operator wait pod \
    --for=condition=Ready \
    -l app.kubernetes.io/component=gpu-operator \
    --timeout=300s
```

Finally ensure that the `nvidia` runtime class is created by running the following command:

```bash
kubectl get runtimeclass nvidia
```

A typical output would look like this:

```bash
$ kubectl get runtimeclass nvidia
NAME     HANDLER   AGE
nvidia   nvidia    16m
```

### Label the GPU nodes

We need to label the GPU nodes `apps=gpu`, so that the Kaito workspace controller can schedule the inference workloads on these nodes. If you are following along the guide, you can run the following command to label the GPU nodes:

```bash
kubectl get nodes \
    -l agentpool="${GPU_NODE_POOL_NAME}" \
    -o name | \
    xargs -I {} \
    kubectl label --overwrite {} apps=gpu
```

> [!TIP]
> If you have used a different set up to create the GPU nodes, you can label the nodes manually by running the following command: `kubectl label node <node-name> apps=gpu`.


## Install Kaito on the Kubernetes cluster

Run the following command to install Kaito:

```bash
helm install workspace \
    ./charts/kaito/workspace \
    --namespace kaito-workspace \
    --create-namespace
```

Ensure that kaito is installed by running the following command:

```bash
kubectl -n kaito-workspace wait pod \
    --for=condition=Ready \
    -l app.kubernetes.io/instance=workspace \
    --timeout=300s
```

## Deploying a model

### Deploy a workspace with a GPU model

To deploy a workspace with a GPU model, run the following command:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-falcon-7b
resource:
  instanceType: "${GPU_NODE_SIZE}"
  labelSelector:
    matchLabels:
      apps: gpu
inference:
  preset:
    name: "falcon-7b"
EOF
```

> [!NOTE]
> In the above configuration you can see we have use a node labelSelector value as `apps: gpu`, this is the same label we have applied when we added the GPU node pool earlier.

Ensure that the workspace is ready by running the following command:

```bash
kubectl get workspace workspace-falcon-7b
```

A typical output would look like this:

```bash
$ kubectl get workspace workspace-falcon-7b
NAME                  INSTANCE                   RESOURCEREADY   INFERENCEREADY   JOBSTARTED   WORKSPACESUCCEEDED   AGE
workspace-falcon-7b   Standard_NC24ads_A100_v4   True            True                          True                 16m
```

### Use the workspace

Run the following command to find the cluster IP to send the request to:

```bash
export CLUSTERIP=$(kubectl get \
    svc workspace-falcon-7b \
    -o jsonpath="{.spec.clusterIPs[0]}")
```

Let's send a request to the workspace to get an inference response. Modify the prompt as you see fit:

```bash
export QUESTION="What's are LLMs?"
```

Run the following command to send the request:

```bash
kubectl run -it --rm \
    --restart=Never \
    curl --image=curlimages/curl \
    -- curl -X POST http://$CLUSTERIP/chat \
    -H "accept: application/json" \
    -H "Content-Type: application/json" \
    -d "{\"prompt\":\"${QUESTION}\"}" | jq
```
