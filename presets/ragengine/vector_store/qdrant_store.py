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
import time

from fastapi import HTTPException
from llama_index.core import StorageContext, VectorStoreIndex
from llama_index.vector_stores.qdrant import QdrantVectorStore
from qdrant_client import AsyncQdrantClient, QdrantClient
from qdrant_client.http import models as rest

from ragengine.config import RAG_HYBRID_SCORE_THRESHOLD, RAG_MAX_TOP_K
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

    Hybrid search: Collections are created with both dense vectors ("text-dense")
    and sparse vectors ("text-sparse" with IDF modifier) for native BM25 support.
    Sparse vectors are generated client-side by fastembed via LlamaIndex's
    QdrantVectorStore(enable_hybrid=True). Retrieval uses LlamaIndex's built-in
    hybrid query mode with Relative Score Fusion.

    Ref: https://developers.llamaindex.ai/python/framework/integrations/vector_stores/qdrant_hybrid/
    """

    _native_hybrid_search: bool = True

    # Sparse model used by fastembed for BM25 tokenization.
    FASTEMBED_SPARSE_MODEL = "Qdrant/bm25"

    # Over-fetch factor for prefetch candidates (multiplied by top_k)
    PREFETCH_MULTIPLIER = 3

    # Running counters for latency average (avoid accessing Histogram internals)
    _latency_sum: float = 0.0
    _latency_count: int = 0

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
        # Track which indexes have had their docstore rebuilt (lazy loading)
        self._docstore_ready: set[str] = set()

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

        # Note: fastembed sparse encoder is managed internally by LlamaIndex's
        # QdrantVectorStore when enable_hybrid=True is set. No manual init needed.

    def _build_vector_store(self, collection_name: str) -> QdrantVectorStore:
        """Build a QdrantVectorStore for the given collection name.

        Pre-creates the Qdrant collection with both dense and sparse (BM25)
        named vectors if it doesn't already exist. For existing collections
        that lack sparse vectors, the sparse config is added via update.

        This enables Qdrant-native hybrid search: dense vectors for semantic
        similarity + sparse vectors with IDF modifier for BM25 keyword matching.
        """
        sparse_vectors_config = {
            "text-sparse": rest.SparseVectorParams(
                modifier=rest.Modifier.IDF,
            ),
        }

        if not self.client.collection_exists(collection_name):
            self.client.create_collection(
                collection_name=collection_name,
                vectors_config={
                    "text-dense": rest.VectorParams(
                        size=self.dimension,
                        distance=rest.Distance.COSINE,
                    ),
                },
                sparse_vectors_config=sparse_vectors_config,
            )
            logger.info(
                f"Pre-created Qdrant collection '{collection_name}' "
                f"with dense vector 'text-dense' (dim={self.dimension}) "
                f"and sparse vector 'text-sparse' (BM25 + IDF)"
            )
        else:
            # Migrate existing collection: add sparse vectors if missing
            self._ensure_sparse_vectors(collection_name, sparse_vectors_config)

        return QdrantVectorStore(
            collection_name=collection_name,
            client=self.client,
            aclient=self.aclient,
            enable_hybrid=True,
            fastembed_sparse_model=self.FASTEMBED_SPARSE_MODEL,
        )

    def _ensure_sparse_vectors(
        self,
        collection_name: str,
        sparse_vectors_config: dict,
    ):
        """Add sparse vectors config to an existing collection if missing.

        This handles the migration path: collections created before hybrid
        search was enabled won't have sparse vectors. We add the config so
        that subsequent indexing operations generate BM25 sparse vectors.
        """
        try:
            collection_info = self.client.get_collection(collection_name)
            existing_sparse = collection_info.config.params.sparse_vectors
            if existing_sparse and "text-sparse" in existing_sparse:
                return  # Already has sparse vectors

            self.client.update_collection(
                collection_name=collection_name,
                sparse_vectors_config=sparse_vectors_config,
            )
            logger.info(
                f"Added sparse vectors config (BM25 + IDF) to existing "
                f"collection '{collection_name}'"
            )
        except Exception as e:
            logger.warning(
                f"Could not add sparse vectors to '{collection_name}': {e}. "
                f"Hybrid search may fall back to dense-only for this collection."
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

        Uses VectorStoreIndex.from_vector_store() to connect to the existing
        collection WITHOUT re-embedding any documents (zero GPU usage).
        Search is available immediately. The docstore is rebuilt lazily
        on first write operation (add/delete/update/list documents).

        This approach avoids both CUDA OOM (no re-embedding) and memory OOM
        (no docstore rebuild for large collections on startup).
        """
        vector_store = self._build_vector_store(collection_name)

        # Connect to existing collection (zero GPU, instant)
        index = VectorStoreIndex.from_vector_store(
            vector_store=vector_store,
            embed_model=self.embed_model,
        )
        index.set_index_id(collection_name)
        self.index_map[collection_name] = index
        logger.info(
            f"Connected index '{collection_name}' to Qdrant "
            f"(search available immediately, docstore lazy)."
        )

    def _ensure_docstore(self, index_name: str):
        """Lazily rebuild docstore for an index if not yet done.

        Called before write operations (add/delete/update/list documents)
        that need the docstore. Search/retrieve does NOT need this.
        """
        if index_name in self._docstore_ready:
            return
        if index_name not in self.index_map:
            return

        logger.info(
            f"Lazy docstore rebuild triggered for '{index_name}'..."
        )
        index = self.index_map[index_name]
        doc_count = self._rebuild_docstore_from_qdrant(index_name, index)
        self._docstore_ready.add(index_name)
        logger.info(
            f"Docstore for '{index_name}' rebuilt with "
            f"{doc_count} document(s)."
        )

    def _rebuild_docstore_from_qdrant(
        self, collection_name: str, index: VectorStoreIndex
    ) -> int:
        """Scroll Qdrant payloads and rebuild the LlamaIndex docstore.

        This is a CPU/network-only operation (zero GPU). It reads point
        payloads from Qdrant and inserts TextNode objects into the index's
        docstore so that document management operations (list, delete,
        dedup check) work correctly.

        Returns the number of unique documents restored.
        """
        from llama_index.core.schema import TextNode, RelatedNodeInfo, NodeRelationship

        docstore = index.docstore
        seen_ref_doc_ids = set()
        node_count = 0
        offset = None

        while True:
            results = self.client.scroll(
                collection_name=collection_name,
                limit=100,
                offset=offset,
                with_payload=True,
                with_vectors=False,  # Zero GPU: only read payloads
            )
            points, next_offset = results

            if not points:
                break

            for point in points:
                payload = point.payload or {}
                node_id = str(point.id)
                ref_doc_id = payload.get("ref_doc_id") or payload.get("doc_id")
                text = payload.get("text", "")
                metadata = payload.get("metadata", {}) or {}

                # _node_content is a JSON string in LlamaIndex payload
                if "_node_content" in payload:
                    import json

                    try:
                        node_content = json.loads(payload["_node_content"])
                        if not text:
                            text = node_content.get("text", "")
                        if not metadata:
                            metadata = node_content.get("metadata", {})
                        # Use the node id from _node_content if available
                        node_id = node_content.get("id_", node_id)
                    except (json.JSONDecodeError, TypeError):
                        pass

                # Build a TextNode and insert into docstore
                node = TextNode(
                    id_=node_id,
                    text=text,
                    metadata=metadata,
                )
                if ref_doc_id:
                    node.relationships[NodeRelationship.SOURCE] = RelatedNodeInfo(
                        node_id=ref_doc_id
                    )
                    seen_ref_doc_ids.add(ref_doc_id)

                docstore.add_documents([node], allow_update=True)
                node_count += 1

            if next_offset is None:
                break
            offset = next_offset

        logger.info(
            f"Docstore rebuild for '{collection_name}': "
            f"{node_count} nodes, {len(seen_ref_doc_ids)} unique documents."
        )
        return len(seen_ref_doc_ids)

    async def _create_new_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Create a new Qdrant collection and index documents into it."""
        vector_store = self._build_vector_store(index_name)
        result = await self._create_index_common(index_name, documents, vector_store)
        # New index has a fully populated docstore from from_documents()
        self._docstore_ready.add(index_name)
        return result

    async def persist(self, index_name: str, path: str):
        """No-op for Qdrant server mode — data is persisted in Qdrant itself."""
        if self.is_server_mode:
            logger.info(
                f"Skipping persist for '{index_name}': "
                "Qdrant server manages its own persistence."
            )
            return
        # In-memory mode: fall back to base class (local file persistence)
        await super().persist(index_name, path)

    async def load(self, index_name: str, path: str, overwrite: bool):
        """No-op for Qdrant server mode — indexes are restored from Qdrant on startup."""
        if self.is_server_mode:
            logger.info(
                f"Skipping load for '{index_name}': "
                "Qdrant server manages its own persistence. "
                "Indexes are restored automatically on startup."
            )
            return
        # In-memory mode: fall back to base class (local file loading)
        await super().load(index_name, path, overwrite)

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
        self._docstore_ready.discard(index_name)
        # Also delete the Qdrant collection
        try:
            self.client.delete_collection(index_name)
            logger.info(f"Deleted Qdrant collection '{index_name}'.")
        except Exception as e:
            logger.warning(
                f"Failed to delete Qdrant collection '{index_name}': {e}"
            )

    # ── Lazy docstore: override write ops that need docstore ──────────

    async def _append_documents_to_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Ensure docstore is ready before appending (dedup check needs it)."""
        self._ensure_docstore(index_name)
        return await super()._append_documents_to_index(index_name, documents)

    async def add_document_to_index(
        self, index_name: str, document: Document, doc_id: str
    ):
        """Ensure docstore is ready before adding (dedup check needs it)."""
        self._ensure_docstore(index_name)
        return await super().add_document_to_index(index_name, document, doc_id)

    async def delete_documents(self, index_name: str, doc_ids: list[str]):
        """Ensure docstore is ready before deleting (ref_doc_info lookup needs it)."""
        self._ensure_docstore(index_name)
        return await super().delete_documents(index_name, doc_ids)

    async def update_documents(self, index_name: str, documents: list[Document]):
        """Ensure docstore is ready before updating (ref_doc_info lookup needs it)."""
        self._ensure_docstore(index_name)
        return await super().update_documents(index_name, documents)

    async def list_documents_in_index(self, index_name: str, **kwargs):
        """Ensure docstore is ready before listing (docstore iteration needs it)."""
        self._ensure_docstore(index_name)
        return await super().list_documents_in_index(index_name, **kwargs)

    async def document_exists(
        self, index_name: str, doc: Document, doc_id: str
    ) -> bool:
        """Ensure docstore is ready before checking existence."""
        self._ensure_docstore(index_name)
        return await super().document_exists(index_name, doc, doc_id)

    async def shutdown(self):
        """Shutdown handler, closes the Qdrant client connection."""
        await super().shutdown()
        try:
            self.client.close()
            logger.info("Qdrant client closed.")
        except Exception as e:
            logger.warning(f"Error closing Qdrant client: {e}")

    # ── Hybrid Search via LlamaIndex ──────────────────────────────────

    async def retrieve(
        self,
        index_name: str,
        query: str,
        max_node_count: int = 5,
        metadata_filter: dict | None = None,
    ):
        """Hybrid retrieve using LlamaIndex's built-in Qdrant hybrid search.

        Uses QdrantVectorStore with enable_hybrid=True, which:
        1. Generates dense embeddings via the configured embed_model
        2. Generates sparse (BM25) embeddings via fastembed
        3. Queries Qdrant with both vector types
        4. Fuses results using Relative Score Fusion (RSF)

        See: https://developers.llamaindex.ai/python/framework/integrations/vector_stores/qdrant_hybrid/
        """
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
            sparse_top_k = top_k * self.PREFETCH_MULTIPLIER

            # Build metadata filters for LlamaIndex if provided
            filters = None
            if metadata_filter:
                from llama_index.core.vector_stores import (
                    MetadataFilter,
                    MetadataFilters,
                )
                filters = MetadataFilters(
                    filters=[
                        MetadataFilter(key=k, value=v)
                        for k, v in metadata_filter.items()
                    ]
                )

            # ── Fusion intercept: wrap _hybrid_fusion_fn to capture
            # dense and sparse results BEFORE fusion, without extra queries.
            # LlamaIndex's aquery() for HYBRID mode calls query_batch_points
            # with 2 sub-queries (dense + sparse) in ONE Qdrant request,
            # then passes both result sets through _hybrid_fusion_fn.
            # We wrap that function to capture the individual results.
            vector_store = self.index_map[index_name].vector_store
            original_fusion_fn = vector_store._hybrid_fusion_fn
            _captured_dense = None
            _captured_sparse = None

            def _instrumented_fusion(dense_result, sparse_result, **kwargs):
                nonlocal _captured_dense, _captured_sparse
                _captured_dense = dense_result
                _captured_sparse = sparse_result
                return original_fusion_fn(dense_result, sparse_result, **kwargs)

            vector_store._hybrid_fusion_fn = _instrumented_fusion

            retriever = self.index_map[index_name].as_retriever(
                similarity_top_k=top_k,
                sparse_top_k=sparse_top_k,
                vector_store_query_mode="hybrid",
                filters=filters,
            )

            start_time = time.time()
            source_nodes = await retriever.aretrieve(query)
            elapsed = time.time() - start_time

            # Restore original fusion function
            vector_store._hybrid_fusion_fn = original_fusion_fn

            # ── Build dense/sparse score lookup tables from captured results
            dense_scores = {}  # node_id -> dense score
            sparse_scores = {}  # node_id -> sparse (BM25) score
            if _captured_dense and _captured_dense.ids:
                for nid, score in zip(_captured_dense.ids, _captured_dense.similarities):
                    dense_scores[nid] = score
            if _captured_sparse and _captured_sparse.ids:
                for nid, score in zip(_captured_sparse.ids, _captured_sparse.similarities):
                    sparse_scores[nid] = score

            logger.info(
                f"Hybrid retrieve for '{index_name}' completed in {elapsed:.3f}s, "
                f"returned {len(source_nodes)} nodes "
                f"(top_k={top_k}, sparse_top_k={sparse_top_k}), "
                f"pre-fusion candidates: dense={len(dense_scores)}, sparse={len(sparse_scores)}"
            )

            # Convert LlamaIndex nodes to standard response format
            # with per-node dense/sparse source attribution
            results = []
            scores = []
            overlap_count = 0
            dense_only_count = 0
            sparse_only_count = 0
            for node in source_nodes:
                score = node.score if node.score is not None else 0.0
                scores.append(score)
                nid = node.node.node_id
                in_dense = nid in dense_scores
                in_sparse = nid in sparse_scores
                if in_dense and in_sparse:
                    source = "both"
                    overlap_count += 1
                elif in_dense:
                    source = "dense_only"
                    dense_only_count += 1
                else:
                    source = "sparse_only"
                    sparse_only_count += 1
                results.append({
                    "doc_id": getattr(node.node, "ref_doc_id", None) or node.node.node_id,
                    "node_id": nid,
                    "text": node.node.get_content(),
                    "score": score,
                    "dense_score": dense_scores.get(nid),
                    "sparse_score": sparse_scores.get(nid),
                    "source": source,
                    "metadata": node.node.metadata if node.node.metadata else None,
                })

            # ── Score threshold filtering ──
            filtered_count = 0
            if RAG_HYBRID_SCORE_THRESHOLD > 0:
                before_len = len(results)
                results = [r for r in results if r["score"] >= RAG_HYBRID_SCORE_THRESHOLD]
                scores = [r["score"] for r in results]
                filtered_count = before_len - len(results)
                if filtered_count > 0:
                    # Recount source breakdown after filtering
                    overlap_count = sum(1 for r in results if r["source"] == "both")
                    dense_only_count = sum(1 for r in results if r["source"] == "dense_only")
                    sparse_only_count = sum(1 for r in results if r["source"] == "sparse_only")
                    logger.info(
                        f"Score threshold={RAG_HYBRID_SCORE_THRESHOLD}: "
                        f"filtered out {filtered_count} low-score nodes, "
                        f"{len(results)} remaining"
                    )

            logger.info(
                f"Source breakdown: both={overlap_count}, "
                f"dense_only={dense_only_count}, sparse_only={sparse_only_count}"
            )

            # Record all hybrid search metrics in one block
            try:
                import statistics
                from ragengine.metrics.prometheus_metrics import (
                    rag_avg_source_score,
                    rag_hybrid_dense_candidates,
                    rag_hybrid_dense_only_count,
                    rag_hybrid_median_score,
                    rag_hybrid_overlap_count,
                    rag_hybrid_retrieve_latency,
                    rag_hybrid_retrieve_latency_avg,
                    rag_hybrid_score_spread,
                    rag_hybrid_search_mode_total,
                    rag_hybrid_sparse_candidates,
                    rag_hybrid_sparse_only_count,
                    rag_hybrid_filtered_count,
                    rag_hybrid_sparse_top_k,
                    rag_hybrid_top_k_requested,
                    rag_hybrid_top_score,
                    rag_lowest_source_score,
                    rag_retrieve_result_count,
                )
                # Mode & latency
                rag_hybrid_search_mode_total.labels(search_mode="hybrid").inc()
                rag_hybrid_retrieve_latency.observe(elapsed)
                # Update running average
                self.__class__._latency_sum += elapsed
                self.__class__._latency_count += 1
                rag_hybrid_retrieve_latency_avg.set(
                    self.__class__._latency_sum / self.__class__._latency_count
                )
                # Request params
                rag_hybrid_top_k_requested.observe(top_k)
                rag_hybrid_sparse_top_k.observe(sparse_top_k)
                # Result count
                rag_retrieve_result_count.observe(len(results))
                # Dense/sparse breakdown (from fusion intercept, no extra queries)
                rag_hybrid_dense_candidates.observe(len(dense_scores))
                rag_hybrid_sparse_candidates.observe(len(sparse_scores))
                rag_hybrid_overlap_count.observe(overlap_count)
                rag_hybrid_dense_only_count.observe(dense_only_count)
                rag_hybrid_sparse_only_count.observe(sparse_only_count)
                rag_hybrid_filtered_count.observe(filtered_count)
                # Score metrics
                if scores:
                    rag_lowest_source_score.observe(min(scores))
                    rag_avg_source_score.observe(sum(scores) / len(scores))
                    rag_hybrid_top_score.observe(max(scores))
                    rag_hybrid_score_spread.observe(max(scores) - min(scores))
                    if len(scores) >= 2:
                        rag_hybrid_median_score.observe(statistics.median(scores))
                    else:
                        rag_hybrid_median_score.observe(scores[0])
            except Exception as metrics_err:
                logger.warning(f"Metrics recording failed: {metrics_err}")  # Metrics should never break retrieval

            return {
                "query": query,
                "results": results,
                "count": len(results),
            }

        except HTTPException:
            raise
        except Exception as e:
            import traceback
            logger.error(f"Retrieve failed for index '{index_name}': {str(e)}")
            logger.error(f"Traceback: {traceback.format_exc()}")
            raise HTTPException(
                status_code=500, detail=f"Retrieve failed: {str(e)}"
            )
