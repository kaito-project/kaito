---
title: API Definitions and Examples
---

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

## Creating an Index With Documents

To add documents to an index or create a new index, use the `/index` API route. This endpoint accepts a POST request with the index name and a list of documents to be indexed.

### Create Index Request

```json
POST /index
{
  "index_name": "rag_index",
  "documents": [
    {
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data.",
      "metadata": {
        "author": "kaito",
      }
    }
  ]
}
```

- `index_name`: The name of the index to create or update.
- `documents`: A list of documents, each with a `text` field and optional `metadata`.

### Create Index Response

```json
[
  {
    "doc_id": "123456",
    "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data.",
    "hash_value": "text_hash_value",
    "metadata": {
      "author": "kaito",
    },
    "is_truncated": false
  }
]
```

Each returned document includes its unique `doc_id`, the original text, metadata, and a flag indicating if the text was truncated. The `doc_id` will be important for document update/delete calls. 


### Splitting Documents with CodeSplitter

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

## List Documents

To retrieve a paginated list of documents from a specific index, use the `/indexes/{index_name}/documents` API route. This endpoint accepts a GET request with optional query parameters for pagination, text truncation, and metadata filtering.

### List Documents Request

```
GET /indexes/rag_index/documents?limit=5&offset=0&max_text_length=500
```

- `limit`: (optional) Maximum number of documents to return (default: 10, max: 100).
- `offset`: (optional) Starting point for the document list (default: 0).
- `max_text_length`: (optional) Maximum text length to return per document (default: 1000).
- `metadata_filter`: (optional) A JSON string representing key-value pairs to filter documents by their metadata.

### List Documents Response

```json
{
  "documents": [
    {
      "doc_id": "123456",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data.",
      "hash_value": "text_hash_value",
      "metadata": {
        "author": "kaito",
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
GET /indexes/rag_index/documents?metadata_filter={"author":"kaito"}
```


## Updating Documents

To update existing documents in a specific index, use the `/indexes/{index_name}/documents` API route. This endpoint accepts a POST request with the index name in the URL and a list of documents to update in the request body.

### Update Documents Request

```json
POST /indexes/rag_index/documents
{
  "documents": [
    {
      "doc_id": "123456",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data. Adding an information retrieval system gives you control over grounding data used by an LLM when it formulates a response.",
      "hash_value": "text_hash_value",
      "metadata": {
        "author": "kaito",
      }
    }
  ]
}
```

- `doc_id`: The unique identifier of the document to update.
- `text`: The new or updated text for the document.
- `metadata`: (Optional) Updated metadata for the document.

### Update Documents Response

```json
{
  "updated_documents": [
    {
      "doc_id": "123456",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model (LLM) like ChatGPT by adding an information retrieval system that provides grounding data. Adding an information retrieval system gives you control over grounding data used by an LLM when it formulates a response.",
      "hash_value": "text_hash_value",
      "metadata": {
        "author": "kaito",
      }
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

## Delete Documents


To delete one or more documents from a specific index, use the `/indexes/{index_name}/documents/delete` API route. This endpoint accepts a POST request with the index name in the URL and a list of document IDs to delete in the request body.

### Delete Documents Request

```json
POST /indexes/rag_index/documents/delete
{
  "doc_ids": ["123456"]
}
```

- `doc_ids`: A list of document IDs to delete from the specified index.

### Delete Documents Response

```json
{
  "deleted_doc_ids": ["123456"],
  "not_found_doc_ids": []
}
```

- `deleted_doc_ids`: Document IDs that were successfully deleted.
- `not_found_doc_ids`: Document IDs that were not found in the index.

Use this endpoint to remove documents that are no longer needed from your index.

## Persist Index

To save (persist) the data of an index to disk, use the `/persist/{index_name}` API route. This endpoint accepts a POST request with the index name in the URL and an optional `path` query parameter specifying where to save the index data.

### Persist Index Request

```
POST /persist/rag_index?path=./custom_path
```

- `index_name`: The name of the index to persist.
- `path`: (optional) The directory path where the index will be saved. If not provided, the default directory is used.

### Persist Index Response

```json
{
  "message": "Successfully persisted index rag_index to ./custom_path/rag_index."
}
```

Use this endpoint to ensure your indexed data is safely stored on disk.

---

## Load Index

To load an existing index from disk, use the `/load/{index_name}` API route. This endpoint accepts a POST request with the index name in the URL, an optional `path` query parameter specifying where to load the index from, and an optional `overwrite` flag.

### Load Index Request

```
POST /load/rag_index?path=./custom_path/rag_index
```

- `index_name`: The name of the index to load.
- `path`: (optional) The path to load the index from. If not provided, the default directory is used.
- `overwrite`: (optional, default: false) If true, will overwrite the existing index if it already exists in memory.

### Load Index Response

```json
{
  "message": "Successfully loaded index rag_index from ./custom_path/rag_index."
}
```

Use this endpoint to restore previously persisted indexes into memory for querying and updates.

## Delete Index

To delete an entire index and all of its documents, use the `/indexes/{index_name}` API route. This endpoint accepts a DELETE request with the index name in the URL. Deleting an index is irreversible and will remove all associated documents from memory.

### Delete Index Request

```
DELETE /indexes/rag_index
```

- `index_name`: The name of the index to delete.

### Delete Index Response

```json
{
  "message": "Successfully deleted index rag_index."
}
```

Use this endpoint to permanently remove an index and all its data when it is no longer needed.

## Query Index

To query a specific index for relevant documents, use the `/query` API route. This endpoint accepts a POST request with the index name, query string, and optional parameters for result count, and LLM generation.

### Query Index Request

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

### Query Index Response

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
        "author": "kaito",
      }
    }
  ],
  "metadata": {
    "2853a565-8c1f-4982-acaa-a0ab52691435": {
      "author": "kaito",
    }
  }
}
```

