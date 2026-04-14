---
title: Retrieval-Augmented Generation (RAG)
---

This document presents how to use the KAITO `ragengine` Custom Resource Definition (CRD) for retrieval-augmented generation workflow. By creating a RAGEngine resource, you can quickly stand up a service that indexes documents and queries them in conjunction with an existing LLM inference endpoint—no need to custom-build pipelines. This enables your large language model to answer questions based on your own private content.

## Installation

> Be sure you've cloned this repo and followed [kaito workspace installation](./installation.md) if you plan to use local embedding model. RAGEngine needs the gpu-provisioner component to provision GPU nodes.

```bash
helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-ragengine kaito/ragengine \
  --namespace kaito-ragengine \
  --create-namespace \
  --take-ownership
```

#### Using Nightly Builds (for testing purpose)
<details>
To install the RAG engine controller using the latest nightly image from GHCR:

:::caution
Nightly builds are **not recommended for production use**. They are built from the latest `main` branch and may contain untested or incomplete features.
:::

```bash
helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-ragengine kaito/ragengine \
  --namespace kaito-ragengine \
  --create-namespace \
  --set image.repository=ghcr.io/kaito-project/kaito/ragengine \
  --set image.tag=nightly-latest \
  --set image.pullPolicy=Always \
  --take-ownership
```

The nightly image is tagged with:

- **`nightly-latest`** — always points to the most recent successful nightly build
- **`nightly-<sha>`** — pinned to a specific commit (12-character short SHA)
</details>

## Verify installation
You can run the following commands to verify the installation of the controllers were successful.

Check status of the Helm chart installations.

```bash
helm list -n kaito-ragengine
```

Check status of the `ragengine`.

```bash
kubectl describe deploy ragengine -n kaito-ragengine
```

## Clean up

```bash
helm uninstall kaito-ragengine
```

## Usage

### Prerequisite
Before creating a RAGEngine, ensure you have an accessible model inference endpoint. This endpoint can be:

1.	A model deployed through KAITO Workspace CRD (e.g., a local Hugging Face model, a vLLM instance, etc.).
2.	An external API (e.g., Huggingface service or other REST-based LLM providers).

### Define the RAGEngine
Create a YAML manifest defining your RAGEngine. Key fields under spec include:

Embedding: how to generate vector embeddings for your documents. You may choose remote or local (one must be left unset if you pick the other):

```yaml
embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
```
InferenceService: points to the LLM endpoint that RAGEngine will call for final text generation.
```yaml
inferenceService:
  url: "<inference-url>/v1/completions"
```
Users also need to specify the GPU SKU used for inference in the `compute` spec. For example,

```yaml
apiVersion: kaito.sh/v1alpha1
kind: RAGEngine
metadata:
  name: ragengine-start
spec:
  compute:
    instanceType: "Standard_NC4as_T4_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
    contextWindowSize: 512    # Modify to fit the model's context window.
```

### Guardrail And Audit Runtime Environment Variables

The current output-guardrails and remote-audit integration is configured through environment variables on the generated RAG service pod. These are not currently part of the `RAGEngine.spec` schema.

In practice, that means:

- define your `RAGEngine` resource as usual
- wait for the generated deployment to exist
- set the runtime environment variables on that deployment

Example:

```bash
kubectl set env deployment/ragengine-start -n default \
  OUTPUT_GUARDRAILS_ENABLED=true \
  OUTPUT_GUARDRAILS_FAIL_OPEN=true \
  OUTPUT_GUARDRAILS_ACTION_ON_HIT=redact \
  OUTPUT_GUARDRAILS_REGEX_PATTERNS='https?://\\S+' \
  OUTPUT_GUARDRAILS_AUDIT_SINKS=log,remote \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL=https://audit.example.com/events \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL=https://audit.example.com/reports \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_HEADER=X-Event-Auth \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN=<event-token> \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_HEADER=X-Report-Auth \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN=<report-token>
```

Recommended variables:

