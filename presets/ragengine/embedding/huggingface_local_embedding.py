# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
from typing import Any, List, Union
import time
from io import BytesIO
from llama_index.embeddings.huggingface import HuggingFaceEmbedding
from .base import BaseEmbeddingModel
from ragengine.metrics.helpers import record_embedding_metrics
from ragengine.metrics.prometheus_metrics import (
    rag_embedding_requests_total, 
    rag_embedding_latency,
    STATUS_SUCCESS,
    STATUS_FAILURE,
    MODE_LOCAL
)

class LocalHuggingFaceEmbedding(HuggingFaceEmbedding, BaseEmbeddingModel):
    """HuggingFace embedding model with metrics collection."""
    
    def _embed_with_retry(
        self,
        inputs: List[Union[str, BytesIO]],
        prompt_name: Any = None,
    ) -> List[List[float]]:
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
            rag_embedding_latency.labels(status=status, mode=MODE_LOCAL).observe(latency)

    def get_embedding_dimension(self) -> int:
        """Infers the embedding dimension by making a local call to get the embedding of a dummy text."""
        dummy_input = "This is a dummy sentence."
        embedding = self.get_text_embedding(dummy_input)
        if embedding is None:
            raise ValueError("Unable to get embedding dimension due to None embedding.")
        return len(embedding)