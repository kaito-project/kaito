# MT-Bench Scores for KAITO Preset Models

## Overview

This file records [MT-Bench](https://arxiv.org/abs/2306.05685) evaluation scores for KAITO's built-in preset models. MT-Bench is a multi-turn benchmark consisting of 80 questions across 8 categories (Writing, Roleplay, Reasoning, Math, Coding, Extraction, STEM, Humanities). Each response is scored 1–10 by a GPT judge model, and the overall score is the average across all categories.

All models were deployed as KAITO Workspace CRs on AKS and evaluated using the vLLM runtime with GPT-5.4 as the judge.

## Scores

| Model | Runtime | Overall | Writing | Roleplay | Reasoning | Math | Coding | Extraction | STEM | Humanities | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|
| openai/gpt-oss-20b | vllm | 6.02 | 6.40 | 6.20 | 5.30 | 8.55 | 6.50 | 7.15 | 4.00 | 4.05 | 2026-04-20 |
| openai/gpt-oss-120b | vllm | 7.42 | 7.50 | 7.45 | 7.00 | 9.80 | 6.20 | 7.90 | 6.80 | 6.70 | 2026-04-20 |
| microsoft/phi-4 | vllm | 7.48 | 7.50 | 7.35 | 7.30 | 9.15 | 6.60 | 8.05 | 6.65 | 7.25 | 2026-04-20 |
| microsoft/Phi-4-mini-instruct | vllm | 6.37 | 6.55 | 6.10 | 4.45 | 7.80 | 5.95 | 6.90 | 6.85 | 6.35 | 2026-04-20 |
| mistralai/Ministral-3-14B-Instruct-2512 | vllm | 7.34 | 8.15 | 7.35 | 6.05 | 9.70 | 6.15 | 8.15 | 6.45 | 6.70 | 2026-04-20 |
| nvidia/NVIDIA-Nemotron-3-Nano-4B-BF16 | vllm | 6.37 | 6.35 | 5.65 | 6.65 | 9.10 | 5.85 | 7.05 | 6.10 | 4.20 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-Nano-9B-v2 | vllm | 5.54 | 6.05 | 4.50 | 5.65 | 7.45 | 4.20 | 6.75 | 5.25 | 4.50 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B-BF16 | vllm | 6.89 | 6.45 | 7.20 | 6.60 | 9.95 | 5.30 | 7.05 | 6.15 | 6.40 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-3-Super-120B-A12B-BF16 | vllm | 6.91 | 7.35 | 7.30 | 7.05 | 9.65 | 6.35 | 7.25 | 5.40 | 4.90 | 2026-04-29 |
| google/gemma-4-31B-it | vllm | 8.56 | 8.20 | 8.50 | 8.70 | 9.90 | 8.35 | 8.35 | 8.35 | 8.10 | 2026-05-04 |
| google/gemma-4-12B-it | vllm | 8.60 | 8.35 | 8.35 | 8.90 | 10.00 | 8.40 | 8.55 | 8.35 | 7.90 | 2026-07-30 |
| google/gemma-4-26B-A4B-it | vllm | 8.49 | 8.30 | 8.50 | 8.80 | 10.00 | 8.25 | 8.20 | 8.05 | 7.85 | 2026-05-04 |
| google/gemma-4-E4B-it | vllm | 7.88 | 8.00 | 8.25 | 8.00 | 9.45 | 7.00 | 8.10 | 7.10 | 7.10 | 2026-05-04 |
| google/gemma-4-E2B-it | vllm | 7.30 | 7.50 | 7.25 | 6.50 | 9.45 | 6.35 | 7.95 | 6.60 | 6.80 | 2026-05-04 |
| mistralai/Mistral-Small-4-119B-2603 | vllm | 7.74 | 7.95 | 7.85 | 7.90 | 9.85 | 7.20 | 7.80 | 6.50 | 6.90 | 2026-05-06 |
| Qwen/Qwen3.5-4B | vllm | 7.41 | 7.40 | 7.35 | 7.85 | 9.45 | 6.25 | 7.25 | 6.95 | 6.75 | 2026-05-07 |
| Qwen/Qwen3.5-9B | vllm | 7.69 | 7.85 | 7.75 | 7.95 | 9.60 | 6.45 | 7.65 | 7.65 | 6.65 | 2026-05-07 |
| Qwen/Qwen3.6-35B-A3B-FP8 | vllm | 8.17 | 7.80 | 8.05 | 8.80 | 9.90 | 7.60 | 8.20 | 7.70 | 7.30 | 2026-05-07 |
| Qwen/Qwen3.6-35B-A3B | vllm | 8.17 | 7.85 | 8.05 | 8.55 | 9.90 | 8.00 | 8.40 | 7.30 | 7.30 | 2026-05-07 |
| Qwen/Qwen3.6-27B | vllm | 8.12 | 8.35 | 8.05 | 8.80 | 9.85 | 7.70 | 8.00 | 7.10 | 7.15 | 2026-05-07 |
| Qwen/Qwen3.8-27B | vllm | 8.49 | 8.15 | 8.35 | 8.55 | 10.00 | 7.90 | 8.45 | 7.95 | 8.55 | 2026-08-14 |
| MiniMaxAI/MiniMax-M2.7 | vllm | 7.16 | 7.15 | 6.90 | 7.29 | 8.72 | 6.90 | 6.95 | 6.25 | 7.15 | 2026-05-13 |
| mistralai/Mistral-Medium-3.5-128B | vllm | 8.18 | 8.05 | 8.10 | 8.15 | 9.55 | 7.40 | 8.60 | 7.60 | 8.00 | 2026-05-14 |
| moonshotai/Kimi-K2.6 | vllm | 8.44 | 8.40 | 8.55 | 8.95 | 9.95 | 6.95 | 8.35 | 7.80 | 8.55 | 2026-05-14 |
| deepseek-ai/DeepSeek-V4-Flash-0731 | vllm | 8.59 | 8.60 | 8.50 | 9.15 | 9.60 | 8.65 | 7.65 | 8.30 | 8.30 | 2026-08-14 |
| deepseek-ai/DeepSeek-V4-Pro | vllm | 8.33 | 8.45 | 8.25 | 8.80 | 9.90 | 7.60 | 8.65 | 8.00 | 7.00 | 2026-07-24 |
| moonshotai/Kimi-K2.7-Code | vllm | 8.74 | 8.25 | 8.15 | 9.0 | 9.75 | 8.5 | 8.5 | 8.95 | 8.8 | 2026-07-31 |
| nvidia/NVIDIA-Nemotron-3-Ultra-550B-A55B-NVFP4 | vllm | 8.64 | 8.55 | 8.65 | 8.9 | 9.1 | 8.55 | 8.25 | 8.2 | 8.9 | 2026-08-01 |
| deepseek-ai/DeepSeek-V3.2 | vllm | 8.61 | 8.55 | 8.45 | 8.7 | 9.85 | 7.9 | 8.4 | 8.5 | 8.55 | 2026-08-02 |
| ibm-granite/granite-4.1-8b | vllm | 7.55 | 7.8 | 7.4 | 5.9 | 9.6 | 6.7 | 8.6 | 7.0 | 7.4 | 2026-08-02 |
| Qwen/Qwen3.8-27B-FP8 | vllm | 8.61 | 8.50 | 8.45 | 8.60 | 9.95 | 8.05 | 8.50 | 8.15 | 8.65 | 2026-09-15 |
