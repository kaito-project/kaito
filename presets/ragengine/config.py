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


# config.py

# Configuration variables are set via environment variables from the RAGEngine CR
# and exposed to the pod. For example, `LLM_INFERENCE_URL` is specified in the CR and
# passed to the pod via environment variables.

import os

"""
=========================================================================
"""

# Embedding configuration
EMBEDDING_SOURCE_TYPE = os.getenv(
    "EMBEDDING_SOURCE_TYPE", "local"
)  # Determines local or remote embedding source

# Local embedding model
LOCAL_EMBEDDING_MODEL_ID = os.getenv(
    "LOCAL_EMBEDDING_MODEL_ID", "BAAI/bge-small-en-v1.5"
)

# Remote embedding model (if not local)
REMOTE_EMBEDDING_URL = os.getenv(
    "REMOTE_EMBEDDING_URL", "http://localhost:5000/embedding"
)
REMOTE_EMBEDDING_ACCESS_SECRET = os.getenv(
    "REMOTE_EMBEDDING_ACCESS_SECRET", "default-access-secret"
)

"""
=========================================================================
"""

# Reranking Configuration
# For now we support simple LLMReranker, future additions would include
# FlagEmbeddingReranker, SentenceTransformerReranker, CohereReranker
LLM_RERANKER_BATCH_SIZE = int(
    os.getenv("LLM_RERANKER_BATCH_SIZE", 5)
)  # Default LLM batch size
LLM_RERANKER_TOP_N = int(
    os.getenv("LLM_RERANKER_TOP_N", 3)
)  # Default top 3 reranked nodes

"""
=========================================================================
"""

# LLM (Large Language Model) configuration
# LLM_INFERENCE_URL will be None if InferenceService is not configured in RAGEngine spec
LLM_INFERENCE_URL = os.getenv("LLM_INFERENCE_URL")
LLM_ACCESS_SECRET = os.getenv("LLM_ACCESS_SECRET", "default-access-secret")
LLM_CONTEXT_WINDOW = int(
    os.getenv("LLM_CONTEXT_WINDOW", 64000)
)  # Default context window size
# LLM_RESPONSE_FIELD = os.getenv("LLM_RESPONSE_FIELD", "result")  # Uncomment if needed in the future


def _parse_csv_env(name: str) -> tuple[str, ...]:
    raw_value = os.getenv(name, "")
    return tuple(item.strip() for item in raw_value.split(",") if item.strip())


OUTPUT_GUARDRAILS_ENABLED = (
    os.getenv("OUTPUT_GUARDRAILS_ENABLED", "false").lower() == "true"
)
OUTPUT_GUARDRAILS_FAIL_OPEN = (
    os.getenv("OUTPUT_GUARDRAILS_FAIL_OPEN", "true").lower() == "true"
)
OUTPUT_GUARDRAILS_ACTION_ON_HIT = os.getenv(
    "OUTPUT_GUARDRAILS_ACTION_ON_HIT", "redact"
).lower()
OUTPUT_GUARDRAILS_REGEX_PATTERNS = _parse_csv_env("OUTPUT_GUARDRAILS_REGEX_PATTERNS")
OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS = _parse_csv_env(
    "OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS"
)
OUTPUT_GUARDRAILS_BLOCK_MESSAGE = os.getenv(
    "OUTPUT_GUARDRAILS_BLOCK_MESSAGE",
    "The model output was blocked by output guardrails.",
)
OUTPUT_GUARDRAILS_STREAM_HOLDBACK_CHARS = int(
    os.getenv("OUTPUT_GUARDRAILS_STREAM_HOLDBACK_CHARS", "32")
)
OUTPUT_GUARDRAILS_AUDIT_SINKS = _parse_csv_env(
    "OUTPUT_GUARDRAILS_AUDIT_SINKS"
) or ("log",)
OUTPUT_GUARDRAILS_AUDIT_FILE_PATH = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_FILE_PATH", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_TIMEOUT_SECONDS = float(
    os.getenv("OUTPUT_GUARDRAILS_AUDIT_REMOTE_TIMEOUT_SECONDS", "3.0")
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_HEADER = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_HEADER", "Authorization"
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN_PREFIX = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN_PREFIX", "Bearer "
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_HEADER = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_HEADER", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN_PREFIX = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN_PREFIX", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_HEADER = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_HEADER", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN_PREFIX = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN_PREFIX", ""
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_MAX_RETRIES = int(
    os.getenv("OUTPUT_GUARDRAILS_AUDIT_REMOTE_MAX_RETRIES", "2")
)
OUTPUT_GUARDRAILS_AUDIT_REMOTE_RETRY_BACKOFF_SECONDS = float(
    os.getenv("OUTPUT_GUARDRAILS_AUDIT_REMOTE_RETRY_BACKOFF_SECONDS", "0.2")
)
OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS = float(
    os.getenv("OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS", "5.0")
)
OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS = int(
    os.getenv("OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS", "128")
)
OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY = os.getenv(
    "OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY", "drop"
).lower()

"""
=========================================================================
"""

# Vector database configuration
# VECTOR_DB_TYPE is injected by the Go controller from CRD spec.storage.vectorDB.engine
# When VectorDB is nil in the CRD, FAISS (in-process) is used by default.
# Supported values: "faiss" (default, in-process), "qdrant" (client-server)
VECTOR_DB_TYPE = os.getenv("VECTOR_DB_TYPE", "faiss")
DEFAULT_VECTOR_DB_PERSIST_DIR = os.getenv("DEFAULT_VECTOR_DB_PERSIST_DIR", "storage")

# Vector DB connection info (injected from CRD spec.storage.vectorDB)
# Used when VECTOR_DB_TYPE is a client-server backend (e.g., qdrant)
VECTOR_DB_URL = os.getenv("VECTOR_DB_URL", None)  # None = in-memory/local mode
VECTOR_DB_ACCESS_SECRET = os.getenv("VECTOR_DB_ACCESS_SECRET", None)

"""
=========================================================================
"""

# RAG configurations
RAG_SIMILARITY_THRESHOLD = float(os.getenv("RAG_SIMILARITY_THRESHOLD", 0.85))
RAG_DEFAULT_CONTEXT_TOKEN_FILL_RATIO = float(
    os.getenv("RAG_CONTEXT_TOKEN_FILL_RATIO", 0.5)
)
# When calculating how many documents to fetch in vector search
# Code splitting has a max of 1500 chars while sentence splitting has a max of 1000.
# If we take the max of the two, we can use 1500 chars / 3 chars per token
# as a conservative estimate we get 500 tokens per doc
RAG_DOCUMENT_NODE_TOKEN_APPROXIMATION = float(
    os.getenv("RAG_DOCUMENT_NODE_TOKEN_APPROXIMATION", 500)
)
# Maximum top_k value for retrieve to prevent excessive memory usage and latency
RAG_MAX_TOP_K = int(os.getenv("RAG_MAX_TOP_K", 300))
