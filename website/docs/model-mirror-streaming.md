---
title: Model Mirror and Streaming
---

This document explains how to enable and use **Model Mirror** and **Model Streaming** in KAITO to accelerate inference cold starts by serving model weights directly from cloud blob storage.

## Overview

By default, a KAITO inference workload downloads the model weights from HuggingFace (or pulls a pre-baked image) when the inference pod starts. For large models this download happens *after* the GPU node is ready, so the GPU sits idle while weights download, and every workspace re-downloads the same model independently.

Model Mirror and Streaming change this with two cooperating pieces:

| Component | What it does |
|-----------|--------------|
| **Model Mirror** | Downloads a model's weights **once** into a blob-backed `PersistentVolumeClaim` and tracks it with a cluster-scoped `ModelMirror` custom resource. The download runs **in parallel** with GPU node provisioning, and the result is shared by every workspace that uses the same model. |
| **Model Streaming** | Makes the inference pod **stream** the weights from blob storage at startup (using the [Run:ai Model Streamer](https://github.com/run-ai/runai-model-streamer)) instead of downloading them locally. |

Streaming builds on top of mirroring: when streaming is enabled, KAITO automatically creates the `ModelMirror` resource for the model, waits for the download to finish, then points the inference pod at the blob path.

## When to Use

Consider enabling Model Streaming when:

- You serve large models and want to reduce **cold-start time** — GPU provisioning and model download overlap instead of running sequentially.
- Multiple workspaces use the **same model** — the weights are downloaded once and shared, rather than re-downloaded per workspace.
- You frequently scale inference replicas up and down and want faster pod startup.

## Requirements and Limitations

:::warning
Model Streaming has the following requirements:

- **vLLM runtime only.** Streaming is not supported for the Transformers runtime. Workspaces that use the Transformers runtime silently fall back to the normal download path.
- **Azure (AKS).** The current implementation streams from Azure Blob Storage using the Azure Blob CSI driver and [Azure Workload Identity](https://azure.github.io/azure-workload-identity/docs/).
- **Workload Identity per namespace.** Each namespace that runs a streaming workspace needs its own annotated `ServiceAccount` and federated identity credential (see [Environment Setup](#environment-setup)).
:::

## Environment Setup

The following steps prepare an AKS cluster for model streaming. They enable the Azure Blob CSI driver, create a tuned `StorageClass`, and configure Workload Identity so inference pods can read from blob storage.

:::note
Your AKS cluster must be created with `--enable-oidc-issuer --enable-workload-identity`. See the [Azure Setup](./azure.md) guide if you need to create or update the cluster.
:::

Set the variables used throughout this guide. If you followed the [Azure Setup](./azure.md) guide, you can reuse the same `RESOURCE_GROUP`, `CLUSTER_NAME`, and `SUBSCRIPTION` values:

```bash
export RESOURCE_GROUP=<your-resource-group>
export CLUSTER_NAME=<your-aks-cluster-name>
export SUBSCRIPTION=$(az account show --query id -o tsv)

# The namespace where you will create streaming workspaces.
export WS_NS=default

# Names used by KAITO (defaults — keep these unless you have a reason to change them).
export SA_NAME=kaito-model-streamer
export SC_NAME=blob-fuse
```

### Step 1: Enable the Blob CSI driver

```bash
az aks update --enable-blob-driver -n "$CLUSTER_NAME" -g "$RESOURCE_GROUP" --yes
```

### Step 2: Create the StorageClass

This `StorageClass` uses BlobFuse (`protocol: fuse2`) with block-cache tuning for fast streaming reads. The CSI driver automatically creates a storage account in the cluster's managed (`MC_`) resource group when the first volume binds.

```bash
cat <<EOF | kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${SC_NAME}
provisioner: blob.csi.azure.com
allowVolumeExpansion: true
parameters:
  protocol: fuse2
  skuName: Premium_LRS
reclaimPolicy: Retain
mountOptions:
  - -o allow_other
  - -o attr_timeout=120
  - -o entry_timeout=120
  - -o negative_timeout=120
  - --cancel-list-on-mount-seconds=10
  - --use-attr-cache=true
  - --block-cache
  - --block-cache-block-size=16
  - --block-cache-pool-size=8192
  - --block-cache-prefetch=11
EOF
```

### Step 3: Configure Workload Identity

Inference pods authenticate to blob storage using Azure Workload Identity through the cluster's **kubelet identity**. Resolve the kubelet identity and the cluster's OIDC issuer:

```bash
KUBELET_RESOURCE_ID=$(az aks show -n "$CLUSTER_NAME" -g "$RESOURCE_GROUP" --query "identityProfile.kubeletidentity.resourceId" -o tsv)
KUBELET_CLIENT_ID=$(az aks show -n "$CLUSTER_NAME" -g "$RESOURCE_GROUP" --query "identityProfile.kubeletidentity.clientId" -o tsv)
KUBELET_PRINCIPAL_ID=$(az identity show --ids "$KUBELET_RESOURCE_ID" --query principalId -o tsv)
KUBELET_IDENTITY_NAME=$(az identity show --ids "$KUBELET_RESOURCE_ID" --query name -o tsv)
KUBELET_IDENTITY_RG=$(az identity show --ids "$KUBELET_RESOURCE_ID" --query resourceGroup -o tsv)

OIDC_ISSUER=$(az aks show -n "$CLUSTER_NAME" -g "$RESOURCE_GROUP" --query "oidcIssuerProfile.issuerUrl" -o tsv)
```

Create a federated identity credential whose subject is the streaming ServiceAccount in your namespace:

```bash
az identity federated-credential create \
  --name "streaming-wi-${CLUSTER_NAME}-${WS_NS}" \
  --identity-name "$KUBELET_IDENTITY_NAME" \
  --resource-group "$KUBELET_IDENTITY_RG" \
  --issuer "$OIDC_ISSUER" \
  --subject "system:serviceaccount:${WS_NS}:${SA_NAME}" \
  --audiences "api://AzureADTokenExchange"
```

Create the ServiceAccount and annotate it with the kubelet identity's client ID:

```bash
kubectl create namespace "$WS_NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl create serviceaccount "$SA_NAME" -n "$WS_NS"
kubectl annotate serviceaccount "$SA_NAME" -n "$WS_NS" \
  "azure.workload.identity/client-id=$KUBELET_CLIENT_ID" --overwrite
```

:::warning Per-namespace setup
The federated identity credential and the annotated ServiceAccount are **namespace-specific**. If you run streaming workspaces in more than one namespace, repeat **Step 3** for each namespace (use a unique `--name` for each federated credential, e.g. `streaming-wi-${CLUSTER_NAME}-<namespace>`).
:::

### Step 4: Grant blob read access

The Blob CSI driver creates a storage account in the cluster's managed (`MC_`) resource group when the first volume binds. Grant the kubelet identity **Storage Blob Data Reader** scoped to that resource group, so the role applies to the storage account regardless of when it is created:

```bash
MC_RG=$(az aks show -n "$CLUSTER_NAME" -g "$RESOURCE_GROUP" --query nodeResourceGroup -o tsv)

az role assignment create \
  --role "Storage Blob Data Reader" \
  --assignee-object-id "$KUBELET_PRINCIPAL_ID" \
  --assignee-principal-type ServicePrincipal \
  --scope "/subscriptions/${SUBSCRIPTION}/resourceGroups/${MC_RG}"
```

## Enable in KAITO

Model Streaming and Model Mirror are controlled by feature gates. Enable them when installing or upgrading the KAITO workspace controller, and point KAITO at the StorageClass and ServiceAccount you created above:

```bash
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set featureGates.ModelMirror=true \
  --set featureGates.ModelStreaming=true \
  --set defaultModelMirrorStorageClass=blob-fuse \
  --set defaultStreamingServiceAccount=kaito-model-streamer \
  --wait \
  --take-ownership
```

:::note
`ModelStreaming` requires `ModelMirror` — enable both. `defaultModelMirrorStorageClass` and `defaultStreamingServiceAccount` set the cluster-wide defaults; individual workspaces can override them with annotations (see [Per-Workspace Configuration](#per-workspace-configuration)).
:::

## Usage

Once the feature gates are enabled, **any vLLM Workspace streams by default** — no extra fields are required.

Create a YAML file named `phi-4-workspace.yaml` with the following content:

```yaml title="phi-4-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
resource:
  instanceType: "Standard_NC48ads_A100_v4" # Specifies the node type that will be auto-provisioned.
  labelSelector:
    matchLabels:
      apps: phi-4-mini
inference:
  preset:
    name: phi-4-mini-instruct
```

Apply your configuration to your cluster:

```bash
kubectl apply -f phi-4-workspace.yaml
```

KAITO will create a `ModelMirror` resource, download the weights to a blob-backed PVC in parallel with GPU provisioning, and start the inference pod streaming the weights from blob storage.

:::note
The inference pod is created only after **both** the `ModelMirror` download has finished (the resource reaches `Ready`) **and** the GPU node has been provisioned. Until then the Workspace stays `Pending` — this is expected, not an error.
:::

### Per-Workspace Configuration

The following annotations on the Workspace override the cluster-wide behavior:

| Annotation | Purpose |
|------------|---------|
| `kaito.sh/model-streaming: "disabled"` | **Opt out** of streaming for this workspace. It uses the normal download-at-runtime path even when the feature gate is on. This annotation is immutable after creation. |
| `kaito.sh/model-mirror-storage-class: "<name>"` | Use a specific StorageClass for this model's mirror instead of `defaultModelMirrorStorageClass`. |
| `kaito.sh/streaming-service-account: "<name>"` | Use a specific ServiceAccount for the streaming pod instead of `defaultStreamingServiceAccount`. The ServiceAccount must exist in the workspace's namespace and be annotated for Workload Identity. |

Example — opt a workspace out of streaming:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-no-streaming
  annotations:
    kaito.sh/model-streaming: "disabled"
resource:
  instanceType: "Standard_NC48ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: no-streaming
inference:
  preset:
    name: phi-4-mini-instruct
```

## Verify

Check that the `ModelMirror` resource was created and reached `Ready`:

```bash
# The ModelMirror resource is cluster-scoped (one per model, shared across workspaces).
kubectl get modelmirrors
```

Once the Workspace reports `Ready` and the inference pod is running, confirm the pod streams the weights — its command should use the `runai_streamer` load format:

```bash
POD=$(kubectl get pods -l kaito.sh/workspace=workspace-phi-4-mini -o jsonpath='{.items[0].metadata.name}')
kubectl get pod "$POD" -o jsonpath='{.spec.containers[0].command}'
# Expected to contain: --load-format=runai_streamer
```

## Troubleshooting

### Workspace creation rejected: CSI driver not found

If the Blob CSI driver is not enabled, the admission webhook rejects the Workspace at creation time:

```
admission webhook "validation.workspace.kaito.sh" denied the request: validation failed:
CSI driver blob.csi.azure.com not found; required for model streaming.
Ensure the CSI driver is enabled on your cluster
```

Enable it — see [Step 1](#step-1-enable-the-blob-csi-driver).

### Workspace stuck in Pending: ServiceAccount not configured

The ServiceAccount is validated when the controller reconciles the Workspace, not at creation time. If it is missing or not annotated for Workload Identity, the Workspace is created but stays `Pending`. The reason is surfaced on the Workspace's `ResourceReady` condition (reason `ModelMirrorNotReady`):

```bash
kubectl describe workspace workspace-phi-4-mini
```

If the ServiceAccount does not exist in the namespace:

```
ResourceReady   False   ModelMirrorNotReady
  Model download has not started (last reconcile error: ServiceAccount
  "kaito-model-streamer" not found in namespace "default")
```

If it exists but is missing the Workload Identity annotation:

```
ResourceReady   False   ModelMirrorNotReady
  Model download has not started (last reconcile error: ServiceAccount
  "kaito-model-streamer" is missing annotation azure.workload.identity/client-id;
  workload identity must be configured for model streaming)
```

Run [Step 3](#step-3-configure-workload-identity) for the Workspace's namespace.

:::note
These checks run only when the model is mirrored for the **first time**. If another Workspace already created the `ModelMirror` resource for the same model, a new Workspace reuses it and skips the ServiceAccount check — but the inference pod still needs a correctly configured ServiceAccount in its own namespace to stream.
:::

### Inference pod crashes after the model download completes

If the `ModelMirror` resource reaches `Ready` (the download succeeded) but the inference pod enters `CrashLoopBackOff`, the kubelet identity likely lacks read access to the storage account. The pod fails while the streamer lists the blobs:

```bash
kubectl logs <pod-name> --previous
```

```
azure.core.exceptions.HttpResponseError: This request is not authorized to perform this operation using this permission.
ErrorCode:AuthorizationPermissionMismatch
```

Confirm [Step 4](#step-4-grant-blob-read-access) granted **Storage Blob Data Reader** to the kubelet identity on the `MC_` resource group. (Azure RBAC changes can take a few minutes to propagate.)

### A Workspace downloads at runtime instead of streaming

Streaming is skipped (the Workspace falls back to the normal download-at-runtime path) when:

- the Workspace has the `kaito.sh/model-streaming: "disabled"` annotation, or
- the Workspace uses the Transformers runtime — streaming requires vLLM.

## Related Documentation

- [Installation](./installation.md) - Install the KAITO workspace controller
- [Azure Setup](./azure.md) - Create an AKS cluster with OIDC issuer and Workload Identity
- [Inference](./inference.md) - General inference documentation
- [Presets](./presets.md) - Supported models
