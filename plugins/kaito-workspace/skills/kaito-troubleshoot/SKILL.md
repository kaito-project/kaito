---
name: kaito-troubleshoot
description: Troubleshoot KAITO Workspace or InferenceSet startup and readiness failures. Use when provisioning is stuck, inference pods are Pending or crashing, or a Workspace or InferenceSet is not becoming Ready.
---

# KAITO startup triage

Find the earliest evidenced blocker for one Workspace. For an InferenceSet, select one failing replica before investigating its infrastructure. The output is a diagnosis and recommended remediation, not an applied fix. Request failures and performance problems after readiness belong to a separate investigation.

## Read-only boundary

An explicitly supplied context, namespace, resource kind, and name authorize reads of that target and its related infrastructure/controller evidence. Ask before cluster access when the target is ambiguous. Keep the context explicit on every request and the namespace explicit for namespaced resources.

Use `kubectl` for resource reads and logs, and `gh` for public issue research; `kubectl kaito` is optional. Stay within existing access. Report missing tools, API discovery failures, Forbidden responses, and missing evidence as limitations rather than empty or healthy results.

Recommend changes without executing them. Cluster mutations, `exec`, debug containers, port forwarding, inference requests, credential retrieval, and cloud administration are outside this skill. Keep raw evidence in the session; redact sensitive values from the report and use only non-sensitive signatures in public searches. Save artifacts only on a separate request.

## 1. Identify the target

Record the snapshot time, API version, UID, generation, deletion state, and conditions. When a condition's `observedGeneration` is present but older than the resource generation, treat it as stale evidence.

Examples use validated shell variables, set in the shell executing the command. `KIND` must be `workspaces.kaito.sh` or `inferencesets.kaito.sh`; `NAME` is the requested name. Required-variable expansions prevent empty values from silently selecting the current context or namespace.

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  get "${KIND:?}" "${NAME:?}" \
  -o jsonpath='{.apiVersion}{" "}{.kind}{" "}{.metadata.name}{"\nUID: "}{.metadata.uid}{"\nGeneration: "}{.metadata.generation}{"\nDeleting: "}{.metadata.deletionTimestamp}{"\nDesired replicas: "}{.spec.replicas}{"\nStatus: "}{.status}{"\n"}'
```

**Complete when:** the exact target is identified, or a targeting/access blocker is reported. Report a terminating target without investigating it as a fresh startup. For a tuning-only Workspace, explain that this skill covers inference readiness instead.

## 2. Select one Workspace

For a Workspace, use its name as `WORKSPACE`. For an InferenceSet, inspect child summaries before fetching any child node or pod evidence:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  get workspaces.kaito.sh -l "inferenceset.kaito.sh/created-by=${NAME:?}" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.creationTimestamp}{"\t"}{.metadata.generation}{"\t"}{.metadata.deletionTimestamp}{"\t"}{.metadata.ownerReferences}{"\t"}{.status.conditions}{"\n"}{end}'
```

Check the owner UID against the InferenceSet UID; names and labels alone are insufficient. If the labeled list is incomplete or inconsistent, inspect namespace-local Workspace identity/owner summaries to find owned children whose label is missing. Count actual owned children rather than assuming `.status.replicas` is the observed count.

Select non-deleting children with current explicit failure evidence first: for example, a condition reporting image-pull failure, a crash, OOM, quota exhaustion, or denied provisioning. Generic False/Unknown conditions and `AwaitingReconciliation` are not explicit failures. Within that group choose the oldest `creationTimestamp`, then name ascending. If none has an explicit failure, choose the oldest NotReady child, with the same tie-breaker.

A missing child is not a pod failure. If replicas are missing and there is no candidate child, inspect the InferenceSet's conditions, events, and relevant controller logs using [events and controller evidence](references/pods-and-logs.md#events-and-controller-evidence), then report the creation blocker or progress. Explicit desired replicas of zero are intentional scale-to-zero; use the served API's default for an omitted count. If all expected children report current readiness, report that no startup blocker is visible in those summaries.

**Complete when:** one Workspace is selected with a stated reason, or the parent-only outcome is reported. Other replicas remain uninvestigated beyond selection summaries.

## 3. Check provisioning and nodes

Read [provisioning](references/provisioning.md) and follow only the detected mode: Karpenter, Azure GPU provisioner, or BYO nodes. Check expected provisioning resources before their Nodes, accounting for the selected Workspace's target node count and ownership.

Inspect non-ready conditions and related events for an explanation. A False condition establishes a blocked stage, not necessarily its cause. Use targeted controller evidence when the resource status is insufficient. If node readiness is satisfied but `ResourceReady` is not, inspect the named prerequisite in the condition, such as model-weight download or a ModelMirror.

**Complete when:** the expected resources and node readiness are accounted for, or an evidenced blocker, progressing state, or evidence gap is identified. Proceed to step 4 only when provisioning permits startup; otherwise report the finding. Inspect downstream evidence only to resolve a contradiction or confirm a blocker. Infrastructure failures do not trigger speculative runtime issue searches.

## 4. Check the selected Workspace's pods

Once provisioning permits inference startup, read [pods and logs](references/pods-and-logs.md). Check the owned workload's desired/current/ready counts, all its expected pods, and the relevant init/main container states. A multi-node Workspace is one replica but can have several pods.

Classify missing workloads, scheduling, image pulls, initialization, crashes, and probe failures before collecting logs. Gather bounded logs from relevant failing containers; use events or targeted controller logs where a container never started.

**Complete when:** the earliest pod/startup blocker has supporting evidence, or startup is progressing, ready, or unresolved with a stated evidence gap. Only a failure involving vLLM or LMCache proceeds to issue research.

## 5. Correlate runtime failures

For an evidenced vLLM/LMCache startup failure, read [upstream issues](references/upstream-issues.md). Search only implicated projects, using observed versions and a sanitized error signature.

**Complete when:** the bounded search is finished, with qualified candidates, no relevant match, or a search-access limitation. An issue match is a lead, not proof that it caused this failure.

## 6. Report the snapshot

Return a concise inline report containing:

- **Target and scope:** context, namespace, resource, selected replica and selection reason, snapshot time; identify uninspected replicas/stages.
- **Finding:** earliest blocked stage, progressing state, no observed readiness blocker, or unresolved diagnosis.
- **Evidence and cause:** resource/container, condition or event reason, timestamps and short redacted excerpts, identifying current/previous log instances; separate observed facts from the likely cause and uncertainty.
- **Next action:** a recommended fix or the exact missing evidence/access needed. Nothing has been changed.
- **Upstream:** up to three qualified issue candidates, no relevant match, search unavailable, or not applicable.

**Complete when:** the finding is supported by cited evidence and the investigation's limits are explicit. A NotReady snapshot alone is not a failure diagnosis. Wait or watch only when explicitly requested; an inconclusive first replica is reported as such rather than silently widening to other replicas.
