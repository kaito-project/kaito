# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from abc import ABC, abstractmethod


class BaseEmbeddingModel(ABC):
    @abstractmethod
    def get_text_embedding(self, text: str):
        """Returns the text embedding for a given input string."""
        pass
    
    @abstractmethod
    def get_embedding_dimension(self) -> int:
        """Returns the embedding dimension for the model."""
        pass