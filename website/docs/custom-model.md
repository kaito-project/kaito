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

## Phase 1 CPU preset support

Phase 1 also supports CPU preset inference referenced through `inference.preset.name`. Both built-in presets and generic Hugging Face model IDs are allowed, as long as the model can resolve through the normal vLLM-compatible preset flow.

### Preset CPU examples

Retained example:

- NAP InferenceSet (default CPU `vllm` runtime): [`examples/inference/kaito_inferenceset_qwen_2.5_0.5b_instruct_cpu_vllm.yaml`](https://github.com/kaito-project/kaito/tree/main/examples/inference/kaito_inferenceset_qwen_2.5_0.5b_instruct_cpu_vllm.yaml)

This example uses `Qwen/Qwen2.5-0.5B-Instruct` on `Standard_D16s_v5`.

### Phase 1 CPU support matrix

| Scenario | Status | Notes |
|---|---|---|
| Preset InferenceSet on CPU | supported | retained example uses default CPU `vllm` runtime |
| CPU preset with NAP | supported | specify a CPU instance type |
| `kaito.sh/run-benchmark` on CPU | not supported | rejected for Phase 1 CPU paths |
| Multi-node CPU inference | not supported | Phase 1 is single-node only |

## Limitations

- **Single-node only**: Distributed inference across multiple nodes is not supported for Phase 1 CPU paths.
- **Preset compatibility still depends on model resolution/runtime support**: generic Hugging Face model IDs must resolve through the normal vLLM-compatible preset flow.
- **CPU benchmark path disabled**: `kaito.sh/run-benchmark` is not supported for Phase 1 CPU inference.
- **No custom-model autoscaling**: These CPU paths do not attempt to wire autoscaling behavior.
