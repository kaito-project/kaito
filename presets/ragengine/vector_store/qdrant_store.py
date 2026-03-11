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

import logging
import time

from fastapi import HTTPException
from llama_index.core import StorageContext, VectorStoreIndex
from llama_index.vector_stores.qdrant import QdrantVectorStore
from qdrant_client import AsyncQdrantClient, QdrantClient

from ragengine.config import RAG_MAX_TOP_K
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.models import Document

from .base import BaseVectorStore

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
