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
- **Single-Source Support**: Start with support for GitHub repositories, and in the future expand to other entities like blob storage, databases, and other common data sources
- **Flexible Scheduling**: Support for single-run, cron-based, and long running indexing operations
- **Incremental Updates**: Efficient processing of only changed documents where possible
- **Error Handling**: Retry mechanisms and status reporting
- **Integration**: Seamless integration with existing RAGEngine CRDs

### Non-Goals

- Real-time streaming data ingestion (initial version focuses on batch processing)
- Complex data transformation pipelines (focuses on document extraction and basic metadata)
- Custom authentication beyond standard Kubernetes secrets

## Proposal

### Auto Indexer CRD Design

```yaml
apiVersion: kaito.sh/v1alpha1
kind: AutoIndexer
metadata:
  name: docs-indexer
  namespace: default
spec:
  ragEngineRef:
    name: my-rag-engine
    namespace: my-rag-ns
    indexName: documentation-index

  dataSource:
    type: GitHub
    gitHub:
      schedule: "0 2 * * *"
      repository: https://github.com/myorg/documentation
      branch: main
      paths:
        - "docs/"
        - "README.md"
        - "*.md"
      excludePaths:
        - ".git/"
		- "*.yaml"
		- "*.yml"
        - "node_modules/"
        - "*.tmp"
      credentials:
        type: SecretRef  # or WorkloadIdentity, ServiceAccount
        secretRef:
          name: github-token
          key: token

  retryPolicy:
    maxRetries: 3
    backoffStrategy: exponential

status:
  lastIndexed: "2023-09-05T02:00:00Z"
  lastCommit: "abc123def456"
  phase: Pending  # or Running, Completed, Failed, Retrying, Unknown
  successfulRunCount: 3
  errorRunCount: 0
  documentsProcessed: 42
  nextScheduledRun: "2023-09-05T04:00:00Z"
  errors: []
  conditions:
    - type: Ready
      status: "False"
      reason: IndexingInProgress
      message: "Currently indexing latest docs from GitHub"
      lastTransitionTime: "2023-09-05T02:00:00Z"

    - type: Scheduled
      status: "True"
      reason: CronScheduleDetected
      message: "Next run at 2023-09-05T04:00:00Z"
      lastTransitionTime: "2023-09-05T02:01:00Z"

    - type: Indexing
      status: "True"
      reason: IndexJobRunning
      message: "Documents are being processed"
      lastTransitionTime: "2023-09-05T02:00:00Z"

    - type: Error
      status: "False"
      reason: LastRunSuccessful
      lastTransitionTime: "2023-09-05T02:00:00Z"
```

## API Design

The AutoIndexer introduces a new Custom Resource Definition (CRD) to KAITO's API.

### CRD Definition

```go
// AutoIndexerSpec defines the desired state of AutoIndexer
type AutoIndexerSpec struct {

	// TODO: CHANGE TO OBJECT REF
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

	// Schedule defines when the indexing should run (cron format)
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^(@(annually|yearly|monthly|weekly|daily|hourly|reboot))|(@every (\d+(ns|us|µs|ms|s|m|h))+)|((((\d+,)+\d+|(\d+(\/|-)\d+)|\d+|\*) ?){5,7})$`
    Schedule string `json:"schedule"`

    // RetryPolicy defines how failed indexing jobs should be retried
    // +optional
    RetryPolicy *RetryPolicySpec `json:"retryPolicy,omitempty"`

    // Suspend can be set to true to suspend the indexing schedule
    // +optional
    Suspend *bool `json:"suspend,omitempty"`
}

// DataSourceSpec defines the source of documents to be indexed
type DataSourceSpec struct {
    // Type specifies the data source type
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Enum=GitHub;S3;API;ConfigMap;Secret
    Type DataSourceType `json:"type"`

    // GitHub defines configuration for GitHub repository data sources
    // +optional
    Git *GitHubDataSourceSpec `json:"gitHub,omitempty"`

	//TODO: Add storage account
	AzureStorageAccount ...

    // // S3 defines configuration for S3-based data sources
    // // +optional
    // S3 *S3DataSourceSpec `json:"s3,omitempty"`

    // API defines configuration for API-based data sources
    // +optional
    API *APIDataSourceSpec `json:"api,omitempty"`

    // ConfigMap defines configuration for ConfigMap-based data sources
    // +optional
    ConfigMap *ConfigMapDataSourceSpec `json:"configMap,omitempty"`

    // // Secret defines configuration for Secret-based data sources
    // // +optional
    // Secret *SecretDataSourceSpec `json:"secret,omitempty"`
}

