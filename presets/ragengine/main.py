# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import List, Optional
from vector_store_manager.manager import VectorStoreManager
from embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from embedding.remote_embedding import RemoteEmbeddingModel
from fastapi import FastAPI, HTTPException, Query
from models import (IndexRequest, ListDocumentsResponse,
                    QueryRequest, QueryResponse, DocumentResponse, HealthStatus)
from vector_store.faiss_store import FaissVectorStoreHandler

from ragengine.config import (REMOTE_EMBEDDING_URL, REMOTE_EMBEDDING_ACCESS_SECRET,
                              EMBEDDING_SOURCE_TYPE, LOCAL_EMBEDDING_MODEL_ID)
from urllib.parse import unquote

app = FastAPI()

# Initialize embedding model
if EMBEDDING_SOURCE_TYPE.lower() == "local":
    embedding_manager = LocalHuggingFaceEmbedding(LOCAL_EMBEDDING_MODEL_ID)
elif EMBEDDING_SOURCE_TYPE.lower() == "remote":
    embedding_manager = RemoteEmbeddingModel(REMOTE_EMBEDDING_URL, REMOTE_EMBEDDING_ACCESS_SECRET)
else:
    raise ValueError("Invalid Embedding Type Specified (Must be Local or Remote)")

# Initialize vector store
# TODO: Dynamically set VectorStore from EnvVars (which ultimately comes from CRD StorageSpec)
vector_store_handler = FaissVectorStoreHandler(embedding_manager)

# Initialize RAG operations
rag_ops = VectorStoreManager(vector_store_handler)

@app.get(
    "/health",
    response_model=HealthStatus,
    summary="Health Check for RAG Engine",
    description="""
    Check the health status of the RAG Engine and its components.

    ## Response Examples:
    - **Success (200):**
      ```json
      {
        "status": "Healthy",
        "detail": null
      }
    """,
)
def health_check():
    try:
        if embedding_manager is None:
            raise HTTPException(status_code=500, detail="Embedding manager not initialized")
        
        if rag_ops is None:
            raise HTTPException(status_code=500, detail="RAG operations not initialized")

        return HealthStatus(status="Healthy")
    
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post(
    "/index",
    response_model=List[DocumentResponse],
    summary="Index Documents",
    description="""
    Add documents to an index or create a new index.

    ## Request Example:
    ```json
    {
      "index_name": "example_index",
      "documents": [
        {"text": "Sample document text.", "metadata": {"author": "John Doe", "category": "example"}}
      ]
    }
    ```

    ## Response Example:
    ```json
    [
      {
        "doc_id": "123456",
        "text": "Sample document text.",
        "hash_value": null,
        "metadata": {"author": "John Doe", "category": "example"},
        "is_truncated": false
      }
    ]
    ```
    """,
)
async def index_documents(request: IndexRequest):
    try:
        doc_ids = await rag_ops.index(request.index_name, request.documents)
        documents = [
            DocumentResponse(doc_id=doc_id, text=doc.text, metadata=doc.metadata)
            for doc_id, doc in zip(doc_ids, request.documents)
        ]
        return documents
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post(
    "/query",
    response_model=QueryResponse,
    summary="Query an Index",
    description="""
    Query a specific index for documents and optionally rerank with an LLM.

    ## Request Example:
    ```json
    {
      "index_name": "example_index",
      "query": "What is RAG?",
      "top_k": 5
    }
    ```

    ## Response Example:
    ```json
    {
      "response": "RAG is retrieval-augmented generation...",
      "source_nodes": [{"node_id": "node1", "text": "RAG explanation...", "score": 0.95, "metadata": {}}],
      "metadata": {}
    }
    ```
    """,
)
async def query_index(request: QueryRequest):
    try:
        llm_params = request.llm_params or {}  # Default to empty dict if no params provided
        rerank_params = request.rerank_params or {}  # Default to empty dict if no params provided
        return await rag_ops.query(
            request.index_name, request.query, request.top_k, llm_params, rerank_params
        )
    except ValueError as ve:
        raise HTTPException(status_code=400, detail=str(ve))  # Validation issue
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"An unexpected error occurred: {str(e)}"
        )

