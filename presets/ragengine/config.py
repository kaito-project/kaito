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
LLM_INFERENCE_URL = os.getenv(
    "LLM_INFERENCE_URL", "http://localhost:5000/v1/completions"
)
LLM_ACCESS_SECRET = os.getenv("LLM_ACCESS_SECRET", "default-access-secret")
LLM_CONTEXT_WINDOW = int(
    os.getenv("LLM_CONTEXT_WINDOW", 64000)
)  # Default context window size
# LLM_RESPONSE_FIELD = os.getenv("LLM_RESPONSE_FIELD", "result")  # Uncomment if needed in the future

"""
=========================================================================
"""

# Vector database configuration
VECTOR_DB_IMPLEMENTATION = os.getenv("VECTOR_DB_IMPLEMENTATION", "faiss")
DEFAULT_VECTOR_DB_PERSIST_DIR = os.getenv("DEFAULT_VECTOR_DB_PERSIST_DIR", "storage")
