# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.
from typing import Any
from llama_index.embeddings.huggingface import HuggingFaceEmbedding
from .base import BaseEmbeddingModel

class LocalHuggingFaceEmbedding(HuggingFaceEmbedding, BaseEmbeddingModel):
    def get_embedding_dimension(self) -> int:
        """Infers the embedding dimension by making a local call to get the embedding of a dummy text."""
        dummy_input = "This is a dummy sentence."
        embedding = self.get_text_embedding(dummy_input)
        return len(embedding)