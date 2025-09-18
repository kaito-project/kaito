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
AutoIndexer Service

This service handles document indexing from various data sources into KAITO RAG engines.
It supports static file data sources and uses the KAITO RAG Client for document indexing.
"""

import argparse
import json
import logging
import os
import sys
import time
from typing import Any

import requests

from data_sources import DataSourceError, StaticDataSourceHandler
from rag_client import KAITORAGClient


# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class AutoIndexerService:
    """
    Main AutoIndexer service that coordinates document indexing from data sources to RAG engines.
    """

    def __init__(self, dry_run: bool = False):
        """Initialize the AutoIndexer service with environment variables."""
        self.dry_run = dry_run
        self.index_name = self._get_required_env("INDEX_NAME")
        self.ragengine_endpoint = self._get_required_env("RAGENGINE_ENDPOINT")
        self.datasource_type = self._get_required_env("DATASOURCE_TYPE")
        self.datasource_config = self._get_optional_env_json("DATASOURCE_CONFIG")
        self.credentials_config = self._get_optional_env_json("CREDENTIALS_CONFIG")
        self.retry_policy = self._get_optional_env_json("RETRY_POLICY")
        
        # Initialize RAG client
        self.rag_client = KAITORAGClient(self.ragengine_endpoint)
        
        # Initialize data source handler
        self.data_source_handler = self._create_data_source_handler()
        
        logger.info(f"AutoIndexer initialized for index '{self.index_name}' with data source type '{self.datasource_type}'")

    def _get_required_env(self, key: str) -> str:
        """Get a required environment variable."""
        value = os.getenv(key)
        if not value:
            raise ValueError(f"Required environment variable {key} is not set")
        return value

    def _get_optional_env_json(self, key: str) -> dict[str, Any] | None:
        """Get an optional environment variable and parse as JSON."""
        value = os.getenv(key)
        if not value:
            return None
        try:
            return json.loads(value)
        except json.JSONDecodeError as e:
            logger.warning(f"Failed to parse {key} as JSON: {e}")
            return None

    def _create_data_source_handler(self):
        """Create the appropriate data source handler based on configuration."""
        if self.datasource_type.lower() == "static":
            return StaticDataSourceHandler(
                config=self.datasource_config or {},
                credentials=self.credentials_config
            )
        else:
            raise ValueError(f"Unsupported data source type: {self.datasource_type}")

    def run(self) -> bool:
        """
        Run the indexing process.
        
        Returns:
            bool: True if successful, False otherwise
        """
        try:
            logger.info("Starting document indexing process")
            
            # Fetch documents from data source
            logger.info("Fetching documents from data source")
            documents = self._fetch_documents()
            
            if not documents:
                logger.warning("No documents found in data source")
                return True  # Not necessarily an error
            
            logger.info(f"Found {len(documents)} documents to index")
            
            # Index documents in RAG engine
            logger.info("Indexing documents in RAG engine")
            if self.dry_run:
                logger.info("Dry-run mode enabled, skipping actual indexing")
                for doc in documents:
                    logger.debug(f"Document to index: {doc}")
                return True

            success = self._index_documents(documents)
            
            if success:
                logger.info("Document indexing completed successfully")
                return True
            else:
                logger.error("Document indexing failed")
                return False
                
        except Exception as e:
            logger.error(f"AutoIndexer service failed: {e}", exc_info=True)
            return False

    def _fetch_documents(self) -> list[dict[str, Any]]:
        """
        Fetch documents from the configured data source.
        
        Returns:
            List[Dict[str, Any]]: List of documents with 'text' and optional 'metadata'
        """
        try:
            return self.data_source_handler.fetch_documents()
        except DataSourceError as e:
            logger.error(f"Failed to fetch documents: {e}")
            raise

    def _index_documents(self, documents: list[dict[str, Any]]) -> bool:
        """
        Index documents in the RAG engine with retry logic.
        
        Args:
            documents: List of documents to index
            
        Returns:
            bool: True if successful, False otherwise
        """
        max_retries = 3
        retry_delay = 1.0
        
        if self.retry_policy:
            max_retries = self.retry_policy.get("max_attempts", max_retries)
            retry_delay = self.retry_policy.get("initial_delay", retry_delay)
        
        for attempt in range(max_retries):
            try:
                logger.info(f"Indexing attempt {attempt + 1}/{max_retries}")
                
                # Check if index exists, create if needed
                self._ensure_index_exists()
                
                # Index documents in batches
                batch_size = 100  # Configurable batch size
                for i in range(0, len(documents), batch_size):
                    batch = documents[i:i + batch_size]
                    logger.info(f"Indexing batch {i//batch_size + 1} ({len(batch)} documents)")
                    
                    response = self.rag_client.index_documents(self.index_name, batch)
                    logger.debug(f"Batch indexing response: {response}")
                
                logger.info("All documents indexed successfully")
                return True
                
            except requests.RequestException as e:
                logger.warning(f"Network error on attempt {attempt + 1}: {e}")
                if attempt < max_retries - 1:
                    logger.info(f"Retrying in {retry_delay} seconds...")
                    time.sleep(retry_delay)
                    retry_delay *= 2  # Exponential backoff
                else:
                    logger.error(f"Failed to index documents after {max_retries} attempts")
                    return False
                    
            except Exception as e:
                logger.error(f"Unexpected error during indexing: {e}")
                return False
        
        return False

    def _ensure_index_exists(self):
        """Ensure the target index exists in the RAG engine."""
        try:
            # List indexes to check if our index exists
            indexes_response = self.rag_client.list_indexes()
            existing_indexes = [idx.get("name", "") for idx in indexes_response.get("indexes", [])]
            
            if self.index_name not in existing_indexes:
                logger.info(f"Index '{self.index_name}' does not exist, it will be created automatically during first indexing")
            else:
                logger.info(f"Index '{self.index_name}' already exists")
                
        except Exception as e:
            # If we can't list indexes, assume the index will be created automatically
            logger.warning(f"Could not verify index existence: {e}")


def main():
    """Main entry point for the AutoIndexer service."""
    parser = argparse.ArgumentParser(description="KAITO AutoIndexer Service")
    parser.add_argument(
        "--mode",
        choices=["index"],
        default="index",
        help="Operation mode (currently only 'index' is supported)"
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Perform a dry run without actually indexing documents"
    )
    parser.add_argument(
        "--log-level",
        choices=["DEBUG", "INFO", "WARNING", "ERROR"],
        default="INFO",
        help="Set the logging level"
    )
    
    args = parser.parse_args()
    
    # Set logging level
    logging.getLogger().setLevel(getattr(logging, args.log_level))
    
    try:
        service = AutoIndexerService(dry_run=args.dry_run)
        success = service.run()
        
        if success:
            logger.info("AutoIndexer service completed successfully")
            sys.exit(0)
        else:
            logger.error("AutoIndexer service failed")
            sys.exit(1)
            
    except Exception as e:
        logger.error(f"Failed to initialize AutoIndexer service: {e}", exc_info=True)
        sys.exit(1)


if __name__ == "__main__":
    main()
