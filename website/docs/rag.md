# Retrieval-Augmented Generation (RAG)

This document presents how to use the KAITO `ragengine` Custom Resource Definition (CRD) for retrieval-augumented generatoin workflow. By creating a RAGEngine resource, you can quickly stand up a service that indexes documents and queries them in conjunction with an existing LLM inference endpoint—no need to custom-build pipelines. This enables your large language model to answer questions based on your own private content.

## Installation

> Be sure you've cloned this repo and followed [kaito workspace installation](./installation.md) if you plan to use local embedding model. RAGEngine needs the gpu-provisioner component to provision GPU nodes.

```bash
helm install ragengine ./charts/kaito/ragengine --namespace kaito-ragengine --create-namespace
```

## Verify installation
You can run the following commands to verify the installation of the controllers were successful.

Check status of the Helm chart installations.

```bash
helm list -n kaito-ragengine
```

Check status of the `ragengine`.

```bash
kubectl describe deploy ragengine -n kaito-ragengine
```

## Clean up

```bash
helm uninstall kaito-ragengine
```

## Usage

### Prerequisite
Before creating a RAGEngine, ensure you have an accessible model inference endpoint. This endpoint can be:

1.	A model deployed through KAITO Workspace CRD (e.g., a local Hugging Face model, a vLLM instance, etc.).
2.	An external API (e.g., Huggingface service or other REST-based LLM providers).

### Define the RAGEngine
Create a YAML manifest defining your RAGEngine. Key fields under spec include:

Embedding: how to generate vector embeddings for your documents. You may choose remote or local (one must be left unset if you pick the other):

```yaml
embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
```
InferenceService: points to the LLM endpoint that RAGEngine will call for final text generation.
```yaml
inferenceService:
  url: "<inference-url>/v1/completions"
```
Users also need to specify the GPU SKU used for inference in the `compute` spec. For example,

```yaml
apiVersion: kaito.sh/v1alpha1
kind: RAGEngine
metadata:
  name: ragengine-start
spec:
  compute:
    instanceType: "Standard_NC4as_T4_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
```

### Apply the manifest
After you create your YAML configuration, run:
```sh
kubectl apply -f examples/RAG/kaito_ragengine_phi_3.yaml
```

## API definitions and examples

A **RAGEngine index** is a logical collection that organizes and stores your documents for retrieval-augmented generation workflows. The relationship between indexes, documents, and document nodes is as follows:

- **Index**: An index is a named container that holds a set of documents. Each index is independent and can be created, updated, queried, persisted, loaded, or deleted via the API.

- **Documents**: Documents are the primary units of content that you add to an index. Each document contains a `text` field (the content to be indexed) and optional `metadata` (such as author, source, or custom tags). When you add documents to an index, each is assigned a unique `doc_id`.

- **Document Nodes**: When a document is ingested, it is automatically split into smaller chunks called *nodes*. The splitting strategy depends on the document type and metadata:
  - By default, documents are split into sentences.
  - If you specify code-aware splitting (using the `split_type` and `language` metadata), the document is split into code blocks or logical code units.
  - Each node represents a chunk of text that is indexed and can be retrieved as part of a query.

**How it works in practice:**
- When you index a document, it is divided into nodes for efficient retrieval and semantic search.
- When you query an index, the engine retrieves the most relevant nodes (not necessarily whole documents) and can use them to generate answers or summaries.
- The `source_nodes` field in query responses contains the actual nodes that matched your query, along with their scores and metadata.

This design enables fine-grained retrieval and more accurate, context-aware responses from your LLM-powered applications.

### Creating an Index With Documents

To add documents to an index or create a new index, use the `/index` API route. This endpoint accepts a POST request with the index name and a list of documents to be indexed.

**Request Example:**

```json
POST /index
{
  "index_name": "rag_index",
  "documents": [
    {
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data.",
      "metadata": {
        "author": "Microsoft",
        "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
      }
    }
  ]
}
```

