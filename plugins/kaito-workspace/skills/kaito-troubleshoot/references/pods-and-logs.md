# Pods, events, and logs

Use the pod section after provisioning permits startup. Use the events/controller section from any stage where direct resource evidence is insufficient.

## Find the owned workload and classify readiness

KAITO normally creates a StatefulSet named after the Workspace in the same namespace. Check its owner UID against the Workspace, and match pod owner UIDs to the StatefulSet. Use its actual selector and desired replicas rather than assuming one pod or treating a worker pod as the serving leader.

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  get statefulset "${WORKSPACE:?}" \
  -o jsonpath='{"UID: "}{.metadata.uid}{"\nOwners: "}{.metadata.ownerReferences}{"\nGeneration: "}{.metadata.generation}{"\nSelector: "}{.spec.selector}{"\nDesired replicas: "}{.spec.replicas}{"\nStatus: "}{.status}{"\n"}'
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  get pods -l "kaito.sh/workspace=${WORKSPACE:?}" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.uid}{"\t"}{.metadata.ownerReferences}{"\t"}{.metadata.deletionTimestamp}{"\t"}{.spec.nodeName}{"\t"}{.status.phase}{"\t"}{.status.conditions}{"\n"}{end}'
```

A NotFound StatefulSet leads to Workspace events/controller evidence, not fabricated pod logs. If the pod label inventory disagrees with the workload selector or owner chain, resolve that discrepancy before diagnosing a candidate. Account for all expected pods within this one Workspace, including missing replicas.

For an affected pod, inspect init/main container state, prior termination, restart counts, images, and probe configuration:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  get pod "${POD:?}" \
  -o jsonpath='{"Init status: "}{.status.initContainerStatuses}{"\nContainer status: "}{.status.containerStatuses}{"\n"}{range .spec.containers[*]}{.name}{" image="}{.image}{" resources="}{.resources}{" startupProbe="}{.startupProbe}{" readinessProbe="}{.readinessProbe}{"\n"}{end}'
```

| Evidence | Investigation |
| --- | --- |
| No pod, or `PodScheduled=False` | Workload/pod events, desired replicas, scheduling constraints, PVC/mount status when referenced; there may be no container logs. |
| Image pull/configuration failure | Waiting reason and events, exact failing image or missing reference; no runtime issue search unless the runtime actually starts and fails. |
| Init container failure | That container's state, events, and available current/previous logs. A download or storage-auth failure is not automatically a vLLM defect. |
| Crash/restarts | Current and previous-instance logs, exit reason/code, OOM evidence, and the first causal error rather than only the final worker-exit message. |
| Running but not Ready | Startup/readiness probe failures, model initialization/download progress, container and pod readiness; `Running` alone is insufficient. |
| Pods Ready but Workspace not Ready | Reconcile current generations and Workspace conditions, including benchmark/model readiness when present; investigate controller evidence rather than launching inference requests. |

Record explicit progress when weights are downloading or initialization is advancing without failure evidence. Empty/missing logs and a young NotReady pod do not establish a runtime bug.

## Bounded container logs

For each relevant failing init/main container, start with the last 200 timestamped lines. Use the discovered container name explicitly:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  logs "${POD:?}" --container "${CONTAINER:?}" --timestamps --tail=200
```

After a restart, also read the previous instance when one exists:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${NAMESPACE:?}" --request-timeout=30s \
  logs "${POD:?}" --container "${CONTAINER:?}" --previous --timestamps --tail=200
```

If the exception or causal startup context is cut off, expand only that container's range, stating what evidence is missing. Use a time range around the failure where available. Stop expanding once the causal excerpt is captured; if rotation or access prevents recovery, report the gap. A previous-log error means previous logs are unavailable, not that the container never failed.

## Events and controller evidence

Prefer events for the exact involved-object UID, so a recreated object with the same name is not confused with its predecessor. `EVENT_NAMESPACE` is the involved resource's namespace; use the default event namespace for cluster-scoped resources:

```bash
kubectl --context "${CONTEXT:?}" --namespace "${EVENT_NAMESPACE:?}" --request-timeout=30s \
  get events --field-selector "involvedObject.uid=${OBJECT_UID:?}" \
  --sort-by=.metadata.creationTimestamp \
  -o custom-columns='FIRST:.firstTimestamp,LAST:.lastTimestamp,EVENT:.eventTime,TYPE:.type,REASON:.reason,MESSAGE:.message,COUNT:.count'
```

If a cluster-scoped object's events are not in that namespace, a UID-filtered `--all-namespaces` read can locate them; retain the UID filter. Event absence may mean expiry or a different recorder, not absence of a failure.

When events and direct state cannot explain the blocker, discover the relevant KAITO or active provisioner controller's namespace, workload selector, pods, and container. Read only its bounded logs using the commands above with that controller namespace/pod/container. Correlate timestamps and resource name/UID; avoid a cluster-wide log sweep. A controller namespace discovered from the target's installation is related evidence, not permission to inspect unrelated workloads.

Sources: [Workspace conditions and failure classification](https://github.com/kaito-project/kaito/blob/main/pkg/workspace/controllers/workspace_controller.go), [workload ownership and selectors](https://github.com/kaito-project/kaito/blob/main/pkg/workspace/manifests/manifests.go), [InferenceSet child construction](https://github.com/kaito-project/kaito/blob/main/pkg/utils/inferenceset/inferenceset.go).