// DataSourceType defines the supported data source types
// +kubebuilder:validation:Enum=GitHub;S3;API;ConfigMap;Secret
type DataSourceType string

const (
    DataSourceTypeGitHub    DataSourceType = "GitHub"
    DataSourceTypeS3        DataSourceType = "S3"
    DataSourceTypeAPI       DataSourceType = "API"
    DataSourceTypeConfigMap DataSourceType = "ConfigMap"
    DataSourceTypeSecret    DataSourceType = "Secret"
)

// GitHubDataSourceSpec defines GitHub repository configuration
type GitDataSourceSpec struct {
    // Repository URL
    // +kubebuilder:validation:Required
    Repository string `json:"repository"`

    // Branch to checkout (default: main)
    // +optional
    Branch string `json:"branch,omitempty"`

    // Specific paths to index within the repository
    // +optional
    Paths []string `json:"paths,omitempty"`

    // Paths to exclude from indexing
    // +optional
    ExcludePaths []string `json:"excludePaths,omitempty"`

    // Credentials for private repositories
    // +optional
    Credentials *CredentialsSpec `json:"credentials,omitempty"`
}

// AzureStorageAccount defines Storage Account configuration
type AzureStorageAccount struct {
    // Storage Account ARM Id
    // +kubebuilder:validation:Required
    Id string `json:"id"`

    // Prefix to filter objects
    // +optional
    Prefix string `json:"prefix,omitempty"`

    // Credentials for S3 access
    // +optional
    Credentials *CredentialsSpec `json:"credentials,omitempty"`
}

// APIDataSourceSpec defines REST API configuration
type StaticDataSourceSpec struct {
    // API endpoint URL
    // +kubebuilder:validation:Required
    Endpoint string `json:"endpoint"`

    // Credentials for API access
    // +optional
    Credentials *CredentialsSpec `json:"credentials,omitempty"`
}

// HeaderValue can be a literal string or reference to a secret
type HeaderValue struct {
    // Value is a literal header value
    // +optional
    Value string `json:"value,omitempty"`

    // SecretRef references a secret key for the header value
    // +optional
    SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
}

// PaginationSpec defines API pagination configuration
type PaginationSpec struct {
    // Type of pagination (cursor, offset, page)
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Enum=cursor;offset;page
    Type string `json:"type"`

    // Parameter name for page size limit
    // +optional
    LimitParam string `json:"limitParam,omitempty"`

    // Parameter name for cursor/offset/page
    // +optional
    CursorParam string `json:"cursorParam,omitempty"`

    // Page size
    // +optional
    PageSize int32 `json:"pageSize,omitempty"`
}

// ConfigMapDataSourceSpec defines ConfigMap configuration
type ConfigMapDataSourceSpec struct {
    // ConfigMap name
    // +kubebuilder:validation:Required
    Name string `json:"name"`

    // Namespace (defaults to AutoIndexer namespace)
    // +optional
    Namespace string `json:"namespace,omitempty"`

    // Specific keys to process (processes all if empty)
    // +optional
    Keys []string `json:"keys,omitempty"`

    // Schedule defines when the indexing should run (cron format)
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=`^(@(annually|yearly|monthly|weekly|daily|hourly|reboot))|(@every (\d+(ns|us|µs|ms|s|m|h))+)|((((\d+,)+\d+|(\d+(\/|-)\d+)|\d+|\*) ?){5,7})$`
    Schedule string `json:"schedule"`
}

// SecretDataSourceSpec defines Secret configuration
type SecretDataSourceSpec struct {
    // Secret name
    // +kubebuilder:validation:Required
    Name string `json:"name"`

    // Namespace (defaults to AutoIndexer namespace)
    // +optional
    Namespace string `json:"namespace,omitempty"`

    // Specific keys to process (processes all if empty)
    // +optional
    Keys []string `json:"keys,omitempty"`
}

