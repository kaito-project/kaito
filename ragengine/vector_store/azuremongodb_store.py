import logging
from typing import List, Dict
import os
from ragengine.models import Document

import pymongo
import json
from llama_index.vector_stores.azurecosmosmongo import (
    AzureCosmosDBMongoDBVectorSearch,
)

from .base import BaseVectorStore

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class AzureCosmosDBMongoDBVectorStoreHandler(BaseVectorStore):
    def __init__(self, embedding_manager):
        super().__init__(embedding_manager)
        self.db_name = os.getenv("AZURE_COSMOSDB_MONGODB_DB_NAME", "vector-store")
        self.collection_name = os.getenv("AZURE_COSMOSDB_MONGODB_COLLECTION_NAME", "vector-store")
        self.connection_string = os.getenv("AZURE_COSMOSDB_MONGODB_URI", "mongodb+srv://myDatabaseUser:D1fficultP%40ssw0rd@cluster0.example.mongodb.net/?retryWrites=true&w=majority")
        self.dimension = self.embedding_manager.get_embedding_dimension()
        try:
            self.mongodb_client = pymongo.MongoClient(self.connection_string)
            self.mongodb_client.admin.command('ping') # Test the connection
        except pymongo.errors.ConnectionError as e:
            raise Exception(f"Failed to connect to MongoDB: {e}")
        # Ensure collection exists
        try:
            self.collection = self.mongodb_client[self.db_name][self.collection_name]
        except Exception as e:
            raise ValueError(f"Failed to access collection '{self.collection_name}' in database '{self.db_name}': {e}")


    def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        vector_store = AzureCosmosDBMongoDBVectorSearch(
            mongodb_client=self.mongodb_client,
            db_name=self.db_name,
            collection_name=self.collection_name,
            index_name=index_name,
            embedding_key=f"{index_name}_embedding",  # Unique field for each index
            cosmos_search_kwargs={
                # TODO: "kind": "vector-hnsw",  # or "vector-ivf", "vector-diskann" (search type)
                "dimensions": self.dimension,
            }
        )
        return self._create_index_common(index_name, documents, vector_store)

    def list_all_indexed_documents(self) -> Dict[str, Dict[str, Dict[str, str]]]:
        indexed_docs = {}  # Accumulate documents across all indexes
        for index_name in self.index_map.keys():
            embedding_key = f"{index_name}_embedding"
            documents = self.collection.find({embedding_key: {"$exists": True}})
            for doc in documents:
                doc_id = doc.get("id")
                if doc_id is None:
                    continue  # Skip if no document ID is found
                indexed_docs.setdefault(index_name, {})[doc_id] = {
                    "text": doc.get("text", ""),
                    "metadata": json.dumps(doc.get("metadata", {})),
                    "content_vector": f"Vector of dimension {len(doc.get(embedding_key, []))}"
                }
        return indexed_docs

    def document_exists(self, index_name: str, doc: Document, doc_id: str) -> bool:
        """AzureCosmosDBMongoDB for checking document existence."""
        if index_name not in self.index_map:
            logger.warning(f"No such index: '{index_name}' exists in vector store.")
            return False
        return doc.text in [doc["text"] for doc in list(self.collection.find({f"{index_name}_embedding": {"$exists": True}}))]

    def _clear_collection_and_indexes(self):
        """Clears all documents and drops all indexes in the collection.

        This method is primarily intended for testing purposes to ensure
        a clean state between tests, preventing index and document conflicts.
        """
        try:
            # Delete all documents in the collection
            self.collection.delete_many({})
            print(f"All documents in collection '{self.collection_name}' have been deleted.")

            # Drop all indexes in the collection
            self.collection.drop_indexes()
            print(f"All indexes in collection '{self.collection_name}' have been dropped.")

        except Exception as e:
            print(f"Failed to clear collection and indexes in '{self.collection_name}': {e}")
