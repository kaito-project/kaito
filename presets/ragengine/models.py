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

class QueryRequest(BaseModel):
    index_name: str
    query: str
    top_k: int = 10
    # Accept a dictionary for our LLM parameters
    llm_params: Optional[Dict[str, Any]] = Field(
        default_factory=dict,
        description="Optional parameters for the language model, e.g., temperature, top_p",
    )
    # Accept a dictionary for rerank parameters
    rerank_params: Optional[Dict[str, Any]] = Field(
        default_factory=dict,
        description="Optional parameters for reranking, e.g., top_n, batch_size",
    )
    stream: Optional[bool] = False

    @model_validator(mode="before")
    def validate_params(cls, values: Dict[str, Any]) -> Dict[str, Any]:
        llm_params = values.get("llm_params", {})
        rerank_params = values.get("rerank_params", {})

        # Validate LLM parameters
        if "temperature" in llm_params and not (0.0 <= llm_params["temperature"] <= 1.0):
            raise ValueError("Temperature must be between 0.0 and 1.0.")
        # TODO: More LLM Param Validations here
        # Validate rerank parameters
        top_k = values.get("top_k")
        if "top_n" in rerank_params and rerank_params["top_n"] > top_k:
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