@app.get(
    "/indexes",
    response_model=List[str],
    summary="List All Indexes",
    description="""
    Retrieve the names of all available indexes.

    ## Response Example:
    ```json
    [
      "example_index",
      "test_index"
    ]
    ```
    """,
)
def list_indexes():
    try:
        return rag_ops.list_indexes()
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get(
    "/indexes/{index_name}/documents",
    response_model=ListDocumentsResponse,
    summary="List Documents in an Index",
    description="""
    Retrieve a paginated list of documents for a given index.

    ## Request Example:
    ```
    GET /indexes/test_index/documents?limit=5&offset=5&max_text_length=500
    ```

    ## Response Example:
    ```json
    {
      "documents": {
        "test_index": [
          {
            "doc_id": "123456",
            "text": "Sample document text.",
            "metadata": {"author": "John Doe"},
            "is_truncated": true
          }
          ...
        ]
      },
      "count": 5,
      "next_offset": 10
    }
    ```
    """,
)
async def list_documents_in_index(
    index_name: str,
    limit: int = Query(10, ge=1, le=100, description="Maximum number of documents to return"),
    offset: int = Query(0, ge=0, description="Starting point for the document list"),
    max_text_length: Optional[int] = Query(1000, ge=1, description="Maximum text length to return"),
):
    """
    Handles URL-encoded index names sent by the client.

    Examples:
    Raw Index Name    | URL-Encoded Form   | Decoded Form
    ------------------|--------------------|--------------
    my_index          | my_index          | my_index
    my index          | my%20index        | my index
    index/name        | index%2Fname      | index/name
    """
    try:
        # Decode the index_name in case it was URL-encoded by the client
        decoded_index_name = unquote(index_name)
        documents = await rag_ops.list_documents_in_index(
            index_name=decoded_index_name,
            limit=limit+1, # Fetch one extra to check for more results
            offset=offset,
            max_text_length=max_text_length
        )
        has_more = len(documents) > limit
        documents = dict(list(documents.items())[:limit]) # Limit the results to the requested count

        return ListDocumentsResponse(
            documents=documents,
            count=len(documents),
            next_offset=offset + limit if has_more else None
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get(
    "/documents",
    response_model=ListDocumentsResponse,
    summary="List All Documents Across Indexes",
    description="""
    Retrieve a paginated list of documents across all indexes.

    ## Request Example:
    ```
    GET /documents?limit=5&offset=0&max_text_length=200
    ```

    ## Response Example:
    ```json
    {
      "documents": {
        "test_index": [
          {
            "doc_id": "abcdef",
            "text": "Shortened sample document...",
            "metadata": {"category": "example"},
            "is_truncated": true
          },
          ...
        ],
        "another_index": [
          {
            "doc_id": "ghijkl",
            "text": "Another sample document...",
            "metadata": {"category": "example"},
            "is_truncated": false
          },
          ...
        ]
      },
      "count": 5,
      "next_offset": 5
    }
    ```
    """,
)
async def list_documents_paginated(
    limit: int = Query(10, ge=1, le=100, description="Maximum number of documents to return"),
    offset: int = Query(0, ge=0, description="Starting point for the document list"),
    max_text_length: Optional[int] = Query(1000, ge=1, description="Maximum text length to return"),
):
    try:
        documents = await rag_ops.list_documents_paginated(
            limit=limit+1, # Fetch one extra to check for more results
            offset=offset,
            max_text_length=max_text_length
        )
        has_more = len(documents) > limit
        documents = dict(list(documents.items())[:limit]) # Limit the results to the requested count

        return ListDocumentsResponse(
            documents=documents,
            count=len(documents),
            next_offset=offset + limit if has_more else None
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=5000, loop="asyncio")
