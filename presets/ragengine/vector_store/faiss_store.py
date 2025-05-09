# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import List

import faiss
from llama_index.vector_stores.faiss import FaissMapVectorStore
from ragengine.models import Document
from ragengine.embedding.base import BaseEmbeddingModel
from .base import BaseVectorStore


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
        vector_store = FaissMapVectorStore(faiss_index=id_index)
        return await self._create_index_common(index_name, documents, vector_store)
