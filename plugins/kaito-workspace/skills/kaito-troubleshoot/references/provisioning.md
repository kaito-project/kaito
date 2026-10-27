# Provisioning and node evidence

Use this reference at the provisioning stage for the selected `WORKSPACE`.

## Identify the active mode

Inspect the running KAITO controller's image and arguments, using the installation's known deployment/namespace. If that location is unknown, a metadata-only deployment inventory can locate it:

```bash
kubectl --context "${CONTEXT:?}" --request-timeout=30s get deployments --all-namespaces \
  -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,IMAGES:.spec.template.spec.containers[*].image'
```

Use the selected deployment's actual pod selector to identify its running controller pod. Read its arguments rather than assuming a Helm default or a namespace name:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${CONTROLLER_NAMESPACE:?}" --request-timeout=30s \
  get pod "${CONTROLLER_POD:?}" \
  -o jsonpath='{range .spec.containers[*]}{.name}{"\nImage: "}{.image}{"\n"}{range .args[*]}{.}{"\n"}{end}{end}'
```

The current controller resolves `--node-provisioner` as `karpenter`, `azure-gpu-provisioner`, or `byo`; this choice also sets its internal node-auto-provisioning feature gate. For an older image without that flag, consult that version's provisioning configuration, including `disableNodeAutoProvisioning`. Missing NodePools alone cannot establish BYO mode. Conflicting controller versions/configuration or inaccessible mode evidence make the mode unresolved.

Read the selected Workspace's resource requirements and node snapshot:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  get workspaces.kaito.sh "${WORKSPACE:?}" \
  -o jsonpath='{"Resource: "}{.resource}{"\nTarget nodes: "}{.status.targetNodeCount}{"\nWorker nodes: "}{.status.workerNodes}{"\nConditions: "}{.status.conditions}{"\n"}'
```

Use `status.targetNodeCount` and the active selection rules, not a guessed model-size-to-node mapping. A missing/uninitialized target count is not proof that zero nodes suffice. Worker-node status is a discovery hint; cross-check live ownership and readiness.

## Mode-specific resource discovery

| Mode | Resource relationship |
| --- | --- |
| Karpenter | Workspace-specific NodePool carries `karpenter.kaito.sh/workspace-name` and `karpenter.kaito.sh/workspace-namespace`. NodeClaims reference that pool via `karpenter.sh/nodepool`; its template propagates Workspace ownership labels to claims and nodes. |
| Azure GPU provisioner | KAITO creates NodeClaims directly, labeled `kaito.sh/workspace` and `kaito.sh/workspacenamespace`. Inspect those claims and their nodes; a per-Workspace NodePool is not required. |
| BYO | No NodePool or NodeClaim is required. Use the Workspace's node selector and inspect the matching existing Nodes. |

For Karpenter, discover the pool by both Workspace labels; do not guess its possibly truncated/hashed name:

```bash
kubectl --context "${CONTEXT:?}" --request-timeout=30s get nodepools.karpenter.sh \
  -l "karpenter.kaito.sh/workspace-name=${WORKSPACE:?},karpenter.kaito.sh/workspace-namespace=${NAMESPACE:?}" \
  -o name
```

For each relevant discovered `NODEPOOL`, inspect its conditions, desired replicas, and NodeClass reference:

```bash
kubectl --context "${CONTEXT:?}" --request-timeout=30s get nodepools.karpenter.sh "${NODEPOOL:?}" \
  -o jsonpath='{"UID: "}{.metadata.uid}{"\nGeneration: "}{.metadata.generation}{"\nDeleting: "}{.metadata.deletionTimestamp}{"\nReplicas: "}{.spec.replicas}{"\nNodeClass: "}{.spec.template.spec.nodeClassRef}{"\nConditions: "}{.status.conditions}{"\n"}'
kubectl --context "${CONTEXT:?}" --request-timeout=30s get nodeclaims.karpenter.sh \
  -l "karpenter.sh/nodepool=${NODEPOOL:?}" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.generation}{"\t"}{.metadata.ownerReferences}{"\t"}{.metadata.deletionTimestamp}{"\t"}{.status.nodeName}{"\t"}{.status.conditions}{"\n"}{end}'
```

Verify pool ownership on claims. If the pool reports a NodeClass problem, follow the actual group/kind/name reference and discover that API before reading its status; Azure and AWS use different NodeClass APIs.