| Variable | Meaning |
|----------|---------|
| `OUTPUT_GUARDRAILS_ENABLED` | Enables output scanning for chat completions |
| `OUTPUT_GUARDRAILS_FAIL_OPEN` | Returns the original model output if scanning fails |
| `OUTPUT_GUARDRAILS_ACTION_ON_HIT` | `redact` or `block` |
| `OUTPUT_GUARDRAILS_REGEX_PATTERNS` | Comma-separated regex scanners |
| `OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS` | Comma-separated substring scanners |
| `OUTPUT_GUARDRAILS_BLOCK_MESSAGE` | Replacement text when `block` is used |
| `OUTPUT_GUARDRAILS_STREAM_HOLDBACK_CHARS` | Streaming holdback window for incremental scanning |
| `OUTPUT_GUARDRAILS_AUDIT_SINKS` | `log`, `file`, `remote`, or a comma-separated combination |
| `OUTPUT_GUARDRAILS_AUDIT_FILE_PATH` | File sink path for JSONL audit records |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL` | Shared remote endpoint fallback |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL` | Endpoint for staged streaming events |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL` | Endpoint for final reports |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_*` | Shared remote auth fallback |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_*` | Event-specific auth override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_*` | Report-specific auth override |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_TIMEOUT_SECONDS` | Remote request timeout |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_MAX_RETRIES` | Number of remote retries |
| `OUTPUT_GUARDRAILS_AUDIT_REMOTE_RETRY_BACKOFF_SECONDS` | Retry backoff multiplier |
| `OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS` | Maximum shutdown wait for pending background audit deliveries |
| `OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS` | Maximum number of concurrent background audit deliveries before backpressure is applied |
| `OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY` | Overflow policy when the in-flight limit is reached: `drop` or `block` |

If both the shared remote auth variables and the event/report-specific auth variables are set, the endpoint-specific values take precedence.

On shutdown, RAGEngine now drains pending background audit deliveries before exit, up to `OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS`. This reduces the chance of losing the last batch of staged events or final reports during pod termination or worker reload.

While the process is still running, background audit delivery is now also bounded by `OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS`. When that limit is reached, `OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY=drop` drops the new background delivery and records metrics, while `block` applies backpressure by awaiting delivery inline on the request path.

The default policy remains `drop`. This favors protecting chat latency when the audit sink is already saturated. Use `block` only when preserving audit completeness is more important than keeping the request path isolated from audit sink slowness.

The Prometheus `/metrics` endpoint also exposes shutdown-drain observability for this path:

- `rag_output_guardrails_audit_shutdown_drain_total{status="success|timeout"}`
- `rag_output_guardrails_audit_shutdown_drain_latency_seconds{status="success|timeout"}`
- `rag_output_guardrails_audit_cancelled_deliveries_total`
- `rag_output_guardrails_audit_in_flight_background_tasks`
- `rag_output_guardrails_audit_backpressure_total{policy="drop|block",delivery_type="event|report"}`
- `rag_output_guardrails_audit_dropped_deliveries_total{delivery_type="event|report"}`