// CredentialsSpec defines authentication credentials
type CredentialsSpec struct {
    // Type specifies the credential type
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Enum=SecretRef;WorkloadIdentity;ServiceAccount
    Type CredentialType `json:"type"`

    // Secret reference containing credentials
    // +optional
    SecretRef *SecretKeyRef `json:"secretRef,omitempty"`

    // WorkloadIdentity configuration for cloud-native authentication
    // +optional
    WorkloadIdentity *WorkloadIdentitySpec `json:"workloadIdentity,omitempty"`

    // ServiceAccount name for Kubernetes service account authentication
    // +optional
    ServiceAccount string `json:"serviceAccount,omitempty"`
}

// CredentialType defines the supported credential types
// +kubebuilder:validation:Enum=SecretRef;WorkloadIdentity;ServiceAccount
type CredentialType string

const (
    CredentialTypeSecretRef        CredentialType = "SecretRef"
    CredentialTypeWorkloadIdentity CredentialType = "WorkloadIdentity"
    CredentialTypeServiceAccount   CredentialType = "ServiceAccount"
)

// WorkloadIdentitySpec defines workload identity configuration
type WorkloadIdentitySpec struct {
    // ClientID for the workload identity
    // +optional
    ClientID string `json:"clientId,omitempty"`

    // TenantID for Azure workload identity
    // +optional
    TenantID string `json:"tenantId,omitempty"`
}

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
    // Maximum number of retries
    // +optional
    MaxRetries int32 `json:"maxRetries,omitempty"`

    // Backoff strategy
    // +optional
    // +kubebuilder:validation:Enum=linear;exponential
    BackoffStrategy string `json:"backoffStrategy,omitempty"`

    // Initial delay between retries
    // +optional
    InitialDelay *metav1.Duration `json:"initialDelay,omitempty"`

    // Maximum delay between retries
    // +optional
    MaxDelay *metav1.Duration `json:"maxDelay,omitempty"`
}

