---
title: Migrate Node Provisioner from gpu-provisioner to Azure Karpenter
authors:
  - "@rambohe"
reviewers:
  - "@TBD"
creation-date: 2026-04-07
last-updated: 2026-04-07
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
  - [NodePool and AKSNodeClass Scoping Strategy](#nodepool-and-aksnodeclass-scoping-strategy)
    - [Scoping Rules](#scoping-rules)
    - [Naming Conventions](#naming-conventions)
    - [Resource Ownership and Lifecycle](#resource-ownership-and-lifecycle)
    - [Handling Existing (Legacy) Workspaces](#handling-existing-legacy-workspaces)
  - [Resource Definitions](#resource-definitions)
    - [NodePool Configuration](#nodepool-configuration)
    - [AKSNodeClass Configuration](#aksnodeclass-configuration)
    - [NodeClaim Configuration](#nodeclaim-configuration)
  - [KAITO Controller Changes](#kaito-controller-changes)
    - [NodeClaim Lifecycle Management](#nodeclaim-lifecycle-management)
    - [Drift Detection and Node Upgrade](#drift-detection-and-node-upgrade)
    - [Preventing Azure Karpenter Interference](#preventing-azure-karpenter-interference)
  - [gpu-provisioner and Azure Karpenter Coexistence](#gpu-provisioner-and-azure-karpenter-coexistence)
- [Risks and Mitigations](#risks-and-mitigations)
- [Alternatives](#alternatives)
- [Implementation History](#implementation-history)

## Glossary

- **gpu-provisioner**: The current node provisioner used by KAITO on Azure, a fork of the early Karpenter project, that provisions GPU nodes based on Machine/NodeClaim CRDs.
- **Azure Karpenter**: The official Azure provider for [Karpenter](https://karpenter.sh/) (`karpenter-provider-azure`), which implements the Karpenter CloudProvider interface for Azure AKS clusters.
- **NodeClaim**: A Karpenter CRD (`karpenter.sh/v1/NodeClaim`) that represents a request for a single node. It is the atomic unit of node provisioning.
- **NodePool**: A Karpenter CRD (`karpenter.sh/v1/NodePool`) that defines a group of nodes with shared configuration, constraints, and disruption policies.
- **AKSNodeClass**: An Azure-specific Karpenter CRD (`karpenter.azure.com/v1beta1/AKSNodeClass`) that defines Azure-specific node configuration (image family, OS disk size, network, etc.).
- **Drift**: A Karpenter mechanism that detects when a NodeClaim's actual state has diverged from its desired state (e.g., Kubernetes version upgrade, node image update).
- **Standalone NodeClaim**: A NodeClaim without the `karpenter.sh/nodepool` label, which is not associated with any NodePool and is not subject to Karpenter's disruption logic.
- **NAP**: Node Auto-Provisioning — KAITO's feature for automatically creating nodes for workspaces.
- **InferenceSet**: A KAITO CRD (`kaito.sh/v1alpha1/InferenceSet`) that manages a group of identical Workspace replicas for horizontal scaling of inference workloads. It creates and manages multiple Workspaces sharing the same model and configuration.

## Summary

Migrate KAITO's node provisioning from gpu-provisioner to Azure Karpenter. KAITO retains full lifecycle management of NodePool, AKSNodeClass, and NodeClaim, while Azure Karpenter only provisions/deletes VMs and provides drift signals. Per-scope NodePool/AKSNodeClass isolation ensures drift signals are scoped per InferenceSet or standalone Workspace. The proposal also addresses gpu-provisioner coexistence during gradual migration.

## Motivation

gpu-provisioner is a KAITO-specific fork with no drift detection, no node upgrade capability, and high maintenance burden. Azure Karpenter is the officially supported Azure node provisioner with drift detection (`K8sVersionDrift`, `ImageDrift`, `NodeClassDrift`), AKS Machine API integration, and active community support. Migrating enables automated node upgrades and aligns with the AKS ecosystem.

### Goals

1. KAITO manages NodeClaim lifecycle (create, monitor, delete). Azure Karpenter only provisions VMs and provides drift signals.
2. Drift-based node upgrade: KAITO watches `Drifted` condition → creates replacement → waits for ready + base image pulled → deletes drifted NodeClaim.
3. Azure Karpenter's consolidation, emptiness, expiration, and auto-scaling must not affect KAITO-managed NodeClaims.
4. Per-scope NodePool/AKSNodeClass: each InferenceSet and standalone Workspace gets its own pair.
5. gpu-provisioner coexistence: existing NodeClaims remain under gpu-provisioner; new ones use Azure Karpenter.
6. Zero Azure Karpenter modification — all integration via existing extension points.

### Non-Goals

1. Replacing Karpenter's pod-driven autoscaling for general workloads.
2. Multi-cloud migration (AWS is out of scope).
3. In-place node updates (this proposal uses replace-based upgrade).
4. Automatic gpu-provisioner retirement.

## Proposal

### Design Principles

1. **KAITO owns the lifecycle**: KAITO creates, monitors, and deletes NodePool, AKSNodeClass, and NodeClaim. Azure Karpenter is a reactive executor.
2. **Real NodePool for drift signals**: NodeClaims must reference a real NodePool → AKSNodeClass chain, because both `IsDrifted()` and the disruption sub-controller skip NodeClaims without a valid NodePool label.
3. **Per-scope isolation**: Each InferenceSet / standalone Workspace gets its own NodePool + AKSNodeClass pair for scoped drift signals.
4. **Do-not-disrupt for protection**: The `karpenter.sh/do-not-disrupt=true` annotation blocks Karpenter's high-level disruption controller while allowing drift condition setting.
5. **NodeClassRef-based coexistence**: Azure Karpenter's `IsManaged()` only processes `AKSNodeClass` NodeClaims, automatically ignoring gpu-provisioner's `KaitoNodeClass` NodeClaims.

### Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                KAITO Controllers (Workspace + InferenceSet)          │
│                                                                      │
│  • Create scoped NodePool + AKSNodeClass per InferenceSet/Workspace  │
│  • Create NodeClaim → Wait Ready → Schedule pods                     │
│  • Watch Drifted condition → Replace drifted nodes                   │
│  • On deletion: cleanup NodeClaims, NodePool, AKSNodeClass           │
└──────────┬──────────────────────┬─────────────────────┬──────────────┘
           │ Create/Delete        │ Watch Status        │ Watch Drifted
           │ NodeClaim            │ Conditions          │ Condition
           ▼                      ▼                     ▼
┌──────────────────────────────────────────────────────────────────────┐
│                     Kubernetes API Server                            │
│                                                                      │
│  NodePool (karpenter.sh/v1)  ── per InferenceSet / Workspace        │
│  AKSNodeClass (karpenter.azure.com/v1beta1)  ── per scope           │
│  NodeClaim (karpenter.sh/v1)  ── per GPU node                       │
└──────────┬──────────────────────┬─────────────────────┬──────────────┘
           │ Lifecycle            │ Drift Detection     │ ✗ Blocked
           │ Controller           │ (sub-controller)    │
           ▼                      ▼                     │
┌──────────────────────────────────────────────────────────────────────┐
│                 Azure Karpenter (NO modifications)                   │
│                                                                      │
│  ✅ Lifecycle Controller                                             │
│     Watches managed NodeClaims → Create/Delete Azure VMs             │
│                                                                      │
│  ✅ Disruption Sub-Controller                                        │
│     Calls IsDrifted() → Sets Drifted condition on NodeClaim status   │
│                                                                      │
│  ✗ High-Level Disruption Controller                                  │
│     BLOCKED by karpenter.sh/do-not-disrupt annotation                │
│                                                                      │
│  ✗ Provisioner (Auto-Scaling)                                        │
│     BLOCKED by NodePool limits=0                                     │
└──────────────────────────────────────────────────────────────────────┘
```

### NodePool and AKSNodeClass Scoping Strategy

Azure Karpenter's drift detection requires a real NodePool → AKSNodeClass chain. The scoping strategy determines the blast radius of drift signals.

#### Scoping Rules

| Workspace Origin | NodePool / AKSNodeClass Scope | Owner |
|-----------------|------------------------------|-------|
| InferenceSet `foo` | All child Workspaces share one NodePool + AKSNodeClass | InferenceSet `foo` |
| InferenceSet `bar` | Separate NodePool + AKSNodeClass | InferenceSet `bar` |
| Standalone Workspace `ws-a` | Dedicated NodePool + AKSNodeClass | Workspace `ws-a` |

InferenceSet Workspaces share because they run the same model — they should drift and upgrade together. Different InferenceSets and standalone Workspaces are isolated so that changing one AKSNodeClass does not trigger drift on unrelated nodes.

#### Naming Conventions

| Resource | NodePool Name | AKSNodeClass Name |
|----------|-------------|-------------------|
| InferenceSet `foo` in `default` ns | `kaito-is-default-foo` | `kaito-nc-is-default-foo` |
| Standalone Workspace `ws-a` in `default` ns | `kaito-ws-default-ws-a` | `kaito-nc-ws-default-ws-a` |

Names use prefix `kaito-`, scope indicator (`is-`/`ws-`), namespace, and resource name. If exceeding 253 characters, a truncated name with hash suffix is used.

#### Resource Ownership and Lifecycle

- **InferenceSet path**: NodePool/AKSNodeClass created when the first child Workspace needs a NodeClaim. Deleted by InferenceSet finalizer after all child Workspaces are cleaned up.
- **Standalone Workspace path**: NodePool/AKSNodeClass created when the Workspace first needs a NodeClaim. Deleted by Workspace finalizer.

Ownership labels on NodePool and AKSNodeClass: `kaito.sh/managed=true`, `kaito.sh/owner-kind`, `kaito.sh/owner-name`, `kaito.sh/owner-namespace`.

#### Handling Existing (Legacy) Workspaces

- Existing gpu-provisioner NodeClaims (with `KaitoNodeClass` reference) remain as-is. Azure Karpenter ignores them via the `IsManaged()` filter.
- When a legacy workspace needs new NodeClaims (scale-up, spec change), KAITO creates them via the Azure Karpenter path and establishes the scoped NodePool/AKSNodeClass at that point.
- No forced re-provisioning. Migration is purely additive — old resources stay on gpu-provisioner until naturally replaced.

### Resource Definitions

#### NodePool Configuration

**Example: NodePool for InferenceSet `llama-serving` in namespace `default`:**

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: kaito-is-default-llama-serving
  labels:
    kaito.sh/managed: "true"
    kaito.sh/owner-kind: InferenceSet
    kaito.sh/owner-name: llama-serving
    kaito.sh/owner-namespace: default
spec:
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: Never          # Disable consolidation
    budgets:
      - nodes: "0"                   # Zero budget = no autonomous disruption actions
  limits:
    cpu: "0"                         # Prevent Karpenter from auto-creating NodeClaims
    memory: "0"
  template:
    metadata:
      labels:
        kaito.sh/managed: "true"
    spec:
      nodeClassRef:
        name: kaito-nc-is-default-llama-serving
        kind: AKSNodeClass
        group: karpenter.azure.com
      taints:
        - key: sku
          value: gpu
          effect: NoSchedule
```

Key settings: `budgets.nodes: "0"` blocks autonomous disruption actions; `limits: 0` prevents auto-scaling; `consolidateAfter: Never` disables consolidation. The drift condition is still set on NodeClaim status regardless of these settings.

#### AKSNodeClass Configuration

**Example: AKSNodeClass for InferenceSet `llama-serving` in namespace `default`:**

```yaml
apiVersion: karpenter.azure.com/v1beta1
kind: AKSNodeClass
metadata:
  name: kaito-nc-is-default-llama-serving
  labels:
    kaito.sh/managed: "true"
    kaito.sh/owner-kind: InferenceSet
    kaito.sh/owner-name: llama-serving
    kaito.sh/owner-namespace: default
  annotations:
    kubernetes.io/description: "AKSNodeClass for InferenceSet llama-serving"
spec:
  imageFamily: Ubuntu2204
  osDiskSizeGB: 128
```

Drift sources: changing `imageFamily` triggers `NodeClassDrift`; AKS K8s version upgrade triggers `K8sVersionDrift`; Azure node image gallery update triggers `ImageDrift`. All drift signals affect **all** NodeClaims scoped to this AKSNodeClass.

#### NodeClaim Configuration

NodeClaims reference the scoped NodePool and AKSNodeClass.

**Example: NodeClaim for a Workspace created by InferenceSet `llama-serving`:**

```yaml
apiVersion: karpenter.sh/v1
kind: NodeClaim
metadata:
  name: ws<hash>
  labels:
    karpenter.sh/nodepool: kaito-is-default-llama-serving   # Scoped NodePool
    kaito.sh/workspace: <workspace-name>
    kaito.sh/workspacenamespace: <workspace-namespace>
    kaito.sh/managed: "true"
  annotations:
    karpenter.sh/do-not-disrupt: "true"     # Prevents autonomous disruption
    kaito.sh/node-image-family: ubuntu
spec:
  nodeClassRef:
    name: kaito-nc-is-default-llama-serving  # Scoped AKSNodeClass
    kind: AKSNodeClass
    group: karpenter.azure.com
  taints:
    - key: sku
      value: gpu
      effect: NoSchedule
  requirements:
    - key: karpenter.sh/nodepool
      operator: In
      values: ["kaito-is-default-llama-serving"]
    - key: node.kubernetes.io/instance-type
      operator: In
      values: ["Standard_NC24ads_A100_v4"]
    - key: kubernetes.io/os
      operator: In
      values: ["linux"]
    - key: karpenter.azure.com/sku-name
      operator: In
      values: ["Standard_NC24ads_A100_v4"]
```

**Key changes from current NodeClaim generation:**
1. `karpenter.sh/nodepool` → Points to a real, scoped NodePool (enables drift detection).
2. `spec.nodeClassRef` → Changed to `Kind=AKSNodeClass, Group=karpenter.azure.com`.
3. `karpenter.sh/do-not-disrupt: "true"` → Retained (blocks disruption, not drift detection).
4. New label `kaito.sh/managed: "true"` → For coexistence filtering.

### KAITO Controller Changes

#### NodeClaim Lifecycle Management

The core flow (create NodeClaim → wait ready → schedule workload → delete on removal) is unchanged. Key modifications:

1. **Scoped resource management**: Before creating NodeClaims, the controller ensures the scoped NodePool and AKSNodeClass exist (idempotent create-if-absent). Scope is determined by the `inferenceset.kaito.sh/created-by` label.
2. **Updated NodeClaim generation**: `nodeClassRef` points to `AKSNodeClass`; `karpenter.sh/nodepool` label uses the scoped name; `kaito.sh/managed=true` label added.
3. **Wait for node ready**: Existing flow (`EnsureNodeClaimsReady` → `EnsureNodesReady`) works as-is.

#### Drift Detection and Node Upgrade

KAITO watches the `Drifted` status condition on NodeClaims and orchestrates a **create-before-delete** node replacement:

1. **Detect drift**: NodeClaim update event (Drifted=True) triggers workspace reconciliation.
2. **Create replacement**: New NodeClaim referencing the same scoped NodePool/AKSNodeClass, labeled with `kaito.sh/replaces-nodeclaim=<drifted-name>`.
3. **Wait for ready + base image pulled**: Reuse existing `EnsureNodeClaimsReady()` / `EnsureNodesReady()`, plus verify model container image is available on the new node.
4. **Cordon + drain drifted node**: Evict pods gracefully.
5. **Delete drifted NodeClaim**: Azure Karpenter's lifecycle controller handles VM deletion.
6. **Reschedule**: StatefulSet controller reschedules pods to the new node.

When an InferenceSet's AKSNodeClass changes, **all** NodeClaims in that scope are marked drifted. KAITO performs a rolling upgrade across Workspaces, replacing nodes one at a time.

#### Preventing Azure Karpenter Interference

| Protection Layer | What It Prevents |
|-----------------|------------------|
| `karpenter.sh/do-not-disrupt: "true"` annotation | High-level disruption controller cannot select KAITO nodes for consolidation, emptiness, or drift replacement |
| `budgets.nodes: "0"` on NodePool | Zero budget prevents any autonomous disruption action |
| `limits: cpu: "0", memory: "0"` on NodePool | Karpenter's auto-scaler cannot create new NodeClaims |
| `consolidateAfter: Never` on NodePool | Disables time-based consolidation |

**Key**: The `do-not-disrupt` annotation blocks the high-level disruption controller (which deletes nodes) but does **NOT** block the NodeClaim disruption sub-controller (which only sets the `Drifted` status condition). This is the mechanism that allows drift detection to work while preventing autonomous node replacement.

### gpu-provisioner and Azure Karpenter Coexistence

The two systems are isolated by `spec.nodeClassRef`. Azure Karpenter's `IsManaged()` only processes `AKSNodeClass` NodeClaims; gpu-provisioner only processes `KaitoNodeClass` NodeClaims — no conflicts.

During coexistence, KAITO counts ready NodeClaims from both backends but creates **new** ones exclusively via Azure Karpenter. Drift handling only applies to `kaito.sh/managed=true` NodeClaims.

#### Migration Path

1. Install Azure Karpenter alongside gpu-provisioner.
2. Update KAITO controller to generate AKSNodeClass-based NodeClaims.
3. Existing workspaces continue on gpu-provisioner — no disruption.
4. New workspaces and scale-up use Azure Karpenter. Scoped NodePool/AKSNodeClass created automatically.
5. Once all gpu-provisioner NodeClaims are naturally replaced, gpu-provisioner can be uninstalled.

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| `IsDrifted()` behavior changes in future Azure Karpenter versions | Pin version; add integration tests for drift behavior |
| `do-not-disrupt` annotation semantics change upstream | Monitor Karpenter releases; e2e tests for disruption protection |
| Frequent node replacements from drift signals | KAITO controls when to act; add delay/rate-limiting |
| Race condition in drift replacement | `kaito.sh/replaces-nodeclaim` label for idempotent tracking; ControllerExpectations mechanism |
| Many scoped NodePools in large clusters | Deterministic naming + labels for filtering; garbage collection on owner deletion |

## Alternatives

### Alternative: Delegate Node Upgrade Entirely to Azure Karpenter

**Description**: Remove the `do-not-disrupt` annotation and let Azure Karpenter's disruption controller handle drifted node replacement autonomously.

**Why this is not feasible**:

Karpenter's disruption controller does not directly recreate the same NodeClaim. It deletes the drifted NodeClaim and relies on the provisioner's scheduling loop to create a replacement. The provisioner selects the best-fit instance type by evaluating **all** NodePools in the cluster — not just the one the drifted NodeClaim belonged to — using cost-optimization and bin-packing algorithms. The replacement instance type may differ from the Workspace's specified `resource.instanceType`.

This replacement path also bypasses KAITO's NodeClaim generation logic entirely. The new NodeClaim will lack KAITO-specific labels (`kaito.sh/workspace`, `kaito.sh/workspacenamespace`), taints, and the `do-not-disrupt` annotation, breaking KAITO's ability to track nodes, manage lifecycle via `ControllerExpectations`, protect nodes from subsequent consolidation, and ensure the correct GPU SKU.

**Decision**: Rejected — Karpenter's disruption pipeline cannot handle KAITO's requirements around specific instance type selection, workspace-scoped label tracking, and controlled replacement orchestration. Using Karpenter only for drift **detection** while KAITO handles drift **response** is the correct separation of concerns.

## Implementation History

- 2026-04-07: Initial proposal created.
