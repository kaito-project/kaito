# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

import logging
from abc import ABC, abstractmethod
from typing import Dict, List
import hashlib
import os

from llama_index.core import Document as LlamaDocument
from llama_index.core.storage.index_store import SimpleIndexStore
from llama_index.core import (StorageContext, VectorStoreIndex)
from llama_index.core.postprocessor import LLMRerank  # Query with LLM Reranking

from ragengine.models import Document
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.inference.inference import Inference
from ragengine.config import (LLM_RERANKER_BATCH_SIZE, LLM_RERANKER_TOP_N, VECTOR_DB_PERSIST_DIR)

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class BaseVectorStore(ABC):
    def __init__(self, embedding_manager: BaseEmbeddingModel):
        self.embedding_manager = embedding_manager
        self.embed_model = self.embedding_manager.model
        self.index_map = {}
        self.index_store = SimpleIndexStore()
        self.llm = Inference()

    @staticmethod
    def generate_doc_id(text: str) -> str:
        """Generates a unique document ID based on the hash of the document text."""
        return hashlib.sha256(text.encode('utf-8')).hexdigest()

    def index_documents(self, index_name: str, documents: List[Document]) -> List[str]:
        """Common indexing logic for all vector stores."""
        if index_name in self.index_map:
            return self._append_documents_to_index(index_name, documents)
        else:
            return self._create_new_index(index_name, documents)

    def _append_documents_to_index(self, index_name: str, documents: List[Document]) -> List[str]:
        """Common logic for appending documents to existing index."""
        logger.info(f"Index {index_name} already exists. Appending documents to existing index.")
        indexed_doc_ids = set()

        for doc in documents:
            doc_id = self.generate_doc_id(doc.text)
            if not self.document_exists(index_name, doc, doc_id):
                self.add_document_to_index(index_name, doc, doc_id)
                indexed_doc_ids.add(doc_id)
            else:
                logger.info(f"Document {doc_id} already exists in index {index_name}. Skipping.")

        if indexed_doc_ids:
            self._persist(index_name)
        return list(indexed_doc_ids)
    
    @abstractmethod
    def _create_new_index(self, index_name: str, documents: List[Document]) -> List[str]:
        """Create a new index - implementation specific to each vector store."""
        pass
    
    def _create_index_common(self, index_name: str, documents: List[Document], vector_store) -> List[str]:
        """Common logic for creating a new index with documents."""
        storage_context = StorageContext.from_defaults(vector_store=vector_store)
        llama_docs = []
        indexed_doc_ids = set()

        for doc in documents:
            doc_id = self.generate_doc_id(doc.text)
            llama_doc = LlamaDocument(id_=doc_id, text=doc.text, metadata=doc.metadata)
            llama_docs.append(llama_doc)
            indexed_doc_ids.add(doc_id)

        if llama_docs:
            index = VectorStoreIndex.from_documents(
                llama_docs,
                storage_context=storage_context,
                embed_model=self.embed_model,
            )
            index.set_index_id(index_name)
            self.index_map[index_name] = index
            self.index_store.add_index_struct(index.index_struct)
            self._persist(index_name)
        return list(indexed_doc_ids)

    def query(self,
              index_name: str,
              query: str,
              top_k: int,
              llm_params: dict,
              rerank_params: dict
    ):
        """
        Query the indexed documents

        Args:
            index_name (str): Name of the index to query
            query (str): Query string
            top_k (int): Number of initial top results to retrieve
            llm_params (dict): Optional parameters for the language model
            rerank_params (dict): Optional configuration for reranking
                - 'top_n' (int): Number of top documents to return after reranking
                - 'choice_batch_size' (int):  Number of documents to process in each batch

        Returns:
            dict: A dictionary containing the response and source nodes.
        """
        if index_name not in self.index_map:
            raise ValueError(f"No such index: '{index_name}' exists.")
        self.llm.set_params(llm_params)

        node_postprocessors = []
        if rerank_params:
            # Set default reranking parameters and merge with provided params
            default_rerank_params = {
                'choice_batch_size': LLM_RERANKER_BATCH_SIZE,  # Default batch size
                'top_n': min(LLM_RERANKER_TOP_N, top_k)        # Limit top_n to top_k by default
            }
            rerank_params = {**default_rerank_params, **rerank_params}

            # Add LLMRerank to postprocessors
            node_postprocessors.append(
                LLMRerank(
                    llm=self.llm,
                    choice_batch_size=rerank_params['choice_batch_size'],
                    top_n=rerank_params['top_n']
                )
            )

        query_engine = self.index_map[index_name].as_query_engine(
            llm=self.llm,
            similarity_top_k=top_k,
            node_postprocessors=node_postprocessors
        )
        query_result = query_engine.query(query)
        return {
            "response": query_result.response,
            "source_nodes": [
                {
                    "node_id": node.node_id,
                    "text": node.text,
                    "score": node.score,
                    "metadata": node.metadata
                }
                for node in query_result.source_nodes
            ],
            "metadata": query_result.metadata,
        }

    def add_document_to_index(self, index_name: str, document: Document, doc_id: str):
        """Common logic for adding a single document."""
        if index_name not in self.index_map:
            raise ValueError(f"No such index: '{index_name}' exists.")
        llama_doc = LlamaDocument(text=document.text, metadata=document.metadata, id_=doc_id)
        self.index_map[index_name].insert(llama_doc)

    def list_all_indexed_documents(self) -> Dict[str, Dict[str, Dict[str, str]]]:
        """Common logic for listing all documents."""
        return {
            index_name: {
                doc_info.ref_doc_id: {
                    "text": doc_info.text, 
                    "hash": doc_info.hash
                } for _, doc_info in vector_store_index.docstore.docs.items()
            }
            for index_name, vector_store_index in self.index_map.items()
        }

    def document_exists(self, index_name: str, doc: Document, doc_id: str) -> bool:
        """Common logic for checking document existence."""
        if index_name not in self.index_map:
            logger.warning(f"No such index: '{index_name}' exists in vector store.")
            return False
        return doc_id in self.index_map[index_name].ref_doc_info

    def _persist_all(self):
        """Common persistence logic."""
        logger.info("Persisting all indexes.")
        self.index_store.persist(os.path.join(VECTOR_DB_PERSIST_DIR, "store.json"))
        for idx in self.index_store.index_structs():
            self._persist(idx.index_id)

    def _persist(self, index_name: str):
        """Common persistence logic for individual index."""
        try:
            logger.info(f"Persisting index {index_name}.")
            self.index_store.persist(os.path.join(VECTOR_DB_PERSIST_DIR, "store.json"))
            assert index_name in self.index_map, f"No such index: '{index_name}' exists."
            storage_context = self.index_map[index_name].storage_context
            # Persist the specific index
            storage_context.persist(persist_dir=os.path.join(VECTOR_DB_PERSIST_DIR, index_name))
            logger.info(f"Successfully persisted index {index_name}.")
        except Exception as e:
            logger.error(f"Failed to persist index {index_name}. Error: {str(e)}")
