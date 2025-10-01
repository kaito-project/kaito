---
title: Proposal for Auto Indexer for RAG
authors:
  - "brfole"
reviewers:
creation-date: 2025-09-05
last-updated: 2025-09-05
status: provisional
---

# Auto Indexer for RAG

## Summary

This proposal introduces an Auto Indexer Custom Resource Definition (CRD) for KAITO that enables automatic creation and maintenance of RAG indexes from various data sources. The Auto Indexer will periodically fetch data from configured sources, process it into documents, and update RAG indexes without manual intervention.

## Motivation

Currently, KAITO's RAG engine requires manual document indexing through API calls. While this provides flexibility, it creates operational overhead for users who want to:

1. **Automatically sync data sources** (Git repositories, blob storage, databases) with RAG indexes
2. **Keep indexes up-to-date** without manual intervention when source data changes
3. **Scale document processing** for large datasets across multiple sources
4. **Standardize data ingestion** workflows across different teams and use cases

### Goals

- **Declarative Configuration**: Users define data sources and sync policies via YAML
- **Single-Source Support**: Start with support for GitHub repositories and Static Files, and in the future expand to other entities like blob storage, databases, and other common data sources
- **Flexible Scheduling**: Support for single-run, cron-based, and long running indexing operations
- **Incremental Updates**: Efficient processing of only changed documents where possible
- **Drift Detection**: Have drift detection per AutoIndexer to validate if there have been changes to the indexed data.
- **Error Handling**: Retry mechanisms and status reporting
- **Integration**: Seamless integration with existing RAGEngine CRDs

### Non-Goals

- Real-time streaming data ingestion (initial version focuses on batch processing)
- Complex data transformation pipelines (focuses on document extraction and basic metadata)
- Custom authentication beyond standard Kubernetes secrets

## API Design

The AutoIndexer introduces a new Custom Resource Definition (CRD) to KAITO's API.

### CRD Definition

```go
// AutoIndexer is the Schema for the autoindexer API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=autoindexers,scope=Namespaced,categories=autoindexer,shortName=ragai
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="ResourceReady",type="string",JSONPath=".status.conditions[?(@.type==\"ResourceReady\")].status",description=""
// +kubebuilder:printcolumn:name="Scheduled",type="string",JSONPath=".status.conditions[?(@.type==\"AutoIndexerScheduled\")].status",description=""
// +kubebuilder:printcolumn:name="Indexing",type="string",JSONPath=".status.conditions[?(@.type==\"AutoIndexerIndexing\")].status",description=""
// +kubebuilder:printcolumn:name="Error",type="string",JSONPath=".status.conditions[?(@.type==\"AutoIndexerError\")].status",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type AutoIndexer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AutoIndexerSpec `json:"spec,omitempty"`

	Status AutoIndexerStatus `json:"status,omitempty"`
}

// AutoIndexerSpec defines the desired state of AutoIndexer
type AutoIndexerSpec struct {

	// RAGEngineRef references the RAGEngine resource to use for indexing
	// +kubebuilder:validation:Required
	RAGEngineRef RAGEngineReference `json:"ragEngineRef"`

	// IndexName is the name of the index where documents will be stored
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9\-]*[a-z0-9]$`
	IndexName string `json:"indexName"`

	// DataSource defines where to retrieve documents for indexing
	// +kubebuilder:validation:Required
	DataSource DataSourceSpec `json:"dataSource"`

	// Credentials for private repositories
	// +optional
	Credentials *CredentialsSpec `json:"credentials,omitempty"`

	// Schedule defines when the indexing should run (cron format)
	// +optional
	// +kubebuilder:validation:Pattern=`^(@(annually|yearly|monthly|weekly|daily|hourly|reboot))|(@every (\d+(ns|us|µs|ms|s|m|h))+)|((((\d+,)+\d+|(\d+(\/|-)\d+)|\d+|\*) ?){5,7})$`
	Schedule *string `json:"schedule,omitempty"`

	// RetryPolicy defines how failed indexing jobs should be retried
	// +optional
	RetryPolicy *RetryPolicySpec `json:"retryPolicy,omitempty"`

	// Suspend can be set to true to suspend the indexing schedule
	// This will also suspend any drift detection for data sources
	// +optional
	Suspend *bool `json:"suspend,omitempty"`
}

