# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
from typing import Any
from llama_index.embeddings.huggingface import HuggingFaceEmbedding
from .base import BaseEmbeddingModel
from ragengine.metrics.helpers import record_embedding_metrics

class LocalHuggingFaceEmbedding(HuggingFaceEmbedding, BaseEmbeddingModel):
    @record_embedding_metrics
    def get_embedding_dimension(self) -> int:
        """Infers the embedding dimension by making a local call to get the embedding of a dummy text."""
        dummy_input = "This is a dummy sentence."
        embedding = self.get_text_embedding(dummy_input)
        if embedding is None:
            raise ValueError("Unable to get embedding dimension due to None embedding.")
        return len(embedding)