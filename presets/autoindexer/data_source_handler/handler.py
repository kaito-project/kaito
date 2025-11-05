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

from autoindexer.k8s.k8s_client import AutoIndexerK8sClient
from autoindexer.rag.rag_client import KAITORAGClient


class DataSourceError(Exception):
    """Exception raised for data source related errors."""
    pass


class DataSourceHandler(ABC):
    """Abstract base class for data source handlers."""

    @abstractmethod
    def update_index(self) -> list[str]:
        """
        Update the index with documents from the data source.

        Returns:
            list[str]: List of error messages, if any
        """
        pass

    @abstractmethod
    def update_autoindexer_status(self):
        """
        Update the AutoIndexer CRD status based on the current index state and indexing status.
        """
        pass