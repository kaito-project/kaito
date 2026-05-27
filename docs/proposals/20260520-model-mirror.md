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

**Azure Blob CSI Driver:** The AKS Blob CSI driver must be enabled on the cluster. It is **disabled by default** on AKS. Once enabled, the driver (`blob.csi.azure.com`) supports two mount protocols:

- **BlobFuse** (default) — FUSE-based userspace filesystem. Translates file operations to Blob REST API calls.
- **NFS** — kernel-level NFS 3.0 mount. Requires Premium storage account. Lower overhead for large sequential writes.

Either protocol works for this feature because:
- The **download Job** writes model files to the PVC via the mounted filesystem (BlobFuse or NFS both work).
- The **inference pod** (RunAI streamer) reads directly from Azure Blob via `az://` URI using the Azure SDK — it does **not** read through the filesystem mount.

NFS is recommended for better download write performance, but BlobFuse is also functional. BlobFuse uses account-key authentication which is more flexible — it even works in non-Azure environments where NFS protocol may not be available. The StorageClass `parameters.protocol` field controls which is used. Users may specify any custom StorageClass name in the CR spec (`spec.storage.storageClassName`).

KAITO will validate that the Blob CSI driver is available (by checking for the `blob.csi.azure.com` CSIDriver object) before creating the PVC. If the driver is not installed, the ModelMirror CR will report a clear error in `status.failureMessage`.

**Enabling the driver and creating the StorageClass** (one-time cluster setup):
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

A cluster-scoped resource in `kaito.sh/v1alpha1` — one per model, shared across all workspaces.

```yaml
apiVersion: kaito.sh/v1alpha1
kind: ModelMirror
metadata:
  name: a3f7b2  # SHA-256 hash of modelID, first 6 characters
spec:
  source:
    registry: huggingface               # "huggingface", "oci" (future)
    modelID: "qwen/qwen2.5-coder-32b-instruct"
    accessSecret:                        # optional: ObjectReference to Secret (cluster-scoped CR needs namespace)
      name: "hf-token-secret"
      namespace: "kaito-workspace"
  storage:
    storageSize: ""                     # auto-computed from model metadata if empty
    storageClassName: "blob-nfs"        # default: NFS (best performance). Also supports BlobFuse or other StorageClass names.
status:
  phase: Pending | Ready
  pvcName: "a3f7b2"
  modelPath: "/models/qwen/qwen2.5-coder-32b-instruct"
  storageURI: "az://container-name/qwen/qwen2.5-coder-32b-instruct"
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
- `spec.source` is generic — not tied to any specific model registry
- `status.storageURI` is provider-specific (az://, s3://) and is what vLLM receives as `--model=`
- `status.pvcName` is auto-generated — consumers read from status, not spec
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

A new controller that watches `ModelMirror` CRs and manages the download lifecycle:

1. **On CR creation:**
   - Validate StorageClass exists (error if not — user prerequisite)
   - Create PVC (auto-sized from model metadata) with a finalizer (`kaito.sh/model-mirror-protection`) to ensure PVC persists after the download Job/pod is deleted
   - Create download Job

2. **On Job completion:**
   - Update CR status to `Ready`
   - Populate `storageURI` (resolved from PVC → PV → CSI volumeHandle)
   - Set `lastDownloadTime`

3. **On Job failure:**
   - Update `failureMessage` from Job condition
   - Delete the failed Job
   - Recreate Job with exponential backoff (1m, 2m, 4m, ... capped at 30m)
   - CR stays in `Pending` phase (never "Failed")

4. **Idempotency:**
   - If CR already `Ready`, no action needed
   - If PVC exists and is bound, skip PVC creation
   - If Job exists and is running, wait for it

### Workspace Controller Integration

When the `ModelStreaming` feature gate is enabled and the workspace does not have the opt-out annotation:

1. **Before node provisioning:** Check if `ModelMirror` CR exists for the workspace's model
   - If not: create it (triggers download in parallel with node provisioning)
   - If exists and `Ready`: proceed immediately
   - If exists and `Pending`: wait

2. **Parallel execution:** Node provisioning proceeds independently of model download

3. **Gate before inference pod creation:** Wait for BOTH:
   - `ModelMirror` CR status = `Ready`
   - Nodes ready

4. **Workspace status:** Surface a `ModelMirrorInProgress` condition with the CR's `failureMessage` if download is failing. This gives users visibility without needing to inspect a separate resource.

### Inference Pod Configuration

When mirror + streaming is active, the inference pod is configured differently from the original path:

| Aspect | Original Path | Streaming Path |
|--------|---------------|----------------|
| `--model=` | Local path or HF ID | `az://<container>/<model-path>` (from CR status) |
| `--load-format=` | `auto` | `runai_streamer` |
| Model weights volume | Mounted at `/workspace/weights` | Not mounted |
| ServiceAccount | default | User-provided SA with workload identity |
| Extra config | None | `--model-loader-extra-config '{"distributed": true}'` (TP>1) |

