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


from typing import Any

from openai.types.chat import (
    ChatCompletion,
)
from pydantic import BaseModel, Field, model_validator


class Document(BaseModel):
    doc_id: str = Field(default="")
    text: str
    metadata: dict | None = Field(default_factory=dict)
    hash_value: str | None = None
    is_truncated: bool = False


class ListDocumentsResponse(BaseModel):
    documents: list[Document]  # List of DocumentResponses
    count: int  # Number of documents in the current response


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


class QueryRequest(BaseModel):
    index_name: str
    query: str
    top_k: int = 5
    # Accept a dictionary for our LLM parameters
    llm_params: dict[str, Any] | None = Field(
        default_factory=dict,
        description="Optional parameters for the language model, e.g., temperature, top_p",
    )
    # Accept a dictionary for rerank parameters
    rerank_params: dict[str, Any] | None = Field(
        default_factory=dict,
        description="Experimental: Optional parameters for reranking. Only 'top_n' and 'choice_batch_size' are supported.",
    )

    @model_validator(mode="after")
    def validate_params(self) -> "QueryRequest":
        # Access fields as attributes
        rerank_params = self.rerank_params
        top_k = self.top_k

        # Validate rerank parameters
        if "top_n" in rerank_params:
            if not isinstance(rerank_params["top_n"], int):
                raise ValueError("Invalid type: 'top_n' must be an integer.")
            if rerank_params["top_n"] > top_k:
                raise ValueError(
                    "Invalid configuration: 'top_n' for reranking cannot exceed 'top_k' from the RAG query."
                )
        return self


# Define models for NodeWithScore, and QueryResponse
class NodeWithScore(BaseModel):
    doc_id: str
    node_id: str
    text: str
    score: float
    metadata: dict | None = None


class QueryResponse(BaseModel):
    response: str
    source_nodes: list[NodeWithScore]
    metadata: dict | None = None


class HealthStatus(BaseModel):
    status: str
    detail: str | None = None


class ChatCompletionResponse(ChatCompletion):
    source_nodes: list[NodeWithScore] | None = None


def messages_to_prompt(messages: list[dict]) -> str:
    """Convert messages to a prompt string."""
    string_messages = []
    for message in messages:
        content = get_message_content(message)
        string_messages.append(f"{message.get('role')}: {content}")
    return "\n".join(string_messages)


def get_message_content(message: dict) -> str:
    """Extract content from a ChatCompletionMessageParam."""
    if message.get("role") == "user":
        if message.get("content"):
            content = message.get("content")
            if isinstance(content, str):
                return content
            elif isinstance(content, dict) and content.get("type") == "text":
                return content.get("text", "")
            elif isinstance(content, list):
                user_text_content = []
                for part in content:
                    if isinstance(part, str):
                        user_text_content.append(part)
                    elif part.get("type") == "text":
                        user_text_content.append(part.get("text", ""))
                return "\n".join(user_text_content)
        return ""
    else:
        return (
            message.get("content", {}).get("text", "")
            if isinstance(message.get("content"), dict)
            else message.get("content", "")
        )
