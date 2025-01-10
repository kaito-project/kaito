# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import Dict, List, Optional

from pydantic import BaseModel, ConfigDict

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
    model_config = ConfigDict(extra='allow')
    rerank_params: Optional[Dict] = None # Accept a dictionary for parameters

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