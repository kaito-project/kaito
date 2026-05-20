---
title: Auto-Upgrade Support for Base Serving Image
authors:
  - "@zhehaoli"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2026-05-07
last-updated: 2026-05-19
status: provisional
---

# Auto-Upgrade Support for Base Serving Image

## Summary

Today when KAITO has a new release, existing Workspaces continue running the
old base image (`kaito-base`). There is no built-in mechanism to upgrade these
workloads without manual intervention. In case of CVEs, users must manually intervene
to update existing workloads

This proposal adds an `autoUpgrade` field to `InferenceSetSpec` that enables the
InferenceSet controller to detect base image version mismatches after a Kaito release
and perform an in-place rolling update of each Workspace's StatefulSet —
preserving persistent volumes (model weights), minimizing downtime, and providing
clear observability.

The scope is limited to the **base serving image** (`kaito-base`). Model weights are
stored on persistent volumes and are unaffected by base image upgrades.

## Motivation

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
reference as `{PRESET_REGISTRY_NAME}/kaito-base:{tag}`. When the controller is
upgraded, this function returns the new tag — but existing Workspaces are left
untouched.

The Workspace controller's `applyInference()` method compares the Workspace's
`workspace.kaito.io/revision` annotation against the existing StatefulSet. If the
revision matches (i.e., the Workspace spec hasn't changed), the StatefulSet is not
updated — even though the controller now embeds a newer base image tag.

**Result:** Existing workloads continue running the old base image indefinitely after
a controller upgrade, unless manually deleted and recreated.

#### Existing `UpdateStrategy` Field

`InferenceSetSpec` already declares an `UpdateStrategy` field typed as
`appsv1.StatefulSetUpdateStrategy` (default `{"type":"RollingUpdate","rollingUpdate":
{"maxUnavailable":1}}`). This field was scaffolded to control how existing Workspaces
are replaced when the InferenceSet spec template changes. However, this field is
**currently unused** — the InferenceSet controller never reads `spec.updateStrategy`
during reconciliation. The auto-upgrade mechanism proposed here operates at a different
level: it updates the StatefulSet *within* each Workspace, rather than replacing
Workspaces themselves.

### Goals

- Enable opt-in automatic base image upgrades for InferenceSet-managed Workspaces
  after a KAITO release.
- Perform in-place rolling updates via StatefulSet's built-in mechanism, preserving
  persistent volumes (model weights are not re-downloaded).
- Upgrade Workspaces sequentially (one at a time) to limit blast radius.
- Support maintenance windows to restrict when upgrades may occur.
- Minimize downtime by pre-downloading the new base image before rolling update.
- Provide clear status reporting so users and automation can observe upgrade progress.

### Non-Goals/Future Work

- **Automatic rollback** — On upgrade failure, the controller halts the rollout.
  Full automatic rollback (reverting already-upgraded Workspaces) is deferred to a
  future proposal.
- **Model weight upgrades** — Model weights are orthogonal to the base image; they are
  stored on persistent volumes and survive pod restarts. A separate mechanism should
  handle model version pinning.
- **Standalone Workspace auto-upgrade** — This proposal targets InferenceSet-managed
  Workspaces only. Standalone Workspace CRs do not have multi-replica management.
  Standalone Workspace upgrades can be considered in a follow-up.
- **Cross-InferenceSet coordination** — Coordinating upgrades across multiple
  InferenceSets (e.g., "upgrade staging first, then production") is out of scope.

## Proposal

### API Changes

#### New Types

```go
// AutoUpgradePolicy configures automatic base image upgrade behavior.
type AutoUpgradePolicy struct {
    // Enabled controls whether the controller automatically upgrades
    // Workspace replicas when a newer base image version is detected
    // after a controller upgrade.
    // +optional
    // +kubebuilder:default:=false
    Enabled bool `json:"enabled"`

    // MaintenanceWindow restricts when upgrades may be applied.
    // If not specified, upgrades may be applied at any time.
    // +optional
    MaintenanceWindow *MaintenanceWindow `json:"maintenanceWindow,omitempty"`
}

// MaintenanceWindow restricts when auto-upgrades may be applied.
// The controller will only begin upgrading Workspaces when the current time
// falls within the specified window.
type MaintenanceWindow struct {
    // Schedule is a cron expression (5-field, UTC) defining when upgrades
    // are permitted to start. The window opens at each cron tick and stays
    // open for Duration.
    // Example: "0 2 * * 6" = every Saturday at 02:00 UTC.
    // +required
    Schedule string `json:"schedule"`

    // Duration specifies how long the maintenance window stays open after
    // each cron tick. If a rollout is still in progress when the window
    // closes, the in-progress Workspace upgrade is allowed to complete
    // (the controller will not start upgrading the next Workspace until the
    // next window opens).
    // Defaults to 4h.
    // +optional
    // +kubebuilder:default:="4h"
    Duration *metav1.Duration `json:"duration,omitempty"`
}
```

**Maintenance window semantics:**
- **Version detection** happens on every reconcile regardless of the window — the
  controller always knows if an update is available (`numDriftedWorkspaces > 0`).
- **Upgrade execution** (tagging Workspaces, pre-downloading images) only happens
  inside the window.
- **In-progress upgrades** are not interrupted: if the window closes while a Workspace's
  StatefulSet is rolling, that Workspace completes its upgrade. The controller pauses
  before starting the *next* Workspace.
- If no `maintenanceWindow` is specified, upgrades may begin at any time.

#### New Labels and Annotations

| Key | Applied To | Purpose |
|---|---|---|
| `kaito.sh/upgrade-to-version` | Workspace | Signals to the Workspace controller that this Workspace should be upgraded to the specified base image version. Set by the AutoUpgradeRunner; retained after upgrade completes as an audit trail. |

#### InferenceSetSpec Change

```go
type InferenceSetSpec struct {
    // ... existing fields ...

    // AutoUpgrade configures automatic base image upgrade behavior.
    // When enabled, the controller detects base image version mismatches
    // after a controller upgrade and performs in-place rolling updates of
    // Workspace StatefulSets.
    // +optional
    AutoUpgrade *AutoUpgradePolicy `json:"autoUpgrade,omitempty"`
}
```

#### New Status Conditions

| Condition Type | Status | Reason | Meaning |
|---|---|---|---|
| `AutoUpgradeFailed` | `True` | `WorkspaceUpgradeFailed` | A Workspace failed to become ready after upgrade; rollout halted |
| `AutoUpgradeFailed` | `False` | `NoFailure` | No upgrade failure (default state) |

Other states are derivable from existing fields:
- **Upgrade available**: `numDriftedWorkspaces > 0`
- **Upgrade in progress**: `numDriftedWorkspaces > 0` AND `AutoUpgradeFailed` is `False`
- **All up-to-date**: `numDriftedWorkspaces == 0`

#### New Status Fields

```go
type AutoUpgradeStatus struct {
    // NumDriftedWorkspaces is the number of Workspaces whose base image version
    // differs from the controller's embedded version. When 0, all Workspaces are
    // up-to-date.
    NumDriftedWorkspaces int `json:"numDriftedWorkspaces"`
    // LastSuccessfulUpgradeTime is the timestamp of the last Workspace that
    // successfully completed an auto-upgrade.
    LastSuccessfulUpgradeTime *metav1.Time `json:"lastSuccessfulUpgradeTime,omitempty"`
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
    maintenanceWindow:
      schedule: "0 2 * * 6"   # Saturdays at 02:00 UTC
      duration: 4h
```

### Controller Behavior Changes

#### AutoUpgradeRunner (Background Goroutine)

The auto-upgrade logic runs as a **dedicated background goroutine** registered with
Kaito's controller manager via `mgr.Add()`. This keeps the InferenceSet controller
unchanged (focused solely on replica management) and provides a natural fit for
time-based maintenance window logic.

```go
type AutoUpgradeRunner struct {
    client   client.Client
    interval time.Duration  // e.g., 5 * time.Minute
}

func (r *AutoUpgradeRunner) Start(ctx context.Context) error {
    ticker := time.NewTicker(r.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            r.reconcileAll(ctx)
        }
    }
}

func (r *AutoUpgradeRunner) NeedLeaderElection() bool { return true }
```

The runner is registered at controller setup time:
```go
mgr.Add(&AutoUpgradeRunner{client: mgr.GetClient(), interval: 5 * time.Minute})
```

Controller-runtime starts the goroutine after leader election and stops it (via
context cancellation) on graceful shutdown or leader loss.

**`reconcileAll()` behavior on each tick:**

1. **List InferenceSets** with `autoUpgrade.enabled: true`.

2. **For each InferenceSet, detect version drift:**
   List Workspaces by the `inferenceset.kaito.io/name` label, fetch each
   Workspace's StatefulSet, and compare the container image tag against the
   controller's embedded version (`metadata.MustGet("base").Tag`).

3. **Update status:**
   Count drifted Workspaces and update `status.autoUpgrade.numDriftedWorkspaces`
   on the InferenceSet. Manage the `AutoUpgradeFailed` condition.

4. **Maintenance window check:**
   If a maintenance window is configured, evaluate whether the current time falls
   within the window (cron schedule + duration). If outside, skip tagging and
   return (the next tick will re-check).

5. **Sequential Workspace upgrade (phase-based):**
   The Runner tracks each Workspace through a state machine:

   | Phase | Condition | Action |
   |---|---|---|
   | **Idle** | No label, image differs from controller version | Select as next candidate |
   | **PreDownload** | Pre-download pod exists, not yet Succeeded | Wait (check on next tick) |
   | **Tag** | Pre-download Succeeded (or timed out) | Add `kaito.sh/upgrade-to-version` label, delete pre-download pod |
   | **Upgrading** | Label set, SS image differs from label value | Wait for Workspace to reach InferenceReady |
   | **Done** | Label set, SS image matches label value | Move to next Workspace |

   Only one Workspace is in PreDownload/Tag/Upgrading at a time.

   **Pre-download details:** The Runner creates a lightweight pod on each node
   where the Workspace's StatefulSet pods are running. The pod uses the new base
   image with a no-op command (`["echo", "predownload-complete"]`), minimal
   resources (10m CPU, 16Mi memory, no GPU), and GPU node tolerations. The pod
   has an owner reference to the InferenceSet for garbage collection. On each
   tick, the Runner checks if the pod has Succeeded. After 30 minutes without
   completion, the Runner proceeds anyway (tags the Workspace; pods will pull
   on-demand).

6. **Failure handling:**
   If a tagged Workspace fails to reach `InferenceReady` within the readiness
   timeout, set `AutoUpgradeFailed=True` on the InferenceSet and stop tagging
   further Workspaces.

7. **Scale interaction:**
   - Scale-up: new Workspaces are created by the InferenceSet controller with the
     current base image (via `GetBaseImageName()`), so they need no upgrade.
   - Scale-down: the InferenceSet controller's existing deletion logic (non-ready
     first) is extended to prefer deleting old-version Workspaces over upgraded ones.

