# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Dict, List
from ragengine.models import Document
import logging
import asyncio

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

    async def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        chroma_collection = self.chroma_client.create_collection(index_name)
        vector_store = ChromaVectorStore(chroma_collection=chroma_collection)
        return await self._create_index_common(index_name, documents, vector_store)

    async def document_exists(self, index_name: str, doc: Document, doc_id: str) -> bool:
        """ChromaDB for checking document existence."""
        if index_name not in self.index_map:
            logger.warning(f"No such index: '{index_name}' exists in vector store.")
            return False
        collection = await asyncio.to_thread(
            lambda: self.chroma_client.get_collection(index_name).get()
        )
        return doc.text in collection["documents"]

    async def list_documents_in_index(self, index_name: str) -> Dict[str, Dict[str, str]]:
        doc_map: Dict[str, Dict[str, str]] = {}
        try:
            collection_info = await asyncio.to_thread(
                lambda: self.chroma_client.get_collection(index_name).get()
            )
            for doc in zip(collection_info["ids"], collection_info["documents"], collection_info["metadatas"]):
                doc_map[doc[0]] = {
                    "text": doc[1],
                    "metadata": json.dumps(doc[2])
                }
        except Exception as e:
            print(f"Failed to get documents from collection '{index_name}': {e}")
        return doc_map

    async def list_all_documents(self) -> Dict[str, Dict[str, Dict[str, str]]]:
        indexed_docs = {} # Accumulate documents across all indexes
        try:
            # Wrap collection operations to avoid blocking
            collection_names = await asyncio.to_thread(self.chroma_client.list_collections)
            for collection_name in collection_names:
                collection_info = await asyncio.to_thread(
                    lambda: self.chroma_client.get_collection(collection_name).get()
                )
                for doc in zip(collection_info["ids"], collection_info["documents"], collection_info["metadatas"]):
                    indexed_docs.setdefault(collection_name, {})[doc[0]] = {
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
            collection_names = self.chroma_client.list_collections()

            # Delete each collection
            for collection_name in collection_names:
                self.chroma_client.delete_collection(name=collection_name)
                print(f"Collection '{collection_name}' has been deleted.")

            print("All collections in the ChromaDB instance have been deleted.")
        except Exception as e:
            print(f"Failed to clear collections in the ChromaDB instance: {e}")
    