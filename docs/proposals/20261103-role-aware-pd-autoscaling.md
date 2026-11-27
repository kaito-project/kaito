---
title: Role-aware autoscaling for prefill/decode disaggregation
authors:
  - "@andyzhangx"
reviewers: []
creation-date: 2026-11-03
last-updated: 2026-11-03
status: provisional
see-also:
  - 20250918-introduce_inferenceset_autoscaling.md
  - 20260424-multiroleinference-pd-disaggregation.md
---

# Role-aware autoscaling for prefill/decode disaggregation

## Summary

Extend `MultiRoleInference` (MRI) with an **opt-in, versioned role-policy annotation**. Project each policy onto the corresponding child `InferenceSet` and reuse the optional `keda-kaito-scaler` integration to manage one `ScaledObject` per role. KEDA/HPA, not MRI, owns the child's steady-state replica count.

Prefill scales on native vLLM `vllm:num_requests_waiting`; decode scales on native `vllm:num_requests_running`. Independent thresholds, positive replica floors, readiness checks, stabilization, and rate limits accommodate different prompt and generation workloads. Native latency and KV metrics inform observation; explicitly derived metrics and a KV scale-down guard are later work.

**This is a design proposal, not an available feature or an installation guide.** The annotation and status contracts below require implementation in KAITO and the scaler. Existing annotations alone cannot enable MRI autoscaling. No deployed runtime or cluster was inspected, and all numerical examples are illustrative, not production defaults.

## Motivation

The [InferenceSet proposal](20250918-introduce_inferenceset_autoscaling.md) established the scalable unit: one replica creates one Workspace, potentially spanning multiple GPUs or nodes. The [MRI proposal](20260424-multiroleinference-pd-disaggregation.md) composes role-specific InferenceSets behind one serving endpoint. Scaling MRI as one homogeneous pool would discard that separation:

- **Prefill** is prompt-compute-intensive and sensitive to admission delay and time to first token (TTFT).
- **Decode** is memory-bandwidth/KV-intensive and sensitive to generation concurrency and inter-token latency.
- Prompt length, generation length, prefix reuse, and routing change the useful prefill/decode ratio; a fixed ratio or one global threshold is insufficient.

