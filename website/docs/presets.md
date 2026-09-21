---
title: Presets
---

## Curated Models

The following HuggingFace models are curated by the KAITO team with first-class support, including validated configurations and optimized inference settings.

| Model Name | Description | License |
|---|---|---|
| deepseek-ai/DeepSeek-V3.2 | https://huggingface.co/deepseek-ai/DeepSeek-V3.2 | MIT |
| deepseek-ai/DeepSeek-V4-Flash-0731 | https://huggingface.co/deepseek-ai/DeepSeek-V4-Flash-0731 | MIT |
| deepseek-ai/DeepSeek-V4-Pro | https://huggingface.co/deepseek-ai/DeepSeek-V4-Pro | MIT |
| google/gemma-4-12B-it | https://huggingface.co/google/gemma-4-12B-it | Apache-2.0 |
| google/gemma-4-26B-A4B-it | https://huggingface.co/google/gemma-4-26B-A4B-it | Apache-2.0 |
| google/gemma-4-31B-it | https://huggingface.co/google/gemma-4-31B-it | Apache-2.0 |
| google/gemma-4-E2B-it | https://huggingface.co/google/gemma-4-E2B-it | Apache-2.0 |
| google/gemma-4-E4B-it | https://huggingface.co/google/gemma-4-E4B-it | Apache-2.0 |
| ibm-granite/granite-4.1-8b | https://huggingface.co/ibm-granite/granite-4.1-8b | Apache-2.0 |
| microsoft/Phi-4-mini-instruct | https://huggingface.co/microsoft/Phi-4-mini-instruct | MIT |
| microsoft/phi-4 | https://huggingface.co/microsoft/phi-4 | MIT |
| MiniMaxAI/MiniMax-M2.7 | https://huggingface.co/MiniMaxAI/MiniMax-M2.7 | Other |
| mistralai/Ministral-3-14B-Instruct-2512 | https://huggingface.co/mistralai/Ministral-3-14B-Instruct-2512 | Apache-2.0 |
| mistralai/Mistral-Medium-3.5-128B | https://huggingface.co/mistralai/Mistral-Medium-3.5-128B | Other |
| mistralai/Mistral-Small-4-119B-2603 | https://huggingface.co/mistralai/Mistral-Small-4-119B-2603 | Apache-2.0 |
| moonshotai/Kimi-K2.6 | https://huggingface.co/moonshotai/Kimi-K2.6 | Modified MIT |
| moonshotai/Kimi-K2.7-Code | https://huggingface.co/moonshotai/Kimi-K2.7-Code | Modified MIT |
| nvidia/NVIDIA-Nemotron-3-Nano-4B-BF16 | https://huggingface.co/nvidia/NVIDIA-Nemotron-3-Nano-4B-BF16 | NVIDIA Nemotron |
| nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B-BF16 | https://huggingface.co/nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B-BF16 | NVIDIA Nemotron |
| nvidia/NVIDIA-Nemotron-3-Super-120B-A12B-BF16 | https://huggingface.co/nvidia/NVIDIA-Nemotron-3-Super-120B-A12B-BF16 | NVIDIA Nemotron |
| nvidia/NVIDIA-Nemotron-3-Ultra-550B-A55B-NVFP4 | https://huggingface.co/nvidia/NVIDIA-Nemotron-3-Ultra-550B-A55B-NVFP4 | OpenMDW-1.1 |
| nvidia/NVIDIA-Nemotron-Nano-9B-v2 | https://huggingface.co/nvidia/NVIDIA-Nemotron-Nano-9B-v2 | NVIDIA Open |
| openai/gpt-oss-20b | https://huggingface.co/openai/gpt-oss-20b | Apache-2.0 |
| openai/gpt-oss-120b | https://huggingface.co/openai/gpt-oss-120b | Apache-2.0 |
| Qwen/Qwen3.5-4B | https://huggingface.co/Qwen/Qwen3.5-4B | Apache-2.0 |
| Qwen/Qwen3.5-9B | https://huggingface.co/Qwen/Qwen3.5-9B | Apache-2.0 |
| Qwen/Qwen3.6-27B | https://huggingface.co/Qwen/Qwen3.6-27B | Apache-2.0 |
| Qwen/Qwen3.6-35B-A3B | https://huggingface.co/Qwen/Qwen3.6-35B-A3B | Apache-2.0 |
| Qwen/Qwen3.6-35B-A3B-FP8 | https://huggingface.co/Qwen/Qwen3.6-35B-A3B-FP8 | Apache-2.0 |
| Qwen/Qwen3.8-27B | https://huggingface.co/Qwen/Qwen3.8-27B | Apache-2.0 |
| Qwen/Qwen3.8-27B-FP8 | https://huggingface.co/Qwen/Qwen3.8-27B-FP8 | Apache-2.0 |


## Generic HuggingFace Models
**NOTE: Generic HuggingFace models support is best-effort only. Please file an issue under https://github.com/kaito-project/kaito/issues/ if your targeted model doesn't work in KAITO.**

Starting from KAITO v0.9.0, generic Hugging Face models are supported on a **best-effort** basis. By specifying a Hugging Face model card ID as `inference.preset.name` in the KAITO workspace or InferenceSet configuration, you can run any Hugging Face model with a model architecture supported by vLLM on KAITO. In this process, KAITO retrieves the model metadata from the Hugging Face website and generates model preset configurations by analyzing this data. During the creation of vLLM inference workloads, KAITO downloads the model weights directly from the Hugging Face site. Below is an example illustrating how to create a Hugging Face inference workload using the model card ID `Qwen/Qwen3-0.6B` from https://huggingface.co/Qwen/Qwen3-0.6B:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: qwen3-06b
resource:
  instanceType: Standard_NC24ads_A100_v4
  labelSelector:
    matchLabels:
      apps: qwen3-06b
inference:
  preset:
    name: Qwen/Qwen3-0.6B
    presetOptions:
      modelAccessSecret: hf-token # Reference to Secret name
```