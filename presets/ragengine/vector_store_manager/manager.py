# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Dict, List, Any

from ragengine.models import Document
from ragengine.vector_store.base import BaseVectorStore

class VectorStoreManager:
    def __init__(self, vector_store: BaseVectorStore):
        self.vector_store = vector_store

    async def index(self, index_name: str, documents: List[Document]) -> List[str]:
        """Index new documents."""
        return await self.vector_store.index_documents(index_name, documents)

    async def query(self,
              index_name: str,
              query: str,
              top_k: int,
              llm_params: dict,
              rerank_params: dict
    ):
        """Query the indexed documents."""
        return await self.vector_store.query(index_name, query, top_k, llm_params, rerank_params)

    def list_indexes(self):
        """List all indexes."""
        return self.vector_store.list_indexes()

    async def list_documents_in_index(self,
            index_name: str,
            limit: int,
            offset: int,
            max_text_length: int,
            metadata_filter: dict,
    ) -> List[Dict[str, Any]]:
        """List all documents in index."""
        return await self.vector_store.list_documents_in_index(
            index_name,
            limit,
            offset,
            max_text_length,
            metadata_filter
        )

    async def persist(self, index_name: str, path: str) -> None:
        """Persist existing index."""
        return await self.vector_store.persist(index_name, path)

    async def load(self, index_name: str, path: str, overwrite: bool) -> None:
        """Load existing index."""
        return await self.vector_store.load(index_name, path, overwrite)

    async def shutdown(self):
        """Shutdown the manager."""
        await self.vector_store.shutdown()
