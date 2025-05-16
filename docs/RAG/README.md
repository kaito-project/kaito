# Kaito RAGEngine

This document presents how to use the Kaito `ragengine` Custom Resource Definition (CRD) for retrieval-augumented generatoin workflow. By creating a RAGEngine resource, you can quickly stand up a service that indexes documents and queries them in conjunction with an existing LLM inference endpoint—no need to custom-build pipelines. This enables your large language model to answer questions based on your own private content.

## Installation

Please check the installation guidance [here](./RAGEngine-installation.md) for deployment using Azure CLI.

## Usage

### Prerequisite
Before creating a RAGEngine, ensure you have an accessible model inference endpoint. This endpoint can be:

1.	A model deployed through Kaito Workspace CRD (e.g., a local Hugging Face model, a vLLM instance, etc.).
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
    instanceType: "Standard_NC6s_v3"  
    labelSelector:
      matchLabels:
        apps: ragengine-example
  embedding:  
    local:
      modelID: "BAAI/bge-small-en-v1.5"       
  inferenceService:  
    url: "<inference-url>/v1/completions" 
```

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

### Apply the Manifest
After you create your YAML configuration, run:
```sh
kubectl apply -f examples/RAG/kaito_ragengine_phi_3.yaml
```

