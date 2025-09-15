# KAITO AutoIndexer Service

The AutoIndexer service is a Python-based application that fetches documents from various data sources and indexes them into KAITO RAG engines. It runs as a Kubernetes Job or CronJob and is configured through environment variables set by the AutoIndexer controller.

## Features

- **Static Data Sources**: Support for direct content, file paths, and HTTP/HTTPS URLs
- **RAG Engine Integration**: Uses the KAITO RAG Client for document indexing
- **Retry Logic**: Configurable retry policies with exponential backoff
- **Batch Processing**: Processes documents in configurable batches
- **Comprehensive Logging**: Detailed logging for monitoring and debugging
- **Error Handling**: Robust error handling with proper status reporting

## Environment Variables

The service is configured through environment variables set by the AutoIndexer controller:

| Variable | Description | Example |
|----------|-------------|---------|
| `INDEX_NAME` | Target index name in RAG engine | `my-documents-index` |
| `RAGENGINE_ENDPOINT` | RAG engine service endpoint | `http://ragengine.default.svc.cluster.local:80` |
| `DATASOURCE_TYPE` | Type of data source | `static` |
| `DATASOURCE_CONFIG` | JSON configuration for data source | `{"static": {"content": ["text"]}}` |
| `CREDENTIALS_CONFIG` | Optional credentials configuration | `{"http_auth": {"username": "user", "password": "pass"}}` |
| `RETRY_POLICY` | Optional retry policy configuration | `{"max_attempts": 3, "initial_delay": 1.0}` |

## Data Source Types

### Static Data Source

The static data source handler supports multiple ways to provide document content:

#### Direct Content
```json
{
  "static": {
    "content": [
      "This is the first document.",
      "This is the second document."
    ]
  }
}
```

#### URL Sources
```json
{
  "static": {
    "urls": [
      "https://example.com/document1.txt",
      "https://example.com/document2.json"
    ],
    "timeout": 30
  }
}
```

#### File Paths
```json
{
  "static": {
    "file_paths": [
      "/data/document1.txt",
      "/data/document2.txt"
    ]
  }
}
```

#### Combined Configuration
```json
{
  "static": {
    "content": ["Direct content here"],
    "urls": ["https://example.com/doc.txt"],
    "file_paths": ["/data/local-doc.txt"]
  }
}
```

## Authentication

The service supports HTTP authentication for URL-based data sources:

```json
{
  "http_auth": {
    "username": "myuser",
    "password": "mypassword"
  },
  "headers": {
    "Authorization": "Bearer token123",
    "X-API-Key": "api-key-value"
  }
}
```

## Retry Policy

Configure retry behavior for failed operations:

```json
{
  "max_attempts": 5,
  "initial_delay": 1.0
}
```

- `max_attempts`: Maximum number of retry attempts (default: 3)
- `initial_delay`: Initial delay in seconds between retries (default: 1.0)
- Uses exponential backoff: delay doubles after each failed attempt

## Usage

### Command Line
```bash
# Basic indexing
python autoindexer_service.py --mode=index

# With custom log level
python autoindexer_service.py --mode=index --log-level=DEBUG

# Dry run (validation only)
python autoindexer_service.py --mode=index --dry-run
```

### Docker
```bash
# Build the image
docker build -t kaito/autoindexer:latest .

# Run with environment variables
docker run --rm \
  -e INDEX_NAME=test-index \
  -e RAGENGINE_ENDPOINT=http://ragengine:80 \
  -e DATASOURCE_TYPE=static \
  -e DATASOURCE_CONFIG='{"static":{"content":["Test document"]}}' \
  kaito/autoindexer:latest
```

## Testing

Run the test suite to verify functionality:

```bash
python test_autoindexer.py
```

The test suite includes:
- Static data source with direct content
- Static data source with file paths
- Static data source with mocked URLs
- Environment variable parsing
- Service initialization and configuration

## Architecture

```
┌─────────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   AutoIndexer       │───▶│  AutoIndexer     │───▶│   RAG Engine    │
│   Controller        │    │  Service (Job)   │    │   Service       │
│                     │    │                  │    │                 │
│ - Creates Job       │    │ - Fetches docs   │    │ - Indexes docs  │
│ - Sets env vars     │    │ - Processes data │    │ - Stores vectors│
│ - Manages lifecycle │    │ - Handles errors │    │ - Provides API  │
└─────────────────────┘    └──────────────────┘    └─────────────────┘
```

## Error Handling

The service implements comprehensive error handling:

1. **Data Source Errors**: Invalid URLs, network timeouts, file access issues
2. **RAG Engine Errors**: Connection failures, authentication issues, API errors
3. **Configuration Errors**: Missing required environment variables, invalid JSON
4. **Resource Errors**: Insufficient memory, disk space issues

All errors are logged with appropriate severity levels and returned as exit codes for Kubernetes job status tracking.

## Logging

The service uses structured logging with the following levels:

- **DEBUG**: Detailed execution information
- **INFO**: General operational messages
- **WARNING**: Potential issues that don't prevent execution
- **ERROR**: Errors that prevent successful completion

Example log output:
```
2024-09-10 15:45:57,123 - __main__ - INFO - AutoIndexer initialized for index 'my-index' with data source type 'static'
2024-09-10 15:45:57,124 - __main__ - INFO - Starting document indexing process
2024-09-10 15:45:57,125 - __main__ - INFO - Fetching documents from data source
2024-09-10 15:45:57,126 - data_sources - INFO - Added 2 direct content documents
2024-09-10 15:45:57,127 - __main__ - INFO - Found 2 documents to index
2024-09-10 15:45:57,128 - __main__ - INFO - Indexing documents in RAG engine
2024-09-10 15:45:57,129 - __main__ - INFO - Indexing attempt 1/3
2024-09-10 15:45:57,130 - __main__ - INFO - Indexing batch 1 (2 documents)
2024-09-10 15:45:57,145 - __main__ - INFO - All documents indexed successfully
2024-09-10 15:45:57,146 - __main__ - INFO - Document indexing completed successfully
```

## Future Enhancements

1. **Git Data Source**: Support for cloning Git repositories and processing files
2. **Document Parsing**: Support for PDF, DOCX, and other document formats
3. **Incremental Indexing**: Track changes and only index modified documents
4. **Metrics**: Prometheus metrics for monitoring and alerting
5. **Webhooks**: Status reporting via webhooks for external integrations

## Security Considerations

1. **Non-root User**: Container runs as non-root user for security
2. **Credential Management**: Sensitive data passed through Kubernetes secrets
3. **Network Policies**: Restrict network access to required services only
4. **Resource Limits**: Configure appropriate CPU and memory limits
