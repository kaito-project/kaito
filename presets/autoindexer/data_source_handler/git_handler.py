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
from typing import Any

from autoindexer.data_source_handler.handler import DataSourceHandler
from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient

logger = logging.getLogger(__name__)

class GitDataSourceHandler(DataSourceHandler):
    """
    Handler for Git data sources.
    
    This handler supports:
    - Cloning Git repositories
    - Checking out specific branches or commits
    - Reading files from the repository
    
    NOTE: This is a placeholder for future implementation
    """

    def __init__(self, index_name: str, config: dict[str, Any], rag_client: KAITORAGClient, autoindexer_client: AutoIndexerK8sClient, credentials: dict[str, Any] | None = None):
        """Initialize the Git data source handler."""
        self.index_name = index_name
        self.config = config
        self.credentials = credentials or {}
        self.rag_client = rag_client
        self.autoindexer_client = autoindexer_client
        raise NotImplementedError("Git data source handler is not yet implemented")

    def update_index(self) -> list[str]:
        """
        Update the index with documents from the data source.

        Returns:
            list[str]: List of error messages, if any
        """
        raise NotImplementedError("Git data source handler is not yet implemented")
    
    def update_autoindexer_status(self):
        """
        Update the AutoIndexer CRD status based on the current index state and indexing status.
        """
        raise NotImplementedError("Git data source handler is not yet implemented")