For the Azure GPU provisioner, discover claims directly:

```bash
kubectl --context "${CONTEXT:?}" --request-timeout=30s get nodeclaims.karpenter.sh \
  -l "kaito.sh/workspace=${WORKSPACE:?},kaito.sh/workspacenamespace=${NAMESPACE:?}" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.generation}{"\t"}{.metadata.deletionTimestamp}{"\t"}{.status.nodeName}{"\t"}{.status.conditions}{"\n"}{end}'
```

For both provisioners, inspect `Ready` and available lifecycle conditions in order: `Launched`, `Registered`, `Initialized`, checking any observed generation against the resource generation. Record the earliest meaningful reason/message. Quota, authorization, or capacity errors explain failures; `AwaitingReconciliation` alone describes progress. Exclude deleting claims from ready capacity. Some legacy claims have no `Ready` condition: KAITO falls back to a populated `status.nodeName`; corroborate with the live Node rather than inventing a failed condition.

With Karpenter, failed claims are often deleted and recreated, so live claims may only show `AwaitingReconciliation`. On Azure:

- Quota, allocation and SKU errors are recorded on the claim's `Launched=Unknown` condition (for example `SubscriptionQuotaReached` or `AllocationFailure`, and `CreateInstanceFailed` on older providers). The provider then caches the instance type as unavailable, so later attempts fail within seconds with a generic `InsufficientCapacityError` Event (`all requested instance types were unavailable`) and their claims are deleted.
- The regional vCPU quota is only reported by an `InsufficientCapacityError` Event, and a failure after Azure accepted the instance only by an `AsyncProvisioningError` Event.
- A node that never registers produces neither; Karpenter sets the NodePool `NodeRegistrationHealthy=False` after two launch or registration timeouts among the last four attempts.

KAITO reports the root cause on the Workspace `NodeClaimReady`/`ResourceReady` conditions while the ready claims do not cover the target. `NodeClaimNotReady` is progress, with the pending lifecycle stage in the message; any other False reason means the latest attempt failed while Karpenter keeps retrying. KAITO reports the first of:

1. a NodePool `ValidationSucceeded=False`;
2. a missing or `Ready=False` NodeClass;
3. a failure on a claim, deleting claims included;
4. the `AsyncProvisioningError` or `InsufficientCapacityError` Warning Events of the pool's claims from the last 4 minutes (listed in the `default` namespace at most every 2 minutes), preferring `AsyncProvisioningError`;
5. the NodePool's `NodeRegistrationHealthy=False`, only while a claim launched at least 10 minutes ago has not registered, as Karpenter resets it only after several registrations.

An invalid NodePool is reported as `NodePoolInvalid` and a missing or `Ready=False` NodeClass as `NodeClassInvalid`, with the NodePool's validation message or the NodeClass's failed conditions as the message. Other failures that need the user to act are reported as `QuotaExceeded`, `CapacityUnavailable`, `NodeImageInvalid`, `NodeBootstrapFailed` or `NodeConfigurationInvalid` (permission, subnet, Azure Policy, resource provider registration or missing referenced resource errors), with the original reason at the start of the message. An `InsufficientCapacityError` other than the vCPU quota is reported as `CapacityUnavailable`, as the pool's single instance type and capacity type have no other offering to fall back on, unless the provider could not resolve the NodeClass ("resolving node class"), which clears once the NodeClass is restored. A capacity failure in one zone (`ZonalAllocationFailure`, `OverconstrainedZonalAllocationFailure`, or "in the selected zone") is reported under its own reason, not as `CapacityUnavailable`, as the provider retries another zone about a second later; once every zone failed, the provider reports a no-offering `InsufficientCapacityError`, which is `CapacityUnavailable`. An asynchronous one (`AsyncProvisioningError`) is still `CapacityUnavailable`. Once a specific failure is reported, a generic `InsufficientCapacityError` or progress does not replace it until a new claim launches, as the claim that carried it is deleted and its retries fail before launching. An invalid NodeClass or NodePool is checked on every reconcile, so it is reported only while it lasts. Until a node that never registers is reported, check the node's boot diagnostics.

An InferenceSet's `InferenceSetReady` condition is True once at least `spec.replicas` of its Workspaces that are not being deleted have `WorkspaceSucceeded=True`, which `status.readyReplicas` counts; otherwise it aggregates its not-ready Workspaces:

