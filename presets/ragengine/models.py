# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Any, Dict, List, Optional

from pydantic import BaseModel, Field, model_validator


class Document(BaseModel):
    text: str
    metadata: Optional[dict] = Field(default_factory=dict)

class DocumentResponse(BaseModel):
    doc_id: str
    text: str
    hash_value: Optional[str] = None
    metadata: Optional[dict] = Field(default_factory=dict)
    is_truncated: bool = False

class ListDocumentsResponse(BaseModel):
    documents: List[DocumentResponse] # List of DocumentResponses
    count: int  # Number of documents in the current response

class IndexRequest(BaseModel):
    index_name: str
    documents: List[Document]

class UpdateDocumentRequest(BaseModel):
    doc_id: str
    text: str
    hash_value: Optional[str] = None
    metadata: Optional[dict] = Field(default_factory=dict)
    is_truncated: bool = False

class QueryRequest(BaseModel):
    index_name: str
    query: str
    top_k: int = 5
    # Accept a dictionary for our LLM parameters
    llm_params: Optional[Dict[str, Any]] = Field(
        default_factory=dict,
        description="Optional parameters for the language model, e.g., temperature, top_p",
    )
    # Accept a dictionary for rerank parameters
    rerank_params: Optional[Dict[str, Any]] = Field(
        default_factory=dict,
        description="Experimental: Optional parameters for reranking. Only 'top_n' and 'choice_batch_size' are supported.",
    )

    @model_validator(mode="after")
    def validate_params(cls, values: "QueryRequest") -> "QueryRequest":
        # Access fields as attributes instead of treating as a dictionary
        llm_params = values.llm_params # Validate LLM parameters on vLLM side
        rerank_params = values.rerank_params
        top_k = values.top_k

        # Validate rerank parameters
        if "top_n" in rerank_params:
            if not isinstance(rerank_params["top_n"], int):
                raise ValueError("Invalid type: 'top_n' must be an integer.")
            if rerank_params["top_n"] > top_k:
                raise ValueError("Invalid configuration: 'top_n' for reranking cannot exceed 'top_k' from the RAG query.")

        return values

# Define models for NodeWithScore, and QueryResponse
class NodeWithScore(BaseModel):
    node_id: str
    text: str
    score: float
    metadata: Optional[dict] = None

class QueryResponse(BaseModel):
    response: str
    source_nodes: List[NodeWithScore]
    metadata: Optional[dict] = None

class HealthStatus(BaseModel):
    status: str
    detail: Optional[str] = None 
