# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


from prometheus_client import Counter, Gauge, Histogram

STATUS_LABEL = "status"
MODE_LABEL = "mode"

# These are the labels that will be used in the metrics
STATUS_SUCCESS = "success"
STATUS_FAILURE = "failure"
MODE_LOCAL = "local"
MODE_REMOTE = "remote"

# Embedding metrics
rag_embedding_latency = Histogram(
    "rag_embedding_latency_seconds",
    "Time to embed in seconds",
    labelnames=[STATUS_LABEL, MODE_LABEL],
)
rag_embedding_requests_total = Counter(
    "rag_embedding_requests_total",
    "Count of successful/failed embed requests",
    labelnames=[STATUS_LABEL, MODE_LABEL],
)

# Query API metrics
rag_query_latency = Histogram(
    "rag_query_latency_seconds",
    "Time to call '/query' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_query_requests_total = Counter(
    "rag_query_requests_total",
    "Count of successful/failed calling '/query' requests",
    labelnames=[STATUS_LABEL],
)

# Chat API metrics
rag_chat_latency = Histogram(
    "rag_chat_latency_seconds",
    "Time to call '/v1/chat/completions' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_chat_requests_total = Counter(
    "rag_chat_requests_total",
    "Count of successful/failed calling '/v1/chat/completions' requests",
    labelnames=[STATUS_LABEL],
)

# Index API metrics
rag_index_latency = Histogram(
    "rag_index_latency_seconds",
    "Time to call '/index' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_index_requests_total = Counter(
    "rag_index_requests_total",
    "Count of successful/failed calling '/index' requests",
    labelnames=[STATUS_LABEL],
)

# Indexes API metrics
rag_indexes_latency = Histogram(
    "rag_indexes_latency_seconds",
    "Time to call '/indexes' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_indexes_requests_total = Counter(
    "rag_indexes_requests_total",
    "Count of successful/failed calling '/indexes' requests",
    labelnames=[STATUS_LABEL],
)

# Indexes document API metrics
rag_indexes_document_latency = Histogram(
    "rag_indexes_document_latency_seconds",
    "Time to call get '/indexes/{index_name}/documents' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_indexes_document_requests_total = Counter(
    "rag_indexes_document_requests_total",
    "Count of successful/failed calling get '/indexes/{index_name}/documents' requests",
    labelnames=[STATUS_LABEL],
)

# Indexes update document API metrics
rag_indexes_update_document_latency = Histogram(
    "rag_indexes_update_document_latency_seconds",
    "Time to call post '/indexes/{index_name}/documents' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_indexes_update_document_requests_total = Counter(
    "rag_indexes_update_document_requests_total",
    "Count of successful/failed calling post '/indexes/{index_name}/documents' requests",
    labelnames=[STATUS_LABEL],
)

# Indexes document API metrics
rag_indexes_delete_document_latency = Histogram(
    "rag_indexes_delete_document_latency_seconds",
    "Time to call '/indexes/{index_name}/documents/delete' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_indexes_delete_document_requests_total = Counter(
    "rag_indexes_delete_document_requests_total",
    "Count of successful/failed calling '/indexes/{index_name}/documents/delete' requests",
    labelnames=[STATUS_LABEL],
)

# Persist API metrics
rag_persist_latency = Histogram(
    "rag_persist_latency_seconds",
    "Time to call '/persist/{index_name}' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_persist_requests_total = Counter(
    "rag_persist_requests_total",
    "Count of successful/failed calling '/persist/{index_name}' requests",
    labelnames=[STATUS_LABEL],
)

# Load API metrics
rag_load_latency = Histogram(
    "rag_load_latency_seconds",
    "Time to call '/load/{index_name}' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_load_requests_total = Counter(
    "rag_load_requests_total",
    "Count of successful/failed calling '/load/{index_name}' requests",
    labelnames=[STATUS_LABEL],
)

# Delete API metrics
rag_delete_latency = Histogram(
    "rag_delete_index_latency_seconds",
    "Time to call delete '/indexes/{index_name}' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_delete_requests_total = Counter(
    "rag_delete_index_requests_total",
    "Count of successful/failed calling delete '/indexes/{index_name}' requests",
    labelnames=[STATUS_LABEL],
)

# End-to-end request metrics
e2e_request_total = Counter(
    "e2e_request_total",
    "Total number of all processed requests",
    labelnames=[STATUS_LABEL],
)
e2e_request_latency_seconds = Histogram(
    "e2e_request_latency_seconds",
    "End to end request latency in seconds",
    labelnames=[STATUS_LABEL],
)

# Active requests gauge
num_requests_running = Gauge(
    "num_requests_running", "Number of requests currently being processed"
)

# RAG source score metrics
rag_lowest_source_score = Histogram(
    "rag_lowest_source_score",
    "Score of the lowest scoring source node (typically the most relevant)",
    buckets=(0.1, 0.2, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0),
)

rag_avg_source_score = Histogram(
    "rag_avg_source_score",
    "Average score of all retrieved source documents in RAG queries",
    buckets=(0.1, 0.2, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0),
)
