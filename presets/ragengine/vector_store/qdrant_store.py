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

"""Qdrant vector store handler following the official LlamaIndex demo.

Ref: https://developers.llamaindex.ai/python/framework/integrations/vector_stores/qdrantindexdemo/
"""

import json
import logging
import time
from typing import Any

from fastapi import HTTPException
from llama_index.core import Document as LlamaDocument
from llama_index.core import StorageContext, VectorStoreIndex
from llama_index.vector_stores.qdrant import QdrantVectorStore
from qdrant_client import AsyncQdrantClient, QdrantClient
from qdrant_client.http.models import (
    FieldCondition,
    Filter,
    FilterSelector,
    MatchValue,
)

from ragengine.config import RAG_MAX_TOP_K
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.models import (
    Document,
    ListDocumentsResponse,
)

from .base import BaseVectorStore

# Qdrant payload key used by LlamaIndex to store the parent document ID.
QDRANT_DOC_ID_KEY = "doc_id"

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class QdrantVectorStoreHandler(BaseVectorStore):
    _native_hybrid_search: bool = True
    _use_async_indexing: bool = False

    def __init__(
        self,
        embed_model: BaseEmbeddingModel,
        vector_db_url: str | None = None,
        vector_db_access_secret: str | None = None,
    ):
        super().__init__(embed_model, use_rwlock=False)

        if vector_db_url is not None:
            self.client = QdrantClient(
                url=vector_db_url, api_key=vector_db_access_secret
            )
            self.aclient = AsyncQdrantClient(
                url=vector_db_url, api_key=vector_db_access_secret
            )
            self._restore_indexes_from_qdrant()
        else:
            self.client = QdrantClient(":memory:")
            self.aclient = AsyncQdrantClient(location=":memory:")
            self.aclient._client.collections = self.client._client.collections

    def _build_vector_store(self, collection_name: str) -> QdrantVectorStore:
        return QdrantVectorStore(
            client=self.client,
            aclient=self.aclient,
            collection_name=collection_name,
            enable_hybrid=True,
            fastembed_sparse_model="Qdrant/bm25",
        )

    def _restore_indexes_from_qdrant(self):
        """Restore indexes from existing Qdrant collections on startup.

        Uses the official LlamaIndex pattern: VectorStoreIndex.from_vector_store()
        Ref: https://developers.llamaindex.ai/python/framework/integrations/vector_stores/qdrantindexdemo/#saving-and-loading
        """
        try:
            collections = self.client.get_collections().collections
            for collection in collections:
                name = collection.name
                try:
                    vector_store = self._build_vector_store(name)
                    index = VectorStoreIndex.from_vector_store(
                        vector_store,
                        embed_model=self.embed_model,
                    )
                    index.set_index_id(name)
                    self.index_map[name] = index
                    logger.info(f"Restored index '{name}' from Qdrant.")
                except Exception as e:
                    logger.error(f"Failed to restore index '{name}': {e}")
        except Exception as e:
            logger.error(f"Failed to list Qdrant collections: {e}")

    async def _create_new_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        vector_store = self._build_vector_store(index_name)
        return await self._create_index_common(index_name, documents, vector_store)

    def _create_storage_context_for_load(
        self, index_name: str, path: str
    ) -> StorageContext:
        vector_store = self._build_vector_store(index_name)
        return StorageContext.from_defaults(persist_dir=path, vector_store=vector_store)

    async def retrieve(
        self,
        index_name: str,
        query: str,
        max_node_count: int = 5,
        metadata_filter: dict | None = None,
    ):
        """Hybrid retrieve with dense/sparse score breakdown.

        Intercepts the _hybrid_fusion_fn to capture individual dense and sparse
        scores before RSF fusion, then includes them in the response.
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

            # Intercept _hybrid_fusion_fn to capture dense/sparse results
            # before RSF fusion, without extra queries.
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
                vector_store_query_mode="hybrid",
            )

            start_time = time.time()
            source_nodes = await retriever.aretrieve(query)
            elapsed = time.time() - start_time

            # Restore original fusion function
            vector_store._hybrid_fusion_fn = original_fusion_fn

            # Build dense/sparse score lookup tables from captured results
            dense_scores: dict[str, float] = {}
            sparse_scores: dict[str, float] = {}
            if _captured_dense and _captured_dense.ids:
                for nid, score in zip(
                    _captured_dense.ids, _captured_dense.similarities
                ):
                    dense_scores[nid] = score
            if _captured_sparse and _captured_sparse.ids:
                for nid, score in zip(
                    _captured_sparse.ids, _captured_sparse.similarities
                ):
                    sparse_scores[nid] = score

            logger.info(
                f"Hybrid retrieve for '{index_name}' completed in {elapsed:.3f}s, "
                f"returned {len(source_nodes)} nodes "
                f"(pre-fusion: dense={len(dense_scores)}, sparse={len(sparse_scores)})"
            )

            results = []
            scores = []
            for node in source_nodes:
                score = node.score if node.score is not None else 0.0
                scores.append(score)
                nid = node.node.node_id
                in_dense = nid in dense_scores
                in_sparse = nid in sparse_scores
                if in_dense and in_sparse:
                    source = "both"
                elif in_dense:
                    source = "dense_only"
                else:
                    source = "sparse_only"
                results.append(
                    {
                        "doc_id": getattr(node.node, "ref_doc_id", None)
                        or node.node.node_id,
                        "node_id": nid,
                        "text": node.node.get_content(),
                        "score": score,
                        "dense_score": dense_scores.get(nid),
                        "sparse_score": sparse_scores.get(nid),
                        "source": source,
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

    # ------------------------------------------------------------------
    # Qdrant-native overrides: bypass empty docstore after restore.
    #
    # After a pod restart, _restore_indexes_from_qdrant() rebuilds the
    # index_map via VectorStoreIndex.from_vector_store(), which creates
    # an *empty* docstore.  All base-class methods that read docstore or
    # ref_doc_info would return nothing.  The overrides below go directly
    # to Qdrant via self.aclient so they work regardless of docstore state.
    # ------------------------------------------------------------------

    def _get_collection_name(self, index_name: str) -> str:
        """Return the Qdrant collection name for the given index."""
        vs = self.index_map[index_name]
        return vs.vector_store.collection_name

    @staticmethod
    def _build_metadata_filter(
        metadata_filter: dict[str, Any] | None,
    ) -> Filter | None:
        """Build a Qdrant Filter from a simple {key: value} metadata dict."""
        if not metadata_filter:
            return None
        return Filter(
            must=[
                FieldCondition(key=k, match=MatchValue(value=v))
                for k, v in metadata_filter.items()
            ]
        )

    @staticmethod
    def _record_to_doc_dict(
        record, max_text_length: int | None = None
    ) -> dict[str, Any]:
        """Convert a Qdrant scroll Record to the dict format expected by
        ListDocumentsResponse."""
        payload = record.payload or {}
        # LlamaIndex stores text inside _node_content JSON blob as well as
        # a top-level 'text' key (when stores_text=True on the vector store).
        text = payload.get("text", "")
        if not text:
            try:
                nc = json.loads(payload.get("_node_content", "{}"))
                text = nc.get("text", "")
            except (json.JSONDecodeError, TypeError):
                text = ""

        is_truncated = bool(max_text_length and len(text) > max_text_length)
        truncated = text[:max_text_length] if is_truncated else text

        # Extract user metadata — everything except LlamaIndex internal keys.
        _internal_keys = {
            "_node_content",
            "_node_type",
            "doc_id",
            "document_id",
            "ref_doc_id",
            "text",
        }
        metadata = {k: v for k, v in payload.items() if k not in _internal_keys}

        # Compute hash by reconstructing a LlamaDocument from text + metadata.
        # This matches the hash that LlamaIndex stores in the docstore.
        try:
            hash_value = LlamaDocument(text=text, metadata=metadata).hash
        except Exception:
            hash_value = None

        return {
            "doc_id": payload.get(QDRANT_DOC_ID_KEY, str(record.id)),
            "text": truncated,
            "hash_value": hash_value,
            "metadata": metadata,
            "is_truncated": is_truncated,
        }

    # --- list ---

    async def _list_documents_in_index(
        self,
        index_name: str,
        limit: int,
        offset: int,
        max_text_length: int | None = None,
        metadata_filter: dict[str, Any] | None = None,
    ) -> ListDocumentsResponse:
        """List documents directly from Qdrant via scroll, bypassing docstore."""
        vector_store_index = self.index_map.get(index_name)
        if not vector_store_index:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        collection = self._get_collection_name(index_name)
        qdrant_filter = self._build_metadata_filter(metadata_filter)

        # Get total count (with filter if applicable).
        count_result = await self.aclient.count(
            collection_name=collection,
            count_filter=qdrant_filter,
            exact=True,
        )
        total_count = count_result.count

        # Scroll with offset/limit.  Qdrant scroll offset is a point ID,
        # not a numeric offset, so we need to skip `offset` records.
        # For simplicity we fetch offset+limit and slice in Python.
        records, _next = await self.aclient.scroll(
            collection_name=collection,
            limit=offset + limit,
            scroll_filter=qdrant_filter,
            with_payload=True,
            with_vectors=False,
        )
        page = records[offset:]

        docs = [self._record_to_doc_dict(r, max_text_length) for r in page]
        return ListDocumentsResponse(
            documents=docs, count=len(docs), total_items=total_count
        )

    # --- document_exists ---

    async def document_exists(
        self, index_name: str, doc: Document, doc_id: str
    ) -> bool:
        """Check whether a doc_id exists in Qdrant (bypasses empty docstore)."""
        if index_name not in self.index_map:
            logger.warning(f"No such index: '{index_name}' exists in vector store.")
            return False

        collection = self._get_collection_name(index_name)
        count_result = await self.aclient.count(
            collection_name=collection,
            count_filter=Filter(
                must=[
                    FieldCondition(
                        key=QDRANT_DOC_ID_KEY, match=MatchValue(value=doc_id)
                    )
                ]
            ),
            exact=True,
        )
        return count_result.count > 0

    # --- delete ---

    async def delete_documents(self, index_name: str, doc_ids: list[str]):
        """Delete documents directly from Qdrant by doc_id payload filter."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        collection = self._get_collection_name(index_name)

        # Determine which doc_ids actually exist.
        found, not_found = [], []
        for doc_id in doc_ids:
            cnt = await self.aclient.count(
                collection_name=collection,
                count_filter=Filter(
                    must=[
                        FieldCondition(
                            key=QDRANT_DOC_ID_KEY, match=MatchValue(value=doc_id)
                        )
                    ]
                ),
                exact=True,
            )
            if cnt.count > 0:
                found.append(doc_id)
            else:
                not_found.append(doc_id)

        op_start = time.time()
        op_status = "success"
        try:
            for doc_id in found:
                await self.aclient.delete(
                    collection_name=collection,
                    points_selector=FilterSelector(
                        filter=Filter(
                            must=[
                                FieldCondition(
                                    key=QDRANT_DOC_ID_KEY,
                                    match=MatchValue(value=doc_id),
                                )
                            ]
                        )
                    ),
                )
            return {"deleted_doc_ids": found, "not_found_doc_ids": not_found}
        except Exception as e:
            op_status = "error"
            logger.error(f"Error deleting documents from Qdrant: {e}")
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

    # --- update ---

    async def update_documents(self, index_name: str, documents: list[Document]):
        """Update documents in Qdrant: delete old points then re-insert."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        collection = self._get_collection_name(index_name)

        found_docs: list[Document] = []
        not_found_docs: list[Document] = []
        for doc in documents:
            cnt = await self.aclient.count(
                collection_name=collection,
                count_filter=Filter(
                    must=[
                        FieldCondition(
                            key=QDRANT_DOC_ID_KEY,
                            match=MatchValue(value=doc.doc_id),
                        )
                    ]
                ),
                exact=True,
            )
            if cnt.count > 0:
                found_docs.append(doc)
            else:
                not_found_docs.append(doc)

        updated_docs: list[Document] = []
        unchanged_docs: list[Document] = []

        try:
            for doc in found_docs:
                # Delete old points for this doc_id, then re-insert.
                await self.aclient.delete(
                    collection_name=collection,
                    points_selector=FilterSelector(
                        filter=Filter(
                            must=[
                                FieldCondition(
                                    key=QDRANT_DOC_ID_KEY,
                                    match=MatchValue(value=doc.doc_id),
                                )
                            ]
                        )
                    ),
                )
                # Insert via VectorStoreIndex so embeddings are computed.
                await self.add_document_to_index(index_name, doc, doc.doc_id)
                updated_docs.append(doc)
        except Exception as e:
            logger.error(f"Error updating documents in Qdrant: {e}")
            raise HTTPException(status_code=500, detail=f"Update failed: {e}")

        return {
            "updated_documents": updated_docs,
            "unchanged_documents": unchanged_docs,
            "not_found_documents": not_found_docs,
        }

    # --- append (dedup check via Qdrant instead of docstore) ---

    async def _append_documents_to_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Append documents, checking for duplicates via Qdrant count instead
        of docstore (which is empty after restore).

        Inserts are sequential to avoid race conditions in Qdrant's in-memory
        client (used in tests) and to keep the vector index consistent.
        """
        logger.info(
            f"Index {index_name} already exists. "
            f"Appending {len(documents)} documents to existing index."
        )
        collection = self._get_collection_name(index_name)
        indexed_docs: list[str | None] = [None] * len(documents)

        for idx, doc in enumerate(documents):
            doc_id = self.generate_doc_id(doc.text)
            cnt = await self.aclient.count(
                collection_name=collection,
                count_filter=Filter(
                    must=[
                        FieldCondition(
                            key=QDRANT_DOC_ID_KEY, match=MatchValue(value=doc_id)
                        )
                    ]
                ),
                exact=True,
            )
            if cnt.count == 0:
                await self.add_document_to_index(index_name, doc, doc_id)
            else:
                logger.info(
                    f"Document {doc_id} already exists in index {index_name}. Skipping."
                )
            indexed_docs[idx] = doc_id

        return indexed_docs