- `index_name`: The name of the index to create or update.
- `documents`: A list of documents, each with a `text` field and optional `metadata`.

**Response Example:**

```json
[
  {
    "doc_id": "123456",
    "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data.",
    "hash_value": "text_hash_value",
    "metadata": {
      "author": "Microsoft",
      "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
    },
    "is_truncated": false
  }
]
```

Each returned document includes its unique `doc_id`, the original text, metadata, and a flag indicating if the text was truncated. The `doc_id` will be important for document update/delete calls. 


#### Splitting Documents with CodeSplitter

By default, RAGEngine splits documents into sentences. However, you can instruct the engine to split documents using the `CodeSplitter` (for code-aware chunking) by providing metadata in your API request.

To use the `CodeSplitter`, set the `split_type` to `"code"` and specify the programming language in the `language` field of the document metadata. For example, when calling the RAGEngine API to index documents:

```json
{
  "documents": [
    {
      "text": "def foo():\n    return 42\n\n# Another function\ndef bar():\n    pass",
      "metadata": {
        "split_type": "code",
        "language": "python"
      }
    }
  ]
}
```

This instructs the RAGEngine to use code-aware splitting for the provided document. If `split_type` is not set or set to any other value, sentence splitting will be used by default.

### List Documents

To retrieve a paginated list of documents from a specific index, use the `/indexes/{index_name}/documents` API route. This endpoint accepts a GET request with optional query parameters for pagination, text truncation, and metadata filtering.

**Request Example:**

```
GET /indexes/rag_index/documents?limit=5&offset=0&max_text_length=500
```

- `limit`: (optional) Maximum number of documents to return (default: 10, max: 100).
- `offset`: (optional) Starting point for the document list (default: 0).
- `max_text_length`: (optional) Maximum text length to return per document (default: 1000).
- `metadata_filter`: (optional) A JSON string representing key-value pairs to filter documents by their metadata.

**Response Example:**

```json
{
  "documents": [
    {
      "doc_id": "123456",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data.",
      "hash_value": "text_hash_value",
      "metadata": {
        "author": "Microsoft",
        "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
      },
      "is_truncated": false
    }
  ],
  "count": 1
}
```

Each document in the response includes its unique `doc_id`, the (possibly truncated) text, metadata, and a flag indicating if the text was truncated.

**Note:**  
If you want to filter documents by metadata, provide the `metadata_filter` parameter as a JSON string. For example:

```
GET /indexes/rag_index/documents?metadata_filter={"author":"Microsoft"}
```


### Updating Documents

To update existing documents in a specific index, use the `/indexes/{index_name}/documents` API route. This endpoint accepts a POST request with the index name in the URL and a list of documents to update in the request body.

**Request Example:**

```json
POST /indexes/rag_index/documents
{
  "documents": [
    {
      "doc_id": "123456",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data. Adding an information retrieval system gives you control over grounding data used by an LLM when it formulates a response.",
      "hash_value": "text_hash_value",
      "metadata": {
        "author": "Microsoft",
        "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
      },
    }
  ]
}
```

- `doc_id`: The unique identifier of the document to update.
- `text`: The new or updated text for the document.
- `metadata`: (Optional) Updated metadata for the document.

**Response Example:**

```json
{
  "updated_documents": [
    {
      "doc_id": "123456",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data. Adding an information retrieval system gives you control over grounding data used by an LLM when it formulates a response.",
      "hash_value": "text_hash_value",
      "metadata": {
        "author": "Microsoft",
        "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
      },
    }
  ],
  "unchanged_documents": [],
  "not_found_documents": []
}
```

- `updated_documents`: Documents that were successfully updated.
- `unchanged_documents`: Documents that were provided but did not require changes.
- `not_found_documents`: Documents with IDs that were not found in the index.

Use this endpoint to keep your indexed documents up to date with the latest content or metadata.

### Delete Documents


To delete one or more documents from a specific index, use the `/indexes/{index_name}/documents/delete` API route. This endpoint accepts a POST request with the index name in the URL and a list of document IDs to delete in the request body.