// RAGEngineReference defines a reference to a ragengine object
type RAGEngineReference struct {
	// Name defines the ragengine name
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace defines the namespace of the ragengine
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// DataSourceSpec defines the source of documents to be indexed
type DataSourceSpec struct {
	// Type specifies the data source type
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Git;Static
	Type DataSourceType `json:"type"`

	// Git defines configuration for Git repository data sources
	// +optional
	Git *GitDataSourceSpec `json:"git,omitempty"`

	// Static defines configuration for static data sources
	// +optional
	Static *StaticDataSourceSpec `json:"static,omitempty"`
}

// DataSourceType defines the supported data source types
// +kubebuilder:validation:Enum=Git;Static
type DataSourceType string

const (
	DataSourceTypeGitHub DataSourceType = "Git"
	DataSourceTypeStatic DataSourceType = "Static"
)

// GitHubDataSourceSpec defines GitHub repository configuration
type GitDataSourceSpec struct {
	// Repository URL
	// +kubebuilder:validation:Required
	RepositoryURL string `json:"repositoryURL"`

	// Branch to checkout (default: main)
	// +kubebuilder:validation:Required
	Branch string `json:"branch"`

	// Commit SHA to checkout. If included, only this commit will be put into the index
	// +optional
	Commit *string `json:"commit,omitempty"`

	// Specific paths to index within the repository.
	// Can be directories, specific files, or specific extension types: /src, main.py, *.go
	// +optional
	Paths []string `json:"paths,omitempty"`

	// Paths to exclude from indexing. ExcludePaths takes priority over Paths.
	// Can be directories, specific files, or specific extension types: /src, main.py, *.go
	// +optional
	ExcludePaths []string `json:"excludePaths,omitempty"`
}

// APIDataSourceSpec defines REST API configuration
type StaticDataSourceSpec struct {
	// data endpoint URLs that should point to individual UTF-8 or pdf files.
	// +kubebuilder:validation:Required
	Endpoints []string `json:"endpoints"`
}

// CredentialsSpec defines authentication credentials
type CredentialsSpec struct {
	// Type specifies the credential type
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=SecretRef
	Type CredentialType `json:"type"`

	// Secret reference containing credentials
	// +optional
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
}

// CredentialType defines the supported credential types
// +kubebuilder:validation:Enum=SecretRef
type CredentialType string

const (
	CredentialTypeSecretRef CredentialType = "SecretRef"
)

// SecretKeyRef references a key in a Secret
type SecretKeyRef struct {
	// Secret name
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key within the secret
	// +kubebuilder:validation:Required
	Key string `json:"key"`
}

// RetryPolicySpec defines retry behavior for failed operations
type RetryPolicySpec struct {
	// Maximum number of retries applied to failed indexing jobs
	// Default is 3
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	// +optional
	MaxRetries *int32 `json:"maxRetries,omitempty"`
}

// AutoIndexerStatus defines the observed state of AutoIndexer
type AutoIndexerStatus struct {
	// LastIndexed timestamp of the last successful indexing
	// +optional
	LastIndexed *metav1.Time `json:"lastIndexed,omitempty"`

	// LastCommit is the last processed commit hash for Git sources
	// +optional
	LastCommit *string `json:"lastCommit,omitempty"`

	// Phase represents the current phase of the AutoIndexer
	// +optional
	// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Retrying;Unknown
	Phase AutoIndexerPhase `json:"phase,omitempty"`

	// SuccessfulRunCount tracks successful indexing runs
	SuccessfulRunCount int32 `json:"successfulRunCount"`

	// ErrorRunCount tracks failed indexing runs
	ErrorRunCount int32 `json:"errorRunCount"`

	// Number of documents processed in the last run
	DocumentsProcessed int32 `json:"documentsProcessed"`

	// NextScheduledRun shows when the next indexing is scheduled
	// +optional
	NextScheduledRun *metav1.Time `json:"nextScheduledRun,omitempty"`

	// observedGeneration represents the .metadata.generation of the Job or CronJob created in the last run
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Errors from the last indexing operation
	// +optional
	Errors []string `json:"errors,omitempty"`

	// Conditions represent the current service state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AutoIndexerPhase defines the current phase of the AutoIndexer
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Retrying;Unknown
type AutoIndexerPhase string

const (
	AutoIndexerPhasePending   AutoIndexerPhase = "Pending"
	AutoIndexerPhaseRunning   AutoIndexerPhase = "Running"
	AutoIndexerPhaseCompleted AutoIndexerPhase = "Completed"
	AutoIndexerPhaseFailed    AutoIndexerPhase = "Failed"
	AutoIndexerPhaseRetrying  AutoIndexerPhase = "Retrying"
	AutoIndexerPhaseUnknown   AutoIndexerPhase = "Unknown"
)
```



### Data Source Example


#### Static

This example demonstrates how to configure an AutoIndexer to periodically fetch and index documents from a set of static file endpoints (such as PDFs or text files hosted on HTTP/S). This is useful for regularly updating a RAG index with files that are externally hosted and do not change frequently.

```yaml
apiVersion: kaito.ai/v1alpha1
kind: AutoIndexer
metadata:
	name: static-files-indexer
spec:
	ragEngineRef:
		name: my-ragengine
		namespace: default
	indexName: my-static-index
	dataSource:
		type: Static
		static:
			endpoints:
				- https://example.com/docs/file1.pdf
				- https://example.com/docs/file2.txt
	schedule: "@daily"
```


#### Git

This example shows how to configure an AutoIndexer to index documents from a Git repository. The indexer will clone the specified repository, process only the listed paths (e.g., `docs/` and `src/`), and exclude any files in the `test/` directory. The schedule is set to run every day at 2am, ensuring the index stays up-to-date with the latest changes in the repository.

```yaml
apiVersion: kaito.ai/v1alpha1
kind: AutoIndexer
metadata:
	name: github-indexer
spec:
	ragEngineRef:
		name: my-ragengine
		namespace: default
	indexName: my-git-index
	dataSource:
		type: Git
		git:
			repositoryURL: https://github.com/org/repo.git
			branch: main
			paths:
				- docs/
				- src/
			excludePaths:
				- test/
	schedule: "0 2 * * *" # every day at 2am
```


##### Single Commit

This example demonstrates how to index a specific commit from a Git repository. The AutoIndexer will fetch only the state of the repository at the given commit SHA, rather than tracking ongoing changes. This is useful for one-time or point-in-time indexing of a codebase or documentation snapshot.

```yaml
apiVersion: kaito.ai/v1alpha1
kind: AutoIndexer
metadata:
	name: github-single-commit-indexer
spec:
	ragEngineRef:
		name: my-ragengine
		namespace: default
	indexName: my-git-index
	dataSource:
		type: Git
		git:
			repositoryURL: https://github.com/org/repo.git
			branch: main
			commit: "abc123def456"
			paths:
				- docs/
```

### Credential Spec


This section shows how to provide credentials to the AutoIndexer for accessing private data sources, such as private GitHub repositories. The credentials are referenced from a Kubernetes Secret, ensuring sensitive information is not stored directly in the CRD.

Example:
```yaml
spec:
	credentials:
		type: SecretRef
		secretRef:
			name: github-token
			key: token
```
Where `github-token` is a Kubernetes secret containing the access token under the key `token`.

#### Future: Secret Store CSI Driver

In the future, support for [Secret Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/) can be added to allow integration with external secret management systems (e.g., AWS Secrets Manager, Azure Key Vault, HashiCorp Vault). This would enable referencing secrets managed outside Kubernetes, improving security and flexibility for enterprise deployments.

## Controller Design

### Reconcile Lifecycle & Controller Responsibilities

The AutoIndexer controller is responsible for ensuring the desired state of each AutoIndexer resource is reflected in the cluster and the external RAG engine. The controller's reconcile loop is designed to be idempotent, robust, and observable. Key responsibilities include:

- Creating or patching a child Job or CronJob based on the presence of `spec.schedule`.
- Ensuring child jobs have the correct pod template, including service account, secret mounts, resource limits, and security context.
- Respecting the `suspend` field: for CronJobs, set `.spec.suspend=true`; for Jobs, do not create new runs if suspended.
- Watching for job completion by monitoring ConfigMaps written by jobs, or by reading Job status.
- Updating the AutoIndexer CRD status with results from the latest run, including `LastIndexed`, `DocumentsProcessed`, `LastCommit`, and error conditions.
- Handling deletion and finalizer logic, ensuring external resources are cleaned up before the CRD is removed.

The controller should use exponential backoff for retries, and ensure that status updates are only made when the observed generation matches the current spec. When running multiple controller replicas, leader election should be enabled to avoid race conditions.

**Operational best practices:**
- Set `concurrencyPolicy: Forbid` on CronJobs to prevent overlapping runs for the same AutoIndexer.
- Limit job history with `successfulJobsHistoryLimit` and `failedJobsHistoryLimit`.
- Use `activeDeadlineSeconds` to bound job runtime and avoid runaway pods.
- Use minimal RBAC for both controller and job pods, and mount credentials as secrets.
- Set OwnerReferences on all child resources for automatic garbage collection.

This design ensures that the controller is robust, observable, and easy to operate, while providing a clear separation of concerns between the controller and the indexing jobs.

### Validation webhook

Admission webhooks can be used to reduce invalid objects and inject defaults before the controller acts. They add operational complexity (certs, admission server), but are valuable in production.


**Validating webhook:**
- Confirm referenced `RAGEngineRef` exists (optionally check it is `Ready`).
- Validate `credentials.secretRef` exists and contains the expected key when `Credentials.Type == SecretRef`.
- Validate `schedule` syntax using a robust cron parser (e.g., robfig/cron).
- Validate `indexName` uniqueness across the referenced `RAGEngine` (optional, scope can be namespace or RAGEngine).
- Block obviously invalid `repositoryURL` format (basic regex/URL parse).
- Ensure `dataSource` definitions are valid.

We will follow the same setup for webhooks as the Workspace and RAGEngine controllers.


### OwnerReferences and Deletion Safety

Child resources created by an AutoIndexer (Jobs, CronJobs, ConfigMaps) will always be created with an `ownerReference` pointing back to the AutoIndexer. This ensures Kubernetes garbage collects them automatically when the AutoIndexer resource is deleted, without requiring custom cleanup logic in the controller.

Because the AutoIndexer does not currently remove documents from the external RAG engine on deletion, **a finalizer is not strictly required**. The external state is intentionally left intact.

However, to avoid deleting an AutoIndexer while it still has an active indexing run:

1. The controller will check for active Jobs when it sees a `DeletionTimestamp` on the AutoIndexer.
2. If Jobs are still running, the controller will update status to `Terminating` and emit a warning Event.
3. Once all active Jobs complete (succeed or fail), the controller allows deletion to proceed naturally through Kubernetes garbage collection.

**Key details:**

* No finalizer is needed unless future versions add RAG document cleanup.
* OwnerReferences guarantee cluster resources are cleaned up automatically.
* The controller’s only responsibility at deletion time is to block premature cleanup while work is in-flight.

This keeps the implementation simple, follows Kubernetes conventions, and avoids dangling child resources while still ensuring safe termination.

### Drift Detection

Drift detection ensures that the RAG index remains consistent with the source data over time, even as documents are added, updated, or deleted at the source. There are several possible approaches to drift detection, each with different trade-offs:

**1. Count-only (recommended for v1):**
The controller periodically queries the RAG engine's list documents API, filtering by autoindexer-specific metadata (e.g., autoindexer name and index). It compares the actual document count in the RAG index to the `lastRunDocuments` value recorded in the AutoIndexer status. If a mismatch is detected, the controller triggers a reindex to bring the index back into compliance.

*Pros:* Simple, fast, and low-overhead. Good for catching obvious drift.
*Cons:* May miss subtle changes (e.g., content changes that don't affect count).

**2. Per-document manifest/checksum:**
Maintain a manifest mapping source paths to document IDs and checksums. On each run, compare the manifest to the current state in the RAG engine to detect precise adds, updates, and deletes. This is more accurate but more complex as the controller itself would need to run very similar logic to the Jobs/CronJobs.

**Chosen approach:**
For the initial implementation, we will use the count-only method. This provides a good balance of simplicity and effectiveness, and can be extended to more precise methods in the future as needed. The count-based approach is sufficient for most operational scenarios and is easy to reason about for both users and operators.

### Status Semantics & Conditions

The AutoIndexer CRD status is the primary way for operators and automation to understand the current and historical state of each indexer. The controller is responsible for keeping status fields and conditions up to date, reflecting both the desired and observed state.

**Status Fields:**
- `Phase`: High-level lifecycle state (`Pending`, `Running`, `Completed`, `Failed`, `Retrying`, `Unknown`).
- `LastIndexed`: Timestamp of the last successful indexing run.
- `LastCommit`: Last processed commit hash for Git sources.
- `DocumentsProcessed`: Number of documents processed in the last run.
- `SuccessfulRunCount` / `ErrorRunCount`: Cumulative counters for successful and failed runs.
- `NextScheduledRun`: When the next run is expected (for scheduled indexers).
- `Errors`: List of error messages from the last run.
- `Conditions`: Array of condition objects for fine-grained state and error reporting.

**Condition Types:**
- `ResourceReady`: The AutoIndexer is ready and its child resources are configured.
- `AutoIndexerScheduled`: CronJob exists and is scheduled.
- `AutoIndexerIndexing`: A job is currently running.
- `AutoIndexerCompleted`: The last run succeeded.
- `AutoIndexerError`: The last run had an error (see `Errors`).
- `AutoIndexerDriftDetected`: Drift detection discovered mismatches.

**Best practices:**
- Use `ObservedGeneration` to ensure status reflects the current spec.
- Include a `LastRunID` or similar field to correlate jobs, ConfigMaps, and logs.
- For long-running or complex jobs, consider adding `LastRunDurationSeconds` for operational dashboards.

The controller should update status promptly after each run, and surface partial failures (e.g., some files failed to process) in the `Errors` field and conditions. This enables both automated remediation and clear operator visibility into the health and progress of each AutoIndexer.

Each AutoIndexer run (Job or CronJob) will write its execution status, including details such as the last processed commit, number of documents processed, and any errors, into a dedicated ConfigMap. The controller will watch these ConfigMaps and use their contents to update the status fields of the corresponding AutoIndexer CRD.

This mechanism decouples the job execution environment from the controller, allowing for robust status reporting even if jobs run in isolated pods. The controller will update fields such as `LastCommit`, `DocumentsProcessed`, `LastIndexed`, and error conditions based on the latest job results found in the ConfigMap.

### RBAC & Security

Proper RBAC (Role-Based Access Control) is essential for both the AutoIndexer controller and the indexing jobs to ensure least-privilege operation and cluster security.

### Controller ServiceAccount (RBAC)

The controller should have a dedicated ServiceAccount with the following permissions:

- `get, list, watch, create, patch, update` on `autoindexers.kaito.ai` (the CRD)
- `update` on `autoindexers.kaito.ai/status` (status subresource)
- `get, list, watch, create, update, patch, delete` on Jobs and CronJobs
- `get, create, update, patch, delete` on ConfigMaps used for run results
- `get, list` on Secrets (to read credentials specified by SecretRef)
- `get, list, watch` on RAGEngine CR if that is a CRD
- `create, list` on Events (optional, for emitting Kubernetes events)

**Security best practices:**
- Use a dedicated ServiceAccount for the controller, not the default.
- Do not grant cluster-wide permissions unless necessary; prefer namespace-scoped roles.
- Use PodSecurity admission or PodSecurityPolicy to restrict controller pod privileges (no hostPath, no privileged, no hostNetwork).

### Indexer Job ServiceAccount (RBAC)

Each indexing job should run with a minimal ServiceAccount, with only the following permissions:

- `get` on referenced Secrets in its namespace (for credentials)
- `create` on result ConfigMaps (if using ConfigMaps for job result communication)

**Security best practices for jobs:**
- Do not allow the job to update CRD status directly (prefer controller to update status).
- Mount credentials as secrets (env)

By following these RBAC and security guidelines, the AutoIndexer system minimizes its attack surface and ensures that both the controller and jobs operate with the least privilege required for their function.


## Service (k8s Job) Design

### Valid Encoding Checks

The AutoIndexer will ensure that all ingested documents are decoded using a robust encoding detection and fallback strategy. For each file or URL:

- Attempt to detect encoding from HTTP headers (e.g., `Content-Type: charset`)
- Use the `chardet` library to guess encoding with high confidence
- Try common encodings in order: UTF-8, UTF-8-SIG, Latin1, CP1252, ISO-8859-1
- If these fail, we will log the errors but continue to index documents.

This ensures that documents with various encodings are handled gracefully, and errors are logged for any files that cannot be decoded, which will be written to the ConfigMap and added to the AutoIndexer Status by the Controller after the run.
### PDF Handling

PDF files are detected by content-type or file extension. The AutoIndexer will extract text from PDFs using the `PyMuPDF` library to extract text from each page. If the extraction is not successful, the document is skipped and an error is logged. This approach maximizes compatibility with a wide range of PDF files and ensures that both text and tabular data are captured where possible.
### RAG Document Handling

Each document ingested by the AutoIndexer will be uploaded to the RAG engine with metadata that includes:
- The AutoIndexer name
- Source URL or file path - used for sequential updates
- Depending on file extension, we will use Sentence or Code Splitting automatically.

This metadata enables filtering on ingested documents for drift detection, and allows for efficient cleanup or re-indexing of documents when source data changes.
#### Git Repository handling

For Git data sources, the AutoIndexer will:

1. Clone the repository locally using the configuration provided (repo URL, branch, commit, etc.)
2. On the first run, index all files matching the configured paths and upload them to the RAG engine with appropriate metadata.
3. On subsequent runs, use the last processed commit (from the status ConfigMap) to determine the commit range. Use the diff between the current and last run commit to identify added, updated, deleted, or renamed files between the last and current commit.
4. For added/updated files: re-index and upload to the RAG engine. For deleted files: remove from the RAG index. For renamed files: treat as delete+add.
5. After processing, update the ConfigMap with the new commit hash and document counts.

This incremental approach ensures efficient updates and minimizes unnecessary reprocessing, while keeping the RAG index in sync with the source repository.

