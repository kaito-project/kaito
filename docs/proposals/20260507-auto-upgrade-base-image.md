---
title: Auto-Upgrade Support for Base Serving Image in InferenceSet
authors:
  - "@zhehaoli"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2026-05-07
last-updated: 2026-05-07
status: provisional
---

# Auto-Upgrade Support for Base Serving Image in InferenceSet

- [Summary](#summary)
- [Motivation](#motivation)
  - [Goals](#goals)
  - [Non-Goals/Future Work](#non-goalsfuture-work)
- [Proposal](#proposal)
  - [API Changes](#api-changes)
  - [Design Decisions](#design-decisions)
    - [Decision 1: Version Discovery Mechanism](#decision-1-version-discovery-mechanism)
    - [Decision 2: Rollout Strategy](#decision-2-rollout-strategy)
  - [Implementation Details](#implementation-details)
    - [Version Discovery Flow](#version-discovery-flow)
    - [Canary Rollout Flow](#canary-rollout-flow)
    - [Failure Handling and Rollback](#failure-handling-and-rollback)
    - [Status Reporting](#status-reporting)
  - [Risks and Mitigations](#risks-and-mitigations)
  - [Test Plan](#test-plan)
- [Implementation History](#implementation-history)

## Summary

KAITO users running inference workloads via InferenceSet have no built-in mechanism to
upgrade the underlying serving stack (base image containing vLLM runtime, system
dependencies, and security patches) without manual intervention and potential downtime.

This proposal adds an `autoUpgrade` field to `InferenceSetSpec` that enables the
InferenceSet controller to periodically detect new base image versions and roll them
out using a canary strategy — ensuring zero-downtime upgrades, automatic failure
detection, and clear observability.

The scope is limited to the **base serving image** (`kaito-base`). Model weights are
managed separately (downloaded at runtime or via OCI artifacts) and are unaffected by
base image upgrades.

## Motivation

### Why Auto-Upgrade?

1. **CVE remediation** — Security vulnerabilities in base images (Ubuntu, Python, CUDA)
   or the vLLM runtime require timely patching. Today, users must manually rebuild and
   redeploy to address CVEs, leaving a window of exposure.

2. **Runtime improvements** — New vLLM releases bring performance optimizations (better
   batching, memory management, kernel improvements) that directly translate to higher
   throughput and lower latency. Users should benefit from these improvements
   seamlessly.

3. **Operational burden** — In production environments with multiple InferenceSets,
   manually tracking and rolling out base image updates is error-prone and
   time-consuming.

### Current State

Today, the base image version is statically embedded in the KAITO controller binary via
`supported_models.yaml`:

```yaml
models:
  - name: base
    type: text-generation
    runtime: tfs
    tag: 0.3.0  # ← pinned at controller build time
```

This tag is resolved at runtime via `GetBaseImageName()` in
`pkg/workspace/inference/preset_inferences.go`, which constructs the full image
reference as `{PRESET_REGISTRY_NAME}/kaito-base:{tag}`. Changing the base image
version requires rebuilding and redeploying the KAITO controller itself — a disruptive
operation that affects all workloads cluster-wide.

#### Existing `UpdateStrategy` Field

`InferenceSetSpec` already declares an `UpdateStrategy` field typed as
`appsv1.StatefulSetUpdateStrategy` (default `{"type":"RollingUpdate","rollingUpdate":
{"maxUnavailable":1}}`). This field was scaffolded to control how existing Workspaces
are replaced when the InferenceSet *spec template* changes (analogous to
`StatefulSet.spec.updateStrategy`). However, a code reference analysis shows that
**this field is currently unused** — the InferenceSet controller never reads
`spec.updateStrategy` during reconciliation. Workspace replacement logic in
`addOrUpdateInferenceSet()` is hardcoded: it counts excess Workspaces, deletes
non-ready ones first, then deletes ready ones, with no reference to the strategy field.

The auto-upgrade proposal introduces `AutoUpgradeStrategy` as a *separate* concern:
- **`UpdateStrategy`** (existing, unused) — intended for **user-initiated spec
  changes** (e.g., changing the model preset or resource template). When wired up in
  the future, it would control how the controller rolls out a new generation of
  Workspaces after a spec change.
- **`AutoUpgradeStrategy`** (proposed) — controls **automatic base image upgrades**
  detected by the controller, independent of any spec change.

These two strategies are orthogonal. A user may change the InferenceSet template (which
triggers `UpdateStrategy` semantics) while an auto-upgrade rollout is in progress. The
controller should serialize these operations: a spec-change rollout takes priority and
pauses the auto-upgrade until the spec change is fully rolled out.

### Goals

- Enable opt-in automatic base image upgrades for InferenceSet workloads.
- Guarantee zero-downtime during upgrades (at least `spec.replicas` pods serve traffic
  at all times).
- Detect and halt failed upgrades automatically, preserving serving capacity.
- Provide clear status reporting so users and automation can observe upgrade progress.
- Support upgrade channels (`stable`, `latest`) to let users control risk tolerance.

### Non-Goals/Future Work

- **Automatic rollback** — On upgrade failure, the controller halts the rollout and
  cleans up the failed canary. Full automatic rollback (reverting already-upgraded
  replicas) is deferred to a future proposal.
- **Model weight upgrades** — Model weights are orthogonal to the base image; they are
  downloaded at runtime or pulled as OCI artifacts. A separate mechanism should handle
  model version pinning.
- **Workspace-level auto-upgrade** — This proposal targets InferenceSet only. Standalone
  Workspace CRs do not have replica management and cannot safely perform canary
  rollouts. Workspace-level upgrades can be considered in a follow-up.
- **Cross-InferenceSet coordination** — Coordinating upgrades across multiple
  InferenceSets (e.g., "upgrade staging first, then production") is out of scope.

## Proposal

### API Changes

#### New Types

```go
// AutoUpgradePolicy configures automatic base image upgrade behavior.
type AutoUpgradePolicy struct {
    // Enabled controls whether the controller automatically upgrades
    // Workspace replicas when a new base image version is available.
    // +optional
    // +kubebuilder:default:=false
    Enabled bool `json:"enabled"`

    // Channel specifies the upgrade channel.
    // "stable" — only stable, tested releases (recommended for production)
    // "latest" — most recent release (for staging/development)
    // +optional
    // +kubebuilder:default:="stable"
    // +kubebuilder:validation:Enum=stable;latest
    Channel string `json:"channel,omitempty"`

    // PollInterval specifies how often the controller checks for new versions.
    // Defaults to 24h. Minimum is 1h.
    // +optional
    PollInterval *metav1.Duration `json:"pollInterval,omitempty"`

    // Strategy specifies how replicas are replaced during an upgrade.
    // Defaults to Canary. The strategy type determines which strategy-specific
    // fields are honored.
    // +optional
    // +kubebuilder:default:={type:"Canary"}
    Strategy AutoUpgradeStrategy `json:"strategy,omitempty"`

    // MaintenanceWindow restricts when upgrades may be applied.
    // If not specified, upgrades may be applied at any time.
    // +optional
    MaintenanceWindow *MaintenanceWindow `json:"maintenanceWindow,omitempty"`
}

// AutoUpgradeStrategy defines the strategy used to replace old replicas
// with upgraded ones. The struct is designed to be extensible — new strategy
// types can be added without breaking existing configurations.
type AutoUpgradeStrategy struct {
    // Type specifies the upgrade strategy.
    // +optional
    // +kubebuilder:default:="Canary"
    // +kubebuilder:validation:Enum=Canary
    Type AutoUpgradeStrategyType `json:"type,omitempty"`

    // Canary holds configuration specific to the Canary strategy.
    // Ignored when Type is not "Canary".
    // +optional
    Canary *CanaryStrategy `json:"canary,omitempty"`
}

// AutoUpgradeStrategyType is a string enum for upgrade strategy types.
// +kubebuilder:validation:Enum=Canary
type AutoUpgradeStrategyType string

const (
    // CanaryStrategyType scales up one new replica, validates it, then
    // removes one old replica. Repeats until all replicas are upgraded.
    // Guarantees zero downtime (serving capacity >= spec.replicas at all times).
    CanaryStrategyType AutoUpgradeStrategyType = "Canary"

    // Future strategy types (e.g. BlueGreen, RollingUpgrade)
)

// CanaryStrategy configures the canary upgrade behavior.
// The canary strategy always scales up before scaling down, guaranteeing
// that serving capacity never drops below spec.replicas.
type CanaryStrategy struct {
    // MaxSurge specifies how many canary Workspaces may be created
    // simultaneously during the upgrade. Higher values speed up the
    // rollout at the cost of additional temporary GPU capacity.
    //   maxSurge=1 (default): sequential — one canary at a time.
    //   maxSurge=N: up to N canaries validated in parallel.
    // Each canary must become ready before its corresponding old replica
    // is removed. The total number of Workspaces during rollout is at
    // most spec.replicas + maxSurge.
    // +optional
    // +kubebuilder:default:=1
    // +kubebuilder:validation:Minimum=1
    MaxSurge int `json:"maxSurge,omitempty"`
}

// MaintenanceWindow restricts when auto-upgrades may be applied.
// The controller will only begin a new rollout (or resume a paused one)
// when the current time falls within the specified window.
type MaintenanceWindow struct {
    // Schedule is a cron expression (5-field, UTC) defining when upgrades
    // are permitted to start. The window opens at each cron tick and stays
    // open for Duration.
    // Example: "0 2 * * 6" = every Saturday at 02:00 UTC.
    // +required
    Schedule string `json:"schedule"`

    // Duration specifies how long the maintenance window stays open after
    // each cron tick. If a rollout is still in progress when the window
    // closes, the current canary step is allowed to complete (the controller
    // will not create a new canary until the next window opens).
    // Defaults to 4h.
    // +optional
    // +kubebuilder:default:="4h"
    Duration *metav1.Duration `json:"duration,omitempty"`
}
```

**Maintenance window semantics:**
- **Version discovery** (polling the registry) happens regardless of the window — the
  controller always knows if an update is available and sets `AutoUpgradeAvailable`.
- **Rollout execution** (creating canary Workspaces) only happens inside the window.
- **In-progress steps** are not interrupted: if the window closes while a canary is
  being validated, that step runs to completion. The controller pauses before starting
  the *next* canary step.
- If no `maintenanceWindow` is specified, upgrades may begin at any time (current
  default behavior).

#### InferenceSetSpec Change

```go
type InferenceSetSpec struct {
    // ... existing fields ...

    // AutoUpgrade configures automatic base image upgrade behavior.
    // When enabled, the controller periodically checks for new base image versions
    // and performs a canary rollout to upgrade Workspace replicas with zero downtime.
    // +optional
    AutoUpgrade *AutoUpgradePolicy `json:"autoUpgrade,omitempty"`
}
```

#### New Status Conditions

| Condition Type | Status | Reason | Meaning |
|---|---|---|---|
| `AutoUpgradeAvailable` | `True` | `NewVersionAvailable` | A newer base image version was detected |
| `AutoUpgradeInProgress` | `True` | `CanaryRollout` | Upgrade rollout is actively in progress |
| `AutoUpgradeInProgress` | `False` | `RolloutComplete` | Upgrade rollout completed successfully |
| `AutoUpgradeFailed` | `True` | `CanaryFailed` | Canary replica failed to become ready |

#### New Status Fields

```go
type AutoUpgradeStatus struct {
    // CurrentVersion is the base image tag currently running on all replicas.
    CurrentVersion string `json:"currentVersion,omitempty"`
    // TargetVersion is the base image tag being rolled out (empty if no upgrade in progress).
    TargetVersion string `json:"targetVersion,omitempty"`
    // UpgradedReplicas is the number of replicas running the target version.
    UpgradedReplicas int `json:"upgradedReplicas,omitempty"`
    // LastCheckTime is the timestamp of the last version check.
    LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`
    // LastUpgradeTime is the timestamp of the last successful upgrade completion.
    LastUpgradeTime *metav1.Time `json:"lastUpgradeTime,omitempty"`
}
```

#### Example InferenceSet Manifest

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: my-llm-service
spec:
  replicas: 3
  labelSelector:
    matchLabels:
      app: my-llm
  template:
    resource:
      instanceType: Standard_NC24ads_A100_v4
    inference:
      preset:
        name: llama-3.1-8b-instruct
  autoUpgrade:
    enabled: true
    channel: stable
    pollInterval: 12h
    strategy:
      type: Canary
      canary:
        maxSurge: 1
    maintenanceWindow:
      schedule: "0 2 * * 6"   # Saturdays at 02:00 UTC
      duration: 4h
```

### Design Decisions

#### Decision 1: Version Discovery Mechanism

How does the controller learn that a new base image version is available?

##### Option A: OCI Registry Tag Listing (Recommended)

The controller queries the OCI registry directly using the [OCI Distribution Spec
tags/list API](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#content-discovery)
to enumerate available tags for the `kaito-base` image.

**How it works:**
1. Controller calls `GET /v2/kaito-base/tags/list` against the configured registry.
2. Tags are parsed as semver (e.g., `0.3.0`, `0.3.1`, `0.4.0`).
3. Based on the channel:
   - `stable`: selects the latest tag matching the current major version (e.g., `0.x.y`).
   - `latest`: selects the highest semver tag.
4. If the selected tag differs from the current tag, an upgrade is triggered.

**Pros:**
- No additional infrastructure required — works with any OCI-compliant registry (ACR,
  ECR, GCR, Docker Hub, Harbor).
- Single source of truth — the registry already hosts the images; tag listing is a
  natural extension.
- Low operational overhead — no extra ConfigMaps, CRDs, or external services to maintain.
- Real-time accuracy — always reflects what is actually published.

**Cons:**
- Requires registry credentials if the registry is private (must be configured via
  imagePullSecrets or workload identity).
- No rich metadata beyond the tag string (e.g., no release notes, no CVE annotations).
- Channel semantics must be encoded in tag naming conventions.

##### Option B: Cluster-Local Version Catalog (ConfigMap/CRD)

A dedicated ConfigMap or custom CRD (`BaseImageVersion`) is maintained in the cluster
that advertises available versions.

**How it works:**
1. A CI/CD pipeline or admin pushes a ConfigMap/CRD with available versions.
2. Controller watches this resource and compares against the current version.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kaito-base-versions
  namespace: kaito-system
data:
  stable: "0.3.1"
  latest: "0.4.0"
  versions: |
    - tag: "0.4.0"
      channel: latest
      releaseDate: "2026-05-01"
    - tag: "0.3.1"
      channel: stable
      releaseDate: "2026-04-15"
```

**Pros:**
- Full control over version metadata (release notes, CVE lists, compatibility matrix).
- No registry credentials needed at runtime.
- Can enforce approval workflows (e.g., only promote to `stable` after testing).
- Fast lookups — no network calls to external registries.

**Cons:**
- Requires additional operational overhead — someone/something must maintain the
  ConfigMap/CRD.
- Introduces a synchronization problem — the catalog can drift from the actual registry
  contents (e.g., a version listed but not yet pushed, or pushed but not yet listed).
- Adds a new dependency to the control plane — if the ConfigMap is deleted or
  misconfigured, auto-upgrade silently stops working.
- Not self-service — users cannot simply push a new image to trigger an upgrade.

##### Option C: Annotation-Driven Triggers

An external system (CI/CD, Flux, ArgoCD) patches an annotation on the InferenceSet to
trigger an upgrade.

```yaml
metadata:
  annotations:
    kaito.sh/target-base-image-version: "0.3.1"
```

**Pros:**
- Maximum flexibility — integrates with any GitOps or CI/CD pipeline.
- No polling overhead — event-driven via Kubernetes watch.
- Clear audit trail via annotation history.

**Cons:**
- Not truly "auto" — requires external orchestration.
- Shifts complexity to the user — they must build and maintain the trigger pipeline.
- No built-in version discovery — the external system must know what versions exist.
- Doesn't address the core user request of "set it and forget it" upgrades.

##### Decision: **Option A (OCI Registry Tag Listing)**

**Rationale:** Option A provides the best balance of reliability and user experience.
It requires zero additional infrastructure, uses the registry as the single source of
truth (eliminating drift), and enables true "set and forget" behavior. The cons are
manageable: private registry credentials are already configured for image pulling (via
imagePullSecrets), and semver tag parsing is well-understood. Option B adds operational
burden that undermines the "auto" in auto-upgrade. Option C is not truly automatic.

To address Option A's metadata limitations, we can add an optional annotation on the
image manifest (via OCI annotations) to carry channel metadata, enabling richer
channel semantics in the future without changing the discovery mechanism.

---

#### Decision 2: Rollout Strategy

How does the controller replace old replicas with new ones?

##### Option A: Canary with Scale-Up-First (Recommended)

1. Scale up one additional Workspace with the new base image.
2. Wait for the canary Workspace to reach `InferenceReady` condition.
3. If ready: delete one old Workspace replica.
4. Repeat until all replicas are upgraded.
5. If the canary fails: delete the canary, halt the rollout, set `AutoUpgradeFailed`
   condition.

**Pros:**
- **Zero downtime guaranteed** — at every point during the rollout, at least
  `spec.replicas` pods are serving traffic.
- **Safe failure mode** — a failed canary is destroyed before any old replica is
  removed; the system self-heals to the previous state.
- **Incremental validation** — each new replica is validated before proceeding.
- **Consistent with InferenceSet semantics** — the controller already manages
  Workspace lifecycle (create, delete, status aggregation).

**Cons:**
- **Requires temporary extra capacity** — during rollout, `replicas + 1` nodes may be
  needed. For GPU workloads this means one extra GPU node per upgrade step.
- **Slower rollout** — sequential canary is inherently slower than in-place or
  blue-green. For N replicas, the rollout takes N × (provision + startup) time.
- **Node provisioning latency** — if NAP (Node Auto Provisioner) is used, provisioning
  a new GPU node can take 5–15 minutes per step.

##### Option B: In-Place Rolling Update

Directly update existing Workspace CRs to use the new base image, relying on
StatefulSet's rolling update to cycle pods.

**Pros:**
- No extra capacity needed — pods are replaced in-place.
- Faster — no node provisioning delay for each step.
- Simpler implementation — leverages existing StatefulSet update mechanics.

**Cons:**
- **Downtime risk** — during pod replacement, the replica is unavailable. With
  `maxUnavailable=1`, one replica is always down during the rollout.
- **Blast radius** — if the new image is broken, the replaced replica is lost. Rolling
  back requires another full rollout cycle.
- **Breaks InferenceSet invariant** — InferenceSet manages Workspaces (not
  StatefulSets directly). Mutating a Workspace's image after creation changes the
  contract: a Workspace is supposed to be an immutable unit created from the
  InferenceSet template.
- **Complex state management** — tracking which Workspaces have been updated vs. not
  introduces mixed-version state that complicates status reporting.

##### Option C: Blue-Green (Full Fleet Replacement)

Create an entirely new set of replicas with the new image, then cut over traffic and
delete the old set.

**Pros:**
- Clean cut-over — all replicas switch at once.
- Simple rollback — keep the old set around until the new set is validated.

**Cons:**
- **Requires 2× capacity** — for the duration of the rollout, both old and new fleets
  exist. For GPU workloads this is prohibitively expensive.
- **All-or-nothing risk** — if the new fleet has subtle issues (e.g., performance
  regression), they affect all traffic at cut-over time.
- **Long provisioning time** — standing up N new GPU nodes simultaneously may strain
  the cloud provider quota.

##### Decision: **Option A (Canary with Scale-Up-First)**

**Rationale:** For GPU-based inference workloads, reliability is paramount. A canary
approach guarantees that serving capacity never drops below the desired replica count.
The extra capacity cost (one GPU node at a time) is acceptable because:

1. The upgrade is temporary — extra capacity is released as old replicas are deleted.
2. GPU node provisioning via NAP is already a core KAITO capability.
3. The alternative (in-place rolling update) risks downtime for each step, which is
   unacceptable for production inference endpoints.

The `maxSurge` field (under `strategy.canary`) lets users trade extra GPU capacity for
faster rollouts: `maxSurge: 1` (default) upgrades one replica at a time; higher values
create multiple canaries in parallel, requiring up to `maxSurge` extra GPU nodes but
completing the rollout proportionally faster.

The API uses a discriminated-union `AutoUpgradeStrategy` struct so that other strategies
can be added as an alternative strategy type in a future proposal without
breaking existing configurations. For this initial implementation, only `Canary` is
supported.

---

### Implementation Details

#### Version Discovery Flow

```
┌──────────────────────────────────────────────────┐
│              InferenceSet Controller             │
│                                                  │
│  1. Reconcile triggered (event or RequeueAfter)  │
│  2. Check autoUpgrade.enabled == true            │
│  3. Check lastCheckTime + pollInterval < now     │
│                                                  │
│  4. Query OCI registry:                          │
│     GET /v2/kaito-base/tags/list                 │
│  5. Parse tags as semver, filter by channel      │
│  6. Compare latest available vs current version  │
│                                                  │
│  7. If new version found:                        │
│     - Set AutoUpgradeAvailable=True              │
│     - Set targetVersion in status                │
│     - Begin canary rollout                       │
│  8. RequeueAfter(pollInterval)                   │
└──────────────────────────────────────────────────┘
```

**Channel Semantics:**
- `stable`: Latest patch version within the current minor version. For example, if
  the current version is `0.3.0`, the stable channel will pick `0.3.1` but not `0.4.0`.
  Minor version bumps (which may include breaking vLLM changes) require explicit user
  action.
- `latest`: The highest semver tag available. Suitable for non-production environments
  where users want to track the bleeding edge.

#### Canary Rollout Flow

```
┌─────────────────────────────────────────────────────────┐
│                   Canary Rollout Loop                   │
│                                                         │
│  State: upgradeStatus.targetVersion != currentVersion   │
│                                                         │
│  For each old replica (up to maxSurge at a time):       │
│                                                         │
│  ┌─────────────────────────────────────────────────┐    │
│  │ Step 1: Create canary Workspace                 │    │
│  │  - Same spec as template                        │    │
│  │  - Override base image tag → targetVersion      │    │
│  │  - Label: kaito.sh/upgrade-canary: "true"       │    │
│  │  - Annotation: kaito.sh/target-version: "0.3.1" │    │
│  └──────────────────┬──────────────────────────────┘    │
│                     │                                   │
│  ┌──────────────────▼──────────────────────────────┐    │
│  │ Step 2: Wait for canary to become ready         │    │
│  │  - Watch for InferenceReady=True condition      │    │
│  │  - Timeout: model readiness timeout (default    │    │
│  │    from preset, typically 20–30 min)            │    │
│  └──────────────────┬──────────────────────────────┘    │
│                     │                                   │
│          ┌──────────┴──────────┐                        │
│          │                     │                        │
│     Ready=True            Ready=False / Timeout         │
│          │                     │                        │
│  ┌───────▼───────┐   ┌────────▼────────────────┐        │
│  │ Step 3a:      │   │ Step 3b:                 │       │
│  │ Promote canary│   │ Delete canary            │       │
│  │  - Remove     │   │ Set AutoUpgradeFailed    │       │
│  │    canary label│  │ Emit warning event       │       │
│  │  - Delete one │   │ HALT rollout             │       │
│  │    old replica│   │ RequeueAfter(pollInterval)│      │
│  │               │   └──────────────────────────┘       │
│  │ Continue to   │                                      │
│  │ next replica  │                                      │
│  └───────────────┘                                      │
│                                                         │
│  After all replicas upgraded:                           │
│  - Set currentVersion = targetVersion                   │
│  - Clear targetVersion                                  │
│  - Set AutoUpgradeInProgress=False                      │
│  - Set lastUpgradeTime = now                            │
│  - Emit normal event: "BaseImageUpgraded"               │
└─────────────────────────────────────────────────────────┘
```

**Key Implementation Notes:**

1. **Image Override Mechanism:** The canary Workspace must use a different base image
   tag than what is embedded in `supported_models.yaml`. This requires a new annotation
   on the Workspace CR:

   ```go
   const AnnotationBaseImageOverride = "kaito.sh/base-image-override"
   ```

   When `GetBaseImageName()` is called during pod spec generation, it checks for this
   annotation first. If present, it uses the override tag instead of the embedded
   default. This avoids modifying the global model metadata, which would affect all
   workspaces cluster-wide.

2. **Rollout State Persistence:** The rollout state (which replicas are old vs. new) is
   tracked via labels and annotations on individual Workspace CRs:
   - `kaito.sh/base-image-version: "0.3.1"` — records the base image version used.
   - `kaito.sh/upgrade-canary: "true"` — identifies canary replicas during rollout.

   This approach is crash-safe: if the controller restarts mid-rollout, it can
   reconstruct the rollout state from Workspace labels.

3. **Interaction with HPA/Autoscaler:** During a canary rollout the replica count
   temporarily exceeds `spec.replicas`. The autoscaler must not interpret the extra
   canary capacity as over-provisioning and scale down healthy replicas. The
   controller achieves this with the following invariants:

   **Canary exclusion from metrics:**
   - Canary Workspaces carry label `kaito.sh/upgrade-canary: "true"`.
   - `status.readyReplicas` and `status.replicas` count only non-canary Workspaces.
   - The autoscaler (HPA/KEDA) observes replica counts via `status.replicas` and the
     scale subresource — so it never sees the temporary +1.

   **Effective view from the autoscaler's perspective:**
   ```
   Before:  3 old replicas         → HPA sees 3/3 ready
   Canary:  3 old + 1 canary       → HPA still sees 3/3 (canary excluded)
   Swap:    2 old + 1 upgraded     → HPA sees 3/3
   Done:    3 upgraded             → HPA sees 3/3
   ```

   **Scale-up during rollout:** If the autoscaler increases `spec.replicas` (e.g.,
   3→5) while a rollout is in progress, the controller creates new replicas at the
   **current (old) version** — the old version is production-proven, whereas the
   target version is still being validated by the canary process. Scaling with an
   unvalidated image would undermine the canary's purpose. The newly created
   old-version replicas are then included in the canary rollout and upgraded in
   subsequent steps.

   **Scale-down during rollout:** If the autoscaler decreases `spec.replicas` during a
   rollout, the controller preferentially deletes old-version replicas first (they are
   being replaced anyway). This naturally accelerates the rollout while respecting the
   new desired count.

   **Replica count reconciliation:** At each reconcile iteration the controller
   computes:
   ```
   activeReplicas = count(Workspaces without kaito.sh/upgrade-canary label)
   deficit = spec.replicas - activeReplicas
   ```
   - If `deficit > 0`: create new Workspaces at target version.
   - If `deficit < 0`: delete old-version Workspaces first, then upgraded ones.
   - Canary Workspace count is always 0 to maxSurge, managed solely by the rollout loop.

  4. **Canary Promotion:** Once a canary Workspace is validated (InferenceReady=True),
   the controller **promotes** it to a regular replica before deleting the old one:
   1. Remove the `kaito.sh/upgrade-canary` label from the Workspace.
   2. The Workspace is now counted in `status.replicas` and `status.readyReplicas`,
      making it visible to the autoscaler and the InferenceSet's replica count logic.
   3. Delete one old-version Workspace.

   This two-step sequence (promote then delete) ensures that at no point is the
   promoted replica invisible to the autoscaler. If the controller crashes between
   promotion and deletion, the next reconcile sees `activeReplicas > spec.replicas`
   and resumes by deleting an old-version Workspace.

#### Failure Handling and Rollback

| Failure Scenario | Controller Behavior |
|---|---|
| Canary pod fails to start (CrashLoopBackOff) | Delete canary Workspace, set `AutoUpgradeFailed=True`, halt rollout |
| Canary pod starts but InferenceReady times out | Delete canary Workspace, set `AutoUpgradeFailed=True`, halt rollout |
| Node provisioning fails (NAP quota exceeded) | Canary Workspace stays pending; controller waits for NAP retry; times out after readiness timeout |
| Registry unreachable during version check | Log warning, retry on next poll interval; do not start rollout |
| Registry returns invalid/unparsable tags | Log warning, skip unparsable tags, continue with valid tags |
| Controller crashes mid-rollout | On restart, reconstruct state from Workspace labels; resume from last completed step |
| User disables autoUpgrade during rollout | Controller detects `enabled=false`, deletes any canary Workspaces, halts rollout |

**Failed Upgrade Recovery:**

When `AutoUpgradeFailed=True`, the controller:
1. Does not retry the same version automatically (prevents infinite failure loops).
2. Clears the failure condition when a *newer* version becomes available (the new
   version may fix the issue).
3. Users can manually clear the condition by removing and re-adding the
   `autoUpgrade` field, or by waiting for a newer version.

#### Status Reporting

```yaml
status:
  replicas: 3
  readyReplicas: 3
  autoUpgrade:
    currentVersion: "0.3.0"
    targetVersion: "0.3.1"          # empty when no upgrade in progress
    upgradedReplicas: 1             # 1 of 3 replicas upgraded so far
    lastCheckTime: "2026-05-07T10:00:00Z"
    lastUpgradeTime: "2026-05-01T08:30:00Z"
  conditions:
    - type: Ready
      status: "True"
    - type: AutoUpgradeAvailable
      status: "True"
      reason: NewVersionAvailable
      message: "New base image version 0.3.1 available (current: 0.3.0)"
    - type: AutoUpgradeInProgress
      status: "True"
      reason: CanaryRollout
      message: "Upgrading replica 1/3 to base image 0.3.1"
```

Users can monitor upgrade progress via:
```bash
kubectl get inferenceset my-llm-service -o jsonpath='{.status.autoUpgrade}'
kubectl get inferenceset my-llm-service -o jsonpath='{.status.conditions[?(@.type=="AutoUpgradeInProgress")]}'
```

Kubernetes events are emitted for key lifecycle transitions:
- `Normal  BaseImageCheckCompleted  New base image version 0.3.1 detected`
- `Normal  CanaryCreated           Created canary workspace my-llm-service-canary-xyz`
- `Normal  CanaryReady             Canary workspace ready, proceeding with rollout`
- `Normal  ReplicaUpgraded         Upgraded replica 1/3 to base image 0.3.1`
- `Normal  BaseImageUpgraded       All replicas upgraded to base image 0.3.1`
- `Warning CanaryFailed            Canary workspace failed to become ready, halting rollout`
- `Warning VersionCheckFailed      Failed to query registry: connection refused`

### Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| New base image introduces vLLM regression | Medium | High | Canary strategy limits blast radius to 1 replica; `stable` channel only allows patch bumps |
| GPU node provisioning delay slows rollout | High | Low | Expected behavior; documented. Users can increase `maxSurge` to parallelize canary steps |
| Registry rate limiting blocks version checks | Low | Low | Default 24h poll interval; exponential backoff on failures |
| Canary repeatedly fails, blocking new rollouts | Low | Medium | Controller clears failure state on newer versions; manual override available |
| Concurrent scaling and upgrade create race | Medium | Medium | Canary label distinguishes upgrade Workspaces from scaling; controller serializes operations |

### Test Plan

1. **Unit tests:**
   - Semver tag parsing and channel filtering logic.
   - Canary rollout state machine transitions.
   - Failure detection and halt logic.
   - Status condition updates.
   - Interaction between scaling and upgrade operations.

2. **Integration tests:**
   - Mock OCI registry returning tag lists; verify version discovery.
   - Simulate canary success: verify old replica is deleted after canary ready.
   - Simulate canary failure: verify canary is deleted and rollout halted.
   - Simulate controller restart mid-rollout: verify state reconstruction from labels.

3. **E2E tests:**
   - Deploy InferenceSet with `autoUpgrade.enabled: true`.
   - Push a new base image tag to the test registry.
   - Verify the controller detects the new version, creates a canary, and completes
     the rollout.

## Implementation History

- 2026-05-07: Initial proposal created.