**Request Example:**

```json
POST /indexes/rag_index/documents/delete
{
  "doc_ids": ["123456"]
}
```

- `doc_ids`: A list of document IDs to delete from the specified index.

**Response Example:**

```json
{
  "deleted_doc_ids": ["123456"],
  "not_found_doc_ids": []
}
```

- `deleted_doc_ids`: Document IDs that were successfully deleted.
- `not_found_doc_ids`: Document IDs that were not found in the index.

Use this endpoint to remove documents that are no longer needed from your index.

### Persist Index

To save (persist) the data of an index to disk, use the `/persist/{index_name}` API route. This endpoint accepts a POST request with the index name in the URL and an optional `path` query parameter specifying where to save the index data.

**Request Example:**

```
POST /persist/rag_index?path=./custom_path
```

- `index_name`: The name of the index to persist.
- `path`: (optional) The directory path where the index will be saved. If not provided, the default directory is used.

**Response Example:**

```json
{
  "message": "Successfully persisted index rag_index to ./custom_path/rag_index."
}
```

Use this endpoint to ensure your indexed data is safely stored on disk.

---

### Load Index

To load an existing index from disk, use the `/load/{index_name}` API route. This endpoint accepts a POST request with the index name in the URL, an optional `path` query parameter specifying where to load the index from, and an optional `overwrite` flag.

**Request Example:**

```
POST /load/rag_index?path=./custom_path/rag_index
```

- `index_name`: The name of the index to load.
- `path`: (optional) The path to load the index from. If not provided, the default directory is used.
- `overwrite`: (optional, default: false) If true, will overwrite the existing index if it already exists in memory.

**Response Example:**

```json
{
  "message": "Successfully loaded index rag_index from ./custom_path/rag_index."
}
```

Use this endpoint to restore previously persisted indexes into memory for querying and updates.

### Delete Index

To delete an entire index and all of its documents, use the `/indexes/{index_name}` API route. This endpoint accepts a DELETE request with the index name in the URL. Deleting an index is irreversible and will remove all associated documents from memory.

**Request Example:**

```
DELETE /indexes/rag_index
```

- `index_name`: The name of the index to delete.

**Response Example:**

```json
{
  "message": "Successfully deleted index rag_index."
}
```

Use this endpoint to permanently remove an index and all its data when it is no longer needed.

### Query Index

To query a specific index for relevant documents, use the `/query` API route. This endpoint accepts a POST request with the index name, query string, and optional parameters for result count, and LLM generation.

**Request Example:**

```json
POST /query
{
  "index_name": "rag_index",
  "query": "What is RAG?",
  "top_k": 5,
  "llm_params": {
    "temperature": 0.7,
    "max_tokens": 2048
  }
}
```

- `index_name`: The name of the index to query.
- `query`: The query string.
- `top_k`: (optional) Number of top documents to retrieve (default: 5).
- `llm_params`: (optional) Parameters for LLM-based generation (e.g., temperature, max_tokens).

**Response Example:**

```json
{
  "response": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model...",
  "source_nodes": [
    {
      "doc_id": "123456",
      "node_id": "2853a565-8c1f-4982-acaa-a0ab52691435",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model...",
      "score": 0.95,
      "metadata": {
        "author": "Microsoft",
        "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
      },
    }
  ],
  "metadata": {
    "2853a565-8c1f-4982-acaa-a0ab52691435": {
      "author": "Microsoft",
      "source": "https://learn.microsoft.com/en-us/azure/search/retrieval-augmented-generation-overview?tabs=docs"
    }
  }
}
```

- `response`: The generated answer or summary from the LLM (if enabled).
- `source_nodes`: List of source nodes with their text, score, and metadata.
- `metadata`: Additional metadata about the query or response.

Use this endpoint to retrieve relevant information from your indexed documents and optionally generate answers using an LLM.


## Example Client

You can leverage the [example_rag_client.py](./example_rag_client.py) as a starting point for a rag client with inputs that match the route documentation above.
