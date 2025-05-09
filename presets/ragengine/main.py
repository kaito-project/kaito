# Copyright (c) Microsoft Corporation.
# Licensed under the MIT license.

from typing import List, Optional
from vector_store_manager.manager import VectorStoreManager
from embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from embedding.remote_embedding import RemoteEmbeddingModel
from fastapi import FastAPI, HTTPException, Query
from models import (IndexRequest, ListDocumentsResponse, UpdateDocumentRequest,
                    QueryRequest, QueryResponse, Document, HealthStatus, DeleteDocumentRequest,
                    DeleteDocumentResponse, UpdateDocumentResponse)
from vector_store.faiss_store import FaissVectorStoreHandler

from ragengine.config import (REMOTE_EMBEDDING_URL, REMOTE_EMBEDDING_ACCESS_SECRET,
                              EMBEDDING_SOURCE_TYPE, LOCAL_EMBEDDING_MODEL_ID, DEFAULT_VECTOR_DB_PERSIST_DIR)
from urllib.parse import unquote
import os

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
    response_model=List[Document],
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
            Document(doc_id=doc_id, text=doc.text, metadata=doc.metadata)
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
      "top_k": 5,
      "llm_params": {"temperature": 0.7, "max_tokens": 2048},
      "rerank_params": {"top_n": 3}  # ⚠️ Experimental Feature
    }
    ```

    ## Experimental Warning:
    - The `rerank_params` option is **experimental** and may cause the query to fail.
    - If `LLMRerank` produces an invalid or unparsable response, an **error will be raised**.
    - Expected format:
      ```
      Answer:
      Doc: 9, Relevance: 7
      Doc: 3, Relevance: 4
      Doc: 7, Relevance: 3
      ```
    - If reranking fails, the request will not return results and will instead raise an error.

    ## Response Example:
    ```json
    {
      "response": "...",
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
    except HTTPException as http_exc:
        # Preserve HTTP exceptions like 422 from reranker
        raise http_exc
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
      "documents": [
        {
          "doc_id": "123456",
          "text": "Sample document text.",
          "metadata": {"author": "John Doe"},
          "is_truncated": true
        },
        {
          "doc_id": "123457",
          "text": "Another document text.",
          "metadata": {"author": "Jane Doe"},
          "is_truncated": false
        }
      ],
      "count": 5
    }
    ```
    """,
)
async def list_documents_in_index(
    index_name: str,
    limit: int = Query(10, ge=1, le=100, description="Maximum number of documents to return"),
    offset: int = Query(0, ge=0, description="Starting point for the document list"),
    max_text_length: Optional[int] = Query(
        1000, 
        ge=1, 
        description="Maximum text length to return **per document**. This does not impose a limit on the total length of all documents returned."
    ),
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
            limit=limit,
            offset=offset,
            max_text_length=max_text_length
        )

        return ListDocumentsResponse(
            documents=documents,
            count=len(documents)
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post(
    "/indexes/{index_name}/documents",
    response_model=UpdateDocumentResponse,
    summary="Update documents in an Index",
    description="""
    Update document in an Index.

    ## Request Example:
    ```json
    POST /indexes/test_index/documents
    {"documents": [{"doc_id": "sampleid", "text": "Sample document text.", "metadata": {"author": "John Doe", "category": "example"}}]}
    ```

    ## Response Example:
    ```json
    {
        "updated_documents": [{"doc_id": "sampleid", "text": "Sample document text.", "metadata": {"author": "John Doe", "category": "example"}}],
        "unchanged_documents": [],
        "not_found_documents": []
    },
    ```
    """,
)
async def update_documents_in_index(
    index_name: str,
    request: UpdateDocumentRequest,
):
    try:
        return await rag_ops.update_documents(
            index_name=index_name,
            documents=request.documents
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post(
    "/indexes/{index_name}/documents/delete",
    response_model=DeleteDocumentResponse,
    summary="Delete documents in an Index",
    description="""
    Delete document in an Index by their ids.

    ## Request Example:
    ```json
    POST /indexes/test_index/documents/delete
    {"doc_ids": ["doc_id_1", "doc_id_2", "doc_id_3"]}
    ```

    ## Response Example:
    ```json
    {
        "deleted_doc_ids": ["doc_id_1", "doc_id_2"],
        "not_found_doc_ids": ["doc_id_3"]
    },
    ```
    """,
)
async def delete_documents_in_index(
    index_name: str,
    request: DeleteDocumentRequest,
):
    try:
        return await rag_ops.delete_documents(
            index_name=index_name,
            doc_ids=request.doc_ids
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post(
    "/persist/{index_name}",
    summary="Persist Index Data to Disk",
    description="""
    Persist the existing index data to disk at a specified location. This ensures that indexed data is saved.

    ## Request Example:
    ```
    POST /persist/example_index?path=./custom_path
    ```

    If no path is provided, the index will be persisted in the default directory.

    ## Response Example:
    ```json
    {
      "message": "Successfully persisted index example_index to ./custom_path/example_index."
    }
    ```
    """
)
async def persist_index(
        index_name: str,
        path: str = Query(DEFAULT_VECTOR_DB_PERSIST_DIR, description="Path where the index will be persisted")
):  # TODO: Provide endpoint for loading existing index(es)
    # TODO: Extend support for other vector databases/integrations besides FAISS
    try:
        # Append index to save path to prevent saving conflicts/overwriting
        path = os.path.join(path, index_name) if path == DEFAULT_VECTOR_DB_PERSIST_DIR else path
        await rag_ops.persist(index_name, path)
        return {"message": f"Successfully persisted index {index_name} to {path}."}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Persistence failed: {str(e)}")

@app.post(
    "/load/{index_name}",
    summary="Load Index Data from Disk",
    description="""
    Load an existing index from disk at a specified location.

    ## Request Example:
    ```
    POST /load/example_index?path=./custom_path/example_index
    ```

    If no path is provided, will attempt to load from the default directory.

    ## Response Example:
    ```json
    {
      "message": "Successfully loaded index example_index from ./custom_path/example_index."
    }
    ```
    """
)
async def load_index(
        index_name: str,
        path: Optional[str] = Query(None, description="Path to load the index from"),
        overwrite: bool = Query(False, description="Overwrite the existing index if it already exists")
):
    # If no path is provided, use the default directory joined with index_name.
    if path is None:
        path = os.path.join(DEFAULT_VECTOR_DB_PERSIST_DIR, index_name)
    try:
        await rag_ops.load(index_name, path, overwrite)
        return {"message": f"Successfully loaded index {index_name} from {path}."}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Loading failed: {str(e)}")

@app.on_event("shutdown")
async def shutdown_event():
    """ Ensure the client is properly closed when the server shuts down. """
    await rag_ops.shutdown()

if __name__ == "__main__":
    # DEBUG: Arize Phoenix
    # import llama_index.core
    # llama_index.core.set_global_handler("arize_phoenix")
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=5000, loop="asyncio")
