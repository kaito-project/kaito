# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Any, Dict, List, Optional

from pydantic import BaseModel, Field, root_validator, ValidationError

class Document(BaseModel):
    text: str
    metadata: Optional[dict] = {}

class DocumentResponse(BaseModel):
    doc_id: str
    text: str
    metadata: Optional[dict] = None

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

    @root_validator(pre=True)
    def validate_params(cls, values):
        llm_params = values.get("llm_params", {})
        rerank_params = values.get("rerank_params", {})

        # Validate LLM parameters
        if "temperature" in llm_params and not (0.0 <= llm_params["temperature"] <= 1.0):
            raise ValueError("Temperature must be between 0.0 and 1.0.")

        # Validate rerank parameters
        top_k = values["top_k"]
        if "top_n" in rerank_params and rerank_params["top_n"] > top_k:
            raise ValueError("Invalid configuration: 'top_n' for reranking cannot exceed 'top_k' from the RAG query.")

        return values

class ListDocumentsResponse(BaseModel):
    documents: Dict[str, Dict[str, Dict[str, str]]]

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