- `response`: The generated answer or summary from the LLM (if enabled).
- `source_nodes`: List of source nodes with their text, score, and metadata.
- `metadata`: Additional metadata about the query or response.

Use this endpoint to retrieve relevant information from your indexed documents and optionally generate answers using an LLM.

## OpenAI-Compatible Chat Completions

The RAGEngine provides an OpenAI-compatible chat completions endpoint at `/v1/chat/completions`. This endpoint allows you to use RAG capabilities with the familiar OpenAI API format, making it easy to integrate with existing applications that use OpenAI's chat completions.

### RAG Bypass Conditions

The chat completions endpoint automatically determines whether to use RAG or pass requests directly to the LLM based on the request content. Requests will **bypass RAG** and be sent directly to the LLM in the following cases:

1. **No Index Specified**: When `index_name` is not provided in the request, the system treats it as a standard LLM request without retrieval.

2. **Tool/Function Calls**: When the request contains `tools` or `functions` parameters, indicating the use of function calling capabilities.

3. **Unsupported Message Roles**: When messages contain roles other than `user`, `system`, `assistant`, or `developer`. 

4. **Non-Text Content**: When user messages contain non-text content types (such as images, audio, or other media formats).

When any of these conditions are met, the request is passed through directly to the underlying LLM without document retrieval or augmentation, ensuring full compatibility with standard OpenAI API usage patterns.

### Chat Completions Request

```json
POST /v1/chat/completions
{
  "index_name": "rag_index",
  "model": "example_model",
  "messages": [
    {"role": "system", "content": "You are a knowledgeable assistant."},
    {"role": "user", "content": "What is RAG?"}
  ],
  "temperature": 0.7,
  "max_tokens": 2048,
  "top_k": 5
}
```

- `index_name`: (optional) The name of the index to query for relevant documents. If not included, the request will be sent to the LLM directly with no additional context.
- `model`: The model identifier (for compatibility).
- `messages`: Array of message objects with `role` and `content` fields.
- `temperature`: (optional) Controls randomness in the response (0.0 to 1.0).
- `max_tokens`: (optional) Maximum number of tokens to generate.
- `top_k`: (optional) Number of top documents to retrieve from the index.

### Chat Completions Response

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
        "content": "RAG stands for Retrieval-Augmented Generation. It's an architecture that augments the capabilities of a Large Language Model by adding an information retrieval system that provides grounding data."
      },
      "finish_reason": "stop"
    }
  ],
  "source_nodes": [
    {
      "doc_id": "123456",
      "node_id": "2853a565-8c1f-4982-acaa-a0ab52691435",
      "text": "Retrieval Augmented Generation (RAG) is an architecture that augments the capabilities of a Large Language Model...",
      "score": 0.95,
      "metadata": {
        "author": "kaito",
      }
    }
  ]
}
```

- `id`: Unique identifier for the chat completion.
- `object`: Type of object returned (`chat.completion`).
- `created`: Unix timestamp of when the completion was created.
- `model`: The model used for the completion.
- `choices`: Array of completion choices (typically one).
- `source_nodes`: (RAG-specific) List of source documents that informed the response.

Use this endpoint to integrate RAG capabilities into applications that already use OpenAI's chat completions API format.

## Example Python Client

You can leverage the [example_rag_client.py](./example_rag_client.py) as a starting point for a rag client with inputs that match the route documentation above.