This proposal adapts the [role-aware autoscaling design note](https://github.com/andyzhangx/demo/blob/fecf817fb2f2776d9409f45964add9b6c13bf484/llm/kaito-llm-d/kaito-role-aware-autoscaling-for-pd.md) to KAITO's proposal format and current controller contracts. The original InferenceSet design is architectural history, not evidence of current built-in scaling policy: this design deliberately uses the separately installed, optional KEDA integration.

### Goals

- Keep MRI as the single user-authored resource for managed P/D autoscaling.
- Scale each role's InferenceSet independently, in Workspace units, with one replica writer.
- Reuse validated scaler settings and resource construction rather than add another autoscaler.
- Specify admission/defaulting, ownership transitions, metric attribution, failure handling, and safe scale-down.
- Require native vLLM signals in the first phase and traceable native-input derivations later.

### Non-goals

- Autoscaling the whole MRI, individual tensor-parallel workers, or GPU nodes directly.
- Scale-to-zero, router/EPP load triggers, predictive scaling, or automatic optimal P/D ratios in v1.
- A new exporter for undefined token backlog, KV pressure, or desired-replica metrics.
- Changing llm-d routing, raw KV-event processing, model placement, fine-tuning, or RAG behavior.
- Shipping a typed autoscaling API alongside the annotation contract in v1.

## Current behavior and integration gaps

Source review baseline: KAITO [`4c450c32`](https://github.com/kaito-project/kaito/tree/4c450c328a21047f7fa576780a086cc1a393cc95), scaler [`3964e100`](https://github.com/kaito-project/keda-kaito-scaler/tree/3964e100f0093ec609b663704e726f2bb3fe8fd9). These are source snapshots, not a tested release combination.

| Area | Existing behavior | Required change |
| --- | --- | --- |
| Role boundary | MRI creates `<mri>-prefill` and `<mri>-decode`, with parent/role labels. Child InferenceSets expose `/scale`. | Keep this boundary; count Workspaces, not GPUs, pods, or tensor-parallel ranks. |
| Replica ownership | MRI reconciliation skips replicas when the role pointer is nil. **The pinned Helm CRD defaults omitted/null replicas to 1; the generated base CRD does not.** `SetDefaults` also sets nil to 1, but MRI registers a validating-only webhook, not a persisting mutator. | Align installed schemas and implement persisted, mode-aware defaulting before advertising nil as managed/external mode. Changing `SetDefaults` or using `replicas: null` alone is insufficient. |
| Annotation placement | MRI copies annotations into child `spec.template.annotations` for downstream workloads, not child `metadata.annotations`. | Project only validated scaler output to child metadata; exclude the policy/control annotations from workload propagation. |
| Child eligibility | Scaler create/update predicates explicitly exclude MRI-owned InferenceSets. | Add a feature-gated, owner-aware managed-child path, including disable/removal cleanup events; copying annotations is insufficient. |
| Existing scaler policy | Structured multi-metric lists, conservative AND combination, readiness gating for scale-up, and HPA stabilization/rate limits already exist. | Reuse them; do not describe the scaler as single-metric-only or as maximum-of-HPA-recommendations. |
| Collection | Model-pod source discovers Workspace Services and scrapes `/metrics`; gauge aggregation is a per-Service average of summed series. | Verify role isolation, series cardinality, coverage, and the actual vLLM endpoint. Decode's routing port is not automatically its metrics port. |
| Missing samples | The current service-average aggregator skips failed Services and treats a missing metric on some successful Services as zero if another Service exposes it. | Add strict full-coverage/freshness semantics for managed MRI policies; existing behavior does **not** meet the proposed safety contract. |
| Scale-down | InferenceSet selects surplus Workspaces using readiness/upgrade constraints and deletes them. | Add or verify an explicit drain protocol for MRI-managed removal; readiness and HPA cooldown alone do not protect in-flight P/D requests. |
| Status | MRI reports conditions and observed generation, not per-role autoscaling state. | Add role-scoped status and applied policy identity; annotation changes need not increment generation. |

See the pinned [MRI types](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/api/v1alpha1/multiroleinference_types.go), [defaulting](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/api/v1alpha1/multiroleinference_default.go), [MRI controller](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/pkg/controllers/multiroleinference/controller.go), [InferenceSet controller](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/pkg/inferenceset/inferenceset_controller.go), [scaler predicates](https://github.com/kaito-project/keda-kaito-scaler/blob/3964e100f0093ec609b663704e726f2bb3fe8fd9/pkg/controllers/autoprovision/auto_provision_controller.go), and [service-average aggregation](https://github.com/kaito-project/keda-kaito-scaler/blob/3964e100f0093ec609b663704e726f2bb3fe8fd9/pkg/aggregator/service_avg_aggregator.go).

## Proposal

### Recommended API: role-aware annotations

Introduce `autoscaling.kaito.sh/roles`, whose value is a strictly parsed YAML/JSON object. Policy keys intentionally reuse the supported scaler annotation suffixes. The map is keyed by role type, never array position.

**Proposed example; not deployable on existing KAITO releases:**

```yaml
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: phi-4-mini
  annotations:
    autoscaling.kaito.sh/roles: |
      version: v1alpha1
      roles:
        prefill:
          min-replicas: "1"
          max-replicas: "8"
          combinepolicy: AND
          scaleupstabilizationwindow: "60"
          scaleupcooldown: "900"
          scaledownstabilizationwindow: "1800"
          scaledowncooldown: "1800"
          metrics:
          - name: vllm:num_requests_waiting
            type: gauge
            source: modelpod
            aggregation: service-avg
            upthreshold: 8
            downthreshold: 1
        decode:
          min-replicas: "2"
          max-replicas: "16"
          combinepolicy: AND
          scaleupstabilizationwindow: "60"
          scaleupcooldown: "900"
          scaledownstabilizationwindow: "1800"
          scaledowncooldown: "1800"
          metrics:
          - name: vllm:num_requests_running
            type: gauge
            source: modelpod
            aggregation: service-avg
            upthreshold: 24
            downthreshold: 4
spec:
  labelSelector:
    matchLabels:
      apps: phi-4-mini
  model:
    name: microsoft/Phi-4-mini-instruct
  roles:
  - type: prefill
    replicas: null
    instanceType: Standard_NC24ads_A100_v4
  - type: decode
    replicas: null
    instanceType: Standard_NC24ads_A100_v4
```

The model/SKU illustrates API placement, not validated capacity. Defaults must be tuned using measured initialization times, traffic, and latency objectives. Positive bounds are in **Workspace replicas**; actual GPU/node consumption depends on model parallelism and per-role placement. Bounds do not reserve capacity or replace quota checks.

#### Contract and validation

- Require `version: v1alpha1`; reject unsupported versions, duplicate YAML keys, malformed input, unknown fields, and roles absent from `spec.roles`. Set a decoded-size limit (proposed 16 KiB), stricter than Kubernetes' total annotation-size limit; reject recursive aliases or excessive parser expansion.
- A role entry enables managed autoscaling. Require string integer bounds `1 <= min-replicas <= max-replicas <= MaxInt32`, and exactly one metric in v1: prefill waiting requests or decode running requests as above. Both must be `gauge`, `modelpod`, `service-avg`.
- Require finite, non-negative thresholds with `downthreshold < upthreshold`. Equality to either threshold holds; strict separation supplies hysteresis. Scalar behavior values use the scaler's string representation.
- The v1 role-key allowlist is exactly `min-replicas`, `max-replicas`, `combinepolicy`, `metrics`, `scaleupstabilizationwindow`, `scaleupcooldown`, `scaledownstabilizationwindow`, and `scaledowncooldown`. Only `AND` is accepted. Omitted behavior fields use documented, version-pinned scaler defaults; do not invent a second algorithm. Validate stabilization windows in `[0, 3600]` seconds and policy periods in `[1, 1800]` seconds against the emitted HPA API.
- Do not accept arbitrary KEDA fields, metric names, scrape URLs, ownership markers, EPP sources, activation/deactivation bands, or derived queries. Per-direction HPA tolerance is not exposed in v1; generated behavior must be compatible with the target Kubernetes/KEDA versions and feature gates.
- Reject a role policy with non-nil `replicas`. Reject global `scaledobject.kaito.sh/*` settings on the same MRI instead of applying one role's settings to both roles.
- Validate create and update admission, including annotation-only updates. Revalidate in reconciliation for persisted invalid objects and version skew. Invalid updates must not silently replace or disable a working policy.
- Serialize the metric list to `scaledobject.kaito.sh/metrics`, prefix scalar keys with `scaledobject.kaito.sh/`, and set `scaledobject.kaito.sh/auto-provision: "true"` on matching **child metadata**. Preserve unrelated metadata and remove stale adapter-owned keys.
- Keep the MRI policy and internal ownership markers out of child templates, Workspaces, and pods. Retain ordinary workload annotation propagation.

For example, the prefill projection includes this generated output (not a second user-authored resource):

```yaml
metadata:
  annotations:
    scaledobject.kaito.sh/auto-provision: "true"
    scaledobject.kaito.sh/min-replicas: "1"
    scaledobject.kaito.sh/max-replicas: "8"
    scaledobject.kaito.sh/combinepolicy: AND
    scaledobject.kaito.sh/metrics: |
      - name: vllm:num_requests_waiting
        type: gauge
        source: modelpod
        aggregation: service-avg
        upthreshold: 8
        downthreshold: 1
```

The adapter also projects the behavior settings and a controller-managed policy digest/parent UID marker. The scaler must verify the live owner and accepted parent policy; a child annotation alone is not proof of managed ownership. Internal marker spelling is an implementation detail, not a user API.

### Alternative: typed role fields

A future API could express the same policy as `spec.roles[].autoscaling`:

```yaml
roles:
- type: prefill
  replicas: null
  autoscaling:
    minReplicas: 1
    maxReplicas: 8
    metrics:
    - name: vllm:num_requests_waiting
      type: gauge
      upThreshold: 8
      downThreshold: 1
```

This is a **proposed fragment**, not an accepted CRD field. Typed fields provide `kubectl explain`, IDE support, structural validation, and potentially finer field ownership if role lists have appropriate list-map schemas. They also require CRD/generated-client changes, defaulting/conversion decisions, and a long-lived compatibility commitment. They do not remove the need for ownership, collection, drain, or status integration.

| Consideration | Role annotation | Typed fields |
| --- | --- | --- |
| First integration | Reuses scaler vocabulary with a small adapter | Adds schema/client work as well as the adapter |
| Validation/discovery | Custom strict parser; opaque to CRD schema | Better field-level tooling |
| Declarative ownership | One annotation value is an atomic policy blob | Potentially finer-grained with correct schemas |
| Evolution | Version the payload; no policy CRD churn | Schema compatibility and conversion obligations |
| Lifecycle/safety | Explicit implementation required | Same implementation required |

**Decision:** use annotations first, not because they are inherently better APIs, but because the optional scaler already exposes this contract and P/D policy is evolving. Reconsider typed fields or a separate policy resource if policy size, composition, or schema tooling justifies migration. Never enable both as authoritative sources for the same role.

### Replica modes and admission defaulting

The desired post-admission ownership matrix is:

| Role replicas | Role policy | Meaning |
| --- | --- | --- |
| Non-nil | Absent | Fixed: MRI enforces replicas |
| Nil | Present | Managed: scaler/HPA controls replicas |
| Nil | Absent | External: adapter does not install a scaler or guarantee one exists |
| Non-nil | Present | Invalid |

**Resolving today's schema and admission defaulting gaps is prerequisite work.** Go decoding does not preserve a distinction between omitted replicas and explicit JSON null. In the pinned [Helm MRI CRD](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/charts/kaito/workspace/templates/kaito.sh_multiroleinferences.yaml#L152-L159), the non-nullable `replicas` schema has `default: 1`: API-server schema defaulting converts omitted/null values to one before webhook admission. The [generated base CRD](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/config/crd/bases/kaito.sh_multiroleinferences.yaml) has no such default. The [MRI webhook registration](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/pkg/workspace/webhooks/webhooks.go#L80-L88) is validating-only; changing `SetDefaults` cannot persist mode-aware defaults through that webhook.

The implementation must remove the unconditional replica schema default and align the API markers, generated base CRD, and Helm-shipped CRD, including installed schemas on upgrade. Then add a mode-aware **mutating/defaulting admission path** with its webhook registration/configuration and serving support. Preserve the legacy create default of one for roles without autoscaling intent by persisting it through mutation; leave managed/external replicas absent (nil after decoding). Explicit null may be pruned to absence by the non-nullable schema, which is acceptable only when no unconditional schema default restores one. Do not repair schema-injected ones by clearing arbitrary non-nil values: explicitly supplied counts with managed/external intent must still fail validation. Keep any validation-time `SetDefaults` handling consistent with mutation, but do not rely on validation to persist changes. Gate activation on the aligned schema and admission path being installed; neither a Go-only defaulting change nor `replicas: null` alone establishes this contract.

To preserve external nil ownership without changing the default for every existing manifest, introduce the companion proposed annotation `autoscaling.kaito.sh/external-roles`, a strict JSON list such as `["decode"]`. An external role must exist, have nil replicas, and have no managed policy; reject duplicate/unknown roles and overlap. The mutating/defaulting path recognizes both managed role entries and external markers. This annotation is a defaulting/mode marker, not another scaling policy. Filter it out of workload propagation too.

For existing persisted nil roles, migration must preserve their external mode rather than silently default them to fixed on update. Implement baseline-aware update handling or an explicit migration before enabling the new defaulting path; test admission with raw create/update payloads, not just controller objects. Removing a managed policy while retaining external ownership requires adding the external-role marker in the same update. Removing policy and setting fixed replicas is the fixed-mode transition. This explicit migration detail refines the source design note's nil/nil semantics to account for actual admission behavior.

## Controller ownership and lifecycle

```text
User MRI policy
  -> MRI adapter: validated role projection, replica-mode handoff
  -> child InferenceSet metadata
  -> keda-kaito-scaler: one role ScaledObject
  -> KEDA: one HPA
  -> HPA: child InferenceSet /scale
  -> InferenceSet: Workspace replicas
```

Lifecycle owner references remain `MRI -> InferenceSet -> ScaledObject -> HPA`. Reconciler responsibility and Kubernetes ownership are different: the scaler constructs ScaledObjects; the MRI controller does not duplicate that code. Never change the user-owned MRI replica field to feed back HPA decisions.

### Reconciliation and transition protocol

1. Parse and validate the full MRI policy/mode state. Detect competing ScaledObjects and HPAs by their target reference, not just expected names. Do not adopt, overwrite, or delete user-owned resources; report `AutoscalerConflict` and do not start another writer. Arbitrary out-of-band controllers cannot all be discovered; document single-writer ownership as an operator requirement.
2. Map children by role plus owner UID. MRI's current watch receives annotation updates; preserve that behavior and test it. Watch child drift and scaler readiness, not only MRI generation. Record an applied digest because metadata-only updates do not advance generation.
3. **Fixed to managed:** admission leaves replicas nil, MRI stops enforcing them, and the scaler validates ownership/conflicts/metrics before activating. A new child's existing InferenceSet default may bootstrap at one. The managed HPA converges to the configured minimum; MRI is not a second recurring minimum-replica writer. Until capacity reaches the minimum, report activation pending rather than autoscaling-ready.
4. **Managed policy update:** validate before replacing projected configuration; reconcile in place without a second ScaledObject/HPA. On dynamic validation failure, preserve the last accepted configuration but suspend unsafe decisions and report degradation.
5. **Managed to fixed:** mark managed scaling disabled; scaler removes its ScaledObject and associated HPA, with replica restoration disabled. MRI confirms the HPA is gone and control is released **before** enforcing the new fixed count. Removing the policy annotation alone is not proof of cleanup. Preserve cleanup identity until completion and requeue across restarts.
6. **Managed to external:** the same cleanup barrier applies, then leave the last replica count unchanged and release ownership. Install no implicit default policy. **External to managed** requires removing the external marker and confirming the previous external scaler is gone before activation.
7. **Deletion/recreation:** use owner UIDs and finalizers where needed to finish managed-scaler cleanup before deleting/reusing children. Removal/disable events must cross the MRI-child predicate boundary. A new MRI or child with a reused name must not inherit the old object's scaler state.

If the scaler is unavailable during cleanup, fixed/external handoff remains pending instead of racing an orphan HPA. RBAC must permit the relevant owner-scoped reads/status/cleanup operations; MRI should not require unconditional KEDA CRD watches when the optional integration is disabled or absent. Report missing CRDs/incompatible components explicitly. Feature-gate disablement must retain cleanup capability until managed resources are gone; it must not simply turn off their watches.

### Safe scale-down is an execution requirement

Low role-average queue/concurrency is a capacity signal, **not proof that any particular replica is idle**. In particular, prefill's zero waiting count can coexist with active prefill/KV transfer, and decode's running gauge does not include all sidecar/router work.

Before deleting a selected Workspace in the managed MRI path:

1. Mark the candidate draining and remove it from selection for new P/D work; wait for routing membership to converge.
2. Allow accepted requests, active generation, and outstanding KV transfers to finish. Use an explicit lifecycle/drain acknowledgment spanning the router/sidecar/runtime, not just a sampled role-average metric.
3. Delete only after acknowledgment. On timeout or unavailable drain state, hold the removal and report a condition/event; do not silently force-delete. A forced administrative removal is a separate operator action.
4. Recheck owner UID, target replicas, and surviving ready capacity after retries/restarts. Cancel a pending drain safely if a new scale-up requires retaining that replica. Coordinate with rolling upgrades to avoid two independent removal paths.

This requires implementation or demonstrated existing end-to-end guarantees; the current InferenceSet deletion logic is not evidence of it. Until the protocol passes load/failure tests, allow an explicitly scale-up-only preview or observation mode, **not unattended production scale-down**. HPA stabilization, PodDisruptionBudgets, and termination grace periods alone do not establish request-level drain safety. Lifecycle acknowledgments are operational safety state, not invented load metrics.

### Status and observability

Propose additive per-role MRI status containing mode, policy digest, child/scaler reference, desired/ready replicas, last successful sample time/coverage, and conditions such as `PolicyAccepted`, `AutoscalerReady`, `MetricsAvailable`, `ReplicaControlReleased`, and `DrainBlocked`. These fields are not present today and require API/generated-artifact work in the implementation PR.

The MRI controller is the sole writer of MRI status, aggregating child/scaler observations. Keep workload readiness separate: healthy pods do not prove autoscaling works, and a missing autoscaler does not necessarily make serving unavailable. Emit bounded Events for invalid configuration, conflicts, missing KEDA, unavailable metrics, stuck initialization, max-capacity limits, cleanup, and drain failures. Repeated identical failures must not create event storms.

## Signals and control semantics

### Native inputs

The following registrations were inspected in vLLM source [`8407bc0b`](https://github.com/vllm-project/vllm/blob/8407bc0bb6b0061dacb731ae3cc54b6035f112b3/vllm/v1/metrics/loggers.py). Source availability is not deployment verification.

| Native metric | Type/unit | Role and action |
| --- | --- | --- |
| `vllm:num_requests_waiting` | Gauge, requests | Prefill v1 scaling signal: waiting **inside vLLM**, not router backlog. Decode can observe it without making it a second trigger. |
| `vllm:num_requests_running` | Gauge, requests | Decode v1 scaling signal: executing model requests, not all P/D in-flight work. |
| `vllm:kv_cache_usage_perc` | Gauge, fraction `[0, 1]` | Decode observation/alert; `0.8` is 80%. Future scale-down guard, not unconditional expansion. |
| `vllm:time_to_first_token_seconds` | Histogram, seconds | Observe engine-local TTFT and tune prefill thresholds/floor. Not client-visible end-to-end P/D TTFT. |
| `vllm:request_queue_time_seconds` | Histogram, seconds | Observe engine queue delay by role; not instantaneous queue depth. |
| `vllm:request_prefill_time_seconds` | Histogram, seconds | Diagnose prefill compute/runtime/SKU behavior. |
| `vllm:request_decode_time_seconds` | Histogram, seconds | Diagnose decode duration in context of generation length. |
| `vllm:inter_token_latency_seconds` | Histogram, seconds | Observe decode latency regression under load. |
| `vllm:generation_tokens_total` | Counter, tokens | Reset-aware rate measures decode output throughput; throughput alone is neither unmet demand nor saturation. |

The generation counter is registered without `_total`; Prometheus exposition adds that suffix. Histograms expose bucket/sum/count series, not an instantaneous scalar. The existing scaler's histogram reduction is a windowed mean (`delta sum / delta count`), **not P95**. Histograms and KV utilization stay observational in v1 even though the generic scaler can accept more metrics.

### v1 decision rules

For each role, let `L` be the validated per-Workspace request-gauge average, `U` the up-threshold, and `D` the down-threshold:

- If all required samples are fresh/complete, `L > U`, and current desired replicas are ready, request scale-up within `[min, max]`.
- If samples are fresh/complete and `L < D`, request scale-down within bounds, subject to stabilization and the candidate-specific drain barrier.
- Otherwise hold. Missing, stale, non-finite, negative, or partially covered request gauges are errors, not zero load.

Reuse the [existing provisioner](https://github.com/kaito-project/keda-kaito-scaler/blob/3964e100f0093ec609b663704e726f2bb3fe8fd9/pkg/scaledobject/provisioner.go): its composite signal is 2.0 for up, 0.5 for down, and 1.0 for hold against target 1; HPA policies cap changes to one replica per permitted period. This is not an immediate optimal-capacity calculation. HPA reconciliation and stabilization may delay a step further. KEDA zero-replica cooldown is distinct from HPA 1-to-N rate limits and is outside this positive-minimum design.

Readiness gating currently affects scale-up, not an end-to-end safe scale-down protocol. A stalled new replica may block further expansion; surface this and investigate scheduling, quota, model loading, or a bad runtime instead of repeatedly allocating more replicas. Readiness must include metric coverage before a new replica joins the denominator. Serving startup/benchmark behavior must be tested for the selected runtime.

### Metric collection and availability gate

- Discover Workspaces by child owner UID and namespace, with MRI/role labels as an additional boundary. Do not merge same-named models across MRIs or clusters. vLLM does not necessarily emit Kubernetes identity labels; discovery must attach that identity.
- For service-average, sum the intended request series within each Workspace, then average over the role's complete eligible Workspace set. Verify multi-engine and tensor-parallel exposition: do not sum duplicated views of the same request or divide by worker count. Reject unsupported heterogeneous topology rather than silently changing threshold units.
- In the [current P/D topology](https://github.com/kaito-project/kaito/blob/4c450c328a21047f7fa576780a086cc1a393cc95/website/docs/prefill-decode-disaggregation.md), prefill vLLM uses port 5000; decode uses a routing sidecar on 5000 and vLLM on 5001. The current scaler discovers Workspace Services, not arbitrary engine endpoints. Verify whether `/metrics` is actually forwarded correctly; otherwise add a role-aware internal metrics Service/endpoint configuration. Do not scrape the sidecar accidentally or assume one global service port works for both roles.
- Pin KAITO, scaler, KEDA, Kubernetes, actual vLLM image digest, connector, and routing versions. Inspect representative real `/metrics` responses under load for names, TYPE, units, labels, and finite values. Repeat for newly created replicas and upgrades.
- Define sample freshness relative to configured scrape interval (proposed maximum age: two intervals), record expected versus observed eligible replicas, and invalidate partial coverage. A missing series on one replica must not contribute zero. Error propagation must reach KEDA/HPA without a downscaling fallback or synthetic low value; test cached/stale values, composite formulas, and HPA behavior end to end.
- A newly enabled policy remains pending on unavailable required metrics. An active policy holds decisions and reports degradation. Optional observation failures disable only that observation. Bounds enforcement/activation must be separately verified so error handling does not unexpectedly create a downscale.
- Use namespace-scoped discovery/RBAC and restricted network access. Do not expose arbitrary URLs through policy annotations. Any new TLS path must verify service identity; do not inherit insecure TLS behavior as a design requirement.

Requests queued before vLLM are invisible to v1. Conservative minimums and measured thresholds mitigate some cold-start delay, but this design does not promise client latency objectives during router-only backlog or unobserved work. Treat those cases as explicit scope limits, not zero demand.

### Later derived signals: native inputs only

The following names are **proposed outputs**, not metrics emitted by vLLM, KAITO, or the current scaler. `role_series` below means deduplicated, fresh native series grouped by cluster/namespace/MRI/role, with engine identity retained where necessary.

| Proposed output | Native input and derivation | Intended use |
| --- | --- | --- |
| `kaito:decode_kv_cache_usage_max` | Maximum of valid decode `vllm:kv_cache_usage_perc`; fraction | Observe hottest engine; evaluate a downscale guard. Fleet average must not hide a hot engine. |
| `kaito:decode_kv_cache_free_fraction_min` | `1 - max(role_series(vllm:kv_cache_usage_perc))`; fraction | Equivalent conservative free-cache view, not free bytes or safe request capacity, and not independent evidence from utilization. |
| `kaito:prefill_ttft_p95_seconds` | `histogram_quantile(0.95, sum of per-series rate(vllm:time_to_first_token_seconds_bucket[5m]))`, grouped by identity and `le`; seconds | Observe tail latency; require sufficient count observations. Never average per-pod P95s or call this client-visible P/D latency. |
| `kaito:decode_generation_tokens_per_second` | Sum of per-series reset-aware `rate(vllm:generation_tokens_total[5m])`; tokens/s | Observe/benchmark throughput, not an independent demand trigger. |

Apply reset handling **before** aggregation. Require enough observations, complete source coverage, and valid windows; a no-traffic histogram is not zero latency. Recording rules may supply these outputs for dashboards. Automated use needs a separately implemented authenticated collection adapter and policy contract; putting their names into the model-pod annotation will not fetch them from Prometheus.

An optional later asymmetric KV policy could allow high running requests to trigger expansion regardless of low KV usage, while allowing low running requests to request contraction only when KV usage is also safe. High KV usage with low concurrency blocks contraction and prompts investigation; it does not alone request expansion. Missing required KV data holds the decision. This **cannot** be implemented by appending KV usage to today's AND list, which would also require high KV utilization for scale-up. It needs a tested policy extension, conservative per-engine/candidate reasoning, and the same drain barrier.

Raw KV events remain routing/cache-affinity/scoring inputs, not autoscaling metrics. Cumulative token counters cannot reconstruct queued prompt-token work. Connector instrumentation, EPP load, synthetic desired-replica signals, and unverified pressure exporters are outside the initial contract.

## Implementation strategy

1. **Admission and controller contracts:** implement strict policy parsing/versioning, aligned base/Helm replica schemas without unconditional defaults, persisted role-mode-aware mutating admission and external-mode migration, consistent validation, metadata projection/filtering, digest tracking, and per-role status. Verify installation/upgrade ordering and add controller/admission tests before opening the scaler eligibility gate.
2. **Scaler integration:** add opt-in MRI owner validation, conflict checks, creation/update/removal handling, ordered handoff, and optional-component compatibility. Preserve standalone InferenceSet semantics. Add role-correct endpoint discovery and strict missing-data behavior without silently changing existing standalone aggregation defaults.
3. **Native-gauge preview:** enable independent positive-minimum scaling only for the two supported gauges after runtime verification. Until drain is proven, restrict the preview to observation/scale-up; do not advertise automatic bidirectional production scaling.
4. **Bidirectional release gate:** implement and validate drain, retries/restarts, concurrent rollouts, metric outages, and cleanup. Tune bounds and HPA behavior with workload data; publish a tested version matrix and operational rollback instructions.
5. **Later observation/policy work:** add native-derived recording rules first, then optional KV guards with an explicit adapter and asymmetric control semantics. Revisit typed API migration only with a single-source handoff plan.

### Testing and acceptance criteria

| Layer | Required evidence |
| --- | --- |
| Parser/admission | Valid YAML/JSON, duplicate/unknown keys, size/alias limits, version rejection, finite thresholds/bounds, role reorder, fixed/policy conflict, external-marker overlap, existing nil-mode migration, annotation-only updates. Exercise raw create/update payloads through API-server schema defaulting and mutating/validating admission for both generated-base and rendered Helm CRD installs/upgrades; read back persisted state. Omitted/null replicas stay nil for managed/external roles, legacy creates persist one, explicit non-nil conflicts are rejected, and both installation paths agree. |
| MRI reconciliation | Different policies on the two children, no blob leakage to workloads, stale-key removal, unrelated metadata preserved, digest acknowledgment, child drift/recreation and owner UID isolation, workload versus autoscaler conditions. |
| Scaler lifecycle | Exactly one ScaledObject/HPA per role, standalone non-regression, manual-target conflicts, policy removal events, unavailable KEDA/scaler, disable/re-enable, finalizers/restarts, fixed/external handoff with no overlapping replica writers or count restoration. Verify scaler-only rollback to external mode with a compatible retained KAITO, and legacy KAITO downgrade only after explicit fixed counts and external-writer release; persisted counts must remain stable through chart/schema rollback and subsequent reconciliation. |
| Collection/control | Correct decode vLLM endpoint, isolation across MRIs/namespaces, multi-engine units, complete/partial failures, missing series, stale cache, NaN/negative values, real observed zero, readiness gate, bounds, hysteresis and HPA rate/stabilization behavior. |
| P/D e2e | Prompt-heavy and generation-heavy bursts move only the intended role; cold starts/quota failures are visible; long generations and KV transfers survive candidate drain; concurrent rollout and scale-up cancel/retry drains safely; metric loss never causes downscale. |
| Later derivations | Reset-aware rate before aggregation, histogram count/quantile correctness, empty windows, identity/deduplication, full KV coverage, unavailable-source behavior, and asymmetric policy truth table. |

Use workload traces to compare client-visible TTFT/inter-token latency, errors, desired/ready replicas, scaling delay, and GPU cost against a fixed-capacity baseline. Engine-local histograms do not substitute for client measurements. All GPU/cluster tests belong in an explicitly approved implementation validation environment; this documentation PR does not claim they ran.

### Upgrade, rollback, and risks

- Install compatible KAITO/scaler components before enabling the opt-in feature. Keep legacy fixed MRI manifests and standalone InferenceSet autoscaling unchanged. Missing optional components must produce actionable state, not silent success.
- **KAITO downgrade:** while the compatible version is still running, transition every managed or external role to an explicit, capacity-appropriate fixed count. For managed roles, wait for ScaledObject/HPA cleanup and replica-control release acknowledgment before MRI enforces that count. For external roles, stop the external replica writer and confirm release before switching to fixed ownership. Verify persisted non-nil counts and remove managed/external annotations through the supported transition path; only then downgrade KAITO and its chart/CRDs. External mode is **not** a safe target for the pinned legacy Helm installation: its unconditional schema default converts nil to one, and the legacy controller then enforces it, potentially competing with an external autoscaler.
- **Scaler-only rollback or external handoff:** external mode is permitted only when the retained KAITO version and installed CRD/admission configuration support and preserve the external-mode contract. Wait for managed ScaledObject/HPA cleanup before starting an external writer; external mode alone does not install one. If that compatibility cannot be guaranteed, use explicit fixed counts instead. In either rollback path, do not remove the managed scaler first and strand an active HPA.
- Annotation updates are atomic blobs and less discoverable than fields; strict validation, digest-based status, examples, and a future migration path mitigate but do not remove that tradeoff.
- Role-average signals can hide hotspots and router backlog; minimum capacity, separate observation, and candidate-specific drain are essential. New replicas do not automatically rebalance existing KV/cache state.
- Conservative one-replica steps/readiness gates may react slowly to bursts. Benchmark the tradeoff rather than claiming SLO guarantees; node provisioning/model initialization are separate latency and capacity constraints.
- Define the drain acknowledgment integration and nil-mode migration mechanism in implementation review. These are release blockers, not reasons to silently rely on cooldown or today's admission behavior.

## Implementation history

- 2026-11-03: Initial proposal; source-reviewed design only. No CRD/controller/scaler behavior changed.

## References

- [Original InferenceSet autoscaling proposal](20250918-introduce_inferenceset_autoscaling.md)
- [MultiRoleInference P/D proposal](20260424-multiroleinference-pd-disaggregation.md)
- [Role-aware design note](https://github.com/andyzhangx/demo/blob/fecf817fb2f2776d9409f45964add9b6c13bf484/llm/kaito-llm-d/kaito-role-aware-autoscaling-for-pd.md)
- [Current scaler configuration and behavior snapshot](https://github.com/kaito-project/keda-kaito-scaler/blob/3964e100f0093ec609b663704e726f2bb3fe8fd9/README.md)
- [Model-pod collection source](https://github.com/kaito-project/keda-kaito-scaler/blob/3964e100f0093ec609b663704e726f2bb3fe8fd9/pkg/metricsource/modelpod_source.go)
- [vLLM production metrics documentation](https://docs.vllm.ai/en/stable/usage/metrics/)
- [vLLM native registrations snapshot](https://github.com/vllm-project/vllm/blob/8407bc0bb6b0061dacb731ae3cc54b6035f112b3/vllm/v1/metrics/loggers.py)
