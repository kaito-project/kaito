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

KAITO also supports basic CPU preset inference referenced through `inference.preset.name`. CPU auto-provisioning classifies Azure CPU SKUs without architecture checks; AWS CPU inference is not supported. Label-only BYO selection remains available for non-AWS CPU nodes. CPU inference defaults to vLLM. Built-in presets and generic Hugging Face model IDs are allowed when supported by the selected runtime. To use Transformers, explicitly set `metadata.annotations["kaito.sh/runtime"]: transformers` and a declared CPU VM `resource.instanceType`; this reuses the configured `kaito-base` image (no separate Transformers CPU image). Transformers requires a preset with existing Transformers parameters; label-only BYO CPU selection remains vLLM-only.

CPU classification, admission, and generated placement are architecture-agnostic; they do not establish runtime image compatibility. The current vLLM CPU image is based on `vllm/vllm-openai-cpu:v0.30.0-x86_64` and supports only x86_64/amd64. ARM64 runtime support is out of scope and untested: accepting an ARM CPU SKU or node does **not** make it a supported deployment with the current image. No multi-architecture image support is added. Use nodes compatible with the selected runtime image.

### Preset CPU examples

Examples:

- NAP InferenceSet (default CPU `vllm` runtime): [`examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml`](https://github.com/kaito-project/kaito/tree/main/examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml)

- NAP InferenceSet (explicit CPU `transformers` runtime): [`examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_transformers.yaml`](https://github.com/kaito-project/kaito/tree/main/examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_transformers.yaml)

Both examples use `google/gemma-4-E4B-it`. Both request `Standard_D16s_v6`. For Transformers, first accept the gated model license on Hugging Face and create a Secret named `hf-token` in the same namespace with an `HF_TOKEN` key containing a token authorized to download the model. The example references it through `presetOptions.modelAccessSecret`. The runtime annotation on the InferenceSet is propagated when child Workspaces are created, not synchronized to existing Workspaces. CPU Transformers launches one process with `--cpu` and `--device_map=cpu`, and uses the native `/health` probe. Gemma uses `bfloat16`. This is text-only serving, but the existing loader still loads the composite model, including its vision/audio components; size memory accordingly. The example has not been validated with a live Gemma Transformers CPU deployment.

### Basic CPU support matrix

| Scenario | Status | Notes |
|---|---|---|
| Preset InferenceSet on CPU | supported | retained example uses default CPU `vllm` runtime |
| CPU preset with NAP | supported | specify an Azure CPU instance type; runtime image architecture must match |
| Transformers preset inference on CPU | supported | explicit runtime annotation + declared CPU instance type; existing `kaito-base` image and compatible preset |
| Post-load benchmark stage on CPU | skipped | basic CPU paths do not run the benchmark stage |
| Multi-node CPU inference | not supported | basic CPU support is single-node only |

### CPU preset limitations

- **Single-node only**: Distributed inference across multiple nodes is not supported for basic CPU paths.
- **Preset compatibility still depends on model resolution/runtime support**: generic Hugging Face model IDs must resolve through the selected runtime; Transformers needs existing Transformers preset parameters.
- **CPU benchmark stage skipped**: basic CPU inference does not run the post-load benchmark stage.
- **No custom-model autoscaling**: These CPU paths do not attempt to wire autoscaling behavior.
