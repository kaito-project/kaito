---
title: Proposal for new model support
authors:
  - "Abhishek Sheth"
reviewers:
  - "KAITO contributor"
creation-date: 2025-10-01
last-updated: 2025-10-01
status: provisional
---

# Title

Add [Google Gemma 3 4B-Instruct](https://huggingface.co/google/gemma-3-4b-it) to KAITO supported model list

## Glossary

N/A

## Summary

- **Model description**: Gemma 3 4B-Instruct is Google's 4-billion parameter lightweight multimodal language model. For more information, refer to the [Google AI Blog](https://ai.google.dev/gemma/docs/core) and access the model on [Hugging Face](https://huggingface.co/google/gemma-3-4b-it).

- **Model usage statistics**: [Hugging Face](https://huggingface.co/google/gemma-3-4b-it)

- **Model license**: Gemma 3 4B-Instruct is distributed under the Gemma Terms of Use.

Sample YAML:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-gemma-3-4b-instruct
spec:
  resource:
    instanceType: Standard_NV36ads_A10_v5
    labelSelector:
      matchLabels:
        apps: gemma-3-4b-instruct
  inference:
    preset:
      name: gemma-3-4b-instruct
  modelAccessSecret: hf-token-secret
```

## Requirements

The following table describes the basic model characteristics and the resource requirements of running it.

| Field | Notes|
|----|----|
| Family name| Gemma 3|
| Type| `image-text-to-text` |
| Download site| https://huggingface.co/google/gemma-3-4b-it |
| Version| [093f9f3](https://huggingface.co/google/gemma-3-4b-it/commit/093f9f388b31de276ce2de164bdc2081324b9767) |
| Storage size| 60GB |
| GPU count| 1 |
| Total GPU memory| 16GB |
| Per GPU memory | N/A |

## Runtimes

This section describes how to configure the runtime framework to support the inference calls.

| Options | Notes|
|----|----|
| Runtime | HuggingFace Transformers, vLLM |
| Distributed Inference| False |
| Custom configurations| Precision: Auto. Gated model requiring HuggingFace token authentication.|

# History

- [x] 10/01/2025: Open proposal PR.
- [ ] 10/01/2025: Start model integration.
- [ ] 10/01/2025: Complete model support.