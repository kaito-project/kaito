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

import time
from io import BytesIO
from typing import Any

from llama_index.embeddings.huggingface import HuggingFaceEmbedding

from ragengine.metrics.prometheus_metrics import (
    MODE_LOCAL,
    STATUS_FAILURE,
    STATUS_SUCCESS,
    rag_embedding_latency,
    rag_embedding_requests_total,
)

from .base import BaseEmbeddingModel


class LocalHuggingFaceEmbedding(HuggingFaceEmbedding, BaseEmbeddingModel):
    """HuggingFace embedding model with metrics collection."""

    def _embed_with_retry(
        self,
        inputs: list[str | BytesIO],
        prompt_name: Any = None,
    ) -> list[list[float]]:
        start_time = time.perf_counter()
        status = STATUS_FAILURE  # Default to failure

        try:
            result = super()._embed_with_retry(inputs, prompt_name)
            status = STATUS_SUCCESS
            return result
        except Exception:
            raise
        finally:
            latency = time.perf_counter() - start_time
            rag_embedding_requests_total.labels(status=status, mode=MODE_LOCAL).inc()
            rag_embedding_latency.labels(status=status, mode=MODE_LOCAL).observe(
                latency
            )

    def get_embedding_dimension(self) -> int:
        """Infers the embedding dimension by making a local call to get the embedding of a dummy text."""
        dummy_input = "This is a dummy sentence."
        embedding = self.get_text_embedding(dummy_input)
        if embedding is None:
            raise ValueError("Unable to get embedding dimension due to None embedding.")
        return len(embedding)
