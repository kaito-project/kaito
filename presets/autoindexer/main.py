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

from autoindexer.config import ACCESS_SECRET, AUTOINDEXER_NAME, NAMESPACE
from autoindexer.data_source_handler.git_handler import GitDataSourceHandler
from autoindexer.data_source_handler.handler import DataSourceError
from autoindexer.data_source_handler.static_handler import (
    StaticDataSourceHandler,
)
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient

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
        self.access_secret = ACCESS_SECRET
        self.autoindexer_name = AUTOINDEXER_NAME
        self.namespace = NAMESPACE

        # Initialize configuration attributes with defaults
        self.index_name = None
        self.ragengine_endpoint = None
        self.datasource_type = None
        self.datasource_config = None
        self.credentials_config = None
        
        # Initialize Kubernetes client for CRD interaction
        self.k8s_client = None
        self.autoindexer_client = None  # Alias for k8s_client for compatibility
        try:
            self.k8s_client = AutoIndexerK8sClient()
            self.autoindexer_client = self.k8s_client  # Set alias
            logger.info("Kubernetes client initialized successfully")
            
            # Try to get configuration from the AutoIndexer CRD
            crd_config = self.k8s_client.get_autoindexer_config()
            if crd_config:
                logger.info("Found AutoIndexer CRD configuration, using it to supplement environment config")
                self._apply_crd_config(crd_config)
            
        except Exception as e:
            logger.warning(f"Failed to initialize Kubernetes client: {e}")
            logger.info("Continuing without Kubernetes integration")
            raise

        # Initialize RAG client (after CRD config is applied)
        if not self.ragengine_endpoint:
            raise ValueError("RAG engine endpoint must be configured via CRD or environment variables")
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
                index_name=self.index_name,
                config=self.datasource_config or {},
                rag_client=self.rag_client,
                autoindexer_client=self.autoindexer_client,
                credentials=self.access_secret
            )
        elif self.datasource_type.lower() == "git":
            return GitDataSourceHandler(
                config=self.datasource_config or {},
                credentials=self.access_secret,
                rag_client=self.rag_client,
                autoindexer_client=self.autoindexer_client
            )
        else:
            raise ValueError(f"Unsupported data source type: {self.datasource_type}")

    def _apply_crd_config(self, crd_config: dict[str, Any]):
        """Apply configuration from AutoIndexer CRD to supplement environment variables."""
        try:
            # Update index name from CRD if not set via environment
            if crd_config.get("indexName"):
                self.index_name = crd_config["indexName"]
                logger.info(f"Using index name from CRD: {self.index_name}")
            else:
                raise ValueError("indexName must be specified via autoindexer CRD")
            
            # Update RAG engine endpoint from CRD if not set via environment
            if crd_config.get("ragEngine"):
                # Construct endpoint from RAG engine name (assuming same namespace)
                rag_engine_name = crd_config["ragEngine"]
                namespace = self.k8s_client.namespace if self.k8s_client else "default"
                self.ragengine_endpoint = f"http://{rag_engine_name}.{namespace}.svc.cluster.local:80"
                logger.info(f"Using RAG engine endpoint from CRD: {self.ragengine_endpoint}")
            else:
                raise ValueError("ragEngine must be specified via autoindexer CRD")
            
            # Update data source configuration from CRD
            if crd_config.get("dataSource"):
                ds_config = crd_config["dataSource"]
                
                # Override data source type if not set via environment
                if ds_config.get("type"):
                    self.datasource_type = ds_config["type"]
                    logger.info(f"Using data source type from CRD: {self.datasource_type}")
                else:
                    raise ValueError("dataSource type must be specified via autoindexer CRD")
                
                # Merge data source configuration
                if not self.datasource_config:
                    self.datasource_config = {}
                
                # Add specific data source configurations based on type
                if ds_config.get("git") and ds_config["type"] == "Git":
                    git_config = ds_config["git"]
                    self.datasource_config.update({
                        "autoindexer_name": self.autoindexer_name,
                        "repository": git_config.get("repository"),
                        "branch": git_config.get("branch", "main"),
                        "commit": git_config.get("commit"),
                        "paths": git_config.get("paths", []),
                        "excludePaths": git_config.get("excludePaths", [])
                    })
                    logger.info("Updated Git data source configuration from CRD")
                
                elif ds_config.get("static") and ds_config["type"] == "Static":
                    static_config = ds_config["static"]
                    self.datasource_config.update({
                        "autoindexer_name": self.autoindexer_name,
                        "urls": static_config.get("urls", [])
                    })
                    logger.info("Updated Static data source configuration from CRD")
                
                else:
                    raise ValueError("Unsupported or missing data source configuration in CRD")
                
        except Exception as e:
            logger.warning(f"Failed to apply CRD configuration: {e}")
            raise

    def run(self) -> bool:
        """
        Run the indexing process.
        
        Returns:
            bool: True if successful, False otherwise
        """
        start_time = time.time()
        
        try:
            logger.info("Starting document indexing process")
            
            # Update phase and status to indicate we're starting
            self._update_indexing_phase("Running")
            self._update_status_condition("AutoIndexerIndexing", "True", "IndexingStarted", "Document indexing process has started")

            # Update the index and check for errors
            self._update_index()
            self._update_autoindexer_status()
                
        except Exception as e:
            # Calculate duration for failure case
            end_time = time.time()
            duration_seconds = int(end_time - start_time)
            
            logger.error(f"AutoIndexer service failed: {e}", exc_info=True)
            self._update_status_condition("AutoIndexerError", "True", "ServiceError", f"AutoIndexer service failed: {str(e)}")
            
            # Try to get document count even in failure case
            try:
                documents_response = self.rag_client.list_documents(self.index_name, metadata_filter={"autoindexer": self.autoindexer_name}, limit=1)
                document_count = documents_response.get("total_items", 0)
            except Exception:
                document_count = 0
                
            self._update_indexing_completion(False, duration_seconds, document_count, None)
            return False

        return True

    def _update_index(self) -> list[str]:
        """
        Update the index with documents from the data source.
        
        Returns:
            List[str]: List of error messages, if any
        """
        try:
            errors = self.data_source_handler.update_index()
            return errors
                
        except DataSourceError as e:
            error_msg = f"Data source error: {e}"
            logger.error(error_msg)
            return [error_msg]
        except Exception as e:
            error_msg = f"Unexpected error during indexing: {e}"
            logger.error(error_msg, exc_info=True)
            return [error_msg]
    
    def _update_autoindexer_status(self):
        """Update the status of the AutoIndexer CRD."""
        try:
            self.data_source_handler.update_autoindexer_status()
        except Exception as e:
            logger.error(f"Failed to update AutoIndexer status: {e}")

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

    def _update_status_condition(self, condition_type: str, status: str, reason: str, message: str):
        """Update status condition in the AutoIndexer CRD if Kubernetes client is available."""
        if self.k8s_client:
            try:
                self.k8s_client.add_status_condition(condition_type, status, reason, message)
            except Exception as e:
                logger.warning(f"Failed to update status condition: {e}")
        else:
            logger.debug(f"Status update (no K8s client): {condition_type}={status} - {reason}: {message}")

    def _update_indexing_progress(self, total_documents: int, processed_documents: int):
        """Update indexing progress in the AutoIndexer CRD if Kubernetes client is available."""
        if self.k8s_client:
            try:
                self.k8s_client.update_indexing_progress(total_documents, processed_documents)
            except Exception as e:
                logger.warning(f"Failed to update indexing progress: {e}")
        else:
            logger.debug(f"Progress update (no K8s client): {processed_documents}/{total_documents} documents processed")

    def _update_indexing_phase(self, phase: str):
        """Update indexing phase in the AutoIndexer CRD if Kubernetes client is available."""
        if self.k8s_client:
            try:
                self.k8s_client.update_indexing_phase(phase)
            except Exception as e:
                logger.warning(f"Failed to update indexing phase: {e}")
        else:
            logger.debug(f"Phase update (no K8s client): {phase}")

    def _update_indexing_completion(self, success: bool, duration_seconds: int, document_count: int, commit_hash: str | None = None):
        """Update status when indexing completes."""
        if self.k8s_client:
            try:
                self.k8s_client.update_indexing_completion(success, duration_seconds, document_count, commit_hash)
            except Exception as e:
                logger.warning(f"Failed to update indexing completion: {e}")
        else:
            status = "success" if success else "failed"
            logger.debug(f"Completion update (no K8s client): {status}, {document_count} documents, {duration_seconds}s")


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
