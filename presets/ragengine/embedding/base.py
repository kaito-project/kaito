# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import List
from abc import ABC, abstractmethod
from llama_index.core.embeddings import BaseEmbedding
import asyncio

class BaseEmbeddingModel(BaseEmbedding, ABC):
    async def _aget_text_embedding(self, text: str) -> List[float]:
        return await asyncio.to_thread(self._get_text_embedding, text)

    async def _aget_query_embedding(self, query: str) -> List[float]:
        return await asyncio.to_thread(self._get_query_embedding, query)

    @abstractmethod
    def get_embedding_dimension(self) -> int:
        """Returns the embedding dimension for the model."""
        pass