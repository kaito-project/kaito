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

Add [Google Gemma 3 27B-Instruct](https://huggingface.co/google/gemma-3-27b-it) to KAITO supported model list

## Glossary

N/A

## Summary

- **Model description**: Gemma 3 27B-Instruct is Google's 27-billion parameter language model. Gemma 3 models are multimodal, handling text and image input and generating text output, with open weights for both pre-trained variants and instruction-tuned variants. Gemma 3 has a large, 128K context window and multilingual support in over 140 languages,. For more information, refer to the [Google AI Blog](https://ai.google.dev/gemma/docs/core) and access the model on [Hugging Face](https://huggingface.co/google/gemma-3-27b-it).

- **Model usage statistics**: [Hugging Face](https://huggingface.co/google/gemma-3-27b-it)

- **Model license**: Gemma 3 27B-Instruct is distributed under the Gemma Terms of Use.

Sample YAML:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-gemma-3-27b-instruct
spec:
  resource:
    instanceType: Standard_NC48ads_A100_v4
    labelSelector:
      matchLabels:
        apps: gemma-3-27b-instruct
  inference:
    preset:
      name: gemma-3-27b-instruct
    accessModes:
      - modelAccessSecret
  modelAccessSecret: hf-token-secret
```

## Requirements

The following table describes the basic model characteristics and the resource requirements of running it.

| Field | Notes|
|----|----|
| Family name| Gemma 3|
| Type| `image-text-to-text` |
| Download site| https://huggingface.co/google/gemma-3-27b-it |
| Version| [005ad34](https://huggingface.co/google/gemma-3-27b-it/commit/005ad3404e59d6023443cb575daa05336842228a) |
| Storage size| 200GB |
| GPU count| 1 (minimum) |
| Total GPU memory| 80GB (total) |
| Per GPU memory | N/A |

## Runtimes

This section describes how to configure the runtime framework to support the inference calls.

| Options | Notes|
|----|----|
| Runtime | HuggingFace Transformers, vLLM |
| Distributed Inference| False |
| Custom configurations| Precision: Auto. Gated model requiring HuggingFace token authentication. |

# History

- [x] 10/01/2025: Open proposal PR.
- [ ] 10/01/2025: Start model integration.
- [ ] 10/01/2025: Complete model support.