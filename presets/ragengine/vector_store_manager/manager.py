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


from typing import Any

from ragengine.models import Document
from ragengine.vector_store.base import BaseVectorStore


class VectorStoreManager:
    def __init__(self, vector_store: BaseVectorStore):
        self.vector_store = vector_store

    async def index(self, index_name: str, documents: list[Document]) -> list[str]:
        """Index new documents."""
        return await self.vector_store.index_documents(index_name, documents)

    async def query(
        self,
        index_name: str,
        query: str,
        top_k: int,
        llm_params: dict,
        rerank_params: dict,
    ):
        """Query the indexed documents."""
        return await self.vector_store.query(
            index_name, query, top_k, llm_params, rerank_params
        )

    async def chat_completion(self, request: dict):
        """Chat completion using the vector store."""
        return await self.vector_store.chat_completion(request)

    def list_indexes(self):
        """List all indexes."""
        return self.vector_store.list_indexes()

    async def list_documents_in_index(
        self,
        index_name: str,
        limit: int,
        offset: int,
        max_text_length: int,
        metadata_filter: dict,
    ) -> list[dict[str, Any]]:
        """List all documents in index."""
        return await self.vector_store.list_documents_in_index(
            index_name, limit, offset, max_text_length, metadata_filter
        )

    async def update_documents(self, index_name: str, documents: list[Document]):
        """Update documents in the index."""
        return await self.vector_store.update_documents(index_name, documents)

    async def delete_documents(self, index_name: str, doc_ids: list[str]) -> list[str]:
        """Delete documents from the index."""
        return await self.vector_store.delete_documents(index_name, doc_ids)

    async def persist(self, index_name: str, path: str) -> None:
        """Persist existing index."""
        return await self.vector_store.persist(index_name, path)

    async def load(self, index_name: str, path: str, overwrite: bool) -> None:
        """Load existing index."""
        return await self.vector_store.load(index_name, path, overwrite)

    async def delete_index(self, index_name: str) -> None:
        """Delete an index."""
        return await self.vector_store.delete_index(index_name)

    async def shutdown(self):
        """Shutdown the manager."""
        await self.vector_store.shutdown()