// AutoIndexerStatus defines the observed state of AutoIndexer
type AutoIndexerStatus struct {
    // LastIndexed timestamp of the last successful indexing
    // +optional
    LastIndexed *metav1.Time `json:"lastIndexed,omitempty"`

    // LastCommit is the last processed commit hash for Git sources
    // +optional
    LastCommit string `json:"lastCommit,omitempty"`

    // Phase represents the current phase of the AutoIndexer
    // +optional
    // +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Retrying;Unknown
    Phase AutoIndexerPhase `json:"phase,omitempty"`

    // SuccessfulRunCount tracks successful indexing runs
    // +optional
    SuccessfulRunCount int32 `json:"successfulRunCount,omitempty"`

    // ErrorRunCount tracks failed indexing runs
    // +optional
    ErrorRunCount int32 `json:"errorRunCount,omitempty"`

    // Number of documents processed in the last run
    // +optional
    DocumentsProcessed int32 `json:"documentsProcessed,omitempty"`

    // NextScheduledRun shows when the next indexing is scheduled
    // +optional
    NextScheduledRun *metav1.Time `json:"nextScheduledRun,omitempty"`

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

### Architectural Benefits

Moving the `indexName` into the data source configuration provides several key advantages:

1. **Per-Source Index Targeting**: Each data source can write to its own dedicated index, enabling better organization and isolation
2. **Flexible Index Strategies**: Different data sources can use different indexing approaches (e.g., separate indices for documentation vs. code)
3. **Simplified Single-Source Model**: Aligns perfectly with the "one data source per AutoIndexer" approach, eliminating coordination complexity
4. **Cleaner Resource Boundaries**: Each AutoIndexer resource has a clear, single responsibility for one data source and one target index

### Design Decision: Single Data Source per AutoIndexer

After careful consideration, we've chosen the **single data source per AutoIndexer** approach for the following reasons:

#### Advantages of Single Data Source Model

1. **Operational Simplicity**
   - Each AutoIndexer has one clear responsibility
   - Simpler debugging and troubleshooting
   - Clear resource ownership and lifecycle management

2. **Independent Scaling**
   - Each data source can be scheduled independently
   - Different retry policies and resource requirements per source
   - No coordination overhead between different source types

3. **Index Flexibility** 
   - Each data source targets its own index
   - Different indexing strategies per data source type
   - Better isolation and organization of indexed content

4. **Resource Management**
   - Kubernetes-native resource model (one CRD per logical unit)
   - Easier RBAC and access control management
   - Cleaner status reporting and monitoring

#### Implementation Benefits

- **No coordination logic needed**: Each AutoIndexer operates independently
- **Simpler controller logic**: Focus on single data source handling
- **Better error isolation**: Failures in one data source don't affect others
- **Easier testing**: Each data source type can be tested in isolation

This approach follows Kubernetes best practices of having focused, single-responsibility resources while providing maximum flexibility through the per-source index configuration.

### Architecture

#### Components

1. **Auto Indexer Controller**
   - Watches AutoIndexer CRDs
   - Manages sync scheduling and execution
   - Handles resource lifecycle

2. **Data Source Adapters**
   - Pluggable adapters for different data source types
   - Handle authentication, connection management
   - Implement incremental update detection

3. **Document Processor**
   - Extracts text from various file formats
   - Applies configured splitting strategies
   - Generates document metadata

4. **Sync Orchestrator** 
   - Coordinates multi-source syncing
   - Manages parallel processing
   - Handles error recovery and retries

#### Data Flow

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Data Sources  │───▶│  Data Adapters   │───▶│ Doc Processor   │
│  - Git repos    │    │  - Git client    │    │ - Text extract  │
│  - Blob storage │    │  - Blob client   │    │ - Splitting     │
│  - Databases    │    │  - DB client     │    │ - Metadata      │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                                         │
┌─────────────────┐    ┌──────────────────┐             │
│   RAG Engine    │◀───│ Sync Orchestrator│◀────────────┘
│  - Index mgmt   │    │ - Batching       │
│  - Doc updates  │    │ - Error handling │
└─────────────────┘    │ - Status updates │
                       └──────────────────┘
```

### Data Source Types

#### Git Repository
```yaml
type: "git"
config:
  repository: "https://github.com/org/repo.git"
  branch: "main"
  paths: ["docs/", "README.md"]
  filePatterns: ["*.md", "*.rst"]
  excludePatterns: [".git/", "node_modules/"]
  authentication:
    secretRef:
      name: "git-credentials"
      key: "token"
```

**Features:**
- Clone/pull repository changes
- Incremental updates based on commit history
- Path and pattern-based filtering
- Support for private repositories via tokens/SSH

#### Azure Blob Storage
```yaml
type: "azureBlob"
config:
  storageAccount: "mystorageaccount"
  containerName: "documents" 
  prefix: "public/"
  filePatterns: ["*.pdf", "*.docx", "*.txt"]
  authentication:
    secretRef:
      name: "azure-storage"
      key: "connectionString"
```

**Features:**
- List and download blob changes
- Incremental updates based on lastModified timestamps
- Pattern-based file filtering
- Support for various authentication methods

#### Database
```yaml
type: "database"
config:
  driver: "postgresql"
  connectionString:
    secretRef:
      name: "db-config"
      key: "connectionString"
  query: "SELECT id, content, updated_at FROM documents WHERE active = true"
  textColumn: "content"
  metadataColumns: ["id", "updated_at"]
  incrementalColumn: "updated_at"
```

**Features:**
- SQL query-based document extraction
- Incremental updates using timestamp columns
- Configurable text and metadata column mapping
- Support for PostgreSQL, MySQL, SQLite

#### Additional Sources (Future)
- **AWS S3**: Similar to Azure Blob with S3-specific authentication
- **Google Cloud Storage**: GCS bucket integration
- **Confluence**: Wiki/documentation platform integration
- **SharePoint**: Microsoft SharePoint document libraries
- **Kusto/Azure Data Explorer**: Query-based data extraction

### Scheduling Options

#### Cron-based Scheduling
```yaml
schedule:
  cron: "0 */6 * * *"  # Every 6 hours
```

#### Event-driven Sync
```yaml
schedule:
  webhook:
    enabled: true
    path: "/webhook"
    authentication:
      secretRef:
        name: "webhook-secret"
        key: "token"
```

#### One-time Execution
```yaml
schedule:
  runOnce: true
```

### Update Strategies

#### Incremental Updates
- **Git**: Use commit history to identify changed files
- **Blob Storage**: Compare lastModified timestamps
- **Database**: Use incremental column (timestamp/sequence)
- **Benefits**: Faster sync times, reduced API calls

#### Full Refresh
- Re-processes all documents from scratch
- Useful for major schema changes or corruption recovery
- Higher resource usage but guaranteed consistency

#### Merge Strategy
- Combines incremental detection with conflict resolution
- Configurable policies for handling document conflicts

### Error Handling and Monitoring

#### Retry Logic
```yaml
retryPolicy:
  maxRetries: 3
  backoffStrategy: "exponential"  # linear, exponential
  initialDelay: "30s"
  maxDelay: "300s"
```

#### Status Reporting
- Per-source sync status and metrics
- Error details and resolution guidance
- Integration with Kubernetes events and metrics

#### Monitoring Integration
- Prometheus metrics for sync performance
- Grafana dashboards for operational visibility
- Alert integration for sync failures

## Implementation Plan

### Phase 1: Core Framework (Milestone 1)
- [ ] AutoIndexer CRD definition and validation
- [ ] Basic controller framework and scheduling
- [ ] Git repository data source adapter
- [ ] Document processing pipeline
- [ ] Integration with existing RAGEngine

### Phase 2: Additional Sources (Milestone 2)
- [ ] Azure Blob Storage adapter
- [ ] Database adapter (PostgreSQL/MySQL)
- [ ] Advanced scheduling (webhooks, one-time)
- [ ] Incremental update mechanisms

### Phase 3: Advanced Features (Milestone 3)
- [ ] Additional data sources (AWS S3, GCS)
- [ ] Custom document splitters
- [ ] Advanced error handling and monitoring
- [ ] Performance optimizations and scaling

### Phase 4: Enterprise Features (Milestone 4)
- [ ] Confluence/SharePoint adapters
- [ ] Advanced authentication methods
- [ ] Multi-tenancy support
- [ ] Backup and disaster recovery

## Security Considerations

### Authentication
- Kubernetes secrets for credential storage
- Support for workload identity/managed identity/service principal
- Token rotation and expiration handling

### Network Security
- Private endpoint support for data sources
- Network policies for controller pods
- TLS encryption for all communications

### RBAC
- Dedicated service accounts for data source access
- Principle of least privilege for resource access
- Audit logging for sync operations

## Alternatives Considered

### External Tools Integration
**Option**: Integrate with existing ETL tools (Apache Airflow, Azure Data Factory)
**Pros**: Mature ecosystem, extensive source support
**Cons**: Additional complexity, external dependencies, not Kubernetes-native

### Serverless Functions
**Option**: Use serverless functions (Azure Functions, AWS Lambda) for sync logic
**Pros**: Automatic scaling, cost-effective for infrequent syncs
**Cons**: Cold start latency, limited runtime, complex state management

### Operator SDK vs Custom Controller
**Option**: Build using Operator SDK framework
**Pros**: Best practices, standardized patterns
**Cons**: Additional learning curve, framework overhead
**Decision**: Use Operator SDK for standardization

## Testing Strategy

### Unit Tests
- Data adapter functionality
- Document processing logic
- Scheduling and retry mechanisms

### Integration Tests  
- End-to-end sync workflows
- RAGEngine integration
- Error scenario handling

### Performance Tests
- Large dataset processing
- Concurrent sync operations  
- Resource utilization

## Documentation Plan

### User Documentation
- Getting started guide
- Data source configuration examples
- Troubleshooting common issues

### Developer Documentation
- Adding new data source adapters
- Custom splitter development
- Controller architecture

### Operations Documentation
- Monitoring and alerting setup
- Performance tuning guidelines
- Backup and recovery procedures

## Success Metrics

### Functional Metrics
- Number of supported data source types
- Sync reliability (>99% success rate)
- Time to initial index creation (<30 minutes for typical datasets)

### Performance Metrics
- Document processing throughput (>1000 docs/minute)
- Incremental sync efficiency (<10% of full sync time)
- Resource utilization optimization

### Adoption Metrics
- Number of AutoIndexer instances deployed
- User satisfaction survey results
- Community contributions and feedback

## Future Enhancements

### Advanced Processing
- ML-based document classification
- Automatic metadata extraction
- Content quality scoring

### Real-time Sync
- Event-driven updates via change streams
- WebSocket-based real-time synchronization
- Stream processing integration

### Multi-tenancy
- Namespace-based isolation
- Resource quotas and limits
- Tenant-specific configurations