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


from abc import ABC, abstractmethod
from typing import Any

from autoindexer.rag.rag_client import KAITORAGClient


class DataSourceError(Exception):
    """Exception raised for data source related errors."""
    pass


class DataSourceHandler(ABC):
    """Abstract base class for data source handlers."""

    @abstractmethod
    def update_index(self, index_name: str, rag_client: KAITORAGClient) -> list[str]:
        """
        Update the index with documents from the data source.
        
        Args:
            index_name: Name of the index to update
            rag_client: Instance of the KAITORAGClient to use for indexing

        Returns:
            list[str]: List of error messages, if any
        """
        pass

    @abstractmethod
    def fetch_documents(self) -> list[dict[str, Any]]:
        """
        Fetch documents from the data source.
        
        Returns:
            list[dict[str, Any]]: List of documents with 'text' and optional 'metadata'
        """
        pass