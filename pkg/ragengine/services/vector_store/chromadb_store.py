# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Dict, List
from services.models import Document
import logging

import chromadb
import json
from llama_index.vector_stores.chroma import ChromaVectorStore

from .base import BaseVectorStore

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class ChromaDBVectorStoreHandler(BaseVectorStore):
    def __init__(self, embedding_manager):
        super().__init__(embedding_manager)
        self.chroma_client = chromadb.EphemeralClient()

    def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        chroma_collection = self.chroma_client.create_collection(index_name)
        vector_store = ChromaVectorStore(chroma_collection=chroma_collection)
        return self._create_index_common(index_name, documents, vector_store)

    def document_exists(self, index_name: str, doc: Document, doc_id: str) -> bool:
        """ChromaDB for checking document existence."""
        if index_name not in self.index_map:
            logger.warning(f"No such index: '{index_name}' exists in vector store.")
            return False
        return doc.text in self.chroma_client.get_collection(name=index_name).get()["documents"]

    def list_all_indexed_documents(self) -> Dict[str, Dict[str, Dict[str, str]]]:
        indexed_docs = {} # Accumulate documents across all indexes
        try:
            for collection in self.chroma_client.list_collections():
                collection_info = collection.get()
                for doc in zip(collection_info["ids"], collection_info["documents"], collection_info["metadatas"]):
                    indexed_docs.setdefault(collection.name, {})[doc[0]] = {
                        "text": doc[1],
                        "metadata": json.dumps(doc[2]),
                    }
        except Exception as e:
            print(f"Failed to get all collections in the ChromaDB instance: {e}")
        return indexed_docs

    def _clear_collection_and_indexes(self):
        """Clears all collections and drops all indexes in the ChromaDB instance.

        This method is primarily intended for testing purposes to ensure
        a clean state between tests, preventing index and document conflicts.
        """
        try:
           # Get all collections
            collections = self.chroma_client.list_collections()

            # Delete each collection
            for collection in collections:
                collection_name = collection.name
                self.chroma_client.delete_collection(name=collection_name)
                print(f"Collection '{collection_name}' has been deleted.")

            print("All collections in the ChromaDB instance have been deleted.")
        except Exception as e:
            print(f"Failed to clear collections in the ChromaDB instance: {e}")
    