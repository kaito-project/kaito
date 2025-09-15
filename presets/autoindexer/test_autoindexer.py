#!/usr/bin/env python3

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

"""
Test script for the AutoIndexer service.

This script demonstrates how to test the AutoIndexer service with different configurations.
"""

import json
import os
import tempfile
from unittest.mock import MagicMock, patch

from data_sources import StaticDataSourceHandler


def test_static_data_source_with_content():
    """Test static data source with direct content."""
    print("Testing static data source with direct content...")
    
    config = {
        "static": {
            "content": [
                "This is the first document content.",
                "This is the second document content.",
            ]
        }
    }
    
    handler = StaticDataSourceHandler(config)
    documents = handler.fetch_documents()
    
    print(f"Fetched {len(documents)} documents:")
    for i, doc in enumerate(documents):
        print(f"  Document {i+1}: {doc['text'][:50]}...")
        print(f"    Metadata: {doc['metadata']}")
    
    assert len(documents) == 2
    print("✓ Direct content test passed\n")


def test_static_data_source_with_file():
    """Test static data source with file path."""
    print("Testing static data source with file...")
    
    # Create a temporary file
    with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
        f.write("This is content from a test file.")
        temp_file_path = f.name
    
    try:
        config = {
            "static": {
                "file_paths": [temp_file_path]
            }
        }
        
        handler = StaticDataSourceHandler(config)
        documents = handler.fetch_documents()
        
        print(f"Fetched {len(documents)} documents:")
        for i, doc in enumerate(documents):
            print(f"  Document {i+1}: {doc['text']}")
            print(f"    Metadata: {doc['metadata']}")
        
        assert len(documents) == 1
        assert documents[0]['text'] == "This is content from a test file."
        print("✓ File path test passed\n")
    
    finally:
        os.unlink(temp_file_path)


def test_static_data_source_with_mock_url():
    """Test static data source with mocked URL."""
    print("Testing static data source with mocked URL...")
    
    config = {
        "static": {
            "urls": ["https://example.com/document.txt"]
        }
    }
    
    # Mock the requests.get call
    with patch('data_sources.requests.get') as mock_get:
        mock_response = MagicMock()
        mock_response.text = "This is content from a URL."
        mock_response.headers = {'content-type': 'text/plain'}
        mock_response.raise_for_status = MagicMock()
        mock_get.return_value = mock_response
        
        handler = StaticDataSourceHandler(config)
        documents = handler.fetch_documents()
        
        print(f"Fetched {len(documents)} documents:")
        for i, doc in enumerate(documents):
            print(f"  Document {i+1}: {doc['text']}")
            print(f"    Metadata: {doc['metadata']}")
        
        assert len(documents) == 1
        assert documents[0]['text'] == "This is content from a URL."
        print("✓ Mocked URL test passed\n")


def test_environment_variable_parsing():
    """Test environment variable parsing simulation."""
    print("Testing environment variable parsing simulation...")
    
    # Simulate environment variables that would be set by Kubernetes
    test_env_vars = {
        "INDEX_NAME": "test-index",
        "RAGENGINE_ENDPOINT": "http://ragengine-service.default.svc.cluster.local:80",
        "DATASOURCE_TYPE": "static",
        "DATASOURCE_CONFIG": json.dumps({
            "static": {
                "content": ["Test document for indexing."]
            }
        }),
        "RETRY_POLICY": json.dumps({
            "max_attempts": 3,
            "initial_delay": 1.0
        })
    }
    
    # Mock environment variables
    with patch.dict(os.environ, test_env_vars):
        from autoindexer_service import AutoIndexerService
        
        # Mock the RAG client to avoid actual network calls
        with patch('autoindexer_service.KAITORAGClient') as mock_rag_client_class:
            mock_rag_client = MagicMock()
            mock_rag_client.list_indexes.return_value = {"indexes": []}
            mock_rag_client.index_documents.return_value = {"status": "success"}
            mock_rag_client_class.return_value = mock_rag_client
            
            # Create service and verify configuration
            service = AutoIndexerService()
            
            print(f"Index name: {service.index_name}")
            print(f"RAGEngine endpoint: {service.ragengine_endpoint}")
            print(f"Data source type: {service.datasource_type}")
            print(f"Data source config: {service.datasource_config}")
            print(f"Retry policy: {service.retry_policy}")
            
            # Test document fetching
            documents = service._fetch_documents()
            print(f"Fetched {len(documents)} documents")
            
            assert service.index_name == "test-index"
            assert service.datasource_type == "static"
            assert len(documents) == 1
            print("✓ Environment variable parsing test passed\n")


def main():
    """Run all tests."""
    print("=" * 60)
    print("KAITO AutoIndexer Service Test Suite")
    print("=" * 60)
    
    try:
        test_static_data_source_with_content()
        test_static_data_source_with_file()
        test_static_data_source_with_mock_url()
        test_environment_variable_parsing()
        
        print("=" * 60)
        print("All tests passed! ✓")
        print("=" * 60)
        
    except Exception as e:
        print(f"Test failed: {e}")
        import traceback
        traceback.print_exc()
        return False
    
    return True


if __name__ == "__main__":
    success = main()
    exit(0 if success else 1)
