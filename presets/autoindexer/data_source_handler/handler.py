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