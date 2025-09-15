# AutoIndexer Controller

The AutoIndexer controller manages automatic document indexing for RAG (Retrieval-Augmented Generation) workflows in KAITO.

## Overview

The AutoIndexer Custom Resource Definition (CRD) allows users to:
- Schedule automatic indexing of documents from various data sources (Git repositories, static APIs, etc.)
- Reference existing RAGEngine resources for processing
- Configure retry policies for failed indexing operations
- Monitor indexing status and progress

## Architecture

### Components

1. **AutoIndexer Controller** (`autoindexer_controller.go`):
   - Main reconciliation logic
   - Manages Job/CronJob creation for indexing tasks
   - Handles finalizer management for cleanup

2. **Status Management** (`autoindexer_status.go`):
   - Updates AutoIndexer status based on job states
   - Manages conditions and progress tracking
   - Calculates next scheduled run times

3. **Garbage Collection** (`autoindexer_gc_finalizer.go`):
   - Handles cleanup of owned resources during deletion
   - Ensures proper removal of Jobs, CronJobs, and related resources

## Key Features

### Data Sources
- **Git repositories**: Clone and index documentation from GitHub/GitLab repos
- **Static APIs**: Fetch and index content from REST endpoints
- Support for authentication via Kubernetes secrets

### Scheduling
- **One-time execution**: Run indexing immediately upon creation
- **Scheduled execution**: Use cron expressions for recurring indexing
- **Suspend capability**: Temporarily disable scheduled indexing

### Integration
- **RAGEngine reference**: Leverage existing RAGEngine resources for processing
- **Kubernetes-native**: Full integration with Kubernetes lifecycle management
- **Status reporting**: Comprehensive status updates and error reporting

## Usage Example

```yaml
apiVersion: kaito.sh/v1alpha1
kind: AutoIndexer
metadata:
  name: documentation-indexer
  namespace: default
spec:
  ragEngineRef:
    name: my-rag-engine
    namespace: default
  indexName: "docs-index"
  dataSource:
    type: Git
    gitHub:
      repository: "https://github.com/example/docs"
      branch: "main"
      paths: ["docs/", "README.md"]
  schedule: "0 2 * * *"  # Daily at 2 AM
  retryPolicy:
    maxRetries: 3
    backoffStrategy: exponential
```

## Controller Integration

To integrate the AutoIndexer controller into your application:

```go
// In your main.go or controller setup
autoIndexerReconciler := controllers.NewAutoIndexerReconciler(
    client,
    scheme,
    log.Log.WithName("controllers").WithName("AutoIndexer"),
    mgr.GetEventRecorderFor("KAITO-AutoIndexer-controller"),
)

if err = autoIndexerReconciler.SetupWithManager(mgr); err != nil {
    klog.ErrorS(err, "unable to create controller", "controller", "AutoIndexer")
    os.Exit(1)
}
```

## Status Conditions

The AutoIndexer reports several condition types:

- `AutoIndexerSucceeded`: Indicates overall success state
- `AutoIndexerScheduled`: Shows if scheduling is active
- `AutoIndexerIndexing`: Indicates active indexing operations
- `AutoIndexerError`: Reports error conditions

## Development Status

This is a basic implementation providing the core controller structure. The following features are planned for future development:

- [ ] Job/CronJob creation logic
- [ ] Document processing and indexing workflows
- [ ] Enhanced data source support
- [ ] Metrics and monitoring integration
- [ ] Advanced retry mechanisms

## Testing

Run the controller tests:

```bash
go test ./pkg/autoindexer/controllers/... -v
```

## Contributing

When extending the AutoIndexer controller:

1. Add new functionality to the appropriate controller files
2. Update tests for new features
3. Regenerate deep copy methods: `make generate`
4. Update documentation as needed
