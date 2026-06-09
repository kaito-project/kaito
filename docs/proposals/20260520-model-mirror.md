---
title: Model Mirror and Streaming Automation
authors:
  - "@scottchen"
reviewers:
  - "@KAITO contributors"
creation-date: 2026-05-20
last-updated: 2026-05-20
status: provisional
---

# Model Mirror and Streaming Automation

## Table of Contents

- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals/Future Work](#non-goalsfuture-work)
- [Proposal](#proposal)
  - [Architecture Overview](#architecture-overview)
  - [ModelMirror Custom Resource](#modelmirror-custom-resource)
  - [ModelMirror Controller](#modelmirror-controller)
  - [Workspace Controller Integration](#workspace-controller-integration)
  - [Inference Pod Configuration](#inference-pod-configuration)
  - [Workload Identity](#workload-identity)
  - [Feature Gate and Opt-Out](#feature-gate-and-opt-out)
  - [Provider Abstraction](#provider-abstraction)
  - [User Stories](#user-stories)
- [Implementation Details](#implementation-details)
  - [CR Naming Convention](#cr-naming-convention)
  - [Storage Account Resolution](#storage-account-resolution)
  - [Download Job Specification](#download-job-specification)
  - [Failure Handling and Retry Strategy](#failure-handling-and-retry-strategy)
  - [Distributed Streaming Configuration](#distributed-streaming-configuration)
- [Test Plan](#test-plan)
- [Risks and Mitigations](#risks-and-mitigations)
- [Alternatives](#alternatives)
- [Upgrade Strategy](#upgrade-strategy)
- [Implementation History](#implementation-history)

## Summary

This proposal introduces automated model mirroring and streaming for KAITO inference workloads. When enabled, KAITO mirrors model weights to a cloud blob storage PVC in parallel with GPU node provisioning, then streams the model directly from blob storage to GPU memory using RunAI Model Streamer. This eliminates the sequential wait for both node provisioning AND model download, reducing end-to-end workspace readiness time significantly.

A new cluster-scoped `ModelMirror` Custom Resource manages the download lifecycle independently from workspaces. Models are downloaded once and shared across all workspaces that reference them. The entire feature is gated behind a feature flag and can be opted out per-workspace via annotation.

## Motivation

Current KAITO model loading follows a sequential path:

1. Provision GPU node (~3-8 minutes)
2. Pull container image (~1-2 minutes)
3. Download/load model weights to GPU (~2-15 minutes depending on model size)

Steps 1-3 are sequential — model weights cannot begin loading until the GPU node is ready. For large models (30-70+ GiB), this adds significant time to workspace readiness.

By mirroring model weights to persistent cloud storage in parallel with GPU provisioning, and streaming them directly to GPU memory via RunAI Model Streamer, we can overlap the download with node provisioning and eliminate the model-to-disk-to-GPU copy.

**POC Results (qwen2.5-coder-32b-instruct, 61 GiB, 2x A100):**
- Download to blob: ~8 minutes (runs in parallel with GPU provisioning)
- Streaming from blob to GPU: 16 seconds (with distributed streaming)
- Effective throughput: 1.92 GiB/s per GPU, 3.8 GiB/s aggregate
- vs. original path: model pull + load takes ~10-15 minutes sequentially after GPU is ready

### Goals

- Reduce end-to-end workspace readiness time by parallelizing model download with GPU provisioning
- Provide a cluster-scoped resource for managing mirrored models (download once, use many times)
- Support multi-cloud with a provider-agnostic CR interface
- Enable distributed streaming for tensor-parallel deployments
- Maintain backward compatibility — existing workspaces work unchanged when feature is off
- Eliminate dependency on local NVMe storage (local-csi-driver) for model weights — when enabled, GPU nodes become stateless with respect to model data.

### Non-Goals/Future Work

- Changing the model inference runtime or architecture
- Supporting model formats other than safetensors (RunAI streamer requires safetensors)
- Automatic model cache eviction or garbage collection (cluster admin manages storage manually)
- Supporting non-blob storage backends (e.g. local NVMe caching) in this iteration
- Model version management or rollback

## Proposal

### Prerequisites

**Storage setup:** The user must configure a StorageClass and ensure the underlying storage driver is available. ModelMirror is storage-agnostic — any StorageClass that supports `ReadWriteMany` PVCs works. For Azure Blob NFS (recommended for streaming):

- **Azure Blob CSI Driver:** Must be enabled on the cluster (disabled by default on AKS). Once enabled, the driver (`blob.csi.azure.com`) supports two mount protocols:

  - **BlobFuse** (default) — FUSE-based userspace filesystem. Translates file operations to Blob REST API calls.
  - **NFS** — kernel-level NFS 3.0 mount. Requires Premium storage account. Lower overhead for large sequential writes.

  Either protocol works for this feature because:
  - The **download Job** writes model files to the PVC via the mounted filesystem (BlobFuse or NFS both work).
  - The **inference pod** (RunAI streamer) reads directly from Azure Blob via `az://` URI using the Azure SDK — it does **not** read through the filesystem mount.

  NFS is recommended for better download write performance, but BlobFuse is also functional. BlobFuse uses account-key authentication which is more flexible — it even works in non-Azure environments where NFS protocol may not be available. The StorageClass `parameters.protocol` field controls which is used.

- **StorageClass:** Must be created with the desired provisioner and parameters.

ModelMirror does not validate the storage driver — if the CSI driver is missing, the PVC will remain unbound and the download Job will stay pending. The user is responsible for ensuring their storage infrastructure is configured.

**Example setup (Azure Blob NFS):**
```bash
# Enable the Blob CSI driver
az aks update --enable-blob-driver -n <cluster> -g <resource-group>

# Create the NFS-backed StorageClass
kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: blob-nfs
provisioner: blob.csi.azure.com
parameters:
  protocol: nfs
  skuName: Premium_LRS
reclaimPolicy: Retain
volumeBindingMode: Immediate
allowVolumeExpansion: true
EOF
```

### Architecture Overview

```
                              ┌─────────────────────────┐
                              │    Workspace Created     │
                              └──────────┬──────────────┘
                                         │
                         ┌───────────────┼───────────────┐
                         │                               │
                         ▼                               ▼
              ┌──────────────────┐            ┌──────────────────┐
              │  ModelMirror   │            │  Node Provisioning│
              │  CR created/     │            │  (Karpenter)      │
              │  checked         │            │                   │
              └────────┬─────────┘            └────────┬─────────┘
                       │                               │
                       ▼                               │
              ┌──────────────────┐                     │
              │  Download Job    │                     │
              │  (HF → Blob PVC)│                     │
              └────────┬─────────┘                     │
                       │                               │
                       ▼                               ▼
              ┌──────────────────┐            ┌──────────────────┐
              │  CR Status:      │            │  GPU Node Ready   │
              │  Ready           │            │                   │
              └────────┬─────────┘            └────────┬─────────┘
                       │                               │
                       └───────────────┬───────────────┘
                                       │
                                       ▼
                              ┌─────────────────────────┐
                              │  Inference Pod Created   │
                              │  (streams from az://)    │
                              └─────────────────────────┘
```

### ModelMirror Custom Resource

A cluster-scoped resource in `kaito.sh/v1alpha1` — one per model, shared across all workspaces. The CR is storage-agnostic: it downloads a model to a PVC using any user-provided StorageClass. Cloud provider-specific logic (streaming URIs, volumeHandle parsing) is handled by the workspace controller, not ModelMirror.

The CR can be created either by the workspace controller (Phase 2) or directly by the user.

```yaml
apiVersion: kaito.sh/v1alpha1
kind: ModelMirror
metadata:
  name: a3f7b2  # SHA-256 hash of modelID, first 6 characters
spec:
  source:
    registry: huggingface               # only supported value
    modelID: "qwen/qwen2.5-coder-32b-instruct"
    accessSecret:                        # optional: ObjectReference to Secret
      name: "hf-token-secret"
      namespace: "kaito-workspace"
  storage:
    size: "70Gi"                        # required — PVC size
    storageClassName: "blob-nfs"        # StorageClass to use
  jobNamespace: "default"               # required — namespace for PVC and download Job
status:
  phase: Pending | Ready
  modelPath: "/models/qwen/qwen2.5-coder-32b-instruct"
  conditions:
    - type: StorageReady
      status: "True"
    - type: Ready
      status: "True"
  failureMessage: ""
  lastDownloadTime: "2026-05-20T10:30:00Z"
```

**Key design decisions:**
- Cluster-scoped: survives workspace deletion, shared across workspaces and namespaces
- **Spec is fully immutable** — validating webhook rejects all spec updates. Delete and recreate to change.
- `spec.source` is generic — not tied to any specific model registry
- All required fields (jobNamespace, size, storageClassName) are in spec — the CR is self-contained at creation time
- No cloud provider-specific fields in status — the workspace controller derives streaming URIs from PVC → PV when needed
- Phase never reaches "Failed" — CR stays in `Pending` and keeps retrying indefinitely

**Relationships:**
- `ModelMirror : Model : PVC = 1:1:1` — each CR manages exactly one model in one PVC
- `ModelMirror : Workspace = 1:N` — multiple workspaces (across namespaces) can reference the same mirror

**Lifecycle and cleanup:**
- The CR persists indefinitely unless manually deleted by the cluster admin
- The **Workspace status** holds a reference to the ModelMirror CR it uses:
  ```yaml
  # In Workspace status
  status:
    modelMirror:
      name: a3f7b2  # reference to the cluster-scoped ModelMirror CR
  ```

### ModelMirror Controller

A new controller that watches `ModelMirror` CRs and manages the download lifecycle. The controller is storage-agnostic — it creates PVCs using the user-provided StorageClass and does not interact with cloud provider APIs.

1. **On CR creation:**
   - Add finalizer `kaito.sh/model-mirror-cleanup` to the CR (ensures proper cleanup on deletion)
   - Validate StorageClass exists (error if not)
   - Create PVC (using CR name in spec.jobNamespace, spec.storage.size) with a finalizer (`kaito.sh/model-mirror-protection`)
   - Create download Job in spec.jobNamespace

2. **On Job completion:**
   - Update CR status to `Ready`
   - Set `status.modelPath` = `/models/{spec.source.modelID}` (verbatim, case-sensitive — matches hfdownloader output directory)
   - Set `lastDownloadTime`

3. **On Job failure:**
   - Update `failureMessage` from Job condition
   - Delete the failed Job
   - Recreate Job after a constant 5-minute interval
   - CR stays in `Pending` phase (never "Failed")

4. **On CR deletion** (finalizer cleanup):
   - Remove finalizer from PVC (allows PVC deletion)
   - Remove CR finalizer (allows CR deletion to complete)

5. **Idempotency:**
   - If CR already `Ready`, no action needed
   - If PVC exists and is bound, skip PVC creation
   - If Job exists and is running, wait for it

### Workspace Controller Integration

When the `ModelStreaming` feature gate is enabled and the workspace does not have the opt-out annotation, the workspace controller is responsible for:

**ModelMirror CR lifecycle:**

1. **Before node provisioning:** Check if `ModelMirror` CR exists for the workspace's model
   - If not: create it with derived spec fields:
     - `jobNamespace`: workspace namespace
     - `size`: read from model metadata (`DiskStorageRequirement` in the model preset registry — all preset models have this information). No HuggingFace API calls needed.
     - `storageClassName`: from workspace annotation `kaito.sh/model-mirror-storage-class` → controller flag `--default-model-mirror-storage-class`
   - If exists and `Ready`: proceed immediately
   - If exists and `Pending`: wait

2. **Parallel execution:** Node provisioning proceeds independently of model download

3. **Gate before inference pod creation:** Wait for BOTH:
   - `ModelMirror` CR status = `Ready`
   - Nodes ready

4. **Workspace status:** Surface a `ModelMirrorInProgress` condition with the CR's `failureMessage` if download is failing. This gives users visibility without needing to inspect a separate resource.

**Cloud provider-specific inference pod configuration (responsibilities moved from ModelMirror controller):**

5. **Storage URI resolution:** Build the streaming URI (e.g., `az://<container>/<modelID>`) from PVC → PV → CSI volumeHandle. This is provider-specific parsing (Azure: `parts[2]` of `#`-separated volumeHandle).

6. **Storage account name:** Extract from PV volumeHandle (Azure: `parts[1]`) for `AZURE_STORAGE_ACCOUNT_NAME` env var on the inference pod.

7. **Workload identity:** Set `azure.workload.identity/use: "true"` label and `serviceAccountName` on the inference pod.

8. **Streaming configuration:** Set `--model=<storageURI>`, `--load-format=runai_streamer`, and distributed streaming config on the inference pod.

These are abstracted behind a `CloudProvider` interface in the workspace controller scope, making it straightforward to add S3/GCS support.

### Inference Pod Configuration

When mirror + streaming is active, the inference pod is configured differently from the original path:

| Aspect | Original Path | Streaming Path |
|--------|---------------|----------------|
| `--model=` | Local path or HF ID | `az://<container>/<model-path>` (resolved by workspace controller from PVC → PV → volumeHandle) |
| `--load-format=` | `auto` | `runai_streamer` |
| Model weights volume | Mounted at `/workspace/weights` | Not mounted |
| ServiceAccount | default | Determined by: workspace annotation `kaito.sh/streaming-service-account` → controller flag `--default-streaming-service-account` |
| Extra config | None | `--model-loader-extra-config '{"distributed": true}'` (TP>1) |

**Environment variables on inference pod:**
- `AZURE_STORAGE_ACCOUNT_NAME` — resolved by the workspace controller from PVC → PV → volumeHandle

**Pre-requisite:** `runai-model-streamer` and `runai-model-streamer-azure` Python packages pre-installed in the KAITO base image.

### Workload Identity

The inference pod needs to authenticate to blob storage for RunAI streamer's direct `az://` access. This uses AKS Workload Identity.

**User responsibilities (per namespace, before creating a workspace):**
1. Create or identify a Managed Identity with blob read access
2. Grant the identity `Storage Blob Data Reader` role on the storage account
3. In each namespace where workspaces will run:
   - Create a ServiceAccount with annotation `azure.workload.identity/client-id: <client-id>`
   - Create a Federated Identity Credential (FIC) linking that SA to the Managed Identity

**KAITO responsibilities:**
1. Accept the ServiceAccount name via workspace annotation `kaito.sh/streaming-service-account` (override) or controller flag `--default-streaming-service-account` (cluster-wide default)
2. Set label `azure.workload.identity/use: "true"` on inference pods
3. Set `spec.serviceAccountName` on the inference pod to the resolved SA name
4. Validate that the SA exists in the workspace namespace before creating the inference pod

**The AKS mutating admission webhook** then automatically injects `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_FEDERATED_TOKEN_FILE` into the inference pod.

**Note:** The download Job does NOT need workload identity — it writes to a CSI-mounted PVC where auth is handled transparently by the CSI driver's identity.

**Blob CSI driver permissions (for dynamic PVC provisioning):**

When no `storageAccount` is specified in the StorageClass, the driver dynamically creates a storage account in the AKS node resource group (`MC_*`). The control plane identity already has the required permissions on this resource group — no additional RBAC setup needed.

**Note:** If the StorageClass specifies a pre-created `storageAccount` in a different resource group, the control plane identity must be explicitly granted `Storage Account Contributor` on that storage account.

**Documentation:** A troubleshooting guide will explain:
- How to verify the federated credential is configured correctly
- How to verify the SA annotation matches the identity client ID
- How to check RBAC on the storage account
- Common errors (token failures, permission denied) and recovery steps

### Feature Gate and Opt-Out

**Feature gates:**

Two separate feature gates control ModelMirror and streaming:

| Gate | Default | Controls |
|---|---|---|
| `ModelMirror` | `false` | ModelMirror controller + webhook. Enables the "download model to PVC" functionality. |
| `ModelStreaming` | `false` | Workspace controller integration + inference pod streaming config. **Requires `ModelMirror=true`.** |

The controller validates at startup that `ModelStreaming=true` requires `ModelMirror=true` and fails with a clear error otherwise.

- `ModelMirror=true` alone: users can manually create ModelMirror CRs to pre-download models to storage.
- `ModelMirror=true,ModelStreaming=true`: workspace controller auto-creates ModelMirror CRs and configures inference pods to stream from blob.

Passed via `--feature-gates=ModelMirror=true,ModelStreaming=true` on the controller.

**Controller flags:**

| Flag | Required | Description | Example |
|---|---|---|---|
| `--default-model-mirror-storage-class` | **Yes** (when feature gate on) | StorageClass used when creating ModelMirror PVCs. Cluster-wide setting. | `blob-nfs` |
| `--default-streaming-service-account` | No (optional) | Default ServiceAccount for inference pods. Useful when SA name is unified across all namespaces. | `kaito-model-streamer` |

These are set via Helm values and require a controller restart (Helm upgrade) to change.

**Workspace annotations:**

| Annotation | Description |
|---|---|
| `kaito.sh/model-mirror-storage-class` | Override the cluster-wide default StorageClass for this workspace's ModelMirror CR (optional) |
| `kaito.sh/streaming-service-account` | ServiceAccount name for this workspace's inference pod. Required when `--default-streaming-service-account` is not set (SA varies per namespace). |

**Resolution order:**
- **StorageClass**: workspace annotation `kaito.sh/model-mirror-storage-class` → controller flag `--default-model-mirror-storage-class`
- **ServiceAccount**: workspace annotation `kaito.sh/streaming-service-account` → controller flag `--default-streaming-service-account` → error if neither set

**Workspace annotation opt-out:**
- `kaito.sh/model-streaming: "disabled"`
- Allows individual workspaces to bypass streaming even when the gate is globally on
- **Immutable:** A validating webhook (gated on `ModelStreaming=true`) rejects updates that add, remove, or change this annotation after workspace creation. If streaming does not work, the user must recreate the workspace.

**Behavior matrix:**

| Feature Gate | Annotation | Result |
|---|---|---|
| `ModelStreaming=false` | (any) | Original path (HF download at runtime) |
| `ModelStreaming=true` | not set | Mirror + stream |
| `ModelStreaming=true` | `"disabled"` | Original path (HF download at runtime) |

### Provider Abstraction

The ModelMirror CRD and controller are storage-agnostic — they work with any StorageClass and do not contain cloud provider-specific logic. Provider-specific concerns (storage URI construction, volumeHandle parsing, streaming env vars, workload identity) are handled by the **workspace controller** (Phase 2), which encapsulates this logic behind a `CloudProvider` interface. The provider is resolved at startup from the existing `CLOUD_PROVIDER` env var (Helm `cloudProviderName`). Currently only Azure is planned; enabling `ModelStreaming` with a non-Azure provider fails at startup with a clear error.

| Concern | Azure Implementation | Future AWS | Future GCP |
|---|---|---|---|
| StorageClass | `blob.csi.azure.com`, NFS protocol, Premium_LRS (requires Blob CSI driver enabled) | EFS CSI | GCS Fuse CSI |
| `storageURI` format | `az://<container>/<path>` | `s3://<bucket>/<path>` | `gs://<bucket>/<path>` |
| Storage resolution | Parse volumeHandle: `MC_rg#account#container##ns#` | PV annotation | PV annotation |
| Streamer env vars | `AZURE_STORAGE_ACCOUNT_NAME` | `AWS_DEFAULT_REGION` | `GOOGLE_APPLICATION_CREDENTIALS` |
| WI mechanism | AKS webhook + federated credential | IRSA | GKE WI |

### User Stories

#### Story 1: First workspace for a model

A user creates a workspace for `qwen/qwen2.5-coder-32b-instruct` with `ModelStreaming=true`. KAITO auto-creates the `ModelMirror` CR. The download Job starts in parallel with GPU provisioning. Once both complete, the inference pod starts and streams the model from blob in ~16 seconds. Total time savings: ~8-12 minutes vs sequential path.

#### Story 2: Second workspace for the same model

Another user (or the same user in a different namespace) creates a workspace for the same model. The `ModelMirror` CR already exists with `phase: Ready`. No download occurs — the workspace proceeds immediately to inference pod creation once GPU is ready.

#### Story 3: Opt-out for debugging

A user encounters a streaming issue and wants to fall back to the original path. They add `kaito.sh/model-streaming: "disabled"` to their workspace. The workspace uses the traditional HF download at runtime path.

#### Story 4: Download failure

A model download fails repeatedly (e.g. network issues, invalid HF token). The `ModelMirror` CR stays in `Pending` with `failureMessage` updated. The workspace shows `ModelMirrorInProgress` condition with the error. The CR keeps retrying every 5 minutes. Once the user fixes the issue (e.g. corrects the HF token Secret), the next retry succeeds and both the CR and workspace proceed.

## Implementation Details

### CR Naming Convention

The CR name is a hash of the HuggingFace model ID (SHA-256, first 6 hex characters). This naming convention is enforced by the workspace controller (Phase 2); when users create CRs directly, they can choose any name.

Examples:
- `qwen/Qwen2.5-Coder-32B-Instruct` → `a3f7b2`
- `microsoft/Phi-4` → `8e2d1f`

The actual model ID is always available via `spec.source.modelID` and surfaced in `kubectl get` output via `additionalPrinterColumns`:

```yaml
additionalPrinterColumns:
  - name: Model
    type: string
    jsonPath: .spec.source.modelID
  - name: Phase
    type: string
    jsonPath: .status.phase
  - name: Age
    type: date
    jsonPath: .metadata.creationTimestamp
```

```
$ kubectl get modelmirrors
NAME     MODEL                              PHASE   AGE
a3f7b2   qwen/Qwen2.5-Coder-32B-Instruct   Ready   2h
8e2d1f   microsoft/Phi-4                    Ready   1d
```

### Storage Account Resolution (Workspace Controller — Phase 2)

The storage account name and container are resolved by the **workspace controller** (not the ModelMirror controller) when configuring the inference pod. The workspace controller reads the PVC using the ModelMirror CR's name (PVC shares the CR name) in `spec.jobNamespace`:

1. Read PVC (name = CR name) from ModelMirror CR's `spec.jobNamespace`
2. Get the bound PV from `pvc.spec.volumeName`
3. Parse the CSI `volumeHandle` on the PV

For Azure Blob NFS, the volumeHandle format is:
```
MC_resourcegroup_aksname_region#storageaccount#containername##namespace#
```

Parts split by `#`:
- `parts[1]` = storage account name
- `parts[2]` = container name

This avoids hardcoding any storage account details in the CR or controller configuration.

### Download Job Specification

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: <cr-name>-download
  ownerReferences:
    - apiVersion: kaito.sh/v1alpha1
      kind: ModelMirror
      name: <cr-name>
spec:
  backoffLimit: 3
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: downloader
          image: <image-with-hfdownloader>  # TODO: Store image on MCR
          command: ["/bin/sh", "-c"]
          args:
            - |
              hfdownloader download "<model-id>" --local-dir /models -F safetensors -E "original"
              # Safety net: remove any remaining empty directories.
              # runai-model-streamer crashes on directories (IsADirectoryError) when pulling
              # files from Azure blob, so we keep only flat files.
              find /models/<model-id>/ -mindepth 1 -type d -exec rm -rf {} + 2>/dev/null || true
          env:
            - name: MODEL_ID
              value: "<model-id>"
            - name: HF_TOKEN
              valueFrom:
                secretKeyRef:
                  name: <access-secret>
                  key: token
                  optional: true
          resources:
            requests:
              cpu: "500m"
              memory: "512Mi"
            limits:
              cpu: "2"
              memory: "2Gi"
          volumeMounts:
            - name: model-storage
              mountPath: /models
      volumes:
        - name: model-storage
          persistentVolumeClaim:
            claimName: <pvc-name>
```

**Download tool:** hfdownloader (Go binary, ~20 MB image). Provides concurrent downloads with built-in per-file retry (4 retries by default). The lightweight image size keeps mirror Job startup fast (seconds, not minutes). The download command includes safetensors files only (`-F safetensors`) and excludes non-safetensor formats (`-E "original"`) since RunAI streamer only reads `*.safetensors` files. A post-download `find` removes any remaining empty directories as a safety net. **Future:** Vendor the Go source into KAITO's CI pipeline and publish to MCR (`mcr.microsoft.com/aks/kaito/hfdownloader`) for full supply-chain ownership.

**Job pod Lifecycle:** Job Pod is kept forever unless manually deleted by the user.

### Failure Handling and Retry Strategy

1. **Within a single Job attempt:** hfdownloader retries each file up to 4 times internally
2. **Job backoff:** Kubernetes retries the pod up to `backoffLimit: 3` times
3. **Job recreation:** When a Job exhausts its backoff limit, the ModelMirror controller:
   - Records the failure message in CR `status.failureMessage`
   - Deletes the failed Job
   - Requeues after a constant 5-minute interval
   - Creates a new Job
4. **No terminal failure state:** The CR never moves to a "Failed" phase. It stays in `Pending` and retries indefinitely until successful or manually deleted.

### Distributed Streaming Configuration

When `tensor-parallel-size > 1`, the RunAI streamer supports distributed loading where model partitions are split across ranks and exchanged via NCCL broadcast:

- Flag: `--model-loader-extra-config '{"distributed": true}'`
- Behavior: Each TP worker reads only its partition from blob (~model_size / tp_size), then broadcasts to other ranks via GPU-to-GPU NCCL
- Result: Near-linear scaling of blob read throughput with TP size

This flag is only added when `tensor-parallel-size > 1`. For single-GPU models, standard streaming is used.

**POC measurements (qwen2.5-coder-32b-instruct, 2x A100):**

| Configuration | Load Time | Throughput |
|---|---|---|
| Without distributed | 22-25s | ~1.2-1.4 GiB/s aggregate |
| With distributed=true | 16s | ~1.9 GiB/s per GPU, ~3.8 GiB/s aggregate |

## Test Plan

### Unit Tests
- ModelMirror controller reconciliation logic
- CR name derivation from model IDs
- Feature gate and annotation logic in workspace controller
- Validating webhook: ModelMirror field validation + spec immutability
- Validating webhook: immutable `kaito.sh/model-streaming` annotation

### Integration Tests
- ModelMirror CR lifecycle (create → download → ready)
- Workspace controller interaction with ModelMirror CR
- Multiple workspaces sharing the same ModelMirror CR

### E2E Tests (separate CI job)

A dedicated CI job with blob CSI driver and workload identity prerequisites:

1. **Mirror + streaming path:**
   - Create workspace with feature gate on → verify CR created → verify download Job → verify inference streams from blob
   - Second workspace for same model → verify no new download, reuses existing CR
   - Workspace with opt-out annotation → verify falls back to original path

2. **Original path (existing tests, unchanged):**
   - Feature gate off → verify HF download at runtime → normal load
   - All existing E2E tests continue to pass without modification

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| RunAI streamer compatibility with future vLLM versions | Pin streamer version in base image; test on upgrade |
| Blob storage performance varies by region/SKU | Document recommended storage SKU (Premium_LRS); POC validated 3.8 GiB/s |
| Workload Identity misconfiguration | Troubleshooting documentation; clear error messages in CR status |
| hfdownloader is third-party, not MCR-hosted | Vendor Go source into KAITO CI and publish to MCR in a future release; pin to known version until then |
| PVC storage cost for unused models | CR persists until manually deleted by cluster admin |
| Model data corruption on blob | hfdownloader verifies file sizes; future: checksum validation |

## Alternatives

### Alternative 1: Download directly to GPU node local disk
- Pros: No blob storage cost, simpler auth
- Cons: Cannot parallelize with node provisioning (node must exist first), not shared across workspaces, lost on node recycle

### Alternative 2: Use OCI artifacts for model streaming
- Pros: Aligns with OCI artifact proposal, no blob storage needed
- Cons: OCI registries don't support range reads needed for streaming; would still need a local download step

### Alternative 3: NFS mount from inference pod (no streaming)
- Pros: Simpler, no RunAI dependency
- Cons: NFS mount performance is poor for random-access tensor loading (~500 MiB/s vs 1.9 GiB/s with streamer)

## Upgrade Strategy

- **Feature gate off by default:** Existing clusters are unaffected. No migration needed.
- **Enabling the feature:** Set `--feature-gates=ModelStreaming=true`, set up workload identity, ensure blob CSI driver is enabled. New workspaces automatically use streaming. Existing workspaces are not retroactively changed.
- **Disabling the feature:** Set feature gate to false. New workspaces use original path. Existing `ModelMirror` CRs and PVCs remain (no automatic cleanup).
- **Base image update required:** `runai-model-streamer` packages must be added to the KAITO base image before enabling the feature gate.
- **Helm chart: local-csi-driver optional:** When ModelStreaming is enabled for all workspaces, local NVMe storage is no longer needed for model weights. The chart will expose `local-csi-driver.enabled` (default: `true`) so users can disable it:
  ```yaml
  # values.yaml
  local-csi-driver:
    enabled: false  # Set to false when using ModelStreaming exclusively
  ```
  Note: only disable if **all** workspaces use streaming. If some workspaces opt out via annotation, local-csi-driver is still needed for those.