#### Workspace Controller

The Workspace controller currently reconciles the StatefulSet via `applyInference()`,
which is called from `addOrUpdateWorkspace()` on every reconcile. The key gate is:

```go
currentRevisionStr, ok := annotations[kaitov1beta1.WorkspaceRevisionAnnotation]
if ok && currentRevisionStr == revisionStr {
    return nil  // ← revision unchanged, no-op
}
```

This means a controller upgrade (which changes the embedded base image tag but not
the Workspace spec revision) does **not** trigger a StatefulSet update. The
auto-upgrade feature adds the following changes:

1. **Upgrade label detection:**
   Before the revision-match early return, `applyInference()` checks for the
   `kaito.sh/upgrade-to-version` label on the Workspace. If present, the method
   proceeds with the StatefulSet update regardless of whether the revision matches.

2. **StatefulSet image update:**
   The current selective update modifies `Env`, `VolumeMounts`, `InitContainers`,
   and `Volumes`. When the upgrade label is present, the update additionally sets:
   - `spec.template.spec.containers[0].image` → the new base image (from
     `GetBaseImageName()`, which now returns the controller's current version).

   After calling `c.Update(ctx, existingStatefulSet)`, the Kubernetes StatefulSet
   controller detects the pod template change and begins a rolling update (one pod
   at a time in reverse ordinal order). The base image is already cached on the
   node (pre-downloaded by the AutoUpgradeRunner before tagging).

3. **Upgrade completion detection:**
   The `applyInference()` call is synchronous — it updates the StatefulSet object
   and returns. The actual pod rolling update happens asynchronously, driven by the
   Kubernetes StatefulSet controller. The Workspace controller detects completion
   via `collectInferenceReadyStatus()`, which checks
   `ss.Status.ReadyReplicas == replicas` on each reconcile. Once all pods are
   running the new image and pass their readiness probes, `InferenceReady`
   becomes `True`.

### Probe Changes

For multi-node distributed inference, the worker pod's startup
and readiness probes must **not** depend on the leader's `/health` endpoint. During a
rolling update, the worker (pod-1) is updated first. If the worker's probes check the
leader's health, a permanent deadlock occurs:

- Worker (new image, potentially new Ray version) cannot join leader (old image, old Ray version)
- Leader cannot serve `/health` without a worker in its placement group
- Worker never becomes Ready → StatefulSet never updates leader

To address this problem, we will update Worker probes to be Ready unconditionally. This is safe because:
- The Service routes traffic only to pod-0 (via `statefulset.kubernetes.io/pod-name` selector)
- Workers don't expose an HTTP endpoint; their readiness is irrelevant to traffic routing
- The leader's probes still gate actual service availability

We will update the existing probe commands with a `POD_INDEX` conditional:

```bash
# Startup probe (benchmark variant)
if [ "$POD_INDEX" = "0" ]; then
  python3 /workspace/vllm/benchmark_entrypoint.py
else
  true
fi

# Readiness/liveness probes (non-benchmark)
if [ "$POD_INDEX" = "0" ]; then
  python3 /workspace/vllm/multi-node-health-check.py readiness --leader-address=... --vllm-port=5000
else
  true
fi
```

The `multi-node-health-check.py` script will also enforce this at runtime:

```python
elif args.probe == "readiness":
    if not is_leader:
        sys.exit(0)  # Worker: always ready
    readiness(args)  # Leader: check /health
```

### Failure Handling

| Failure Scenario | Controller Behavior |
|---|---|
| Pod fails to start after image update (CrashLoopBackOff) | StatefulSet pauses rolling update; Workspace controller waits for readiness timeout, then sets `AutoUpgradeFailed=True` and halts |
| Pod starts but readiness probe times out | Same as above — Workspace fails to reach `InferenceReady`, rollout halts |
| Pre-download pod times out | Delete pre-download pod, log warning, proceed with upgrade (pods pull image on-demand) |
| Controller crashes mid-upgrade | On restart, reconstruct state from Workspace labels; resume from last completed Workspace |
| User disables autoUpgrade during rollout | Controller stops tagging new Workspaces; in-progress Workspace upgrade completes; rollout halts |
| Distributed inference: leader fails to start Ray cluster | Workspace fails readiness; treated as failed upgrade |

**Failed Upgrade Recovery:**

When `AutoUpgradeFailed=True`, the controller:
1. Does not continue upgrading remaining Workspaces (halts at first failure).
2. Retries the failed Workspace on the next controller upgrade (when a newer
   base image version becomes available).
3. Users can manually clear the failure by removing and re-adding the
   `autoUpgrade` field, or by deleting the failed Workspace (InferenceSet will
   recreate it with the new version).

### Status Reporting

```yaml
status:
  replicas: 3
  readyReplicas: 3
  autoUpgrade:
    numDriftedWorkspaces: 2
    lastSuccessfulUpgradeTime: "2026-05-14T02:30:00Z"
  conditions:
    - type: Ready
      status: "True"
    - type: AutoUpgradeFailed
      status: "False"
      reason: NoFailure
```

Users can monitor upgrade progress via:
```bash
kubectl get inferenceset my-llm-service -o jsonpath='{.status.autoUpgrade}'
```

## Proof of Concept
We built a proof of concept and measured the downtime during upgrade.

### Setup
- Cluster: AKS with 2x Standard_NC24ads_A100_v4 GPU nodes (1x A100 80GB each)
- Model: `openai/gpt-oss-120b` (121.54 GiB)
- Upgrade base image from 0.2.8 to 0.3.0, and manually patch StatefulSet to trigger a rolling upgrade

### Results
The downtime was around 7 minutes when the new base image was pre-downloaded,
which was mainly caused by:
- Startup benchmark (execution + drain): ~200s (can we skip running benchmark on upgrade?)
- Ray cluster formation: ~60s
- Pod termination/recreation overhead: ~60s
- CUDAGraph capture + torch.compile + vLLM init: ~50s
- Model weight loading: ~15s (if from page cache), ~300s (if from NVMe)
- New base image pull: 0s (if pre-downloaded), ~90s (if pulled on-the-fly)

A detailed breakdown is as below:

| Phase | Duration | Timestamps (UTC) |
|---|---|---|
| SS terminates pod-1 + recreates with 0.3.0 | ~31s | 21:03:00 → 21:03:31 |
| Pod-1 Ready (worker probe: immediate exit 0) | ~13s | 21:03:31 → 21:03:44 |
| SS terminates pod-0 + recreates with 0.3.0 | ~29s | 21:03:44 → 21:04:13 |
| Pod-0 container start + Ray head init | ~10s | 21:04:13 → 21:04:23 |
| Ray worker (pod-1) joins cluster | ~57s | 21:04:23 → 21:05:20 |
| vLLM V1 engine init + placement group | ~5s | 21:05:20 → 21:05:25 |
| Weight loading (15 shards, page cache) | ~7s | 21:05:25 → 21:05:52 (approx) |
| torch.compile (AOT cache hit, both ranks) | ~15s | 21:05:52 → 21:06:07 |
| CUDAGraph capture + server ready | ~24s | 21:06:07 → 21:06:31 |
| Startup benchmark execution | ~71s | 21:06:34 → 21:07:45 |
| Benchmark drain | ~137s | 21:07:45 → 21:10:02 |
| **Total (patch → InferenceReady)** | **7m03s** | 21:03:00 → 21:10:03 |

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| New base image introduces vLLM regression | Medium | High | Sequential upgrade limits blast radius to 1 Workspace; base image tested before release |
| Pod restart causes brief per-Workspace downtime | High | Low | Expected behavior for in-place update; pre-download minimizes restart time; other replicas serve traffic |
| Distributed inference cluster disruption during upgrade | High | Medium | Expected and documented; entire cluster restarts simultaneously (`podManagementPolicy: Parallel`), minimizing transition window |
| Model weights incompatible with new base image | Low | High | Model weights are format-stable (safetensors/GGUF); base image upgrades change runtime, not model format |
| Pre-download pod fails to schedule on GPU node | Low | Low | Timeout and fallback to on-demand pull; upgrade proceeds regardless |
| Concurrent scaling and upgrade create race | Medium | Low | Scale-up creates Workspaces at new version; scale-down deletes old-version Workspaces first |

## Test Plan

1. **Unit tests:**
   - Version comparison logic (label vs. controller version).
   - Upgrade label lifecycle (set, detect, remove).
   - Maintenance window evaluation (cron parsing, duration check).
   - Status condition updates through upgrade lifecycle.
   - Interaction between scaling and upgrade operations.

2. **E2E tests:**
   - Deploy InferenceSet with `autoUpgrade.enabled: true`.
   - Upgrade the KAITO controller to a version with a new base image tag.
   - Verify the controller detects the mismatch, pre-downloads the image, and
     pre-downloads images and sequentially upgrades all Workspaces.
   - Verify PV retention: model weights are not re-downloaded after upgrade.
   - Distributed inference E2E: verify multi-node Workspace upgrades correctly
     (all pods updated simultaneously, Ray cluster reforms, Workspace reaches ready).

## Alternative Considered
  - Statefulset now supports upgrading more than one pods in parallel by configuring
    the `.spec.updateStrategy.rollingUpdate.maxUnavailable` field. However, this is a
    Kubernetes v1.35 beta feature. In future, we can leverage this feature to
    reduce downtime in multi-node rolling upgrade. 
 
## Implementation History
- 2026-05-07: Initial proposal created.
- 2026-05-15: Revised to use in-place StatefulSet rolling update, sequential Workspace
  upgrade via labels and image pre-download.
- 2026-05-19 Revised to use sequential rolling upgrade for StatefulSet.
- 2026-05-20 Revised to manage autoupgrade with a dedicate goroutine.
