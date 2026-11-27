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

import math
import re
import zlib
from tempfile import TemporaryDirectory
from unittest.mock import patch

import pytest
from llama_index.core import VectorStoreIndex
from llama_index.core.schema import TextNode

from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.models import Document
from ragengine.vector_store.faiss_store import FaissVectorStoreHandler
from ragengine.vector_store.retriever.hybrid_retriever import HybridRetriever

CATS = [f"The cat number {i} sat on the mat." for i in range(12)]
REVENUE = "Quarterly revenue grew by twelve percent."


class BagOfWordsEmbedding(BaseEmbeddingModel):
    """Deterministic embedding: normalized word counts hashed into buckets."""

    def get_embedding_dimension(self) -> int:
        return 64

    def _get_text_embedding(self, text: str) -> list[float]:
        vector = [0.0] * self.get_embedding_dimension()
        for word in re.findall(r"\w+", text.lower()):
            vector[zlib.crc32(word.encode()) % len(vector)] += 1.0
        norm = math.sqrt(sum(v * v for v in vector)) or 1.0
        return [v / norm for v in vector]

    def _get_query_embedding(self, query: str) -> list[float]:
        return self._get_text_embedding(query)


async def faiss_store_with_docs() -> FaissVectorStoreHandler:
    store = FaissVectorStoreHandler(BagOfWordsEmbedding())
    docs = [Document(text=text, metadata={"topic": "cats"}) for text in CATS]
    docs.append(Document(text=REVENUE, metadata={"topic": "finance"}))
    await store.index_documents("filter_index", docs)
    return store


@pytest.mark.asyncio
async def test_retrieve_with_metadata_filter_on_faiss():
    """FAISS cannot filter by metadata itself; /retrieve still applies the filter."""
    store = await faiss_store_with_docs()

    result = await store.retrieve(
        index_name="filter_index",
        query=CATS[3],
        max_node_count=3,
        metadata_filter={"topic": "cats"},
    )

    # Which cats come first is the ranking's business (#2533); here, only
    # that the filter is applied and the request succeeds.
    assert len(result["results"]) == 3
    assert all(r["metadata"]["topic"] == "cats" for r in result["results"])


@pytest.mark.asyncio
async def test_selective_metadata_filter_on_faiss_finds_nodes_beyond_the_pool():
    """The only matching node is the one least like the query: it is still found."""
    store = await faiss_store_with_docs()

    result = await store.retrieve(
        index_name="filter_index",
        query=CATS[0],
        max_node_count=2,
        metadata_filter={"topic": "finance"},
    )
    assert [r["text"] for r in result["results"]] == [REVENUE]

    # The same without BM25, on the vector-only path.
    with patch.object(HybridRetriever, "_build_bm25_retriever", return_value=None):
        result = await store.retrieve(
            index_name="filter_index",
            query=CATS[0],
            max_node_count=2,
            metadata_filter={"topic": "finance"},
        )
    assert [r["text"] for r in result["results"]] == [REVENUE]


@pytest.mark.asyncio
async def test_metadata_filter_on_loaded_json_index():
    """An index loaded from llama-index's JSON format filters in its own store."""
    embed_model = BagOfWordsEmbedding()
    nodes = [TextNode(text=text, metadata={"topic": "cats"}) for text in CATS]
    nodes.append(TextNode(text=REVENUE, metadata={"topic": "finance"}))
    index = VectorStoreIndex(nodes=nodes, embed_model=embed_model)
    store = FaissVectorStoreHandler(embed_model)
    with TemporaryDirectory() as path:
        index.storage_context.persist(persist_dir=path)
        await store.load("loaded_index", path, overwrite=False)

    result = await store.retrieve(
        index_name="loaded_index",
        query=CATS[0],
        max_node_count=2,
        metadata_filter={"topic": "finance"},
    )
    assert [r["text"] for r in result["results"]] == [REVENUE]
