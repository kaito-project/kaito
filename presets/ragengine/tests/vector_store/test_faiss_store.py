# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import pytest
import os

from tempfile import TemporaryDirectory
from ragengine.tests.vector_store.test_base_store import BaseVectorStoreTest
from ragengine.vector_store.faiss_store import FaissVectorStoreHandler

class TestFaissVectorStore(BaseVectorStoreTest):
    """Test implementation for FAISS vector store."""
    
    @pytest.fixture
    def vector_store_manager(self, init_embed_manager):
        with TemporaryDirectory() as temp_dir:
            print(f"Saving temporary test storage at: {temp_dir}")
            os.environ['PERSIST_DIR'] = temp_dir
            yield FaissVectorStoreHandler(init_embed_manager)

    @pytest.mark.asyncio
    async def check_indexed_documents(self, vector_store_manager):
        expected_output = {
            'index1': [
                {
                    'hash_value': '1e64a170be48c45efeaa8667ab35919106da0489ec99a11d0029f2842db133aa',
                    'text': 'First document in index1',
                    'is_truncated': False,
                    'metadata': {
                        'type': 'text',
                    },
                }
            ],
            'index2': [
                {
                    'hash_value': 'a222f875b83ce8b6eb72b3cae278b620de9bcc7c6b73222424d3ce979d1a463b',
                    'text': 'First document in index2',
                    'is_truncated': False,
                    'metadata': {
                        'type': 'text',
                    },
                }
            ]
        }

        response = await vector_store_manager.list_all_documents(
            limit=10,
            offset=0,
            max_text_length=1000
        )
        # Remove `doc_id` from response before asserting
        def remove_doc_id(data):
            return {
                index: [{k: v for k, v in doc.items() if k != 'doc_id'} for doc in docs]
                for index, docs in data.items()
            }

        assert remove_doc_id(response) == expected_output

    @property
    def expected_query_score(self):
        """Override this in implementation-specific test classes."""
        return 0.5795239210128784