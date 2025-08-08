---
title: Retrieval-Augmented Generation (RAG)
---

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