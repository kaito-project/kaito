from typing import List
from ragengine.models import Document

import chromadb
from llama_index.vector_stores.chroma import ChromaVectorStore
from .base import BaseVectorStore

class ChromaDBVectorStoreHandler(BaseVectorStore):
    def __init__(self, embedding_manager):
        super().__init__(embedding_manager)
        self.chroma_client = chromadb.EphemeralClient()

    def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        chroma_collection = self.chroma_client.create_collection(index_name)
        vector_store = ChromaVectorStore(chroma_collection=chroma_collection)
        return self._create_index_common(index_name, documents, vector_store)
