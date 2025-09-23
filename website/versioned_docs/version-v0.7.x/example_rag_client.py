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

import requests


class KAITORAGClient:
    """
    A bare bones client for interacting with the RAGEngine API.
    This client provides methods to index documents, query the engine,
    update documents, delete documents, list documents, and manage indexes.
    """

    def __init__(self, base_url):
        self.base_url = base_url
        self.headers = {
            "Content-Type": "application/json",
            "Accept": "application/json",
        }

    def index_documents(self, index_name, documents):
        """
        Index documents in the RAGEngine.
        documents: list of dicts, each with 'text' and optional 'metadata'
        """
        url = f"{self.base_url}/index"
        payload = {"index_name": index_name, "documents": documents}
        resp = requests.post(url, json=payload, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def query(self, index_name, query, llm_temperature, llm_max_tokens, top_k=5):
        """
        Query the RAGEngine.
        query: str, the query text
        top_k: int, number of results to return
        metadata: optional dict, additional metadata for the query
        """
        url = f"{self.base_url}/query"
        payload = {
            "index_name": index_name,
            "query": query,
            "top_k": top_k,
            "llm_params": {
                "temperature": llm_temperature,
                "max_tokens": llm_max_tokens,
            },
        }
        resp = requests.post(url, json=payload, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def update_documents(self, index_name, documents):
        """
        Update a document in the RAGEngine.
        documents: list of dicts, each with 'id', 'text', and optional 'metadata'
        """
        url = f"{self.base_url}/indexes/{index_name}/documents"
        payload = {"documents": documents}
        resp = requests.post(url, json=payload, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def delete_documents(self, index_name, document_ids):
        """
        Delete a document from the RAGEngine by its ID.
        """
        url = f"{self.base_url}/indexes/{index_name}/documents/delete"
        payload = {"doc_ids": document_ids}
        resp = requests.post(url, json=payload, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def list_documents(self, index_name, metadata_filter, limit=10, offset=0):
        """
        List documents in the RAGEngine.
        """
        url = f"{self.base_url}/indexes/{index_name}/documents"
        params = {"limit": limit, "offset": offset}
        if metadata_filter:
            params["metadata_filter"] = json.dumps(metadata_filter)
        resp = requests.get(url, headers=self.headers, params=params)
        resp.raise_for_status()
        return resp.json()

    def list_indexes(self):
        """
        List all indexes in the RAGEngine.
        """
        url = f"{self.base_url}/indexes"
        resp = requests.get(url, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def persist_index(self, index_name, path="/tmp"):
        """
        Persist an index in the RAGEngine.
        """
        url = f"{self.base_url}/persist/{index_name}"
        params = {"path": path}
        resp = requests.post(url, params=params, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def load_index(self, index_name, path="/tmp", overwrite=True):
        """
        Load an index in the RAGEngine.
        """
        url = f"{self.base_url}/load/{index_name}"
        params = {"path": path, "overwrite": overwrite}
        resp = requests.post(url, params=params, headers=self.headers)
        resp.raise_for_status()
        return resp.json()

    def delete_index(self, index_name):
        """
        Delete an index from the RAGEngine.
        """
        url = f"{self.base_url}/indexes/{index_name}"
        resp = requests.delete(url, headers=self.headers)
        resp.raise_for_status()
        return resp.json()
