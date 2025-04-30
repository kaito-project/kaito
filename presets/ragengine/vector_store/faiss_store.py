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

    async def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        faiss_index = faiss.IndexFlatL2(self.dimension)
        # we cant use the IndexFlatL2 directly as its delete functionality changes document ids.
        # we can wrap it in the IDMap to keep the same functionality but also be able to index by ids and support delete with llama_index
        # https://github.com/facebookresearch/faiss/wiki/Faiss-indexes#supported-operations
        id_index = faiss.IndexIDMap(faiss_index)
        vector_store = FaissVectorMapStore(faiss_index=id_index)
        return await self._create_index_common(index_name, documents, vector_store)

    # this function should be removes and the BaseVectorStore should be used instead,
    # but there is a bug in llama_index that prevents this from working
    async def delete_document(self, index_name: str, doc_id: str):
        """
        Delete a document from the index.
        Args:
            index_name (str): The name of the index to delete from.
            doc_id (str): The document ID to delete.
        """
        if index_name not in self.index_map:
            raise ValueError(f"No such index: '{index_name}' exists.")
        retrieved_doc = await self.index_map[index_name].docstore.aget_ref_doc_info(doc_id)
        if not retrieved_doc:
            return
        faiss_doc_id = self.index_map[index_name].storage_context.vector_store.ref_doc_id_map.get(doc_id)
        if faiss_doc_id is not None:
            if self.use_rwlock:
                async with self.rwlock.writer_lock:
                    tasks = [
                        # delete references which can be done in parallel
                        asyncio.to_thread(self.index_map[index_name].storage_context.vector_store.delete, doc_id),
                        asyncio.to_thread(self.index_map[index_name].index_struct.delete, str(faiss_doc_id)),
                        self.index_map[index_name]._adelete_from_docstore(doc_id),
                    ]
                    await asyncio.gather(*tasks)
                    # update index_structs at different levels
                    self.index_map[index_name].storage_context.index_store.add_index_struct(self.index_map[index_name]._index_struct)
                    self.index_store.add_index_struct(self.index_map[index_name]._index_struct)
            else:
                tasks = [
                    asyncio.to_thread(self.index_map[index_name].storage_context.vector_store.delete, doc_id),
                    asyncio.to_thread(self.index_map[index_name].index_struct.delete, str(faiss_doc_id)),
                    self.index_map[index_name]._adelete_from_docstore(doc_id),
                ]
                await asyncio.gather(*tasks)
                self.index_map[index_name].storage_context.index_store.add_index_struct(self.index_map[index_name]._index_struct)
                self.index_store.add_index_struct(self.index_map[index_name]._index_struct)
    
    async def update_document(self, index_name: str, doc_id: str, document: Document):
        """
        Update a document in the index.
        Args:
            index_name (str): The name of the index to update.
            doc_id (str): The document ID to update.
            document (Document): The new document to add.
        """
        if index_name not in self.index_map:
            raise ValueError(f"No such index: '{index_name}' exists.")
        if self.use_rwlock:
            async with self.rwlock.writer_lock:
                await self.delete_document(index_name, doc_id)
                await self.add_document_to_index(index_name, document, doc_id)
        else:
            await self.delete_document(index_name, doc_id)
            await self.add_document_to_index(index_name, document, doc_id)
