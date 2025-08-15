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


import asyncio
import hashlib
import logging
import os
import time
import uuid
from abc import ABC, abstractmethod
from itertools import islice
from typing import Any

import aiorwlock
from fastapi import HTTPException
from llama_index.core import Document as LlamaDocument
from llama_index.core import StorageContext, VectorStoreIndex, load_index_from_storage
from llama_index.core.chat_engine.types import ChatMode
from llama_index.core.postprocessor import LLMRerank  # Query with LLM Reranking
from llama_index.core.storage.docstore import SimpleDocumentStore
from llama_index.vector_stores.faiss import FaissMapVectorStore
from openai.types.chat import ChatCompletionContentPartTextParam, CompletionCreateParams
from pydantic import ValidationError

from ragengine.config import LLM_RERANKER_BATCH_SIZE, LLM_RERANKER_TOP_N
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.inference.inference import Inference
from ragengine.models import ChatCompletionResponse, Document, messages_to_prompt
from ragengine.vector_store.transformers.custom_transformer import CustomTransformer

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class BaseVectorStore(ABC):
    def __init__(self, embed_model: BaseEmbeddingModel, use_rwlock: bool = False):
        super().__init__()
        self.llm = Inference()
        self.embed_model = embed_model
        self.index_map = {}
        # Use a reader/writer lock only if needed
        self.use_rwlock = use_rwlock
        self.rwlock = aiorwlock.RWLock() if self.use_rwlock else None
        self.custom_transformer = CustomTransformer()

    @staticmethod
    def generate_doc_id(text: str) -> str:
        """Generates a unique document ID based on the hash of the document text."""
        return hashlib.sha256(text.encode("utf-8")).hexdigest()

    async def shutdown(self):
        await self.llm.aclose()

    async def index_documents(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Common indexing logic for all vector stores."""
        if index_name in self.index_map:
            return await self._append_documents_to_index(index_name, documents)
        else:
            return await self._create_new_index(index_name, documents)

    async def _append_documents_to_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Common logic for appending documents to existing index."""
        logger.info(
            f"Index {index_name} already exists. Appending documents to existing index."
        )
        indexed_docs = [None] * len(documents)

        async def handle_document(doc_index: int, doc: Document):
            doc_id = self.generate_doc_id(doc.text)
            if self.use_rwlock:
                async with self.rwlock.reader_lock:
                    retrieved_doc = await self.index_map[
                        index_name
                    ].docstore.aget_ref_doc_info(doc_id)
            else:
                retrieved_doc = await self.index_map[
                    index_name
                ].docstore.aget_ref_doc_info(doc_id)
            if not retrieved_doc:
                await self.add_document_to_index(index_name, doc, doc_id)
            else:
                logger.info(
                    f"Document {doc_id} already exists in index {index_name}. Skipping."
                )
            indexed_docs[doc_index] = doc_id

        # Gather all coroutines for processing documents
        await asyncio.gather(
            *(handle_document(idx, doc) for idx, doc in enumerate(documents))
        )
        return indexed_docs

    @abstractmethod
    async def _create_new_index(
        self, index_name: str, documents: list[Document]
    ) -> list[str]:
        """Create a new index - implementation specific to each vector store."""
        pass

    async def _create_index_common(
        self, index_name: str, documents: list[Document], vector_store
    ) -> list[str]:
        """Common logic for creating a new index with documents."""
        storage_context = StorageContext.from_defaults(vector_store=vector_store)
        llama_docs = []
        indexed_doc_ids = [None] * len(documents)

        for idx, doc in enumerate(documents):
            doc_id = self.generate_doc_id(doc.text)
            llama_doc = LlamaDocument(id_=doc_id, text=doc.text, metadata=doc.metadata)
            llama_docs.append(llama_doc)
            indexed_doc_ids[idx] = doc_id

        if llama_docs:
            if self.use_rwlock:
                async with self.rwlock.writer_lock:
                    index = await asyncio.to_thread(
                        VectorStoreIndex.from_documents,
                        llama_docs,
                        storage_context=storage_context,
                        embed_model=self.embed_model,
                        use_async=True,
                        transformations=[self.custom_transformer],
                    )
                    index.set_index_id(index_name)
                    self.index_map[index_name] = index
            else:
                index = await asyncio.to_thread(
                    VectorStoreIndex.from_documents,
                    llama_docs,
                    storage_context=storage_context,
                    embed_model=self.embed_model,
                    use_async=True,
                    transformations=[self.custom_transformer],
                )
                index.set_index_id(index_name)
                self.index_map[index_name] = index
        return indexed_doc_ids

    async def query(
        self,
        index_name: str,
        query: str,
        top_k: int,
        llm_params: dict,
        rerank_params: dict,
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
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        node_postprocessors = []
        if rerank_params:
            # Set default reranking parameters and merge with provided params
            default_rerank_params = {
                "choice_batch_size": LLM_RERANKER_BATCH_SIZE,  # Default batch size
                "top_n": min(
                    LLM_RERANKER_TOP_N, top_k
                ),  # Limit top_n to top_k by default
            }
            rerank_params = {**default_rerank_params, **rerank_params}

            # Add LLMRerank to postprocessors
            node_postprocessors.append(
                LLMRerank(
                    llm=self.llm,
                    choice_batch_size=rerank_params["choice_batch_size"],
                    top_n=rerank_params["top_n"],
                )
            )

        query_engine = self.index_map[index_name].as_chat_engine(
            llm=self.llm,
            similarity_top_k=top_k,
            node_postprocessors=node_postprocessors,
            chat_mode=ChatMode.CONDENSE_PLUS_CONTEXT,
            verbose=True,
        )

        if self.use_rwlock:
            async with self.rwlock.reader_lock:
                self.llm.set_params(llm_params)
                query_result = await query_engine.achat(query)
        else:
            self.llm.set_params(llm_params)
            query_result = await query_engine.achat(query)
        return {
            "response": query_result.response,
            "source_nodes": [
                {
                    "doc_id": source_node.node.ref_doc_id,
                    "node_id": source_node.node_id,
                    "text": source_node.text,
                    "score": source_node.score,
                    "metadata": source_node.metadata,
                }
                for source_node in query_result.source_nodes
            ],
            "metadata": query_result.metadata,
        }

    async def chat_completion(self, request: dict) -> ChatCompletionResponse:
        """
        Create a chat completion based on the provided request.

        Args:
            request (ChatCompletionRequest): The request containing the chat messages and parameters.

        Returns:
            ChatCompletionResponse: The response containing the generated chat completion.
        """
        if (
            request.get("index_name")
            and request.get("index_name") not in self.index_map
        ):
            raise HTTPException(
                status_code=404,
                detail=f"No such index: '{request.get('index_name')}' exists.",
            )

        llm_params = {}
        if request.get("model") is not None:
            llm_params["model"] = request.get("model")
        if request.get("temperature") is not None:
            llm_params["temperature"] = request.get("temperature")
        if request.get("top_p") is not None:
            llm_params["top_p"] = request.get("top_p")
        if request.get("max_tokens") is not None:
            llm_params["max_tokens"] = request.get("max_tokens")

        logger.info("converting request to OpenAI format")
        openai_request = None
        last_error = None
        for model_cls in CompletionCreateParams.__args__:
            try:
                openai_request = model_cls(**request)
                break
            except ValidationError as e:
                last_error = e

        if openai_request is None:
            logger.error(f"Invalid request format: {str(last_error)}")
            raise HTTPException(
                status_code=400, detail=f"Invalid request format: {str(last_error)}"
            )

        if not request.get("index_name"):
            logger.info(
                "Request does not specify an index, passing through to LLM directly."
            )
            return await self.llm.chat_completions_passthrough(openai_request)

        if request.get("tools") or request.get("functions"):
            logger.info(
                "Request contains tools or functions, passing through to LLM directly."
            )
            return await self.llm.chat_completions_passthrough(openai_request)

        # Only support RAG usage on user/system/developer roles in messages and only text content
        for message in request.get("messages", []):
            # Every message must have a role
            if not message.get("role"):
                raise HTTPException(
                    status_code=400,
                    detail="Invalid request format: messages must contain 'role'.",
                )

            # Every message must have content aside from assistant role messages
            if message.get("role") != "assistant" and message.get("content") is None:
                raise HTTPException(
                    status_code=400,
                    detail=f"Invalid request format: messages must contain 'content' for role '{message.get('role')}'.",
                )

            # Only user, system, and developer roles are supported for RAG
            if message.get("role") not in ["user", "system", "assistant", "developer"]:
                logger.info(
                    f"Request contains unsupported role '{message.get('role')}' in messages, passing through to LLM directly."
                )
                return await self.llm.chat_completions_passthrough(openai_request)

            # User message content can be a range of options, but we only support text content for RAG
            if message.get("role") == "user":
                if message.get("content"):
                    content = message.get("content")
                    if isinstance(content, list):
                        for part in content:
                            if not isinstance(part, str) and part.get("type") != "text":
                                logger.info(
                                    "Request contains unsupported content type in user message, passing through to LLM directly."
                                )
                                return await self.llm.chat_completions_passthrough(
                                    openai_request
                                )
                    elif isinstance(content, str | ChatCompletionContentPartTextParam):
                        pass
                    else:
                        logger.info(
                            f"Request contains unsupported content type '{type(content)}' in messages, passing through to LLM directly."
                        )
                        return await self.llm.chat_completions_passthrough(
                            openai_request
                        )
                else:
                    logger.error(
                        "Invalid request format: user messages must contain 'content'."
                    )
                    raise HTTPException(
                        status_code=400,
                        detail="Invalid request format: user messages must contain 'content'.",
                    )

        prompt = messages_to_prompt(request.get("messages", []))

        logger.info(
            f"Creating chat engine for index '{request.get('index_name')}' with prompt: {prompt}"
        )
        chat_engine = self.index_map[request.get("index_name")].as_chat_engine(
            llm=self.llm,
            similarity_top_k=request.get("top_k", 5),
            chat_mode=ChatMode.CONDENSE_PLUS_CONTEXT,
            verbose=True,
        )

        logger.info("Processing chat completion request with prompt.")
        try:
            if self.use_rwlock:
                async with self.rwlock.reader_lock:
                    self.llm.set_params(llm_params)
                    chat_result = await chat_engine.achat(prompt)
            else:
                self.llm.set_params(llm_params)
                chat_result = await chat_engine.achat(prompt)

            return ChatCompletionResponse(
                id=uuid.uuid4().hex,
                object="chat.completion",
                created=int(time.time()),
                model=request.get("model"),
                choices=[
                    {
                        "message": {
                            "role": "assistant",
                            "content": chat_result.response,
                        },
                        "finish_reason": "stop",
                        "index": 0,
                    }
                ],
                source_nodes=[
                    {
                        "doc_id": source_node.node.ref_doc_id,
                        "node_id": source_node.node_id,
                        "text": source_node.text,
                        "score": source_node.score,
                        "metadata": source_node.metadata,
                    }
                    for source_node in chat_result.source_nodes
                ],
            )
        except Exception as e:
            logger.error(f"Error during chat completion: {str(e)}")
            raise HTTPException(
                status_code=500, detail=f"Chat completion failed: {str(e)}"
            )

    async def add_document_to_index(
        self, index_name: str, document: Document, doc_id: str
    ):
        """Common logic for adding a single document."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        llama_doc = LlamaDocument(
            id_=doc_id,
            text=document.text,
            metadata=document.metadata,
            excluded_llm_metadata_keys=[key for key in document.metadata],
        )

        if self.use_rwlock:
            async with self.rwlock.writer_lock:
                retrieved_doc = await self.index_map[
                    index_name
                ].docstore.aget_ref_doc_info(doc_id)
                if retrieved_doc:
                    logger.info(
                        f"Document {doc_id} already exists in index {index_name} (double-check). Skipping insertion."
                    )
                    return
                # Proceed with insertion only if the document is absent
                await asyncio.to_thread(self.index_map[index_name].insert, llama_doc)
        else:
            await asyncio.to_thread(self.index_map[index_name].insert, llama_doc)

    def list_indexes(self) -> list[str]:
        return list(self.index_map)

    async def delete_documents(self, index_name: str, doc_ids: list[str]):
        """Common logic for deleting a document."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        not_found_docs = []
        found_docs = []
        for doc_id in doc_ids:
            if doc_id in self.index_map[index_name].ref_doc_info:
                found_docs.append(doc_id)
            else:
                not_found_docs.append(doc_id)

        try:
            if self.use_rwlock:
                async with self.rwlock.writer_lock:
                    await asyncio.gather(
                        *(
                            self.index_map[index_name].adelete_ref_doc(
                                doc_id, delete_from_docstore=True
                            )
                            for doc_id in doc_ids
                        ),
                        return_exceptions=True,
                    )
            else:
                await asyncio.gather(
                    *(
                        self.index_map[index_name].adelete_ref_doc(
                            doc_id, delete_from_docstore=True
                        )
                        for doc_id in doc_ids
                    ),
                    return_exceptions=True,
                )

            return {"deleted_doc_ids": found_docs, "not_found_doc_ids": not_found_docs}
        except NotImplementedError as e:
            logger.error(f"Delete operation is not implemented for index {index_name}.")
            raise HTTPException(status_code=501, detail=f"Loading failed: {str(e)}")
        except Exception as e:
            logger.error(f"Error deleting documents from index {index_name}: {str(e)}")
            raise HTTPException(status_code=500, detail=f"Loading failed: {str(e)}")

    async def update_documents(self, index_name: str, documents: list[Document]):
        """Common logic for updating a document."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        not_found_docs = []
        found_docs = []
        llama_docs = []
        for document in documents:
            if document.doc_id in self.index_map[index_name].ref_doc_info:
                found_docs.append(document)
                llama_docs.append(
                    LlamaDocument(
                        id_=document.doc_id,
                        text=document.text,
                        metadata=document.metadata,
                        excluded_llm_metadata_keys=[key for key in document.metadata],
                    )
                )
            else:
                not_found_docs.append(document)

        try:
            if self.use_rwlock:
                async with self.rwlock.writer_lock:
                    refreshed_docs = self.index_map[index_name].refresh_ref_docs(
                        llama_docs
                    )
            else:
                refreshed_docs = self.index_map[index_name].refresh_ref_docs(llama_docs)

            updated_docs = []
            unchanged_docs = []
            for doc, was_updated in zip(found_docs, refreshed_docs):
                if was_updated:
                    updated_docs.append(doc)
                else:
                    unchanged_docs.append(doc)
            return {
                "updated_documents": updated_docs,
                "unchanged_documents": unchanged_docs,
                "not_found_documents": not_found_docs,
            }
        except NotImplementedError as e:
            logger.error(f"Update operation is not implemented for index {index_name}.")
            raise HTTPException(status_code=501, detail=f"Loading failed: {str(e)}")
        except Exception as e:
            logger.error(f"Error updating documents in index {index_name}: {str(e)}")
            raise HTTPException(status_code=500, detail=f"Loading failed: {str(e)}")

    async def _process_document(
        self, doc_id: str, doc_stub, doc_store, max_text_length: int | None
    ):
        """
        Helper to process and format a single document.
        """
        try:
            if isinstance(doc_store, SimpleDocumentStore):
                text, hash_value, ref_doc_id = (
                    doc_stub.text,
                    doc_stub.hash,
                    doc_stub.ref_doc_id,
                )
            else:
                doc_info = await doc_store.aget_document(doc_id)
                text, hash_value, ref_doc_id = (
                    doc_info.text,
                    doc_info.hash,
                    doc_stub.ref_doc_id,
                )

            # Truncate if needed
            is_truncated = bool(max_text_length and len(text) > max_text_length)
            truncated_text = text[:max_text_length] if is_truncated else text

            return {
                "doc_id": ref_doc_id,
                "text": truncated_text,
                "hash_value": hash_value,
                "metadata": getattr(doc_stub, "metadata", {}),
                "is_truncated": is_truncated,
            }
        except Exception as e:
            logger.error(f"Error processing document {doc_id}: {str(e)}")
            return None  # Explicitly return None for failed documents

    async def list_documents_in_index(
        self,
        index_name: str,
        limit: int,
        offset: int,
        max_text_length: int | None = None,
        metadata_filter: dict[str, Any] | None = None,
    ) -> list[dict[str, Any]]:
        """
        Return a dictionary of document metadata for the given index.
        """
        vector_store_index = self.index_map.get(index_name)
        if not vector_store_index:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        doc_store = vector_store_index.docstore
        doc_store_items = doc_store.docs.items()
        if metadata_filter is not None:
            docs_items = await self._filter_documents(
                doc_store_items, metadata_filter, offset, limit
            )
        else:
            docs_items = islice(doc_store_items, offset, offset + limit)

        # Process documents concurrently, handling exceptions
        docs = await asyncio.gather(
            *(
                self._process_document(doc_id, doc_stub, doc_store, max_text_length)
                for doc_id, doc_stub in docs_items
            ),
            return_exceptions=True,
        )

        # Return list of valid documents
        return [doc for doc in docs if isinstance(doc, dict)]

    async def delete_index(self, index_name: str):
        """Common logic for deleting an index."""
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        if self.use_rwlock:
            async with self.rwlock.writer_lock:
                del self.index_map[index_name]
        else:
            del self.index_map[index_name]

        logger.info(f"Index {index_name} deleted successfully.")

    async def _filter_documents(self, doc_items, metadata_filter, offset, limit):
        """
        Filter documents based on metadata.
        """
        filtered_docs = []
        for doc_id, doc_stub in doc_items:
            doc_metadata = getattr(doc_stub, "metadata", {})
            if all(doc_metadata.get(k) == v for k, v in metadata_filter.items()):
                filtered_docs.append((doc_id, doc_stub))
        return islice(filtered_docs, offset, offset + limit)

    async def document_exists(
        self, index_name: str, doc: Document, doc_id: str
    ) -> bool:
        """Common logic for checking document existence."""
        if index_name not in self.index_map:
            logger.warning(f"No such index: '{index_name}' exists in vector store.")
            return False
        return doc_id in self.index_map[index_name].ref_doc_info

    async def persist(self, index_name: str, path: str):
        """Common persistence logic for individual index."""

        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        if self.use_rwlock:
            async with self.rwlock.writer_lock:
                await self._persist_internal(index_name, path)
        else:
            await self._persist_internal(index_name, path)

    async def _persist_internal(self, index_name: str, path: str):
        """Common persistence logic for individual index."""
        try:
            # Ensure the directory exists
            os.makedirs(path, exist_ok=True)
            if index_name not in self.index_map:
                raise HTTPException(
                    status_code=404, detail=f"No such index: '{index_name}' exists."
                )

            # Persist the specific index
            storage_context = self.index_map[index_name].storage_context
            await asyncio.to_thread(storage_context.persist, path)
            logger.info(f"Successfully persisted index {index_name}.")
        except Exception as e:
            logger.error(f"Failed to persist index {index_name}. Error: {str(e)}")
            raise HTTPException(status_code=500, detail=f"Persistence failed: {str(e)}")

    async def load(self, index_name: str, path: str, overwrite: bool):
        """Common logic for loading an index."""
        # Check path existence before acquiring any lock
        if not os.path.exists(path):
            raise HTTPException(status_code=404, detail=f"Path does not exist: {path}")
        if self.use_rwlock:
            async with self.rwlock.writer_lock:
                await self._load_internal(index_name, path, overwrite)
        else:
            await self._load_internal(index_name, path, overwrite)

    async def _load_internal(self, index_name: str, path: str, overwrite: bool):
        """Common logic for loading an index."""
        try:
            if index_name in self.index_map and not overwrite:
                raise HTTPException(
                    status_code=409,
                    detail=f"Index '{index_name}' already exists. Use a different name or delete the existing index first.",
                )

            logger.info(f"Loading index {index_name} from {path}.")

            try:
                storage_context = StorageContext.from_defaults(persist_dir=path)
            except UnicodeDecodeError:
                # Failed to load the index in the default json format, trying faissdb
                faiss_vs = FaissMapVectorStore.from_persist_dir(persist_dir=path)
                storage_context = StorageContext.from_defaults(
                    persist_dir=path, vector_store=faiss_vs
                )
            except Exception as e:
                logger.error(f"Failed to load index '{index_name}'. Error: {str(e)}")
                raise HTTPException(status_code=500, detail=f"Loading failed: {str(e)}")

            logger.info(
                f"Loading index '{index_name}' using the workspace's embedding model."
            )
            # Load the index using the workspace's embedding model, assuming all indices
            # were created using the same embedding model currently in use.
            loaded_index = await asyncio.to_thread(
                load_index_from_storage,
                storage_context,
                embed_model=self.embed_model,
                show_progress=True,
            )
            self.index_map[index_name] = loaded_index
            logger.info(f"Successfully loaded index {index_name}.")
        except Exception as e:
            logger.error(f"Failed to load index {index_name}. Error: {str(e)}")
            raise HTTPException(status_code=500, detail=f"Loading failed: {str(e)}")
