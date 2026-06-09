# Model Catalog

## Overview

`model_catalog.yaml` is the registry of all models that KAITO provides first-class support for. It is **auto-generated** by the tool at `presets/workspace/generator/update_model_catalog/main.go` and should not be edited manually. NOTE: `presets/workspace/models/supported_models.yaml` is only used for version tracking of KAITO's base image. Other contents in the file are deprecated. 

## Catalog Entry Fields

Each model entry in `model_catalog.yaml` contains the following fields:

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | HuggingFace repo ID (e.g. `microsoft/phi-4`) |
| `description` | No | Auto-populated with the HuggingFace model URL |
| `license` | No | License from the HuggingFace model card |
| `pipelineTag` | No | HuggingFace pipeline tag (e.g. `text-generation`) |
| `baseModel` | No | Parent model(s) from the HuggingFace model card |
| `modelFileSize` | Yes | Total size of model weight files |
| `architectures` | Yes | Model architecture(s) from `config.json` |
| `modelTokenLimit` | Yes | Maximum sequence length |
| `hiddenSize` | Yes | Hidden dimension size |
| `numHiddenLayers` | Yes | Number of transformer layers |
| `numAttentionHeads` | Yes | Number of attention heads |
| `numKeyValueHeads` | Yes | Number of key-value heads |
| `headDim` | No | Head dimension (only when non-standard) |
| `kvLoraRank` | No | KV LoRA rank (DeepSeek-style models) |
| `qkRopeHeadDim` | No | QK RoPE head dimension (DeepSeek-style models) |
| `quantMethod` | No | Quantization method (e.g. `fp8`, `awq`, `gptq`) |
| `quantBits` | No | Quantization bit width |
| `loadFormat` | No | vLLM load format (only when not `auto`) |
| `configFormat` | No | vLLM config format (only when not `auto`) |
| `tokenizerMode` | No | vLLM tokenizer mode (only when not `auto`) |

## How to Onboard a New Model

### Prerequisites

- The model must be hosted on [HuggingFace](https://huggingface.co) with a valid `config.json`.
- You need a HuggingFace access token (set via `HF_TOKEN` environment variable) if the model is gated.

### Steps

1. **Run the generator with the new model's repo ID:**

   ```sh
   go run ./presets/workspace/generator/update_model_catalog --repos <org>/<model-name>
   ```

   For example, to add `Qwen/Qwen3-8B`:

   ```sh
   go run ./presets/workspace/generator/update_model_catalog --repos Qwen/Qwen3-8B
   ```

   Multiple models can be added at once:

   ```sh
   go run ./presets/workspace/generator/update_model_catalog --repos org/model1,org/model2
   ```

2. **Review the generated entry.** **If the model has missing or non-standard HuggingFace metadata**, add an override entry in `presets/workspace/generator/generator.go` under the `catalogOverrides` map.

3. **Add model-specific configurations to `presets/workspace/generator/generator.go` if necessary** (e.g. reasoning parser, tool call parser, etc).

4. **Deploy and verify with MT-Bench.** Deploy the model as a KAITO Workspace CR and run the MT-Bench evaluation following `benchmarks/mt_bench/README.md`. Record the scores in `presets/workspace/models/model_catalog_mtbench_scores.md`.

5. **(Optional) Add an E2E test for the new model** to `test/e2e/preset_vllm_test.go`. This ensures the model can be deployed and serve inference requests in CI.

6. **Submit a pull request** with the updated `model_catalog.yaml`, `model_catalog_mtbench_scores.md` and other necessary changes.

## Refreshing All Existing Entries

To update metadata for all models already in the catalog (e.g. after HuggingFace updates model cards):

```sh
go run ./presets/workspace/generator/update_model_catalog
```
