# KAITO RAGEngine Helm Chart

KAITO RAGEngine provides Retrieval-Augmented Generation (RAG) capabilities for AI workloads in Kubernetes. This Helm chart installs the RAGEngine controller that manages RAGEngine custom resources, enabling you to deploy and manage RAG services with embedding models and vector stores.

## Install

```bash
helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito/ragengine \
  --namespace kaito-ragengine \
  --create-namespace \
  --take-ownership
```

## Prerequisites

- Kubernetes 1.20+
- Helm 3.0+
- KAITO Workspace controller (if using with workspace resources)

## Usage

After installing the RAGEngine Helm chart, you can create RAGEngine resources to deploy RAG services:

```yaml
apiVersion: kaito.sh/v1alpha1
kind: RAGEngine
metadata:
  name: ragengine-example
spec:
  compute:
    instanceType: "Standard_NC6s_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
```

### Runtime Environment Configuration For The RAG Service Pod

The Helm chart installs the RAGEngine controller. The output-guardrails and remote-audit settings described in the preset service are runtime environment variables for the generated RAG service pod, not Helm values on the controller chart and not fields in the `RAGEngine` CRD schema.

That means these settings should be applied to the RAG service deployment created for a specific `RAGEngine` resource. For example:

```bash
kubectl set env deployment/ragengine-example -n <namespace> \
  OUTPUT_GUARDRAILS_ENABLED=true \
  OUTPUT_GUARDRAILS_ACTION_ON_HIT=redact \
  OUTPUT_GUARDRAILS_REGEX_PATTERNS='https?://\\S+' \
  OUTPUT_GUARDRAILS_AUDIT_SINKS=log,remote \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL=https://audit.example.com/events \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL=https://audit.example.com/reports
```

Common runtime variables include:

| Variable | Purpose |
|----------|---------|
| `OUTPUT_GUARDRAILS_ENABLED` | Enables output guardrail scanning on `/v1/chat/completions` |
| `OUTPUT_GUARDRAILS_FAIL_OPEN` | Allows responses to pass through if guardrail evaluation fails |
| `OUTPUT_GUARDRAILS_ACTION_ON_HIT` | Chooses `redact` or `block` when a scanner triggers |
| `OUTPUT_GUARDRAILS_REGEX_PATTERNS` | Comma-separated regex scanners |
| `OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS` | Comma-separated substring scanners |
| `OUTPUT_GUARDRAILS_BLOCK_MESSAGE` | Replacement message for `block` mode |
| `OUTPUT_GUARDRAILS_STREAM_HOLDBACK_CHARS` | Holdback buffer for streaming inspection |
| `OUTPUT_GUARDRAILS_AUDIT_SINKS` | Audit sinks such as `log`, `file`, or `remote` |
| `OUTPUT_GUARDRAILS_AUDIT_FILE_PATH` | JSONL target for the `file` sink |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL` | Shared fallback URL for remote event/report delivery |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL` | Dedicated remote endpoint for staged streaming events |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL` | Dedicated remote endpoint for final audit reports |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_HEADER` | Shared fallback auth header |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN` | Shared fallback auth token |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN_PREFIX` | Shared fallback token prefix, for example `Bearer ` |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_HEADER` | Event-specific auth header override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN` | Event-specific auth token override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN_PREFIX` | Event-specific token prefix override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_HEADER` | Report-specific auth header override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN` | Report-specific auth token override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN_PREFIX` | Report-specific token prefix override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_TIMEOUT_SECONDS` | Remote sink timeout |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_MAX_RETRIES` | Remote sink retry count |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_RETRY_BACKOFF_SECONDS` | Retry backoff multiplier |
| `OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS` | Maximum shutdown wait for pending background audit deliveries |
| `OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS` | Maximum number of concurrent background audit deliveries before backpressure is applied |
| `OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY` | Overflow policy when the in-flight limit is reached: `drop` or `block` |

If both shared and event/report-specific remote settings are present, the endpoint-specific settings win. If only the shared remote settings are present, both staged events and final reports use the shared endpoint and token.

During shutdown, RAGEngine now drains pending background audit deliveries before exiting, up to `OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS`. This is intended to preserve the tail end of audit traffic during pod termination and worker reload.

During normal serving, background audit delivery is also bounded by `OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS`. If that limit is reached, `OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY=drop` drops the new delivery and records the overflow, while `block` waits for delivery inline instead of creating another background task.

The default remains `drop` because this keeps audit sink overload from spilling directly into user-facing chat latency. Teams that need stricter audit completeness can switch to `block`, but they should treat that as a latency tradeoff rather than a free improvement.

These shutdown-drain metrics are available on the existing Prometheus `/metrics` endpoint:

- `rag_output_guardrails_audit_shutdown_drain_total{status="success|timeout"}`
- `rag_output_guardrails_audit_shutdown_drain_latency_seconds{status="success|timeout"}`
- `rag_output_guardrails_audit_cancelled_deliveries_total`
- `rag_output_guardrails_audit_in_flight_background_tasks`
- `rag_output_guardrails_audit_backpressure_total{policy="drop|block",delivery_type="event|report"}`
- `rag_output_guardrails_audit_dropped_deliveries_total{delivery_type="event|report"}`

Reusable monitoring examples for these metrics are available in `examples/RAG/monitoring/`, including a `PrometheusRule` for the two warning alerts and a Grafana dashboard `ConfigMap` for the key shutdown-drain and backlog views.

Current limitation: these guardrail runtime settings are not yet modeled as `RAGEngine.spec` fields. If the controller later rolls the generated deployment because of a CR-driven update, operators should verify that their runtime env overrides are still present.

### Using Persistent Storage

To enable persistent storage for vector indexes, add a `storage` specification:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-ragengine-vector-db
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: managed-csi-premium
  resources:
    requests:
      storage: 50Gi
---
apiVersion: kaito.sh/v1alpha1
kind: RAGEngine
metadata:
  name: ragengine-with-storage
spec:
  compute:
    instanceType: "Standard_NC6s_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  storage:
    persistentVolumeClaim: pvc-ragengine-vector-db
    mountPath: /mnt/vector-db
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
```