Basic monitoring examples are available in [examples/RAG/monitoring/ragengine-output-guardrails-prometheusrule.yaml](https://github.com/kaito-project/kaito/blob/main/examples/RAG/monitoring/ragengine-output-guardrails-prometheusrule.yaml), [examples/RAG/monitoring/ragengine-output-guardrails-dashboard.json](https://github.com/kaito-project/kaito/blob/main/examples/RAG/monitoring/ragengine-output-guardrails-dashboard.json), and [examples/RAG/monitoring/kustomization.yaml](https://github.com/kaito-project/kaito/blob/main/examples/RAG/monitoring/kustomization.yaml). These examples intentionally omit `metadata.namespace` so each user can apply them into their own namespace with `kubectl -n <namespace> apply -f ...` or `kubectl apply -k ...`, or inject the namespace through their preferred overlay tool. A ready-to-use Kustomize overlay example is also included under `examples/RAG/monitoring/kustomize/` for teams that want to override namespace, alert severity, and the Grafana Prometheus datasource UID together.

:::note
Because these settings are runtime env vars rather than CRD fields today, operators should re-check them after a CR-driven rollout or reconciliation that recreates the generated deployment.
:::

### Vector Store Backends

RAGEngine supports multiple vector store backends. The backend is selected via the `storage.vectorDB` field in the RAGEngine spec.

#### FAISS (Default)

If no `storage.vectorDB` is specified, RAGEngine uses FAISS as the default in-memory vector store. FAISS is lightweight and requires no external dependencies, making it ideal for development and small-scale deployments.

```yaml
apiVersion: kaito.sh/v1beta1
kind: RAGEngine
metadata:
  name: ragengine-faiss
spec:
  compute:
    instanceType: "Standard_NV36ads_A10_v5"
    labelSelector:
      matchLabels:
        apps: ragengine-faiss
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    contextWindowSize: 4096
```

#### Qdrant

[Qdrant](https://qdrant.tech/) is a high-performance vector database that supports hybrid search (dense + sparse embeddings). When using Qdrant as the backend, RAGEngine automatically enables:

- **Hybrid search** with dense embeddings (from your configured embedding model) and BM25 sparse embeddings (via [fastembed](https://github.com/qdrant/fastembed))
- **Reciprocal Rank Fusion (RRF)** to combine dense and sparse retrieval results
- **Automatic index restore** on pod restart — indexes are restored from Qdrant collections, so no data is lost even without a PVC
- **All CRUD operations** (list, update, delete, document existence checks) operate directly against Qdrant, ensuring consistency after restarts

**Step 1: Deploy Qdrant in your cluster**

You can use the provided example manifest:

```bash
kubectl apply -f examples/RAG/qdrant-deployment.yaml
```

This deploys a single-replica Qdrant instance with a PersistentVolumeClaim for data durability. Verify it's ready:

```bash
kubectl wait --for=condition=available deployment/qdrant --timeout=120s
```

**Step 2: Create the RAGEngine with Qdrant backend**

```yaml
apiVersion: kaito.sh/v1beta1
kind: RAGEngine
metadata:
  name: ragengine-qdrant
spec:
  compute:
    instanceType: "Standard_NV36ads_A10_v5"
    labelSelector:
      matchLabels:
        apps: ragengine-qdrant
  storage:
    vectorDB:
      engine: "qdrant"
      url: "http://qdrant.default.svc.cluster.local:6333"
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    contextWindowSize: 4096
```

See [examples/RAG/kaito_ragengine_qdrant.yaml](https://github.com/kaito-project/kaito/blob/main/examples/RAG/kaito_ragengine_qdrant.yaml) for the full example.

:::tip
Since Qdrant persists data in its own storage, the RAGEngine pod can restart without losing indexed documents. On startup, the service automatically discovers existing Qdrant collections and restores them as indexes.
:::

### Persistent Storage (Optional)
RAGEngine supports persistent storage for vector indexes using Kubernetes PersistentVolumeClaims (PVC). When configured, indexed documents are automatically saved to persistent storage and restored on pod restarts. Users can also manually persist and load indexes using the RAG service API endpoints (`/persist/{index_name}` and `/load/{index_name}`).

**Example with Azure Disk PVC:**

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
  name: ragengine-with-pvc
spec:
  compute:
    instanceType: "Standard_NC4as_T4_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  storage:
    persistentVolume:
      persistentVolumeClaim: pvc-ragengine-vector-db
      mountPath: /mnt/vector-db
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
    contextWindowSize: 512
```

**Key points:**
- Indexes are automatically persisted when the pod terminates (via PreStop lifecycle hook)
- Indexes are automatically restored when the pod starts (via PostStart lifecycle hook)
- Snapshots are stored with timestamps and the 5 most recent snapshots are retained
- Storage class should support ReadWriteOnce access mode

### Apply the manifest
After you create your YAML configuration, run:
```sh
kubectl apply -f examples/RAG/kaito_ragengine_phi_3.yaml
```

## AutoIndexer

The AutoIndexer is a companion controller that automatically indexes documents from a Git repository into a RAGEngine index. It watches for changes in the repository and keeps the index up to date.

### Install the AutoIndexer Controller

```bash
helm install kaito-autoindexer \
  oci://ghcr.io/kaito-project/charts/autoindexer \
  --version 0.0.0-dev.2 \
  --namespace kaito-autoindexer \
  --create-namespace
```

### Deploy an AutoIndexer Resource

Create a YAML manifest for the AutoIndexer and apply it:

```yaml
apiVersion: autoindexer.kaito.sh/v1alpha1
kind: AutoIndexer
metadata:
  name: my-wiki-autoindexer
spec:
  credentials:
    secretRef:
      key: token
      name: ado-pat-secret
    type: SecretRef
  dataSource:
    git:
      branch: wikiMaster
      paths:
      - '*.md'
      repository: <your-git-repo-url>
    type: Git
  driftRemediationPolicy:
    strategy: Manual
  indexName: my-wiki-index
  ragEngine: ragengine-qdrant
```

```bash
kubectl apply -f autoindexer.yaml
```

**Key fields:**
- `credentials`: Reference to a Kubernetes Secret containing the Git access token.
- `dataSource.git.branch`: The branch to watch for changes.
- `dataSource.git.paths`: Glob patterns for files to index (e.g., `*.md` for Markdown files).
- `dataSource.git.repository`: The Git repository URL.
- `driftRemediationPolicy.strategy`: How to handle drift — `Manual` requires explicit re-indexing triggers.
- `indexName`: The name of the RAGEngine index to populate.
- `ragEngine`: The name of the RAGEngine resource to target.

You can monitor the AutoIndexer job status with:

```bash
kubectl get autoindexer
kubectl get jobs | grep autoindexer
```