- Terminal reasons need the user to act and do not recover on their own: the seven node provisioning reasons above, `ContainerOOMKilled`, `ImagePullError`, `ContainerStartError`, `ContainerCrashLoopBackOff`, `SASTokenFetchFailed`, `ModelBundleMismatch`, the `ModelStreaming*` validation reasons and `DownloadOOMKilled`.
- Known in-progress reasons such as `NodeClaimNotReady`, `AwaitingReconciliation`, `ModelMirrorNotReady`, `ModelMirrorPending` or `WorkspaceInferenceStatusPending` are progress; any other reason is a failure.
- A Workspace's root cause is a condition with a terminal reason; otherwise a failing condition among `ResourceReady`, `NodeClaimReady`, `NodesReady`, `ModelConfigReady` and `ModelMirrorReady`, otherwise the first of them that is False, and `InferenceReady` only once those are ready.
- Terminal Workspaces are listed first, then failing ones, then progressing ones. Within each, node causes come before model causes (`ModelConfigReady`, `ModelMirrorReady`) and inference causes (`InferenceReady`), as nodes block the later stages, then Workspaces are listed by name. The reason is that of the first listed Workspace, and the message lists up to three Workspaces.
- An InferenceSet whose `InferenceSetReady` reason is terminal for its current generation is terminated until the failure is fixed.

Account for pre-existing or legacy capacity when the installed provisioner supports it. Current Karpenter can omit a new pool when existing capacity covers the target; the Azure provisioner can also reuse matching nodes. Explain a missing resource only after establishing that additional provisioned capacity is actually required.

## Check live Nodes

Use NodeClaim `status.nodeName`, Workspace `status.workerNodes`, and mode-specific selectors to find candidates. Managed node selectors must include the Workspace name and namespace ownership keys from the table, in addition to applicable user labels; a shared InferenceSet user selector alone can include sibling nodes. BYO uses the user's selector without requiring managed ownership labels.

When discovering nodes by selector, build `NODE_SELECTOR` from those inspected selection requirements:

```bash
kubectl --context "${CONTEXT:?}" --request-timeout=30s get nodes \
  -l "${NODE_SELECTOR:?}" -o name
```

For each relevant `NODE`:

```bash
kubectl --context "${CONTEXT:?}" --request-timeout=30s get node "${NODE:?}" \
  -o jsonpath='{"UID: "}{.metadata.uid}{"\nLabels: "}{.metadata.labels}{"\nDeleting: "}{.metadata.deletionTimestamp}{"\nUnschedulable: "}{.spec.unschedulable}{"\nTaints: "}{.spec.taints}{"\nConditions: "}{.status.conditions}{"\nCapacity: "}{.status.capacity}{"\nAllocatable: "}{.status.allocatable}{"\n"}'
```

Check `Ready=True`, deletion state, disk/memory/PID pressure, cordoning, taints, selector compatibility, and GPU/device-plugin evidence. Compare the required resource type, including MIG resources for a partitioned Workspace. Allocatable is a total resource budget, not currently unused GPU capacity; scheduling evidence is needed to conclude exhaustion.

Compare required and observed ready capacity. NodePool readiness alone does not prove that enough Nodes exist, and Node readiness alone does not establish that the workload can request its required GPU resources.

For a non-ready resource without an adequate explanation, use its UID-scoped events and, if needed, targeted provisioner/controller logs from [events and controller evidence](pods-and-logs.md#events-and-controller-evidence). If those reads fail, preserve the access/error distinction. When model-weight readiness is the remaining prerequisite, follow the referenced download/ModelMirror condition instead of misclassifying it as a node failure.

## Source of the relationships

These commands reflect the repository's [provisioner factory](https://github.com/kaito-project/kaito/blob/main/pkg/nodeprovision/manager/factory.go), [node selection](https://github.com/kaito-project/kaito/blob/main/pkg/nodeprovision/nodes.go), [NodePool labels](https://github.com/kaito-project/kaito/blob/main/pkg/nodeprovision/karpenter/nodepool.go), and [NodeClaim handling](https://github.com/kaito-project/kaito/blob/main/pkg/utils/nodeclaim/nodeclaim.go). For older installations, resolve differences against the installed KAITO version rather than treating missing fields as failure.