With persistent storage configured, vector indexes are automatically saved during pod termination and restored on startup.

## Values

| Key                          | Type   | Default                                      | Description                                                   |
|------------------------------|--------|----------------------------------------------|---------------------------------------------------------------|
| affinity                     | object | `{}`                                         | Pod affinity settings                                         |
| cloudProviderName            | string | `"azure"`                                    | Karpenter cloud provider name. Values can be "azure" or "aws" |
| image.pullPolicy             | string | `"IfNotPresent"`                             | Image pull policy                                             |
| image.repository             | string | `"mcr.microsoft.com/aks/kaito/ragengine"`    | RAGEngine controller image repository                         |
| image.tag                    | string | `"0.0.1"`                                    | RAGEngine controller image tag                                |
| imagePullSecrets             | list   | `[]`                                         | Image pull secrets                                            |
| nodeSelector                 | object | `{}`                                         | Node selector for pod assignment                              |
| podAnnotations               | object | `{}`                                         | Pod annotations                                               |
| podSecurityContext.runAsNonRoot | bool | `true`                                       | Run container as non-root user                                |
| presetRagRegistryName        | string | `"aimodelsregistrytest.azurecr.io"`          | Registry for preset RAG service images                        |
| presetRagImageName           | string | `"kaito-rag-service"`                        | Name of the preset RAG service image                          |
| presetRagImageTag            | string | `"0.3.2"`                                    | Tag of the preset RAG service image                           |
| replicaCount                 | int    | `1`                                          | Number of replicas for the RAGEngine controller              |
| resources.limits.cpu         | string | `"500m"`                                     | CPU resource limits                                           |
| resources.limits.memory      | string | `"128Mi"`                                    | Memory resource limits                                        |
| resources.requests.cpu       | string | `"10m"`                                      | CPU resource requests                                         |
| resources.requests.memory    | string | `"64Mi"`                                     | Memory resource requests                                      |
| securityContext.allowPrivilegeEscalation | bool | `false`                           | Allow privilege escalation                                    |
| securityContext.capabilities.drop[0] | string | `"ALL"`                               | Capabilities to drop                                          |
| tolerations                  | list   | `[]`                                         | Pod tolerations                                               |
| webhook.port                 | int    | `9443`                                       | Webhook server port                                           |

## Contributing

Please refer to the [KAITO project contribution guidelines](https://github.com/kaito-project/kaito/blob/main/CONTRIBUTING.md) for information on how to contribute to this project.
