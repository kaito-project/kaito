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

# Indexes retrieve API metrics
rag_indexes_retrieve_latency = Histogram(
    "rag_indexes_retrieve_latency_seconds",
    "Time to call post '/retrieve' API in seconds",
    labelnames=[STATUS_LABEL],
)
rag_indexes_retrieve_requests_total = Counter(
    "rag_indexes_retrieve_requests_total",
    "Count of successful/failed calling post '/retrieve' requests",
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

# Output guardrails audit shutdown metrics
rag_output_guardrails_audit_shutdown_drain_total = Counter(
    "rag_output_guardrails_audit_shutdown_drain_total",
    "Count of guardrail audit shutdown drain attempts by outcome",
    labelnames=[STATUS_LABEL],
)

rag_output_guardrails_audit_shutdown_drain_latency = Histogram(
    "rag_output_guardrails_audit_shutdown_drain_latency_seconds",
    "Time spent draining pending background guardrail audit deliveries during shutdown",
    labelnames=[STATUS_LABEL],
    buckets=(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0),
)

rag_output_guardrails_audit_cancelled_deliveries_total = Counter(
    "rag_output_guardrails_audit_cancelled_deliveries_total",
    "Number of background guardrail audit deliveries cancelled during shutdown drain timeout",
)

rag_output_guardrails_audit_in_flight_background_tasks = Gauge(
    "rag_output_guardrails_audit_in_flight_background_tasks",
    "Number of background guardrail audit delivery tasks currently in flight",
)

rag_output_guardrails_audit_backpressure_total = Counter(
    "rag_output_guardrails_audit_backpressure_total",
    "Number of times guardrail audit background delivery hit the configured in-flight limit",
    labelnames=["policy", "delivery_type"],
)

rag_output_guardrails_audit_dropped_deliveries_total = Counter(
    "rag_output_guardrails_audit_dropped_deliveries_total",
    "Number of guardrail audit deliveries dropped due to backpressure policy",
    labelnames=["delivery_type"],
)

# Vector store operation latency (used by base.py and qdrant_store.py)
rag_vector_store_operation_latency = Histogram(
    "rag_vector_store_operation_latency_seconds",
    "Latency of vector store backend operations (insert, query, delete)",
    labelnames=["operation", STATUS_LABEL],
    buckets=(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0),
)

# Retrieve result count
rag_retrieve_result_count = Histogram(
    "rag_retrieve_result_count",
    "Number of document chunks returned per retrieve call",
    buckets=(0, 1, 2, 3, 5, 10, 20, 50, 100, 200, 300),
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

# ── Hybrid Search (BM25) Metrics ────────────────────────────────────

SEARCH_MODE_LABEL = "search_mode"  # "hybrid" or "dense_only"

rag_hybrid_search_mode_total = Counter(
    "rag_hybrid_search_mode_total",
    "Number of retrieve calls by search mode",
    labelnames=[SEARCH_MODE_LABEL],
)

rag_hybrid_retrieve_latency = Histogram(
    "rag_hybrid_retrieve_latency_seconds",
    "End-to-end latency of hybrid retrieve (dense+sparse encode, query, fusion)",
    buckets=(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0),
)

rag_hybrid_retrieve_latency_avg = Gauge(
    "rag_hybrid_retrieve_latency_avg_seconds",
    "Running average latency of hybrid retrieve calls (sum / count)",
)

rag_hybrid_top_score = Histogram(
    "rag_hybrid_top_score",
    "Highest (best) fused score among results per hybrid retrieve call",
    buckets=(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0),
)

rag_hybrid_median_score = Histogram(
    "rag_hybrid_median_score",
    "Median fused score of results per hybrid retrieve call",
    buckets=(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0),
)

rag_hybrid_score_spread = Histogram(
    "rag_hybrid_score_spread",
    "Score spread (max - min) per hybrid retrieve call, lower = more consistent results",
    buckets=(0.0, 0.05, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5, 0.7, 1.0),
)

rag_hybrid_top_k_requested = Histogram(
    "rag_hybrid_top_k_requested",
    "The top_k value requested per hybrid retrieve call",
    buckets=(1, 2, 3, 5, 10, 20, 50, 100),
)

rag_hybrid_sparse_top_k = Histogram(
    "rag_hybrid_sparse_top_k",
    "The sparse_top_k (prefetch) value used per hybrid retrieve call",
    buckets=(3, 6, 9, 15, 30, 60, 150, 300),
)

# Dense vs Sparse contribution breakdown (captured from fusion intercept, no extra queries)
rag_hybrid_dense_candidates = Histogram(
    "rag_hybrid_dense_candidates",
    "Number of dense (vector) candidate nodes returned before fusion",
    buckets=(0, 1, 2, 3, 5, 10, 20, 50, 100),
)

rag_hybrid_sparse_candidates = Histogram(
    "rag_hybrid_sparse_candidates",
    "Number of sparse (BM25) candidate nodes returned before fusion",
    buckets=(0, 1, 2, 3, 5, 10, 20, 50, 100),
)

rag_hybrid_overlap_count = Histogram(
    "rag_hybrid_overlap_count",
    "Number of final result nodes that appear in BOTH dense and sparse results",
    buckets=(0, 1, 2, 3, 5, 10, 20, 50),
)

rag_hybrid_dense_only_count = Histogram(
    "rag_hybrid_dense_only_count",
    "Number of final result nodes that came from dense (vector) search only",
    buckets=(0, 1, 2, 3, 5, 10, 20, 50),
)

rag_hybrid_sparse_only_count = Histogram(
    "rag_hybrid_sparse_only_count",
    "Number of final result nodes that came from sparse (BM25) search only",
    buckets=(0, 1, 2, 3, 5, 10, 20, 50),
)
