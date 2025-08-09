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


import faiss
from llama_index.vector_stores.faiss import FaissMapVectorStore

from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.models import Document

from .base import BaseVectorStore


class FaissVectorStoreHandler(BaseVectorStore):
    def __init__(self, embed_model: BaseEmbeddingModel):
        super().__init__(embed_model, use_rwlock=True)
        self.dimension = self.embed_model.get_embedding_dimension()

    async def _create_new_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        faiss_index = faiss.IndexFlatL2(self.dimension)
        # we cant use the IndexFlatL2 directly as its delete functionality changes document ids.
        # we can wrap it in the IDMap to keep the same functionality but also be able to index by ids and support delete with llama_index
        # https://github.com/facebookresearch/faiss/wiki/Faiss-indexes#supported-operations
        id_index = faiss.IndexIDMap(faiss_index)
        vector_store = FaissMapVectorStore(faiss_index=id_index)
        return await self._create_index_common(index_name, documents, vector_store)
