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
from llama_index.core.base.llms.types import MessageRole
from llama_index.core.chat_engine.types import ChatMode
from llama_index.core.storage.docstore import SimpleDocumentStore
from llama_index.vector_stores.faiss import FaissMapVectorStore
from openai.types.chat import ChatCompletionContentPartTextParam, CompletionCreateParams
from pydantic import ValidationError

from ragengine.config import (
    RAG_DEFAULT_CONTEXT_TOKEN_FILL_RATIO,
    RAG_DOCUMENT_NODE_TOKEN_APPROXIMATION,
    RAG_MAX_TOP_K,
    RAG_SIMILARITY_THRESHOLD,
)
from ragengine.embedding.base import BaseEmbeddingModel
from ragengine.inference.inference import Inference
from ragengine.inference.retrieve_llm import RetrieveLLM
from ragengine.models import (
    ChatCompletionResponse,
    Document,
    ListDocumentsResponse,
    input_messages_to_llamaindex_messages,
)
from ragengine.vector_store.node_processors.contex_selection_node_processor import (
    ContextSelectionProcessor,
)
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

        if request.get("context_token_ratio") and (
            request.get("context_token_ratio") < 0.2
            or request.get("context_token_ratio") > 0.8
        ):
            raise HTTPException(
                status_code=400,
                detail=f"Invalid context_token_ratio: {request.get('context_token_ratio')}. Must be between 0.2 and 0.8.",
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

        max_tokens = request.get("max_tokens")
        messages = input_messages_to_llamaindex_messages(request.get("messages", []))

        total_prompt_for_token_aprox = ""
        chat_history = []
        user_messages_for_prompt = []
        assistant_message_found = False
        # Process messages in reverse order to find the last x user messages since last assistant message.
        # We want to pull all user messages after the last assistant message as we want to run vector search
        # only on the user messages to have the most relevant content. The rest of the messages will be added
        # as chat history. This will allow them to be passed into the LLM for context in the order they were received.
        for i in range(len(messages) - 1, -1, -1):
            message = messages[i]
            message_content = message.content
            total_prompt_for_token_aprox = (
                total_prompt_for_token_aprox + "\n\n" + message_content
            )
            if message.role == MessageRole.USER and not assistant_message_found:
                # Collect the last x user messages for combining into the prompt
                user_messages_for_prompt.insert(0, message_content)
            else:
                if message.role == MessageRole.ASSISTANT:
                    assistant_message_found = True
                chat_history.insert(0, message)  # Insert at beginning to maintain order

        # Combine the last consecutive user messages into a single string as the prompt
        # Generally there should only be 1 user message after the latest assistant message
        # but there could be more in some cases
        user_prompt = (
            "\n\n".join(user_messages_for_prompt) if user_messages_for_prompt else ""
        )

        # Validate we have a user prompt. if not using tools/etc. we should have a user prompt
        # as rag retrieve should only be run on user input.
        if user_prompt == "":
            logger.error(
                "There must be a user prompt since the latest assistant message."
            )
            raise HTTPException(
                status_code=400,
                detail="There must be a user prompt since the latest assistant message.",
            )

        prompt_len = self.llm.count_tokens(total_prompt_for_token_aprox)
        if prompt_len > self.llm.metadata.context_window:
            logger.error(
                f"Prompt length ({prompt_len}) exceeds context window ({self.llm.metadata.context_window})."
            )
            raise HTTPException(
                status_code=400, detail="Prompt length exceeds context window."
            )

        if max_tokens and max_tokens > self.llm.metadata.context_window - prompt_len:
            # max_tokens is greater than the available tokens
            # this edit will make sure we dont add more context in the rag than we should
            # we also handle updating the max_tokens within the inference code before shipping to LLM based off added context
            logger.warning(
                f"max_tokens ({max_tokens}) is greater than available context after prompt consideration. Setting to {self.llm.metadata.context_window - prompt_len}."
            )
            max_tokens = self.llm.metadata.context_window - prompt_len

        logger.info(
            f"Creating chat engine for index '{request.get('index_name')}' with prompt size: {prompt_len}"
        )
        # top_k is the max amount of nodes we will fetch from the vector store to add as context onto the request to the llm.
        # This is calculated based on the available context window and the prompt length.
        # This calculation is based off latency considerations for faiss which is in memory. We might need to revisit this
        # calculation for external database vector stores.
        top_k = max(
            100,
            int(
                (self.llm.metadata.context_window - prompt_len)
                / RAG_DOCUMENT_NODE_TOKEN_APPROXIMATION
            ),
        )
        chat_engine = self.index_map[request.get("index_name")].as_chat_engine(
            llm=self.llm,
            similarity_top_k=top_k,
            chat_mode=ChatMode.CONTEXT,
            node_postprocessors=[
                ContextSelectionProcessor(
                    rag_context_token_fill_ratio=request.get(
                        "context_token_ratio", RAG_DEFAULT_CONTEXT_TOKEN_FILL_RATIO
                    ),
                    llm=self.llm,
                    max_tokens=max_tokens,
                    similarity_threshold=RAG_SIMILARITY_THRESHOLD,
                )
            ],
        )

        logger.info("Processing chat completion request with prompt.")
        try:
            if self.use_rwlock:
                async with self.rwlock.reader_lock:
                    self.llm.set_params(llm_params)
                    chat_result = await chat_engine.achat(
                        user_prompt, chat_history=chat_history
                    )
            else:
                self.llm.set_params(llm_params)
                chat_result = await chat_engine.achat(
                    user_prompt, chat_history=chat_history
                )

            # If no relevant context is found, the llamaindex response synthesizers will send an empty response without hitting the llm.
            # We will instead send the passthrough request directly to the llm rather than send an empty response to the user
            # https://github.com/run-llama/llama_index/blob/8469a034226d20b70a667dc7faf013770716709f/llama-index-core/llama_index/core/response_synthesizers/base.py#L271
            if (
                len(chat_result.source_nodes) == 0
                and chat_result.response.strip() == "Empty Response"
            ):
                logger.info(
                    "No relevant context found for the query. Falling back to passthrough request"
                )
                return await self.llm.chat_completions_passthrough(openai_request)

            # Try to get usage from LLM response via the last_usage attribute
            # The Inference class stores usage from the last LLM API call
            llm_usage = None
            if hasattr(self.llm, "last_usage") and self.llm.last_usage:
                llm_usage = self.llm.last_usage

            if llm_usage:
                usage = llm_usage
                logger.info(
                    f"Token usage from LLM API response: {llm_usage.get('total_tokens', 0)} total tokens "
                    f"(prompt: {llm_usage.get('prompt_tokens', 0)}, completion: {llm_usage.get('completion_tokens', 0)})"
                )
            else:
                # Fallback to manual calculation if LLM doesn't return usage
                usage = {
                    "prompt_tokens": self.llm.count_tokens(
                        total_prompt_for_token_aprox
                    ),
                    "completion_tokens": self.llm.count_tokens(
                        chat_result.response or ""
                    ),
                    "total_tokens": self.llm.count_tokens(total_prompt_for_token_aprox)
                    + self.llm.count_tokens(chat_result.response or ""),
                }
                logger.info(
                    f"Token usage calculated by manual estimation: {usage['total_tokens']} total tokens "
                    f"(prompt: {usage['prompt_tokens']}, completion: {usage['completion_tokens']})"
                )

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
                usage=usage,
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
    ) -> ListDocumentsResponse:
        """
        Return a dictionary of document metadata for the given index.
        """
        if self.use_rwlock:
            async with self.rwlock.reader_lock:
                return await self._list_documents_in_index(
                    index_name, limit, offset, max_text_length, metadata_filter
                )

        return await self._list_documents_in_index(
            index_name, limit, offset, max_text_length, metadata_filter
        )

    async def _list_documents_in_index(
        self,
        index_name: str,
        limit: int,
        offset: int,
        max_text_length: int | None = None,
        metadata_filter: dict[str, Any] | None = None,
    ) -> ListDocumentsResponse:
        vector_store_index = self.index_map.get(index_name)
        if not vector_store_index:
            raise HTTPException(
                status_code=404, detail=f"No such index: '{index_name}' exists."
            )

        doc_store = vector_store_index.docstore
        doc_store_items = doc_store.docs.items()
        total_count = len(doc_store_items)
        if metadata_filter is not None:
            docs_items, total_count = await self._filter_documents(
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
        docs = [doc for doc in docs if isinstance(doc, dict)]
        return ListDocumentsResponse(
            documents=docs, count=len(docs), total_items=total_count
        )

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
        total_count = 0
        filtered_docs = []
        for doc_id, doc_stub in doc_items:
            doc_metadata = getattr(doc_stub, "metadata", {})
            if all(doc_metadata.get(k) == v for k, v in metadata_filter.items()):
                if total_count >= offset and len(filtered_docs) < limit:
                    filtered_docs.append((doc_id, doc_stub))
                total_count += 1
        return filtered_docs, total_count

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

    async def retrieve(
        self,
        index_name: str,
        query: str,
        max_node_count: int = 5,
        metadata_filter: dict | None = None,
    ):
        """
        Retrieve relevant documents based on a query string.
        Uses the same chat_engine as chat_completion to ensure identical output.
        """
        if index_name not in self.index_map:
            raise HTTPException(
                status_code=404,
                detail=f"No such index: '{index_name}' exists.",
            )

        try:
            # Direct query string, no message processing needed
            user_prompt = query
            chat_history = []

            if not user_prompt or user_prompt.strip() == "":
                raise HTTPException(
                    status_code=400,
                    detail="Query string cannot be empty.",
                )

            # Use max_node_count as top_k, but cap at RAG_MAX_TOP_K to prevent excessive resource usage
            top_k = min(max_node_count, RAG_MAX_TOP_K)

            # Create a custom LLM that captures the messages sent to it
            captured_messages = []
            captured_source_nodes = []

            chat_engine = self.index_map[index_name].as_chat_engine(
                llm=RetrieveLLM(
                    messages_list=captured_messages,
                    nodes_list=captured_source_nodes,
                    original_llm=self.llm,
                ),
                similarity_top_k=top_k,
                chat_mode=ChatMode.CONTEXT,
            )

            # Call chat_engine to build the context and messages
            if self.use_rwlock:
                async with self.rwlock.reader_lock:
                    result = await chat_engine.achat(
                        user_prompt, chat_history=chat_history
                    )
            else:
                result = await chat_engine.achat(user_prompt, chat_history=chat_history)

            logger.info(f"Captured {len(captured_messages)} messages from chat_engine")

            # Get source nodes from the result
            source_nodes_list = (
                result.source_nodes if hasattr(result, "source_nodes") else []
            )

            # Apply metadata filter if provided
            if metadata_filter:
                source_nodes_list = [
                    node
                    for node in source_nodes_list
                    if all(
                        (node.node.metadata or {}).get(k) == v
                        for k, v in metadata_filter.items()
                    )
                ]

            # Convert source_nodes_list to serializable format
            results = []
            for node in source_nodes_list:
                results.append(
                    {
                        "doc_id": node.node.node_id,  # Use node_id as doc_id
                        "node_id": node.node.node_id,
                        "text": node.node.get_content(),
                        "score": node.score if node.score is not None else 0.0,
                        "metadata": node.node.metadata if node.node.metadata else None,
                    }
                )

            # Return the original retrieved nodes
            return {
                "query": user_prompt,
                "results": results,
                "count": len(results),
            }

        except Exception as e:
            import traceback

            logger.error(f"Retrieve failed for index '{index_name}': {str(e)}")
            logger.error(f"Traceback: {traceback.format_exc()}")
            raise HTTPException(status_code=500, detail=f"Retrieve failed: {str(e)}")
