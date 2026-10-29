---
title: Custom Model Integration
---

## Using the KAITO base image

The KAITO base image includes both HuggingFace and vLLM runtime libraries along with corresponding FastAPI server scripts. This provides a convenient way to run **any** HuggingFace model using the **HuggingFace** runtime.

Note that custom-model deployment remains a narrow, explicitly-scoped path in KAITO.

Here is a **[sample deployment YAML](https://github.com/kaito-project/kaito/tree/main/examples/custom-model-integration/custom-model-deployment.yaml)**. To use it:
1. Specify the HuggingFace model ID in the container command.
2. For models that require a HuggingFace token to download, users need to add the token to the specified secret.

The script downloads model weights during server bootstrap, eliminating the need to pre-bake them into the container image.

## Basic CPU preset support

KAITO also supports basic CPU preset inference referenced through `inference.preset.name`. This path requires the `enableCPUInference` feature gate to be enabled on the workspace manager. CPU inference defaults to vLLM. Built-in presets and generic Hugging Face model IDs are allowed when supported by the selected runtime. To use Transformers, explicitly set `metadata.annotations["kaito.sh/runtime"]: transformers` and an amd64 CPU VM `resource.instanceType`; this reuses the configured `kaito-base` image (no separate Transformers CPU image). Transformers requires a preset with existing Transformers parameters; label-only BYO CPU selection remains vLLM-only.

### Preset CPU examples

Examples:

- NAP InferenceSet (default CPU `vllm` runtime): [`examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml`](https://github.com/kaito-project/kaito/tree/main/examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml)

- NAP InferenceSet (explicit CPU `transformers` runtime): [`examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_transformers.yaml`](https://github.com/kaito-project/kaito/tree/main/examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_transformers.yaml)

Both examples use `google/gemma-4-E4B-it`. Both request `Standard_D16s_v6`. For Transformers, first accept the gated model license on Hugging Face and create a Secret named `hf-token` in the same namespace with an `HF_TOKEN` key containing a token authorized to download the model. The example references it through `presetOptions.modelAccessSecret`. The runtime annotation on the InferenceSet is propagated to child Workspaces. CPU Transformers launches one process with `--cpu` and `--device_map=cpu`, and uses the native `/health` probe. Gemma uses `bfloat16`. This is text-only serving, but the existing loader still loads the composite model, including its vision/audio components; size memory accordingly. The example has not been validated with a live Gemma Transformers CPU deployment.

### Basic CPU support matrix

| Scenario | Status | Notes |
|---|---|---|
| Preset InferenceSet on CPU | supported with `enableCPUInference` | retained example uses default CPU `vllm` runtime |
| CPU preset with NAP | supported with `enableCPUInference` | specify an amd64 CPU instance type |
| Transformers preset inference on CPU | supported with `enableCPUInference` | explicit runtime annotation + amd64 CPU instance type; existing `kaito-base` image and compatible preset |
| Post-load benchmark stage on CPU | skipped | basic CPU paths do not run the benchmark stage |
| Multi-node CPU inference | not supported | basic CPU support is single-node only |

## Limitations

- **Single-node only**: Distributed inference across multiple nodes is not supported for basic CPU paths.
- **Preset compatibility still depends on model resolution/runtime support**: generic Hugging Face model IDs must resolve through the selected runtime; Transformers needs existing Transformers preset parameters.
- **CPU benchmark stage skipped**: basic CPU inference does not run the post-load benchmark stage.
- **No custom-model autoscaling**: These CPU paths do not attempt to wire autoscaling behavior.
