---
title: Migrate Node Provisioner from gpu-provisioner to Azure Karpenter
authors:
  - "@rambohe-ch"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
  - "@2170chm"
creation-date: 2026-04-07
last-updated: 2026-04-10
status: provisional
---

# Migrate Node Provisioner from gpu-provisioner to Azure Karpenter

## Table of Contents

- [Glossary](#glossary)
- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Proposal](#proposal)
  - [Design Principles](#design-principles)
  - [Architecture Overview](#architecture-overview)
  - [NodePool and AKSNodeClass Strategy](#nodepool-and-aksnodeclass-strategy)
    - [Global AKSNodeClass](#global-aksnodeclass)
    - [Per-Workspace NodePool](#per-workspace-nodepool)
    - [Naming Conventions](#naming-conventions)
    - [Resource Ownership and Lifecycle](#resource-ownership-and-lifecycle)
    - [Handling Existing (Legacy) Workspaces](#handling-existing-legacy-workspaces)
  - [Resource Definitions](#resource-definitions)
    - [AKSNodeClass Configuration](#aksnodeclass-configuration)
    - [NodePool Configuration (InferenceSet Workspace)](#nodepool-configuration-inferenceset-workspace)
    - [NodePool Configuration (Standalone Workspace)](#nodepool-configuration-standalone-workspace)
  - [Workload Scheduling](#workload-scheduling)
  - [Replicas Calculation](#replicas-calculation)
  - [Drift Orchestration](#drift-orchestration)
  - [KAITO Controller Changes](#kaito-controller-changes)
    - [New CRD Dependencies](#new-crd-dependencies)
    - [Provisioner Abstraction Layer](#provisioner-abstraction-layer)
    - [NodePool Lifecycle Management](#nodepool-lifecycle-management)
    - [Legacy NodeClaim GC Controller](#legacy-nodeclaim-gc-controller)
  - [gpu-provisioner and Azure Karpenter Coexistence](#gpu-provisioner-and-azure-karpenter-coexistence)
  - [Deletion Strategy](#deletion-strategy)
- [Known Issues](#known-issues)
  - [GPU Zonal Provisioning Failure](#gpu-zonal-provisioning-failure)
- [Risks and Mitigations](#risks-and-mitigations)
- [Alternatives](#alternatives)
- [Notes](#notes)
- [Implementation History](#implementation-history)

## Glossary

- **gpu-provisioner**: The current node provisioner used by KAITO on Azure, a fork of the early Karpenter project, that provisions GPU nodes based on Machine/NodeClaim CRDs.
- **Azure Karpenter**: The official Azure provider for [Karpenter](https://karpenter.sh/) (`karpenter-provider-azure`), which implements the Karpenter CloudProvider interface for Azure AKS clusters.
- **NodeClaim**: A Karpenter CRD (`karpenter.sh/v1/NodeClaim`) that represents a request for a single node. It is the atomic unit of node provisioning.
- **NodePool**: A Karpenter CRD (`karpenter.sh/v1/NodePool`) that defines a group of nodes with shared configuration, constraints, and disruption policies. When `spec.replicas` is set, Karpenter uses static provisioning to maintain exactly that many NodeClaims.
- **AKSNodeClass**: An Azure-specific Karpenter CRD (`karpenter.azure.com/v1beta1/AKSNodeClass`) that defines Azure-specific node configuration (image family, OS disk size, network, etc.).
- **Drift**: A Karpenter mechanism that detects when a NodeClaim's actual state has diverged from its desired state (e.g., Kubernetes version upgrade, node image update).
- **Static Provisioning**: A Karpenter mode where NodePool `spec.replicas` controls the exact number of NodeClaims. Karpenter automatically creates/deletes NodeClaims to match the desired replica count.
- **NAP**: Node Auto-Provisioning — KAITO's feature for automatically creating nodes for workspaces.
- **InferenceSet**: A KAITO CRD (`kaito.sh/v1alpha1/InferenceSet`) that manages a group of identical Workspace replicas for horizontal scaling of inference workloads. It creates and manages multiple Workspaces sharing the same model and configuration.

## Summary

Migrate KAITO's node provisioning from gpu-provisioner to Azure Karpenter. Two global AKSNodeClasses (`image-family-ubuntu`, `image-family-azure-linux`) are pre-installed at KAITO deployment time — all NodePools reference one of these. KAITO creates a per-Workspace NodePool (with `replicas`), while Azure Karpenter handles NodeClaim/Node creation, VM lifecycle, and drift-based node upgrades. Per-Workspace NodePool isolation ensures clean scale-down semantics (deleting a Workspace's NodePool cascades exactly its NodeClaims). Drift at the InferenceSet level is orchestrated by KAITO via toggling NodePool disruption budgets one Workspace at a time. gpu-provisioner is retained only for cleaning up legacy NodeClaims during the coexistence period. A legacy NodeClaim GC controller handles leaked NodeClaims from the gpu-provisioner era.

## Motivation

gpu-provisioner is a KAITO-specific fork with no drift detection, no node upgrade capability, and high maintenance burden. Azure Karpenter is the officially supported Azure node provisioner with drift detection (`K8sVersionDrift`, `ImageDrift`, `NodeClassDrift`), AKS Machine API integration, and active community support. Migrating enables automated node upgrades and aligns with the AKS ecosystem.

### Goals

1. KAITO manages NodePool lifecycle (create, delete). Two global AKSNodeClasses are pre-installed at deployment time. Azure Karpenter manages NodeClaim/Node creation, VM provisioning, and drift-based node upgrades.
2. Per-Workspace NodePool: each Workspace (whether from InferenceSet or standalone) gets its own NodePool referencing a global AKSNodeClass.
3. Drift orchestration at InferenceSet level: KAITO controls drift upgrade pace by toggling per-Workspace NodePool disruption budgets one at a time.
4. gpu-provisioner coexistence: gpu-provisioner **no longer creates new resources**, retained only for cleaning up legacy NodeClaims. All new resources managed by Azure Karpenter.
5. Zero Azure Karpenter modification — all integration via existing extension points.
6. Legacy NodeClaim GC controller cleans up leaked NodeClaims from the gpu-provisioner era.

### Non-Goals

1. Replacing Karpenter's pod-driven autoscaling for general workloads.
2. Multi-cloud migration (AWS is out of scope).
3. In-place node updates (Karpenter uses replace-based upgrade via drift).
4. Automatic gpu-provisioner retirement.

## Proposal

### Design Principles

1. **Clear responsibility split**: KAITO creates/deletes NodePools. Global AKSNodeClasses are pre-installed. Azure Karpenter creates/deletes NodeClaims and Nodes, provisions/deprovisions VMs, and handles drift-based node upgrades.
2. **Static provisioning via replicas**: NodePool `spec.replicas` controls the exact number of NodeClaims. Karpenter's static provisioning/deprovisioning controller automatically maintains the desired count.
3. **Global AKSNodeClass**: Two AKSNodeClasses (`image-family-ubuntu`, `image-family-azure-linux`) are pre-installed at KAITO deployment time. All NodePools reference one of these — KAITO never creates or deletes AKSNodeClass resources at runtime.
4. **Per-Workspace NodePool for clean scale-down**: Each Workspace gets its own NodePool. Deleting a Workspace's NodePool cascades exactly its NodeClaims — no ambiguity about which nodes to remove.
5. **Budget-controlled drift orchestration**: Per-Workspace NodePools default to `budgets.nodes: "0"` (drift replacement blocked). KAITO orchestrates InferenceSet-level drift by toggling one Workspace's NodePool budget at a time, preventing concurrent upgrades across the InferenceSet.
6. **Label-based isolation for coexistence**: New NodeClaims use `karpenter.kaito.sh/*` labels, legacy NodeClaims retain `kaito.sh/workspace` labels. The two provisioners cannot interfere with each other.
7. **NodeClassRef-based coexistence**: Azure Karpenter's `IsManaged()` only processes `AKSNodeClass` NodeClaims, automatically ignoring gpu-provisioner's `KaitoNodeClass` NodeClaims.

### Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                KAITO Controllers (Workspace + InferenceSet)          │
│                                                                      │
│  • Create per-Workspace NodePool (referencing global AKSNodeClass)    │
│  • Set NodePool replicas = TargetNodeCount                           │
│  • Watch NodeClaim Ready condition → Schedule workload pods          │
│  • Drift orchestration: toggle NodePool budget per Workspace         │
│  • On deletion: delete NodePool (cascade cleanup)                    │
│  • Legacy NodeClaim GC controller: clean up leaked old NodeClaims    │
└──────────┬──────────────────────┬────────────────────────────────────┘
           │ Create/Update/Delete │ Watch NodeClaim
           │ NodePool             │ Ready + Drifted
           ▼                      ▼
┌──────────────────────────────────────────────────────────────────────┐
│                     Kubernetes API Server                            │
│                                                                      │
│  AKSNodeClass (karpenter.azure.com/v1beta1)  ── 2 global instances   │
│    image-family-ubuntu, image-family-azure-linux (pre-installed)                         │
│  NodePool (karpenter.sh/v1)  ── per Workspace                       │
│  NodeClaim (karpenter.sh/v1)  ── per GPU node (created by Karpenter)│
└──────────┬──────────────────────┬─────────────────────┬──────────────┘
           │ Static Provisioning  │ Drift Detection     │ Drift-Based
           │ Controller           │ (sub-controller)    │ Replacement
           ▼                      ▼                     ▼
┌──────────────────────────────────────────────────────────────────────┐
│                 Azure Karpenter (NO modifications)                   │
│                                                                      │
│  ✅ Static Provisioning Controller                                   │
│     Watches NodePool replicas → Create/Delete NodeClaims to match    │
│                                                                      │
│  ✅ Lifecycle Controller                                             │
│     Watches NodeClaims → Create/Delete Azure VMs                     │
│                                                                      │
│  ✅ Disruption Controller (Drift)                                    │
│     Detects drift → Sets Drifted condition on NodeClaims             │
│     Replaces drifted nodes ONLY when budget allows (nodes>0)         │
│                                                                      │
│  ✗ Dynamic Auto-Scaling                                              │
│     NOT used — static replicas mode only                             │
└──────────────────────────────────────────────────────────────────────┘
```

### NodePool and AKSNodeClass Strategy

#### Global AKSNodeClass

Two AKSNodeClasses are **pre-installed** when KAITO is deployed (via Helm chart or created at KAITO controller startup). They are global, shared by all Workspaces, and never created or deleted at runtime:

| AKSNodeClass | Image Family | Use Case |
|-------------|-------------|----------|
| `image-family-ubuntu` | Ubuntu | Default for most GPU workloads |
| `image-family-azure-linux` | AzureLinux | For workloads requiring AzureLinux |

All NodePools reference one of these two AKSNodeClasses via `nodeClassRef`. Drift signals from AKSNodeClass changes (K8s version upgrade, node image update) affect all NodeClaims referencing that AKSNodeClass globally.

#### Per-Workspace NodePool

Each Workspace gets its own NodePool. This ensures clean scale-down semantics: deleting a Workspace's NodePool cascades exactly its NodeClaims/Nodes/VMs.

| Workspace Origin | NodePool | AKSNodeClass | Replicas |
|-----------------|----------|-------------|----------|
| InferenceSet `foo`, Workspace `default/foo-0` | `default-foo-0` | `image-family-ubuntu` (global) | `workspace.Status.TargetNodeCount` |
| InferenceSet `foo`, Workspace `default/foo-1` | `default-foo-1` | `image-family-ubuntu` (global) | `workspace.Status.TargetNodeCount` |
| Standalone Workspace `prod/ws-a` | `prod-ws-a` | `image-family-ubuntu` (global) | `workspace.Status.TargetNodeCount` |

**Why per-Workspace NodePool**: When an InferenceSet scales down, specific Workspaces are removed. The corresponding NodePool is deleted, and Karpenter cascades the cleanup to exactly that Workspace's NodeClaims/Nodes/VMs. A shared NodePool cannot guarantee which NodeClaims are removed during scale-down.

#### Naming Conventions

| Resource | Name | Example |
|----------|------|---------|
| NodePool | `<workspace-namespace>-<workspace-name>` | `default-foo-0`, `prod-ws-a` |
| AKSNodeClass (global) | `image-family-ubuntu`, `image-family-azure-linux` | Pre-installed, not dynamically named |

NodePool names use `<workspace-namespace>-<workspace-name>` to ensure uniqueness across namespaces (NodePool is cluster-scoped, Workspace is namespace-scoped). If exceeding 253 characters, a truncated name with hash suffix is used.

#### Resource Ownership and Lifecycle

- **AKSNodeClass**: Pre-installed globally. Never created or deleted by KAITO controllers at runtime.
- **NodePool**: One per Workspace. Created when the Workspace is created. Deleted when the Workspace is removed (scale-down, standalone deletion, or InferenceSet deletion).

Management label on NodePool: `karpenter.kaito.sh/managed-by: kaito`.

#### Handling Existing (Legacy) Workspaces

- Existing gpu-provisioner NodeClaims (with `KaitoNodeClass` reference and `kaito.sh/workspace` label) remain as-is. Azure Karpenter ignores them via the `IsManaged()` filter.
- gpu-provisioner **no longer creates any new resources**. It is retained only as a cleanup tool for legacy NodeClaims.
- No forced re-provisioning. Migration is purely additive — new InferenceSets and Workspaces use Azure Karpenter exclusively.
- Legacy NodeClaims are directly deleted by KAITO controller when their owning Workspace/InferenceSet is deleted.

### Resource Definitions

#### AKSNodeClass Configuration

Two global AKSNodeClasses are pre-installed when KAITO is deployed:

```yaml
apiVersion: karpenter.azure.com/v1beta1
kind: AKSNodeClass
metadata:
  name: image-family-ubuntu
  labels:
    karpenter.kaito.sh/managed-by: kaito
spec:
  imageFamily: Ubuntu
  osDiskSizeGB: 300
---
apiVersion: karpenter.azure.com/v1beta1
kind: AKSNodeClass
metadata:
  name: image-family-azure-linux
  labels:
    karpenter.kaito.sh/managed-by: kaito
spec:
  imageFamily: AzureLinux
  osDiskSizeGB: 300
```

These are installed via the KAITO Helm chart or created at KAITO controller startup. They are never created or deleted at runtime.

Drift sources: AKS K8s version upgrade triggers `K8sVersionDrift`; Azure node image gallery update triggers `ImageDrift`. Since all KAITO-managed NodePools reference one of these two global AKSNodeClasses, drift signals affect all KAITO NodeClaims globally. The budget-controlled drift orchestration ensures upgrades are performed one Workspace at a time within each InferenceSet.

#### NodePool Configuration (InferenceSet Workspace)

Each Workspace within an InferenceSet gets its own NodePool, referencing a global AKSNodeClass. NodePool disruption budget defaults to `nodes: "0"` — drift replacement is **blocked** until KAITO's drift orchestrator enables it.

**Example: NodePool for Workspace `default/llama-serving-0` (child of InferenceSet `llama-serving`, TargetNodeCount=2):**

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default-llama-serving-0
  labels:
    karpenter.kaito.sh/managed-by: kaito
spec:
  replicas: 2    # = workspace.Status.TargetNodeCount
  template:
    metadata:
      labels:
        karpenter.kaito.sh/workspace: "llama-serving-0"
    spec:
      nodeClassRef:
        group: karpenter.azure.com
        kind: AKSNodeClass
        name: image-family-ubuntu                    # Global AKSNodeClass
      requirements:
        - key: "node.kubernetes.io/instance-type"
          operator: In
          values: ["Standard_NC24ads_A100_v4"]
      taints:
        - key: "karpenter.kaito.sh/workspace"
          value: "llama-serving-0"
          effect: NoSchedule
  disruption:
    budgets:
      - nodes: "0"                         # Default: drift replacement BLOCKED
        reasons: [Drifted]                 # KAITO toggles to "1" during drift orchestration
```

Key settings:
- `replicas: 2` — Static provisioning: Karpenter maintains exactly 2 NodeClaims for this Workspace.
- `disruption.budgets.nodes: "0"` — Drift replacement is blocked by default. Karpenter still detects drift and sets the `Drifted` condition on NodeClaims, but cannot autonomously replace them. KAITO's drift orchestrator patches this to `"1"` one Workspace at a time (see [Drift Orchestration](#drift-orchestration)).
- No `limits` field — KAITO controls the total node count via `replicas`.
- Template labels and taints use `karpenter.kaito.sh/workspace` for per-Workspace scheduling.

**InferenceSet `llama-serving` (Replicas=3, TargetNodeCount=2) creates 3 NodePools:**

| Workspace | NodePool | Replicas | AKSNodeClass |
|-----------|----------|----------|-------------|
| `default/llama-serving-0` | `default-llama-serving-0` | 2 | `image-family-ubuntu` (global) |
| `default/llama-serving-1` | `default-llama-serving-1` | 2 | `image-family-ubuntu` (global) |
| `default/llama-serving-2` | `default-llama-serving-2` | 2 | `image-family-ubuntu` (global) |

Total NodeClaims: 6 (3 Workspaces × 2 TargetNodeCount).

#### NodePool Configuration (Standalone Workspace)

Standalone Workspaces also reference a global AKSNodeClass. Drift replacement budget defaults to `nodes: "1"` since there is no InferenceSet-level coordination needed.

**Example: NodePool for standalone Workspace `default/my-workspace` (TargetNodeCount=2):**

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default-my-workspace
  labels:
    karpenter.kaito.sh/managed-by: kaito
spec:
  replicas: 2    # = workspace.Status.TargetNodeCount
  template:
    metadata:
      labels:
        karpenter.kaito.sh/workspace: "my-workspace"
    spec:
      nodeClassRef:
        group: karpenter.azure.com
        kind: AKSNodeClass
        name: image-family-ubuntu                    # Global AKSNodeClass
      requirements:
        - key: "node.kubernetes.io/instance-type"
          operator: In
          values: ["Standard_NC24ads_A100_v4"]
      taints:
        - key: "karpenter.kaito.sh/workspace"
          value: "my-workspace"
          effect: NoSchedule
  disruption:
    budgets:
      - nodes: "1"                         # Allow Karpenter to replace 1 node at a time
        reasons: [Drifted]
```

### Workload Scheduling

Workloads use `nodeSelector` and `tolerations` that match the NodePool template labels and taints. Since every Workspace (whether InferenceSet child or standalone) has its own NodePool with workspace-scoped labels/taints, the scheduling configuration is uniform:

```yaml
spec:
  nodeSelector:
    karpenter.kaito.sh/workspace: "<workspace-name>"
  tolerations:
    - key: "karpenter.kaito.sh/workspace"
      value: "<workspace-name>"
      operator: Equal
      effect: NoSchedule
```

This applies to **both** InferenceSet Workspaces and standalone Workspaces. Each Workspace's workload is scheduled exclusively onto nodes provisioned by its dedicated NodePool.

### Replicas Calculation

Since each Workspace has its own NodePool, the replicas calculation is straightforward:

```
// Both InferenceSet Workspace and Standalone Workspace
nodePoolReplicas = workspace.Status.TargetNodeCount
```

`workspace.Status.TargetNodeCount` is immutable, so the NodePool `replicas` is set once at creation time and never changes.

**InferenceSet scale-up/scale-down** does not change any NodePool's `replicas`. Instead:
- **Scale-up**: New Workspaces are created → KAITO creates new NodePools (one per new Workspace) with `replicas = TargetNodeCount`.
- **Scale-down**: Workspaces are removed → KAITO deletes the corresponding NodePools → Karpenter cascades cleanup.

### Drift Orchestration

When drift signals occur (K8s version upgrade, node image update), all NodeClaims referencing the global AKSNodeClass are marked with the `Drifted` condition. Since all KAITO NodePools reference the same global AKSNodeClass, drift signals are cluster-wide. Without coordination, all per-Workspace NodePools would attempt drift replacement simultaneously, potentially disrupting all InferenceSets.

KAITO implements an **InferenceSet-level drift orchestrator** that controls the pace of upgrades by toggling NodePool disruption budgets:

#### Default State

All per-Workspace NodePools for InferenceSet Workspaces have `disruption.budgets.nodes: "0"`. Karpenter detects drift and sets the `Drifted` condition on NodeClaims, but **cannot replace** them.

#### Drift Upgrade Flow

```
1. KAITO watches NodeClaims across all Workspaces in the InferenceSet
2. Detects Drifted=True on NodeClaims
3. Selects ONE Workspace (e.g., llama-serving-0) for upgrade
4. Patches NodePool default-llama-serving-0: disruption.budgets.nodes = "1"
5. Karpenter replaces drifted nodes in default-llama-serving-0 (one at a time)
6. KAITO waits until all NodeClaims in default-llama-serving-0 are no longer drifted
7. Patches NodePool default-llama-serving-0: disruption.budgets.nodes = "0" (restore)
8. Selects next Workspace (e.g., llama-serving-1) → repeat from step 4
9. Continue until all Workspaces are upgraded
```

#### Standalone Workspaces

Standalone Workspace NodePools use `disruption.budgets.nodes: "1"` by default — no orchestration needed since there is no InferenceSet-level coordination.

### KAITO Controller Changes

#### New CRD Dependencies

Add `karpenter.sh/v1` (NodePool, NodeClaim) and `karpenter.azure.com/v1beta1` (AKSNodeClass) client/informer dependencies.

#### Provisioner Abstraction Layer

Abstract the provisioner interface with two implementations:

- **gpu-provisioner implementation**: **Only retains cleanup capability** (NodeClaim deletion). No creation logic.
- **Karpenter implementation**: Responsible for creating NodePool, updating NodePool disruption budget (for drift orchestration), and deleting NodePool.
- **All new NodeClaim/Node creation** happens via Karpenter (automatically via NodePool replicas).
- **AKSNodeClass** is pre-installed globally — not managed by the provisioner interface.

KAITO controller distinguishes management paths by provisioner type annotation on InferenceSet/Workspace (e.g., `kaito.sh/provisioner: karpenter` or `kaito.sh/provisioner: gpu-provisioner`). New resources always use the Karpenter path.

**Interface Definition:**

```go
// NodeProvisioner abstracts node provisioning for a Workspace.
// Callers pass the Workspace object directly — all internal resources
// (NodePool, NodeClaim) are managed by the implementation.
// The implementation derives resource names, labels, taints, and disruption
// budgets from the Workspace object. AKSNodeClass is pre-installed globally
// and is NOT managed by this interface.
//
// Two implementations:
//   - KarpenterProvisioner: creates NodePool referencing global AKSNodeClass;
//     Karpenter auto-creates NodeClaims via static provisioning.
//   - GpuProvisioner: legacy cleanup only (deletes old NodeClaims).
type NodeProvisioner interface {
    // ProvisionNodes ensures the per-Workspace NodePool exists and is
    // progressing toward Ready.
    //
    // The implementation reads the following from the Workspace object:
    //   - ws.Name / ws.Namespace
    //   - ws.Resource.InstanceType (GPU SKU)
    //   - ws.Status.TargetNodeCount (NodePool replicas)
    //   - ws.Labels["inferenceset.kaito.sh/created-by"] (InferenceSet name, if any)
    //
    // Karpenter impl:
    //   Create per-Workspace NodePool (<workspace-namespace>-<workspace-name>) with
    //   replicas=TargetNodeCount, appropriate disruption budget
    //   (nodes="0" for InferenceSet, nodes="1" for standalone),
    //   labels, taints, and nodeClassRef pointing to global AKSNodeClass.
    //   Karpenter's static provisioning controller then creates NodeClaims
    //   automatically.
    //
    // gpu-provisioner impl: returns ErrNotSupported (no new creation).
    ProvisionNodes(ctx context.Context, ws *v1beta1.Workspace) error

    // DeprovisionNodes removes the per-Workspace NodePool.
    //
    // Karpenter impl:
    //   Delete per-Workspace NodePool (<workspace-namespace>-<workspace-name>).
    //   Karpenter cascades: NodeClaim → Node → Azure VM.
    //
    // gpu-provisioner impl:
    //   Deletes legacy NodeClaims with kaito.sh/workspace label.
    DeprovisionNodes(ctx context.Context, ws *v1beta1.Workspace) error

    // EnableDrift enables drift replacement for the Workspace's NodePool
    // by patching its disruption budget to allow node replacement.
    // Used by the InferenceSet drift orchestrator.
    //
    // Karpenter impl: patches budget nodes="0" → "1".
    // gpu-provisioner impl: no-op.
    EnableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error

    // DisableDrift disables drift replacement for the Workspace's NodePool
    // by patching its disruption budget back to zero.
    //
    // Karpenter impl: patches budget nodes="1" → "0".
    // gpu-provisioner impl: no-op.
    DisableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error
}
```

#### NodePool Lifecycle Management

- **InferenceSet scenario**: When each child Workspace is created → create per-Workspace NodePool (`<workspace-namespace>-<workspace-name>`) referencing global AKSNodeClass.
- **Standalone Workspace scenario**: Workspace creation → create NodePool referencing global AKSNodeClass.
- InferenceSet scale-up → new Workspaces trigger new NodePool creation.
- InferenceSet scale-down → removed Workspaces trigger NodePool deletion.
- InferenceSet/Workspace deletion → see [Deletion Strategy](#deletion-strategy).

**Remove direct Azure Compute API dependency** (in the Karpenter path): Azure VM lifecycle is managed entirely by Karpenter's cloud provider.

#### Legacy NodeClaim GC Controller

A dedicated garbage collection controller handles leaked legacy NodeClaims from the gpu-provisioner era.

**Problem scenario**: A legacy Workspace's node crashes, but the gpu-provisioner NodeClaim is not cleaned up (e.g., gpu-provisioner is no longer running or has a bug). When the Workspace is reconciled by the new KAITO controller, it creates a new NodePool via Azure Karpenter, and Karpenter provisions new NodeClaims. The old gpu-provisioner NodeClaim becomes an orphan — it references `KaitoNodeClass` (which Azure Karpenter ignores), its backing node no longer exists, and no controller is managing it.

**GC Controller behavior**:

```
Periodically (e.g., every 10 seconds):
  List all Workspaces that have legacy gpu-provisioner NodeClaims
    (i.e., Workspaces whose NodeClaims carry kaito.sh/workspace label)
  For each such Workspace:
    List Karpenter-managed NodeClaims for this Workspace
      (label karpenter.kaito.sh/workspace=<workspace-name>)
    if all Karpenter NodeClaims are Ready:
      List legacy NodeClaims for this Workspace
        (label kaito.sh/workspace=<workspace-name>)
      if any legacy NodeClaims still exist:
        → Delete these legacy NodeClaims (they have been superseded
          by Karpenter-managed nodes that are already Ready)
```

**Key points**:
- Runs as a **periodic scan** (e.g., every 10 seconds), not event-driven.
- Scans **legacy Workspaces** (those with gpu-provisioner NodeClaims), not new Karpenter-managed Workspaces.
- Only deletes a legacy NodeClaim when the replacement Karpenter NodeClaims are **Ready** — ensures the Workspace's workload has a healthy node before removing the old one.
- Only targets NodeClaims with `kaito.sh/workspace` label (legacy gpu-provisioner NodeClaims). Never touches `karpenter.kaito.sh/*`-labeled NodeClaims.
- Complements the synchronous cleanup in `DeprovisionNodes()` — the GC controller is a safety net for cases where the Workspace is not being deleted.

### gpu-provisioner and Azure Karpenter Coexistence

The two systems are isolated by both `spec.nodeClassRef` and labels:

- **Azure Karpenter** only processes `AKSNodeClass` NodeClaims. NodeClaims/Nodes carry `karpenter.kaito.sh/workspace` labels.
- **gpu-provisioner** only processes `KaitoNodeClass` NodeClaims. NodeClaims/Nodes carry `kaito.sh/workspace` labels.
- Karpenter does not recognize or manage gpu-provisioner NodeClaims (they don't reference `AKSNodeClass`).
- gpu-provisioner does not manage Karpenter NodeClaims (they don't have `kaito.sh/workspace` labels).

No cross-interference is possible.

#### Migration Path

1. Install Azure Karpenter alongside gpu-provisioner.
2. Update KAITO controller to use the Karpenter provisioner interface for all new resources.
3. gpu-provisioner **stops creating new resources** — retained solely for legacy cleanup.
4. Existing workspaces continue with gpu-provisioner NodeClaims — no disruption.
5. New InferenceSets and Workspaces use Azure Karpenter exclusively. Per-Workspace NodePool created automatically, referencing global AKSNodeClass.
6. Legacy NodeClaim GC controller cleans up leaked NodeClaims from gpu-provisioner era.
7. Once all gpu-provisioner NodeClaims are deleted (either naturally, via GC, or via accelerated migration), gpu-provisioner can be uninstalled.

### Deletion Strategy

**When deleting a Workspace (InferenceSet scale-down or standalone deletion):**

```
if workspace has legacy gpu-provisioner NodeClaims (kaito.sh/workspace label):
    → directly delete those NodeClaims (gpu-provisioner cleanup interface)

// Delete the per-Workspace NodePool
→ delete NodePool (<workspace-namespace>-<workspace-name>)
    → Karpenter cascades via OwnerReference: NodeClaim → Node → Azure VM

// Global AKSNodeClass is NOT deleted (it is pre-installed and shared)
```

**When deleting an InferenceSet:**

```
→ delete all child Workspaces (existing InferenceSet deletion logic)
    → each Workspace deletion triggers its own cleanup (see above)
```

Key points:
- **Legacy NodeClaims**: KAITO controller directly deletes legacy NodeClaims with `kaito.sh/workspace` label during Workspace deletion. The Legacy NodeClaim GC controller also handles leaked NodeClaims asynchronously.
- **Per-Workspace NodePool deletion**: Deleting a NodePool cascades cleanup to exactly that Workspace's NodeClaims/Nodes/VMs. No ambiguity about which nodes are removed.
- **InferenceSet deletion**: Follows existing logic — deletes all child Workspaces. Each Workspace deletion handles its own NodePool cleanup. No InferenceSet-level NodePool management needed.
- **AKSNodeClass**: Never deleted at runtime. Global AKSNodeClasses persist across all Workspace lifecycles.

## Known Issues

### GPU Zonal Provisioning Failure

**Problem**: GPU SKUs such as A100 and A10 are reported as zone-capable by the Azure Resource API, but **explicit zonal VM creation frequently fails** while **non-zonal (regional) creation succeeds**. This is because available GPU capacity often exists only in regional (non-zone-mapped) hardware pools. Azure Karpenter currently treats zone-listed SKUs as zonal-only and has **no fallback from zonal to non-zonal provisioning**, causing GPU NodeClaim creation to fail.

**Impact on KAITO**: This is a **critical blocker** for KAITO's migration. KAITO workloads are predominantly GPU-based (A100, H100, A10), and the inability to provision these SKUs reliably through Karpenter makes the migration non-viable without a workaround.

**Proposed solution in Azure Karpenter**: Introduce an opt-in NodePool requirement:

```
karpenter.azure.com/zone-placement = zonal | regional
```

- `zonal` — current default; Karpenter pins VM to a specific zone.
- `regional` — Karpenter creates VM without specifying a zone, allowing Azure RP to place it in any available hardware pool.

Regional nodes would surface as zone `"0"` in Kubernetes, preserving compatibility with topology spread constraints. This approach mirrors AKS behavior but provides more flexibility since Karpenter can mix zonal and regional NodePools.

**KAITO's approach**: KAITO sets `karpenter.azure.com/zone-placement=regional` as a default requirement on all KAITO-managed NodePools. This ensures GPU NodeClaims are provisioned non-zonally, avoiding the capacity issue. If Azure Karpenter has not yet implemented this requirement, KAITO can use a temporary annotation or label as a short-term workaround while the formal API is being designed.

**Status**: Pending Azure Karpenter support. The Azure Karpenter team has acknowledged the issue and is evaluating the `zone-placement` requirement approach.

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| `replicas` field is alpha in Karpenter | Verify the deployed Azure Karpenter version has this feature enabled |
| `IsDrifted()` behavior changes in future Azure Karpenter versions | Pin version; add integration tests for drift behavior |
| Concurrent drift replacement across InferenceSet Workspaces | Per-Workspace NodePools default to `budgets.nodes: "0"`; KAITO drift orchestrator toggles one Workspace at a time |
| Many per-Workspace NodePools in large clusters | Deterministic naming + labels for filtering; garbage collection on owner deletion. NodePool is lightweight (no heavyweight state) |
| GPU zonal provisioning failures (A100, A10) | Set `karpenter.azure.com/zone-placement=regional` on KAITO NodePools; track Azure Karpenter support for this requirement |
| Legacy NodeClaim leaks during migration | Legacy NodeClaim GC controller detects and cleans up orphaned/superseded NodeClaims |
| `budgets.nodes: "0"` semantics change upstream | Monitor Karpenter releases; e2e tests for budget behavior; this is a well-documented feature |
| Drift signals are global (all KAITO NodeClaims affected simultaneously) | Budget-controlled drift orchestration ensures upgrades happen one Workspace at a time per InferenceSet |

## Alternatives

### Alternative A: KAITO Directly Creates and Manages NodeClaims

**Description**: KAITO creates NodeClaims directly (instead of using NodePool `replicas`), manages the full NodeClaim lifecycle, and uses `karpenter.sh/do-not-disrupt=true` annotation to prevent Karpenter from autonomously replacing nodes. KAITO would watch the `Drifted` status condition and orchestrate a manual create-before-delete node replacement.

**Why this was not chosen**:

1. **Duplicates Karpenter functionality**: KAITO would need to implement its own NodeClaim lifecycle management (create, monitor ready, replace on drift, delete), which duplicates what Karpenter's static provisioning and disruption controllers already provide.
2. **Complex drift handling**: KAITO would need to implement a full create-before-delete replacement workflow: detect drift → create a replacement NodeClaim → wait for ready + image pull → cordon + drain the drifted node → delete the drifted NodeClaim → reschedule. This is complex and error-prone.
3. **Higher maintenance burden**: Any changes to Karpenter's NodeClaim API or disruption semantics (e.g., changes to `do-not-disrupt` annotation behavior) would require corresponding KAITO changes.
4. **NodePool `limits: 0` and `budgets: nodes: "0"` are fragile**: These settings to prevent Karpenter interference may not be stable/supported long-term.

**Decision**: Rejected — Using NodePool `replicas` for static provisioning and Karpenter's built-in disruption controller for drift handling is simpler, more maintainable, and leverages Karpenter's native capabilities.

### Alternative B: Shared NodePool per InferenceSet (1 InferenceSet : 1 NodePool)

**Description**: All Workspaces within an InferenceSet share a single NodePool with `replicas = inferenceSet.Spec.Replicas × workspace.Status.TargetNodeCount`. Scale-down is achieved by patching NodePool `replicas`.

**Why this was not chosen**:

1. **Ambiguous scale-down**: When reducing `replicas`, Karpenter's static deprovisioning controller selects which NodeClaims to delete. Since all NodeClaims in the shared NodePool have identical labels (no workspace-level distinction), Karpenter may delete NodeClaims belonging to Workspaces that should be retained.
2. **No Workspace-NodeClaim mapping**: NodeClaims created by Karpenter from the NodePool template do not carry workspace-specific labels. KAITO cannot determine which NodeClaim belongs to which Workspace without maintaining a separate mapping (e.g., via Pod → Node → NodeClaim chain), which is fragile and adds complexity.
3. **Workarounds are complex**: Mitigations such as pre-deleting specific NodeClaims before patching replicas, or using `do-not-disrupt` annotations on surviving NodeClaims, all introduce race conditions between KAITO and Karpenter's reconcile loop.

**Decision**: Rejected — Per-Workspace NodePool ensures deterministic scale-down by cascading NodePool deletion to exactly the target Workspace's NodeClaims.

## Notes

1. **`replicas` field is alpha**: Ensure the deployed Azure Karpenter version has this feature enabled.
2. **Static NodePool cannot be converted to Dynamic**: Once `replicas` is set at creation, it cannot be removed (CEL validation constraint). KAITO's use case does not require this conversion.
3. **Per-Workspace NodePool count**: An InferenceSet with `replicas=N` creates N NodePools. NodePool is a lightweight CRD — Karpenter is designed to handle many NodePools in a cluster.
4. **`workspace.Status.TargetNodeCount` is immutable**: NodePool `replicas` is set once at creation and never patched. InferenceSet scale-up/down is handled by creating/deleting NodePools, not by patching replicas.
5. **Drift orchestration is InferenceSet-scoped**: The budget toggle mechanism only applies to NodePools belonging to the same InferenceSet. Standalone Workspace NodePools allow drift replacement by default (`budgets.nodes: "1"`).
6. **Global AKSNodeClass**: Two pre-installed AKSNodeClasses (`image-family-ubuntu`, `image-family-azure-linux`) simplify the design — KAITO never creates or deletes AKSNodeClass at runtime. The trade-off is that drift signals are global (not scoped per InferenceSet), but budget-controlled drift orchestration mitigates the blast radius.
7. **Legacy NodeClaim GC**: The GC controller is a safety net for leaked legacy NodeClaims. It is not on the critical path — normal cleanup happens via `DeprovisionNodes()` during Workspace/InferenceSet deletion.

## Implementation History

- 2026-04-07: Initial proposal created.
- 2026-04-09: Revised to v2 — switched from KAITO-managed NodeClaims to Karpenter static provisioning via NodePool replicas; delegated drift handling to Karpenter; simplified gpu-provisioner to cleanup-only role.
- 2026-04-09: Revised to v3 — per-Workspace NodePool with shared AKSNodeClass for clean scale-down; added InferenceSet-level drift orchestration via budget toggling.
- 2026-04-10: Revised to v4 — global pre-installed AKSNodeClasses (image-family-ubuntu, image-family-azure-linux); removed per-scope AKSNodeClass creation/deletion; added Legacy NodeClaim GC controller.
