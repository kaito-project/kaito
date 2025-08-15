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


import json
import os
import time
from urllib.parse import unquote

from embedding.huggingface_local_embedding import LocalHuggingFaceEmbedding
from embedding.remote_embedding import RemoteEmbeddingModel
from fastapi import FastAPI, HTTPException, Query, Request
from models import (
    ChatCompletionResponse,
    DeleteDocumentRequest,
    DeleteDocumentResponse,
    Document,
    HealthStatus,
    IndexRequest,
    ListDocumentsResponse,
    QueryRequest,
    QueryResponse,
    UpdateDocumentRequest,
    UpdateDocumentResponse,
)

# Import Prometheus client for metrics collection
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest
from starlette.responses import Response
from vector_store.faiss_store import FaissVectorStoreHandler
from vector_store_manager.manager import VectorStoreManager

from ragengine.config import (
    DEFAULT_VECTOR_DB_PERSIST_DIR,
    EMBEDDING_SOURCE_TYPE,
    LOCAL_EMBEDDING_MODEL_ID,
    REMOTE_EMBEDDING_ACCESS_SECRET,
    REMOTE_EMBEDDING_URL,
)
from ragengine.metrics.prometheus_metrics import (
    MODE_LOCAL,
    MODE_REMOTE,
    STATUS_FAILURE,
    STATUS_SUCCESS,
    e2e_request_latency_seconds,
    e2e_request_total,
    num_requests_running,
    rag_avg_source_score,
    rag_chat_latency,
    rag_chat_requests_total,
    rag_delete_latency,
    rag_delete_requests_total,
    rag_index_latency,
    rag_index_requests_total,
    rag_indexes_delete_document_latency,
    rag_indexes_delete_document_requests_total,
    rag_indexes_document_latency,
    rag_indexes_document_requests_total,
    rag_indexes_latency,
    rag_indexes_requests_total,
    rag_indexes_update_document_latency,
    rag_indexes_update_document_requests_total,
    rag_load_latency,
    rag_load_requests_total,
    rag_lowest_source_score,
    rag_persist_latency,
    rag_persist_requests_total,
    rag_query_latency,
    rag_query_requests_total,
)

# Import Prometheus client for metrics collection


app = FastAPI()


@app.middleware("http")
async def track_requests(request: Request, call_next):
    tracked_paths = [
        "/query",
        "/index",
        "/indexes",
        "/persist",
        "/load",
        "/v1/chat/completions",
    ]

    should_track = any(request.url.path.startswith(path) for path in tracked_paths)

    if not should_track:
        return await call_next(request)

    num_requests_running.inc()
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        response = await call_next(request)
        status = STATUS_SUCCESS
        return response
    except Exception:
        raise
    finally:
        num_requests_running.dec()
        e2e_request_latency_seconds.labels(status=status).observe(
            time.perf_counter() - start_time
        )
        e2e_request_total.labels(status=status).inc()


# Initialize embedding model
if EMBEDDING_SOURCE_TYPE.lower() == MODE_LOCAL:
    embedding_manager = LocalHuggingFaceEmbedding(LOCAL_EMBEDDING_MODEL_ID)
elif EMBEDDING_SOURCE_TYPE.lower() == MODE_REMOTE:
    embedding_manager = RemoteEmbeddingModel(
        REMOTE_EMBEDDING_URL, REMOTE_EMBEDDING_ACCESS_SECRET
    )
else:
    raise ValueError("Invalid Embedding Type Specified (Must be Local or Remote)")

# Initialize vector store
# TODO: Dynamically set VectorStore from EnvVars (which ultimately comes from CRD StorageSpec)
vector_store_handler = FaissVectorStoreHandler(embedding_manager)

# Initialize RAG operations
rag_ops = VectorStoreManager(vector_store_handler)


