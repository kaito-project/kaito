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


from llama_index.core.base.llms.types import ChatMessage, MessageRole
from openai.types.chat import (
    ChatCompletion,
)
from pydantic import BaseModel, Field

from ragengine.config import RAG_MAX_TOP_K


class Document(BaseModel):
    doc_id: str = Field(default="")
    text: str
    metadata: dict | None = Field(default_factory=dict)
    hash_value: str | None = None
    is_truncated: bool = False


class ListDocumentsResponse(BaseModel):
    documents: list[Document]  # List of DocumentResponses
    count: int  # Number of documents in the current response
    total_items: int  # Total number of document with filters applied


class IndexRequest(BaseModel):
    index_name: str
    documents: list[Document]


class UpdateDocumentRequest(BaseModel):
    documents: list[Document]


class UpdateDocumentResponse(BaseModel):
    updated_documents: list[Document]
    unchanged_documents: list[Document]
    not_found_documents: list[Document]


class DeleteDocumentRequest(BaseModel):
    doc_ids: list[str]


class DeleteDocumentResponse(BaseModel):
    deleted_doc_ids: list[str]
    not_found_doc_ids: list[str]


# Define models for NodeWithScore (must be before RetrieveResponse)
class NodeWithScore(BaseModel):
    doc_id: str
    node_id: str
    text: str
    score: float
    dense_score: float | None = None
    sparse_score: float | None = None
    source: str | None = None  # "both", "dense_only", "sparse_only"
    metadata: dict | None = None


class RetrieveRequest(BaseModel):
    index_name: str = Field(..., description="Name of the index to retrieve from")
    query: str = Field(..., description="User query string for retrieve")
    max_node_count: int = Field(
        default=5,
        ge=1,
        le=RAG_MAX_TOP_K,
        description=f"Maximum number of documents to return (default: 5, max: {RAG_MAX_TOP_K})",
    )
    metadata_filter: dict | None = Field(
        default=None, description="Optional metadata filter for retrieve results"
    )


class RetrieveResponse(BaseModel):
    query: str = Field(..., description="The query used for retrieve")
    results: list[NodeWithScore] = Field(
        ..., description="List of retrieved documents with scores"
    )
    count: int = Field(..., description="Number of retrieved documents")


class HealthStatus(BaseModel):
    status: str
    detail: str | None = None


class ChatCompletionResponse(ChatCompletion):
    source_nodes: list[NodeWithScore] | None = None


def input_messages_to_llamaindex_messages(messages: list[dict]) -> list[ChatMessage]:
    """Convert openai messages from chat/completions requests into LlamaIndex ChatMessages."""
    resp_messages = []
    for message in messages:
        content = get_message_content(message)
        new_message = ChatMessage(content)
        new_message.role = MessageRole(message.get("role"))
        resp_messages.append(new_message)
    return resp_messages


def _join_text_parts(parts: list) -> str:
    """Join the text of an OpenAI content-parts array, skipping non-text parts."""
    texts = []
    for part in parts:
        if isinstance(part, str):
            texts.append(part)
        elif isinstance(part, dict) and part.get("type") == "text":
            texts.append(part.get("text", ""))
    return "\n".join(texts)


def get_message_content(message: dict) -> str:
    """Extract the text of a ChatCompletionMessageParam as a plain string.

    Content can be a string, a single text part, or an array of text parts.
    A missing or null content (allowed for assistant messages) is an empty string.
    """
    content = message.get("content")
    if message.get("role") == "user":
        if content:
            if isinstance(content, str):
                return content
            elif isinstance(content, dict) and content.get("type") == "text":
                return content.get("text", "")
            elif isinstance(content, list):
                return _join_text_parts(content)
        return ""
    if isinstance(content, dict):
        return content.get("text", "")
    if isinstance(content, list):
        return _join_text_parts(content)
    return content or ""
