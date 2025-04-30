# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import List

import faiss
import asyncio
import numpy as np
from llama_index.vector_stores.faiss import FaissVectorStore
from ragengine.models import Document
from ragengine.embedding.base import BaseEmbeddingModel
from .base import BaseVectorStore
from .faiss_map_store import FaissVectorMapStore


class FaissVectorStoreHandler(BaseVectorStore):
    def __init__(self, embed_model: BaseEmbeddingModel):
        super().__init__(embed_model, use_rwlock=True)
        self.dimension = self.embed_model.get_embedding_dimension()
        self.fiass_index_reference = {}

    async def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        faiss_index = faiss.IndexFlatL2(self.dimension)
        id_index = faiss.IndexIDMap(faiss_index)
        self.fiass_index_reference[index_name] = id_index
        vector_store = FaissVectorMapStore(faiss_index=id_index)
        return await self._create_index_common(index_name, documents, vector_store)

    async def delete_document(self, index_name: str, doc_id: str):
        if index_name not in self.fiass_index_reference:
            raise ValueError(f"No such index: '{index_name}' exists.")
        retrieved_doc = await self.index_map[index_name].docstore.aget_ref_doc_info(doc_id)
        if not retrieved_doc:
            return
        faiss_doc_id = self._get_faiss_doc_id(index_name, retrieved_doc.node_ids[0])
        if faiss_doc_id is not None:
            if self.use_rwlock:
                async with self.rwlock.writer_lock:
                    tasks = [
                        asyncio.to_thread(self.fiass_index_reference[index_name].remove_ids, np.array([np.int64(faiss_doc_id)], dtype=np.int64)),
                        asyncio.to_thread(self.index_map[index_name].index_struct.delete, faiss_doc_id),
                        self.index_map[index_name]._adelete_from_docstore(doc_id),
                    ]
                    await asyncio.gather(*tasks)
                    self.index_map[index_name]._storage_context.index_store.add_index_struct(self.index_map[index_name]._index_struct)
                    self.index_store.add_index_struct(self.index_map[index_name]._index_struct)
            else:
                tasks = [
                    asyncio.to_thread(self.fiass_index_reference[index_name].remove_ids, np.array([np.int64(faiss_doc_id)], dtype=np.int64)),
                    asyncio.to_thread(self.index_map[index_name].index_struct.delete, faiss_doc_id),
                    self.index_map[index_name]._adelete_from_docstore(doc_id),
                ]
                await asyncio.gather(*tasks)
                self.index_map[index_name]._storage_context.index_store.add_index_struct(self.index_map[index_name]._index_struct)
                self.index_store.add_index_struct(self.index_map[index_name]._index_struct)
    
    async def update_document(self, index_name: str, doc_id: str, document: Document):
        if index_name not in self.index_map:
            raise ValueError(f"No such index: '{index_name}' exists.")
        if self.use_rwlock:
            async with self.rwlock.writer_lock:
                await self.delete_document(index_name, doc_id)
                await self.add_document_to_index(index_name, document, doc_id)
        else:
            await self.delete_document(index_name, doc_id)
            await self.add_document_to_index(index_name, document, doc_id)

    def _get_faiss_doc_id(self, index_name: str, doc_id: str) -> str:
        if index_name not in self.index_map:
            raise ValueError(f"No such index: '{index_name}' exists.")

        for k, v in self.index_map[index_name].index_struct.nodes_dict.items():
            if v == doc_id:
                return k
        return None