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


import contextlib
import json
import logging
import time
from typing import Any

from fastapi import HTTPException
from llama_index.core import Document as LlamaDocument
from llama_index.core import StorageContext, VectorStoreIndex
from llama_index.vector_stores.milvus import MilvusVectorStore
from pymilvus import MilvusClient

from ragengine.config import RAG_MAX_TOP_K
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.models import (
    Document,
    ListDocumentsResponse,
)

from .base import BaseVectorStore

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class MilvusVectorStoreHandler(BaseVectorStore):
    """Vector store handler using Milvus as the backend.

    In server mode (vector_db_url is set), connects to an external Milvus
    service (e.g. Milvus Standalone/Cluster). On startup, existing collections
    are automatically discovered and the in-memory index_map is rebuilt from
    Milvus data, so no lifecycle hook snapshots are required.

    In local mode (vector_db_url is None), uses Milvus Lite (embedded) which
    stores data in a local file. Suitable for testing and development.
    """

    # Milvus sync client causes nested event loop errors when use_async=True
    # inside FastAPI's async context. Disable async indexing for Milvus.
    _use_async_indexing: bool = False

    def __init__(
        self,
        embed_model: BaseEmbeddingModel,
        vector_db_url: str | None = None,
        vector_db_access_secret: str | None = None,
    ):
        # Milvus handles concurrency internally, no need for rwlock
        super().__init__(embed_model, use_rwlock=False)
        self.dimension = self.embed_model.get_embedding_dimension()
        self.is_server_mode = vector_db_url is not None

        if self.is_server_mode:
            self.milvus_uri = vector_db_url
            self.milvus_token = vector_db_access_secret or ""
            self.client = MilvusClient(
                uri=self.milvus_uri,
                token=self.milvus_token,
            )
            logger.info(f"Connected to Milvus server at {vector_db_url}")
            # Restore index_map from existing Milvus collections
            self._restore_indexes_from_milvus()
        else:
            # Milvus Lite (embedded) mode for testing and development
            self.milvus_uri = "./milvus_ragengine.db"
            self.milvus_token = ""
            self.client = MilvusClient(uri=self.milvus_uri)
            logger.info(
                "Using Milvus Lite (embedded) mode — data stored in ./milvus_ragengine.db"
            )

    def _build_vector_store(self, collection_name: str) -> MilvusVectorStore:
        """Build a MilvusVectorStore for the given collection name.

        MilvusVectorStore auto-creates the collection if it doesn't exist
        when `dim` is provided. We use COSINE similarity by default to match
        the behaviour of the Qdrant backend.
        """
        return MilvusVectorStore(
            uri=self.milvus_uri,
            token=self.milvus_token,
            collection_name=collection_name,
            dim=self.dimension,
            similarity_metric="COSINE",
            overwrite=False,
        )

    def _restore_indexes_from_milvus(self):
        """Discover existing Milvus collections and rebuild index_map on startup.

        For each collection found in Milvus we:
        1. Create a MilvusVectorStore pointing to that collection
        2. Query all entities to rebuild the LlamaIndex docstore
        3. Create a VectorStoreIndex with the restored data

        This ensures that after a Pod restart, all data that was in Milvus
        is immediately available again.
        """
        try:
            collections = self.client.list_collections()
            if not collections:
                logger.info("No existing Milvus collections found.")
                return

            logger.info(
                f"Found {len(collections)} existing Milvus collection(s): {collections}"
            )

            for coll_name in collections:
                try:
                    self._restore_single_index(coll_name)
                    logger.info(f"Restored index '{coll_name}' from Milvus.")
                except Exception as e:
                    logger.error(f"Failed to restore index '{coll_name}': {e}")
        except Exception as e:
            logger.error(f"Failed to discover Milvus collections: {e}")

    def _restore_single_index(self, collection_name: str):
        """Restore a single index from an existing Milvus collection.

        Queries all entities from the collection to rebuild the docstore,
        then creates a VectorStoreIndex connected to the collection.
        """
        vector_store = self._build_vector_store(collection_name)
        storage_context = StorageContext.from_defaults(vector_store=vector_store)

        # Query all entities from Milvus to rebuild the docstore
        llama_docs = self._query_collection_to_docs(collection_name)

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

    def _query_collection_to_docs(self, collection_name: str) -> list[LlamaDocument]:
        """Query all entities in a Milvus collection and convert to LlamaIndex Documents.

        Milvus stores document text and metadata in entity fields (put there by
        LlamaIndex's MilvusVectorStore). We read them back to rebuild the docstore.
        """
        llama_docs = []
        seen_ref_doc_ids = set()  # Deduplicate by ref_doc_id (original document)

        try:
            # Get total count first
            count_result = self.client.query(
                collection_name=collection_name,
                output_fields=["count(*)"],
            )
            total = count_result[0].get("count(*)", 0) if count_result else 0

            if total == 0:
                return llama_docs

            # Query all entities (Milvus doesn't have scroll API like Qdrant;
            # use query with limit). For large collections, batch if needed.
            batch_size = 1000
            offset = 0

            while offset < total:
                results = self.client.query(
                    collection_name=collection_name,
                    filter="",
                    output_fields=["*"],
                    limit=batch_size,
                    offset=offset,
                )

                if not results:
                    break

                for entity in results:
                    # LlamaIndex MilvusVectorStore stores these fields:
                    #  - doc_id (ref_doc_id of the original document)
                    #  - text (the chunk text)
                    #  - id (node_id, the primary key)
                    ref_doc_id = entity.get("doc_id", "")
                    text = entity.get("text", "")

                    # Try extracting metadata from _node_content if present
                    metadata = {}
                    node_content_str = entity.get("_node_content", "")
                    if node_content_str:
                        try:
                            node_content = json.loads(node_content_str)
                            if not text:
                                text = node_content.get("text", "")
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

                offset += len(results)

        except Exception as e:
            logger.error(
                f"Failed to query entities from Milvus collection "
                f"'{collection_name}': {e}"
            )

        return llama_docs

    async def _create_new_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Create a new Milvus collection and index documents into it."""
        vector_store = self._build_vector_store(index_name)
        return await self._create_index_common(index_name, documents, vector_store)

    def _create_storage_context_for_load(
        self, index_name: str, path: str
    ) -> StorageContext:
        """Create storage context that reconnects to an existing Milvus collection.

        The vector data lives in Milvus (persistent in server mode), while
        the docstore and index_store are loaded from the persist directory.
        """
        vector_store = self._build_vector_store(index_name)
        return StorageContext.from_defaults(persist_dir=path, vector_store=vector_store)

    def list_indexes(self) -> list[str]:
        """List indexes — also include Milvus collections that might not yet
        be in index_map (e.g., created externally)."""
        try:
            milvus_names = set(self.client.list_collections())
            memory_names = set(self.index_map.keys())
            return list(milvus_names | memory_names)
        except Exception as e:
            logger.warning(f"Failed to list Milvus collections: {e}")
        return list(self.index_map)

    async def delete_index(self, index_name: str):
        """Delete the index from memory and the corresponding Milvus collection."""
        # Remove from index_map (via parent)
        await super().delete_index(index_name)
        # Also drop the Milvus collection
        try:
            self.client.drop_collection(index_name)
            logger.info(f"Dropped Milvus collection '{index_name}'.")
        except Exception as e:
            logger.warning(f"Failed to drop Milvus collection '{index_name}': {e}")

    async def shutdown(self):
        """Shutdown handler, closes the Milvus client connection."""
        await super().shutdown()
        try:
            self.client.close()
            logger.info("Milvus client closed.")
        except Exception as e:
            logger.warning(f"Error closing Milvus client: {e}")

    # ------------------------------------------------------------------
    # Milvus-native overrides
    #
    # Although _restore_single_index rebuilds the docstore (unlike Qdrant),
    # providing direct Milvus overrides ensures:
    #   - Pagination via Milvus query (more efficient for large collections)
    #   - Metadata filtering at the Milvus level
    #   - Consistent delete behaviour (removes from both Milvus and docstore)
    #   - document_exists check directly against Milvus
    # ------------------------------------------------------------------

    # LlamaIndex MilvusVectorStore stores doc_id in this field
    MILVUS_DOC_ID_FIELD = "doc_id"

    @staticmethod
    def _build_milvus_filter(
        metadata_filter: dict[str, Any] | None,
    ) -> str:
        """Build a Milvus boolean filter expression from a {key: value} dict."""
        if not metadata_filter:
            return ""
        parts = []
        for k, v in metadata_filter.items():
            if isinstance(v, str):
                parts.append(f'{k} == "{v}"')
            else:
                parts.append(f"{k} == {v}")
        return " and ".join(parts)

    @staticmethod
    def _entity_to_doc_dict(
        entity: dict, max_text_length: int | None = None
    ) -> dict[str, Any]:
        """Convert a Milvus query result entity to the dict format expected by
        ListDocumentsResponse."""
        text = entity.get("text", "")
        if not text:
            try:
                nc = json.loads(entity.get("_node_content", "{}"))
                text = nc.get("text", "")
            except (json.JSONDecodeError, TypeError):
                text = ""

        is_truncated = bool(max_text_length and len(text) > max_text_length)
        truncated = text[:max_text_length] if is_truncated else text

        # Extract user metadata — everything except Milvus/LlamaIndex internal fields
        _internal_keys = {
            "id",
            "pk",
            "doc_id",
            "text",
            "embedding",
            "_node_content",
            "_node_type",
            "document_id",
            "ref_doc_id",
        }
        metadata = {k: v for k, v in entity.items() if k not in _internal_keys}

        # Compute hash consistently with LlamaIndex
        try:
            hash_value = LlamaDocument(text=text, metadata=metadata).hash
        except Exception:
            hash_value = None

        return {
            "doc_id": entity.get("doc_id", str(entity.get("id", ""))),
            "text": truncated,
            "hash_value": hash_value,
            "metadata": metadata,
            "is_truncated": is_truncated,
        }

    async def _list_documents_in_index(
        self,
        index_name: str,
        limit: int,
        offset: int,
        max_text_length: int | None = None,
        metadata_filter: dict[str, Any] | None = None,
    ) -> ListDocumentsResponse:
        """List documents by querying Milvus directly with pagination."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        collection_name = index_name
        filter_expr = self._build_milvus_filter(metadata_filter) or ""

        try:
            # Get total count
            count_result = self.client.query(
                collection_name=collection_name,
                filter=filter_expr if filter_expr else "",
                output_fields=["count(*)"],
            )
            total_count = count_result[0].get("count(*)", 0) if count_result else 0

            # Milvus returns chunks (nodes), but we want unique documents.
            # Deduplicate by doc_id — query enough entities to fill the page.
            # For simplicity, query a window and deduplicate in Python.
            seen_doc_ids: set[str] = set()
            docs: list[dict[str, Any]] = []
            batch_size = max(limit * 3, 100)  # Over-fetch to handle chunk dedup
            milvus_offset = 0
            skipped = 0

            while len(docs) < limit and milvus_offset < total_count:
                entities = self.client.query(
                    collection_name=collection_name,
                    filter=filter_expr if filter_expr else "",
                    output_fields=["*"],
                    limit=batch_size,
                    offset=milvus_offset,
                )
                if not entities:
                    break

                for entity in entities:
                    doc_id = entity.get("doc_id", str(entity.get("id", "")))
                    if doc_id in seen_doc_ids:
                        continue
                    seen_doc_ids.add(doc_id)

                    if skipped < offset:
                        skipped += 1
                        continue

                    if len(docs) >= limit:
                        break

                    docs.append(self._entity_to_doc_dict(entity, max_text_length))

                milvus_offset += len(entities)

            # Total unique doc count (approximate — use count of unique doc_ids seen)
            # For an exact count we'd need to scan all, which is expensive.
            # Use total_count as an upper bound.
            return ListDocumentsResponse(
                documents=docs, count=len(docs), total_items=total_count
            )
        except Exception as e:
            logger.error(
                f"Failed to list documents in Milvus collection '{index_name}': {e}"
            )
            raise HTTPException(status_code=500, detail=f"List documents failed: {e}")

    async def document_exists(
        self, index_name: str, doc: Document, doc_id: str
    ) -> bool:
        """Check document existence by querying Milvus directly."""
        if index_name not in self.index_map:
            return False
        try:
            result = self.client.query(
                collection_name=index_name,
                filter=f'{self.MILVUS_DOC_ID_FIELD} == "{doc_id}"',
                output_fields=["doc_id"],
                limit=1,
            )
            return len(result) > 0
        except Exception as e:
            logger.warning(f"Milvus document_exists check failed: {e}")
            return False

    async def delete_documents(self, index_name: str, doc_ids: list[str]):
        """Delete documents from both Milvus and the in-memory docstore."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        found_docs = []
        not_found_docs = []

        op_start = time.time()
        op_status = "success"
        try:
            for doc_id in doc_ids:
                # Check existence in Milvus
                result = self.client.query(
                    collection_name=index_name,
                    filter=f'{self.MILVUS_DOC_ID_FIELD} == "{doc_id}"',
                    output_fields=["id"],
                    limit=1,
                )
                if result:
                    # Delete all points with this doc_id (could be multiple chunks)
                    self.client.delete(
                        collection_name=index_name,
                        filter=f'{self.MILVUS_DOC_ID_FIELD} == "{doc_id}"',
                    )
                    # Also remove from in-memory docstore if present
                    with contextlib.suppress(Exception):
                        await self.index_map[index_name].adelete_ref_doc(
                            doc_id, delete_from_docstore=True
                        )
                    found_docs.append(doc_id)
                else:
                    not_found_docs.append(doc_id)

            return {"deleted_doc_ids": found_docs, "not_found_doc_ids": not_found_docs}
        except Exception as e:
            op_status = "error"
            logger.error(f"Error deleting documents from Milvus: {e}")
            raise HTTPException(status_code=500, detail=f"Delete failed: {e}")
        finally:
            try:
                from ragengine.metrics.prometheus_metrics import (
                    rag_vector_store_operation_latency,
                )

                rag_vector_store_operation_latency.labels(
                    operation="delete", status=op_status
                ).observe(time.time() - op_start)
            except Exception:
                pass

    async def update_documents(self, index_name: str, documents: list[Document]):
        """Update documents in Milvus: delete old chunks and re-insert."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        updated_docs = []
        unchanged_docs = []
        not_found_docs = []

        try:
            for doc in documents:
                # Check existence
                result = self.client.query(
                    collection_name=index_name,
                    filter=f'{self.MILVUS_DOC_ID_FIELD} == "{doc.doc_id}"',
                    output_fields=["text"],
                    limit=1,
                )
                if not result:
                    not_found_docs.append(doc)
                    continue

                # Compare hash to skip unchanged documents
                old_text = result[0].get("text", "")
                new_hash = LlamaDocument(text=doc.text, metadata=doc.metadata).hash
                old_hash = LlamaDocument(text=old_text, metadata={}).hash
                if new_hash == old_hash:
                    unchanged_docs.append(doc)
                    continue

                # Delete old chunks, then re-insert
                self.client.delete(
                    collection_name=index_name,
                    filter=f'{self.MILVUS_DOC_ID_FIELD} == "{doc.doc_id}"',
                )
                with contextlib.suppress(Exception):
                    await self.index_map[index_name].adelete_ref_doc(
                        doc.doc_id, delete_from_docstore=True
                    )

                await self.add_document_to_index(index_name, doc, doc.doc_id)
                updated_docs.append(doc)

            return {
                "updated_documents": updated_docs,
                "unchanged_documents": unchanged_docs,
                "not_found_documents": not_found_docs,
            }
        except Exception as e:
            logger.error(f"Error updating documents in Milvus: {e}")
            raise HTTPException(status_code=500, detail=f"Update failed: {e}")

    async def _append_documents_to_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Append documents with dedup check via Milvus query."""
        logger.info(
            f"Index {index_name} already exists. "
            f"Appending {len(documents)} documents to existing index."
        )
        indexed_docs: list[str | None] = [None] * len(documents)

        for idx, doc in enumerate(documents):
            doc_id = self.generate_doc_id(doc.text)
            result = self.client.query(
                collection_name=index_name,
                filter=f'{self.MILVUS_DOC_ID_FIELD} == "{doc_id}"',
                output_fields=["doc_id"],
                limit=1,
            )
            if not result:
                await self.add_document_to_index(index_name, doc, doc_id)
            else:
                logger.info(
                    f"Document {doc_id} already exists in index {index_name}. Skipping."
                )
            indexed_docs[idx] = doc_id

        return indexed_docs

    async def retrieve(
        self,
        index_name: str,
        query: str,
        max_node_count: int = 5,
        metadata_filter: dict | None = None,
    ):
        """Retrieve documents from Milvus using vector similarity search."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404,
                detail=f"No such index: '{index_name}' exists.",
            )

        try:
            if not query or query.strip() == "":
                raise HTTPException(
                    status_code=400,
                    detail="Query string cannot be empty.",
                )

            top_k = min(max_node_count, RAG_MAX_TOP_K)

            retriever = self.index_map[index_name].as_retriever(
                similarity_top_k=top_k,
            )

            start_time = time.time()
            source_nodes_list = await retriever.aretrieve(query)
            elapsed = time.time() - start_time

            logger.info(
                f"Milvus retrieve for '{index_name}' completed in {elapsed:.3f}s, "
                f"returned {len(source_nodes_list)} nodes"
            )

            results = []
            scores = []
            for node in source_nodes_list:
                score = node.score if node.score is not None else 0.0
                scores.append(score)
                results.append(
                    {
                        "doc_id": getattr(node.node, "ref_doc_id", None)
                        or node.node.node_id,
                        "node_id": node.node.node_id,
                        "text": node.node.get_content(),
                        "score": score,
                        "metadata": node.node.metadata if node.node.metadata else None,
                    }
                )

            # Record metrics
            try:
                from ragengine.metrics.prometheus_metrics import (
                    rag_avg_source_score,
                    rag_lowest_source_score,
                    rag_retrieve_result_count,
                    rag_vector_store_operation_latency,
                )

                rag_retrieve_result_count.observe(len(results))
                rag_vector_store_operation_latency.labels(
                    operation="query", status="success"
                ).observe(elapsed)
                if scores:
                    rag_lowest_source_score.observe(min(scores))
                    rag_avg_source_score.observe(sum(scores) / len(scores))
            except Exception:
                pass

            return {
                "query": query,
                "results": results,
                "count": len(results),
            }

        except HTTPException:
            raise
        except Exception as e:
            import traceback

            logger.error(f"Retrieve failed for index '{index_name}': {e}")
            logger.error(f"Traceback: {traceback.format_exc()}")
            raise HTTPException(status_code=500, detail=f"Retrieve failed: {e}")
