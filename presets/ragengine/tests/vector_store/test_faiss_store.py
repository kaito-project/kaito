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

    def check_indexed_documents(self, vector_store_manager):
        expected_output = {
            'index1': {"87117028123498eb7d757b1507aa3e840c63294f94c27cb5ec83c939dedb32fd": {
                'hash': '1e64a170be48c45efeaa8667ab35919106da0489ec99a11d0029f2842db133aa',
                'text': 'First document in index1'
            }},
            'index2': {"49b198c0e126a99e1975f17b564756c25b4ad691a57eda583e232fd9bee6de91": {
                'hash': 'a222f875b83ce8b6eb72b3cae278b620de9bcc7c6b73222424d3ce979d1a463b',
                'text': 'First document in index2'
            }}
        }
        assert vector_store_manager.list_all_indexed_documents() == expected_output

    @property
    def expected_query_score(self):
        """Override this in implementation-specific test classes."""
        return 0.5795239210128784