**Environment variables on inference pod:**
- `AZURE_STORAGE_ACCOUNT_NAME` — resolved dynamically from PVC → PV → volumeHandle

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
1. Accept the ServiceAccount name via the ModelMirror CR or workspace annotation
2. Set label `azure.workload.identity/use: "true"` on inference pods
3. Set `spec.serviceAccountName` on the inference pod to the user-provided SA

**The AKS mutating admission webhook** then automatically injects `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_FEDERATED_TOKEN_FILE` into the inference pod.

**Note:** The download Job does NOT need workload identity — it writes to a CSI-mounted PVC where auth is handled transparently by the CSI driver's identity.

**Blob CSI driver permissions (for dynamic PVC provisioning):**

The Blob CSI driver controller needs write permission on a resource group to dynamically create storage accounts when a PVC is provisioned:

- **AKS managed Blob CSI driver** (enabled via `az aks update --enable-blob-driver`): The driver automatically has write permission on the AKS **node resource group** (`MC_*`). No additional configuration needed — storage accounts are created in the node resource group by default.
- **Open-source Blob CSI driver** (self-installed via Helm): The user must manually grant the driver's identity `Contributor` role on the node resource group. See [Blob CSI driver install docs](https://github.com/kubernetes-sigs/blob-csi-driver/blob/master/docs/install-driver-on-aks.md) for details.

**Documentation:** A troubleshooting guide will explain:
- How to verify the federated credential is configured correctly
- How to verify the SA annotation matches the identity client ID
- How to check RBAC on the storage account
- Common errors (token failures, permission denied) and recovery steps

### Feature Gate and Opt-Out

**Feature gate:**
- Name: `ModelStreaming`
- Default: `false` (opt-in initially)
- Passed via `--feature-gates=ModelStreaming=true` on the controller

**Workspace annotation opt-out:**
- `kaito.sh/model-streaming: "disabled"`
- Allows individual workspaces to bypass streaming even when the gate is globally on
- **Immutable:** A validating webhook rejects updates that add, remove, or change this annotation after workspace creation. If streaming does not work, the user must recreate the workspace.

**Behavior matrix:**

| Feature Gate | Annotation | Result |
|---|---|---|
| `ModelStreaming=false` | (any) | Original path (HF download at runtime) |
| `ModelStreaming=true` | not set | Mirror + stream |
| `ModelStreaming=true` | `"disabled"` | Original path (HF download at runtime) |

### Provider Abstraction

The CR interface is cloud-agnostic. Provider-specific logic is encapsulated in internal implementations selected by the existing `--cloud-provider` controller flag.

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

A model download fails repeatedly (e.g. network issues, invalid HF token). The `ModelMirror` CR stays in `Pending` with `failureMessage` updated. The workspace shows `ModelMirrorInProgress` condition with the error. The CR keeps retrying with exponential backoff. Once the user fixes the issue (e.g. corrects the HF token Secret), the next retry succeeds and both the CR and workspace proceed.

## Implementation Details

### CR Naming Convention

The CR name is a hash of the HuggingFace model ID (SHA-256, first 6 hex characters):

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

### Storage Account Resolution

The storage account name and container are resolved dynamically at runtime from the PVC referenced in the CR status:

1. Read PVC from CR `status.pvcName`
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
  ttlSecondsAfterFinished: 86400  # 24 hours for log inspection
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: downloader
          image: <image-with-hfdownloader>  # TODO: find MCR-hosted alternative
          command: ["/bin/sh", "-c"]
          args: ["<download-script>"]
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

**Download tool:** hfdownloader (Go binary, ~20 MB image). Provides concurrent downloads with built-in per-file retry (4 retries by default). The lightweight image size keeps mirror Job startup fast (seconds, not minutes). **Future:** Vendor the Go source into KAITO's CI pipeline and publish to MCR (`mcr.microsoft.com/aks/kaito/hfdownloader`) for full supply-chain ownership.

**Job pod TTL:** Set to 24 hours after completion so operators can inspect logs for download issues before the pod is garbage collected.

### Failure Handling and Retry Strategy

1. **Within a single Job attempt:** hfdownloader retries each file up to 4 times internally
2. **Job backoff:** Kubernetes retries the pod up to `backoffLimit: 3` times
3. **Job recreation:** When a Job exhausts its backoff limit, the ModelMirror controller:
   - Records the failure message in CR `status.failureMessage`
   - Deletes the failed Job
   - Waits with exponential backoff (1m, 2m, 4m, 8m, 16m, 30m cap)
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
- Storage URI resolution from PVC/PV
- Feature gate and annotation logic in workspace controller

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
