# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from prometheus_client import Counter, Histogram

# Search metrics
rag_search_latency = Histogram('rag_search_latency_seconds', 'Time to search in seconds')
rag_search_failure = Counter('rag_search_failure_total', 'Count of failed search requests')
rag_search_success = Counter('rag_search_success_total', 'Count of successful search requests')

# Embedding metrics
rag_embedding_latency = Histogram('rag_embedding_latency_seconds', 'Time to embed in seconds')
rag_embedding_failure = Counter('rag_embedding_failure_total', 'Count of failed embed requests')
rag_embedding_success = Counter('rag_embedding_success_total', 'Count of successful embed requests')

# Query API metrics
rag_query_latency = Histogram('rag_query_latency_seconds', 'Time to call \'/query\' API in seconds')
rag_query_failure = Counter('rag_query_failure_total', 'Count of failed calling \'/query\' requests')
rag_query_success = Counter('rag_query_success_total', 'Count of successful calling \'/query\' requests')

# Index API metrics
rag_index_latency = Histogram('rag_index_latency_seconds', 'Time to call \'/index\' API in seconds')
rag_index_failure = Counter('rag_index_failure_total', 'Count of failed calling \'/index\' requests')
rag_index_success = Counter('rag_index_success_total', 'Count of successful calling \'/index\' requests')

# Indexes API metrics
rag_indexes_latency = Histogram('rag_indexes_latency_seconds', 'Time to call \'/indexes\' API in seconds')
rag_indexes_failure = Counter('rag_indexes_failure_total', 'Count of failed calling \'/indexes\' requests')
rag_indexes_success = Counter('rag_indexes_success_total', 'Count of successful calling \'/indexes\' requests')

# Indexes document API metrics
rag_indexes_document_latency = Histogram('rag_indexes_document_latency_seconds', 'Time to call \'/indexes/{index_name}/documents\' API in seconds')
rag_indexes_document_failure = Counter('rag_indexes_document_failure_total', 'Count of failed calling \'/indexes/{index_name}/documents\' requests')
rag_indexes_document_success = Counter('rag_indexes_document_success_total', 'Count of successful calling \'/indexes/{index_name}/documents\' requests')

# Persist API metrics
rag_persist_latency = Histogram('rag_persist_latency_seconds', 'Time to call \'/persist/{index_name}\' API in seconds')
rag_persist_failure = Counter('rag_persist_failure_total', 'Count of failed calling \'/persist/{index_name}\' requests')
rag_persist_success = Counter('rag_persist_success_total', 'Count of successful calling \'/persist/{index_name}\' requests')

# Load API metrics
rag_load_latency = Histogram('rag_load_latency_seconds', 'Time to call \'/load/{index_name}\' API in seconds')
rag_load_failure = Counter('rag_load_failure_total', 'Count of failed calling \'/load/{index_name}\' requests')
rag_load_success = Counter('rag_load_success_total', 'Count of successful calling \'/load/{index_name}\' requests') 