---
title: Custom Model Integration
---

## Using the KAITO base image

The KAITO base image includes both HuggingFace and vLLM runtime libraries along with corresponding FastAPI server scripts. This provides a convenient way to run **any** HuggingFace model using the **HuggingFace** runtime.

Note that the vLLM runtime is not supported for arbitrary custom model deployment.

Here is a **[sample deployment YAML](https://github.com/kaito-project/kaito/tree/main/examples/custom-model-integration/custom-model-deployment.yaml)**. To use it:
1. Specify the HuggingFace model ID in the container command.
2. For models that require a HuggingFace token to download, users need to add the token to the specified secret.

The script downloads model weights during server bootstrap, eliminating the need to pre-bake them into the container image.

## Limitations

- **Hugging Face runtime only**: Only HuggingFace runtime is supported for custom models. Inference performance may be slower compared to vLLM runtime if the model supports both.
- **No multi-node inference**: Distributed inference across multiple nodes is not supported for custom model.
- **No autoscaling**: KAITO autoscaling relies on metrics exposed by vLLM runtime, they are unavailable in HuggingFace runtime.
- **No presets**: Users must manually modify the command line in the pod template for parameter changes.

## Basic CPU preset support

CPU preset inference supports **vLLM only**, with the `vllm` feature gate enabled.
Declare a non-GPU, non-ARM Azure VM SKU in `resource.instanceType`, such as
`Standard_D16s_v6`. Label-only BYO CPU selection, custom-template CPU inference,
Transformers CPU inference, AWS CPU, and ARM64 are not supported. The CPU image
uses `vllm/vllm-openai-cpu:v0.30.0-x86_64`.

See the [Gemma vLLM CPU InferenceSet example](https://github.com/kaito-project/kaito/tree/main/examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml).
Built-in presets and generic Hugging Face model IDs are supported subject to vLLM
compatibility and available memory. For gated models, accept the model license
and configure `presetOptions.modelAccessSecret` with an `HF_TOKEN` secret.

Basic CPU inference is single-node only and skips the post-load benchmark stage.
CPU custom-model autoscaling is not provided. Live model loading and ARM64
runtime compatibility have not been validated by these unit tests.
