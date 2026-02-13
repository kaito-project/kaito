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


import logging

from llama_index.core import Document as LlamaDocument
from llama_index.core import StorageContext, VectorStoreIndex
from llama_index.vector_stores.qdrant import QdrantVectorStore
from qdrant_client import AsyncQdrantClient, QdrantClient

from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.models import Document

from .base import BaseVectorStore

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class QdrantVectorStoreHandler(BaseVectorStore):
    """Vector store handler using Qdrant as the backend.

    In server mode (qdrant_url is set), Qdrant manages its own persistence
    for vector data. On startup, existing collections are automatically
    discovered and the in-memory index_map is rebuilt from Qdrant's data,
    so no lifecycle hook snapshots are required for vector data recovery.

    In in-memory mode (qdrant_url is None), behavior is similar to FAISS —
    all data lives in-process and is lost on restart.
    """

    def __init__(
        self,
        embed_model: BaseEmbeddingModel,
        vector_db_url: str | None = None,
        vector_db_access_secret: str | None = None,
    ):
        # Qdrant server handles concurrency internally, no need for rwlock
        super().__init__(embed_model, use_rwlock=False)
        self.dimension = self.embed_model.get_embedding_dimension()
        self.is_server_mode = vector_db_url is not None

        if self.is_server_mode:
            self.client = QdrantClient(
                url=vector_db_url,
                api_key=vector_db_access_secret,
            )
            self.aclient = AsyncQdrantClient(
                url=vector_db_url,
                api_key=vector_db_access_secret,
            )
            logger.info(f"Connected to Qdrant server at {vector_db_url}")
            # Restore index_map from existing Qdrant collections
            self._restore_indexes_from_qdrant()
        else:
            # In-memory mode for testing and development
            self.client = QdrantClient(":memory:")
            self.aclient = AsyncQdrantClient(location=":memory:")
            logger.info(
                "Using in-memory Qdrant (data will not persist across restarts)"
            )

    def _build_vector_store(self, collection_name: str) -> QdrantVectorStore:
        """Build a QdrantVectorStore for the given collection name.

        Pre-creates the Qdrant collection with named vectors if it doesn't
        already exist. This works around a bug in llama-index-vector-stores-qdrant
        where non-hybrid mode creates unnamed vectors but _build_points() always
        uses the named vector format ("text-dense"), causing a schema mismatch.
        """
        from qdrant_client.http import models as rest

        if not self.client.collection_exists(collection_name):
            self.client.create_collection(
                collection_name=collection_name,
                vectors_config={
                    "text-dense": rest.VectorParams(
                        size=self.dimension,
                        distance=rest.Distance.COSINE,
                    ),
                },
            )
            logger.info(
                f"Pre-created Qdrant collection '{collection_name}' "
                f"with named vector 'text-dense' (dim={self.dimension})"
            )

        return QdrantVectorStore(
            collection_name=collection_name,
            client=self.client,
            aclient=self.aclient,
        )

    def _restore_indexes_from_qdrant(self):
        """Discover existing Qdrant collections and rebuild index_map on startup.

        This is the key method that eliminates the dependency on lifecycle hook
        snapshots. For each collection found in Qdrant, we:
        1. Create a QdrantVectorStore pointing to that collection
        2. Scroll all points to rebuild the LlamaIndex docstore
        3. Create a VectorStoreIndex with the restored data

        This ensures that after a Pod restart (even a hard kill), all data
        that was in Qdrant is immediately available again.
        """
        try:
            collections = self.client.get_collections().collections
            if not collections:
                logger.info("No existing Qdrant collections found.")
                return

            logger.info(
                f"Found {len(collections)} existing Qdrant collection(s): "
                f"{[c.name for c in collections]}"
            )

            for collection in collections:
                coll_name = collection.name
                try:
                    self._restore_single_index(coll_name)
                    logger.info(f"✓ Restored index '{coll_name}' from Qdrant.")
                except Exception as e:
                    logger.error(
                        f"✗ Failed to restore index '{coll_name}': {e}"
                    )
        except Exception as e:
            logger.error(f"Failed to discover Qdrant collections: {e}")

    def _restore_single_index(self, collection_name: str):
        """Restore a single index from an existing Qdrant collection.

        Scrolls all points from the collection to rebuild the docstore,
        then creates a VectorStoreIndex connected to the collection.
        """
        vector_store = self._build_vector_store(collection_name)
        storage_context = StorageContext.from_defaults(vector_store=vector_store)

        # Scroll all points from Qdrant to rebuild the docstore
        llama_docs = self._scroll_collection_to_docs(collection_name)

        if llama_docs:
            index = VectorStoreIndex.from_documents(
                llama_docs,
                storage_context=storage_context,
                embed_model=self.embed_model,
                use_async=False,
                transformations=[self.custom_transformer],
            )
        else:
            # Empty collection — create an empty index pointing to it
            index = VectorStoreIndex.from_vector_store(
                vector_store=vector_store,
                embed_model=self.embed_model,
            )

        index.set_index_id(collection_name)
        self.index_map[collection_name] = index
        logger.info(
            f"Restored index '{collection_name}' with {len(llama_docs)} document(s)."
        )

    def _scroll_collection_to_docs(
        self, collection_name: str
    ) -> list[LlamaDocument]:
        """Scroll all points in a Qdrant collection and convert to LlamaIndex Documents.

        Qdrant stores document text and metadata in point payloads (put there by
        LlamaIndex's QdrantVectorStore). We read them back to rebuild the docstore.
        """
        llama_docs = []
        seen_ref_doc_ids = set()  # Deduplicate by ref_doc_id (original document)
        offset = None

        while True:
            results = self.client.scroll(
                collection_name=collection_name,
                limit=100,
                offset=offset,
                with_payload=True,
                with_vectors=False,  # We don't need vectors, just payload
            )
            points, next_offset = results

            if not points:
                break

            for point in points:
                payload = point.payload or {}
                # LlamaIndex stores these fields in the Qdrant payload
                ref_doc_id = payload.get("ref_doc_id") or payload.get("doc_id")
                text = payload.get("text", "") or payload.get("_node_content", "")
                metadata = payload.get("metadata", {}) or {}

                # _node_content is a JSON string in LlamaIndex; extract text from it
                if not text and "_node_content" in payload:
                    import json

                    try:
                        node_content = json.loads(payload["_node_content"])
                        text = node_content.get("text", "")
                        if not metadata:
                            metadata = node_content.get("metadata", {})
                    except (json.JSONDecodeError, TypeError):
                        pass

                if ref_doc_id and ref_doc_id not in seen_ref_doc_ids:
                    seen_ref_doc_ids.add(ref_doc_id)
                    llama_docs.append(
                        LlamaDocument(
                            id_=ref_doc_id,
                            text=text,
                            metadata=metadata,
                        )
                    )

            if next_offset is None:
                break
            offset = next_offset

        return llama_docs

    async def _create_new_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Create a new Qdrant collection and index documents into it."""
        vector_store = self._build_vector_store(index_name)
        return await self._create_index_common(index_name, documents, vector_store)

    def _create_storage_context_for_load(
        self, index_name: str, path: str
    ) -> StorageContext:
        """Create storage context that reconnects to an existing Qdrant collection.

        The vector data lives in Qdrant (persistent in server mode), while
        the docstore and index_store are loaded from the persist directory.
        """
        vector_store = self._build_vector_store(index_name)
        return StorageContext.from_defaults(
            persist_dir=path, vector_store=vector_store
        )

    def list_indexes(self) -> list[str]:
        """List indexes — in server mode, also include Qdrant collections
        that might not yet be in index_map (e.g., created externally)."""
        if self.is_server_mode:
            try:
                collections = self.client.get_collections().collections
                qdrant_names = {c.name for c in collections}
                memory_names = set(self.index_map.keys())
                return list(qdrant_names | memory_names)
            except Exception as e:
                logger.warning(f"Failed to list Qdrant collections: {e}")
        return list(self.index_map)

    async def delete_index(self, index_name: str):
        """Delete the index from memory and the corresponding Qdrant collection."""
        # Remove from index_map (via parent)
        await super().delete_index(index_name)
        # Also delete the Qdrant collection
        try:
            self.client.delete_collection(index_name)
            logger.info(f"Deleted Qdrant collection '{index_name}'.")
        except Exception as e:
            logger.warning(
                f"Failed to delete Qdrant collection '{index_name}': {e}"
            )

    async def shutdown(self):
        """Shutdown handler, closes the Qdrant client connection."""
        await super().shutdown()
        try:
            self.client.close()
            logger.info("Qdrant client closed.")
        except Exception as e:
            logger.warning(f"Error closing Qdrant client: {e}")