@app.get("/metrics", tags=["Monitoring"])
async def metrics():
    """
    Expose Prometheus metrics.
    Returns metrics data in Prometheus exposition format.
    """
    try:
        # Serialize and return all Prometheus metrics collected in this process
        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)

    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


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
            raise HTTPException(
                status_code=500, detail="Embedding manager not initialized"
            )

        if rag_ops is None:
            raise HTTPException(
                status_code=500, detail="RAG operations not initialized"
            )

        return HealthStatus(status="Healthy")

    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post(
    "/index",
    response_model=list[Document],
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
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        doc_ids = await rag_ops.index(request.index_name, request.documents)
        documents = [
            Document(doc_id=doc_id, text=doc.text, metadata=doc.metadata)
            for doc_id, doc in zip(doc_ids, request.documents, strict=False)
        ]
        status = STATUS_SUCCESS
        return documents
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
    finally:
        # Record metrics once in finally block
        rag_index_requests_total.labels(status=status).inc()
        rag_index_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


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
      "source_nodes": [{"doc_id": "doc1", "node_id": "node1", "text": "RAG explanation...", "score": 0.95, "metadata": {}}],
      "metadata": {}
    }
    ```
    """,
)
async def query_index(request: QueryRequest):
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        llm_params = (
            request.llm_params or {}
        )  # Default to empty dict if no params provided
        rerank_params = (
            request.rerank_params or {}
        )  # Default to empty dict if no params provided

        result_dict = await rag_ops.query(
            request.index_name, request.query, request.top_k, llm_params, rerank_params
        )

        result = QueryResponse(
            response=result_dict["response"],
            source_nodes=result_dict["source_nodes"],
            metadata=result_dict.get("metadata", {}),
        )

        # Record source retrieval quality metrics
        if result.source_nodes and result.response:
            lowest_score_node = min(result.source_nodes, key=lambda x: x.score)
            rag_lowest_source_score.observe(lowest_score_node.score)

            scores = [node.score for node in result.source_nodes]
            avg_score = sum(scores) / len(scores)
            rag_avg_source_score.observe(avg_score)

        status = STATUS_SUCCESS
        return result
    except HTTPException as http_exc:
        # Preserve HTTP exceptions like 422 from reranker
        raise http_exc
    except ValueError as ve:
        raise HTTPException(status_code=400, detail=str(ve))  # Validation issue
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"An unexpected error occurred: {str(e)}"
        )
    finally:
        # Record metrics once in finally block
        rag_query_requests_total.labels(status=status).inc()
        rag_query_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


@app.post(
    "/v1/chat/completions",
    response_model=ChatCompletionResponse,
    summary="OpenAI-Compatible Chat Completions API",
    description="""
    OpenAI-compatible chat completions endpoint with RAG capabilities.

    ## Request Example:
    ```json
    {
      "index_name": "example_index",
      "model": "example_model",
      "messages": [
        {"role": "system", "content": "You are a knowledgeable assistant."},
        {"role": "user", "content": "What is RAG?"}
      ],
      "temperature": 0.7,
      "max_tokens": 2048,
      "top_k": 5,
      "rerank_params": {"top_n": 3}
    }
    ```

    ## Response Example:
    ```json
    {
      "id": "chatcmpl-123",
      "object": "chat.completion",
      "created": 1677652288,
      "model": "example_model",
      "choices": [
        {
          "index": 0,
          "message": {
            "role": "assistant",
            "content": "RAG stands for Retrieval-Augmented Generation..."
          },
          "finish_reason": "stop"
        }
      ],
      "usage": {
        "prompt_tokens": 56,
        "completion_tokens": 31,
        "total_tokens": 87
      },
      "source_nodes": [...]
    }
    ```
    """,
)
async def chat_completions(request: dict):
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status
    try:
        response = await rag_ops.chat_completion(request)
        status = STATUS_SUCCESS
        return response
    except HTTPException as http_exc:
        # Preserve HTTP exceptions like 422 from reranker
        raise http_exc
    except ValueError as ve:
        raise HTTPException(status_code=400, detail=str(ve))  # Validation issue
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"An unexpected error occurred: {str(e)}"
        )
    finally:
        # Record metrics once in finally block
        rag_chat_requests_total.labels(status=status).inc()
        rag_chat_latency.labels(status=status).observe(time.perf_counter() - start_time)


@app.get(
    "/indexes",
    response_model=list[str],
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
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        result = rag_ops.list_indexes()
        status = STATUS_SUCCESS
        return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
    finally:
        # Record metrics once in finally block
        rag_indexes_requests_total.labels(status=status).inc()
        rag_indexes_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


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
    limit: int = Query(
        10, ge=1, le=100, description="Maximum number of documents to return"
    ),
    offset: int = Query(0, ge=0, description="Starting point for the document list"),
    max_text_length: int | None = Query(
        1000,
        ge=1,
        description="Maximum text length to return **per document**. This does not impose a limit on the total length of all documents returned.",
    ),
    metadata_filter: str | None | None = Query(
        None,
        description="Optional metadata filter to apply when listing documents. This should be a dictionary with key-value pairs to match against document metadata.",
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
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        if metadata_filter:
            # Attempt to parse the metadata filter as a JSON string
            try:
                metadata_filter = json.loads(metadata_filter)
            except json.JSONDecodeError:
                raise HTTPException(
                    status_code=400,
                    detail="Invalid metadata filter format. Must be a valid JSON string.",
                )

        # Decode the index_name in case it was URL-encoded by the client
        decoded_index_name = unquote(index_name)
        documents = await rag_ops.list_documents_in_index(
            index_name=decoded_index_name,
            limit=limit,
            offset=offset,
            max_text_length=max_text_length,
            metadata_filter=metadata_filter,
        )

        result = ListDocumentsResponse(documents=documents, count=len(documents))
        status = STATUS_SUCCESS
        return result
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
    finally:
        # Record metrics once in finally block
        rag_indexes_document_requests_total.labels(status=status).inc()
        rag_indexes_document_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


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
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        result = await rag_ops.update_documents(
            index_name=index_name, documents=request.documents
        )
        status = STATUS_SUCCESS
        return result
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
    finally:
        # Record metrics once in finally block
        rag_indexes_update_document_requests_total.labels(status=status).inc()
        rag_indexes_update_document_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


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
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        result = await rag_ops.delete_documents(
            index_name=index_name, doc_ids=request.doc_ids
        )
        status = STATUS_SUCCESS
        return result
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
    finally:
        # Record metrics once in finally block
        rag_indexes_delete_document_requests_total.labels(status=status).inc()
        rag_indexes_delete_document_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


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
    """,
)
async def persist_index(
    index_name: str,
    path: str = Query(
        DEFAULT_VECTOR_DB_PERSIST_DIR,
        description="Path where the index will be persisted",
    ),
):
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        # Append index to save path to prevent saving conflicts/overwriting
        path = (
            os.path.join(path, index_name)
            if path == DEFAULT_VECTOR_DB_PERSIST_DIR
            else path
        )
        await rag_ops.persist(index_name, path)
        status = STATUS_SUCCESS
        return {"message": f"Successfully persisted index {index_name} to {path}."}
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Persistence failed: {str(e)}")
    finally:
        # Record metrics once in finally block
        rag_persist_requests_total.labels(status=status).inc()
        rag_persist_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


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
    """,
)
async def load_index(
    index_name: str,
    path: str | None = Query(None, description="Path to load the index from"),
    overwrite: bool = Query(
        False, description="Overwrite the existing index if it already exists"
    ),
):
    # If no path is provided, use the default directory joined with index_name.
    if path is None:
        path = os.path.join(DEFAULT_VECTOR_DB_PERSIST_DIR, index_name)

    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        await rag_ops.load(index_name, path, overwrite)
        status = STATUS_SUCCESS
        return {"message": f"Successfully loaded index {index_name} from {path}."}
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Loading failed: {str(e)}")
    finally:
        # Record metrics once in finally block
        rag_load_requests_total.labels(status=status).inc()
        rag_load_latency.labels(status=status).observe(time.perf_counter() - start_time)


@app.delete(
    "/indexes/{index_name}",
    summary="Delete the Index",
    description="""
    Delete an existing index

    ## Request Example:
    ```
    DELETE /indexes/test_index
    ```

    ## Response Example:
    ```json
    {
      "message": "Successfully deleted index example_index."
    }
    ```
    """,
)
async def delete_index(index_name: str):
    """
    Deletes an index by its name.

    This function is not exposed via an API endpoint but can be used internally.
    """
    start_time = time.perf_counter()
    status = STATUS_FAILURE  # Default status

    try:
        await rag_ops.delete_index(index_name)
        status = STATUS_SUCCESS
        return {"message": f"Successfully deleted index {index_name}."}
    except HTTPException as http_exc:
        raise http_exc
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Deletion failed: {str(e)}")
    finally:
        # Record metrics once in finally block
        rag_delete_requests_total.labels(status=status).inc()
        rag_delete_latency.labels(status=status).observe(
            time.perf_counter() - start_time
        )


@app.on_event("shutdown")
async def shutdown_event():
    """Ensure the client is properly closed when the server shuts down."""
    await rag_ops.shutdown()


if __name__ == "__main__":
    # DEBUG: Arize Phoenix
    # import llama_index.core
    # llama_index.core.set_global_handler("arize_phoenix")
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=5000, loop="asyncio")
