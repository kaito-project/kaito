
# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import pytest
import os

from tempfile import TemporaryDirectory
from ragengine.tests.vector_store.test_base_store import BaseVectorStoreTest
from ragengine.vector_store.chromadb_store import ChromaDBVectorStoreHandler

class TestChromaDBVectorStore(BaseVectorStoreTest):
    """Test implementation for ChromaDB vector store."""
    
    @pytest.fixture
    def vector_store_manager(self, init_embed_manager):
        with TemporaryDirectory() as temp_dir:
            print(f"Saving temporary test storage at: {temp_dir}")
            os.environ['PERSIST_DIR'] = temp_dir
            manager = ChromaDBVectorStoreHandler(init_embed_manager)
            manager._clear_collection_and_indexes()
            yield manager

    @pytest.mark.asyncio
    async def check_indexed_documents(self, vector_store_manager):
        indexed_docs = await vector_store_manager.list_all_documents()
        assert len(indexed_docs) == 2
        assert list(indexed_docs["index1"].values())[0]["text"] == "First document in index1"
        assert list(indexed_docs["index2"].values())[0]["text"] == "First document in index2"

    @property
    def expected_query_score(self):
        """Override this in implementation-specific test classes."""
        return 0.5601649858735368
