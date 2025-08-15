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

import json
from typing import Any

import requests

from ragengine.metrics.helpers import record_embedding_metrics

from .base import BaseEmbeddingModel


class RemoteEmbeddingModel(BaseEmbeddingModel):
    def __init__(self, model_url: str, api_key: str, /, **data: Any):
        """
        Initialize the RemoteEmbeddingModel.

        Args:
            model_url (str): The URL of the embedding model API endpoint.
            api_key (str): The API key for accessing the API.
        """
        super().__init__(**data)
        self.model_url = model_url
        self.api_key = api_key

    @record_embedding_metrics
    def _get_text_embedding(self, text: str):
        """Returns the text embedding for a given input string."""
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }
        payload = {"inputs": text}

        try:
            response = requests.post(
                self.model_url, headers=headers, data=json.dumps(payload)
            )
            response.raise_for_status()  # Raise an HTTPError for bad responses
            embedding = response.json()  # Assumes the API returns JSON
            if isinstance(embedding, list):
                return embedding
            else:
                raise ValueError("Unexpected response format. Expected a list.")
        except requests.exceptions.RequestException as e:
            raise RuntimeError(f"Failed to get embedding from remote model: {e}")

    def _get_query_embedding(self, query: str):
        return self._get_text_embedding(query)

    def get_embedding_dimension(self) -> int:
        """Infers the embedding dimension by making a remote call to get the embedding of a dummy text."""
        dummy_input = "This is a dummy sentence."
        embedding = self._get_text_embedding(dummy_input)

        return len(embedding)
