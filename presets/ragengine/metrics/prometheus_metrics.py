# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from prometheus_client import Counter, Histogram

# Embedding metrics
rag_embedding_latency = Histogram('rag_embedding_latency_seconds', 'Time to embed in seconds', labelnames=['status'])
rag_embedding_requests_total = Counter('rag_embedding_requests_total', 'Count of successful/failed embed requests', labelnames=['status'])

# Query API metrics
rag_query_latency = Histogram('rag_query_latency_seconds', 'Time to call \'/query\' API in seconds', labelnames=['status'])
rag_query_requests_total = Counter('rag_query_requests_total', 'Count of successful/failed calling \'/query\' requests', labelnames=['status'])

# Index API metrics
rag_index_latency = Histogram('rag_index_latency_seconds', 'Time to call \'/index\' API in seconds', labelnames=['status'])
rag_index_requests_total = Counter('rag_index_requests_total', 'Count of successful/failed calling \'/index\' requests', labelnames=['status'])

# Indexes API metrics
rag_indexes_latency = Histogram('rag_indexes_latency_seconds', 'Time to call \'/indexes\' API in seconds', labelnames=['status'])
rag_indexes_requests_total = Counter('rag_indexes_requests_total', 'Count of successful/failed calling \'/indexes\' requests',  labelnames=['status'])

# Indexes document API metrics
rag_indexes_document_latency = Histogram('rag_indexes_document_latency_seconds', 'Time to call \'/indexes/{index_name}/documents\' API in seconds', labelnames=['status'])
rag_indexes_document_requests_total = Counter('rag_indexes_document_requests_totall', 'Count of successful/failed calling \'/indexes/{index_name}/documents\' requests', labelnames=['status'])

# Persist API metrics
rag_persist_latency = Histogram('rag_persist_latency_seconds', 'Time to call \'/persist/{index_name}\' API in seconds', labelnames=['status'])
rag_persist_requests_total = Counter('rag_persist_requests_total', 'Count of successful/failed calling \'/persist/{index_name}\' requests', labelnames=['status'])

# Load API metrics
rag_load_latency = Histogram('rag_load_latency_seconds', 'Time to call \'/load/{index_name}\' API in seconds', labelnames=['status'])
rag_load_requests_total = Counter('rag_load_requests_total', 'Count of successful/failed calling \'/load/{index_name}\' requests', labelnames=['status'])