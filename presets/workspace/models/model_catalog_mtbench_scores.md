# MT-Bench Scores for KAITO Preset Models

## Overview

This file records [MT-Bench](https://arxiv.org/abs/2306.05685) evaluation scores for KAITO's built-in preset models. MT-Bench is a multi-turn benchmark consisting of 80 questions across 8 categories (Writing, Roleplay, Reasoning, Math, Coding, Extraction, STEM, Humanities). Each response is scored 1–10 by a GPT judge model, and the overall score is the average across all categories.

All models were deployed as KAITO Workspace CRs on AKS and evaluated using the vLLM runtime with GPT-5.4 as the judge.

## Scores

| Model | Runtime | Overall | Writing | Roleplay | Reasoning | Math | Coding | Extraction | STEM | Humanities | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|
| microsoft/Phi-3-mini-4k-instruct | vllm | 6.23 | 6.70 | 6.65 | 4.85 | 6.35 | 5.75 | 6.65 | 6.30 | 6.60 | 2026-04-20 |
| microsoft/Phi-3-mini-128k-instruct | vllm | 5.71 | 6.60 | 6.00 | 5.55 | 5.60 | 4.55 | 5.45 | 6.05 | 5.85 | 2026-04-20 |
| microsoft/Phi-3-medium-4k-instruct | vllm | 6.89 | 7.30 | 6.65 | 6.35 | 7.95 | 6.10 | 7.70 | 6.15 | 6.90 | 2026-04-20 |
| microsoft/Phi-3-medium-128k-instruct | vllm | 6.58 | 7.15 | 6.45 | 6.35 | 7.20 | 5.55 | 7.55 | 6.20 | 6.15 | 2026-04-20 |
| microsoft/Phi-3.5-mini-instruct | vllm | 6.24 | 7.25 | 6.15 | 5.65 | 6.45 | 5.20 | 6.45 | 6.25 | 6.55 | 2026-04-20 |
| meta-llama/Llama-3.1-8B-Instruct | vllm | 6.03 | 7.10 | 6.00 | 3.80 | 7.05 | 5.30 | 6.85 | 5.55 | 6.60 | 2026-04-20 |
| deepseek-ai/DeepSeek-R1-Distill-Llama-8B | vllm | 4.60 | 5.30 | 4.45 | 5.30 | 5.40 | 2.05 | 6.50 | 3.95 | 3.85 | 2026-04-20 |
| deepseek-ai/DeepSeek-R1-Distill-Qwen-14B | vllm | 5.36 | 5.95 | 5.55 | 7.20 | 4.85 | 3.80 | 6.80 | 4.85 | 3.90 | 2026-04-20 |
| Qwen/Qwen2.5-Coder-7B-Instruct | vllm | 4.81 | 5.70 | 3.50 | 2.80 | 6.45 | 5.40 | 5.60 | 5.00 | 4.00 | 2026-04-20 |
| Qwen/Qwen2.5-Coder-32B-Instruct | vllm | 5.85 | 5.60 | 5.75 | 5.30 | 5.80 | 7.30 | 5.40 | 6.15 | 5.50 | 2026-04-20 |
| openai/gpt-oss-20b | vllm | 6.02 | 6.40 | 6.20 | 5.30 | 8.55 | 6.50 | 7.15 | 4.00 | 4.05 | 2026-04-20 |
| meta-llama/Llama-3.3-70B-Instruct | vllm | 7.34 | 7.20 | 7.35 | 7.50 | 8.80 | 6.15 | 8.20 | 6.45 | 7.10 | 2026-04-20 |
| openai/gpt-oss-120b | vllm | 7.42 | 7.50 | 7.45 | 7.00 | 9.80 | 6.20 | 7.90 | 6.80 | 6.70 | 2026-04-20 |
| microsoft/phi-4 | vllm | 7.48 | 7.50 | 7.35 | 7.30 | 9.15 | 6.60 | 8.05 | 6.65 | 7.25 | 2026-04-20 |
| microsoft/Phi-4-mini-instruct | vllm | 6.37 | 6.55 | 6.10 | 4.45 | 7.80 | 5.95 | 6.90 | 6.85 | 6.35 | 2026-04-20 |
| google/gemma-3-4b-it | vllm | 6.81 | 7.65 | 7.20 | 5.30 | 8.70 | 6.25 | 6.45 | 5.95 | 6.95 | 2026-04-20 |
| google/gemma-3-27b-it | vllm | 7.68 | 7.95 | 7.95 | 7.00 | 9.40 | 6.55 | 8.55 | 7.10 | 6.95 | 2026-04-20 |
| mistralai/Mistral-7B-Instruct-v0.3 | vllm | 5.54 | 6.85 | 5.95 | 4.35 | 3.65 | 4.45 | 6.75 | 5.70 | 6.65 | 2026-04-20 |
| mistralai/Ministral-3-3B-Instruct-2512 | vllm | 6.47 | 7.50 | 6.35 | 5.10 | 7.10 | 6.40 | 7.70 | 6.05 | 5.55 | 2026-04-20 |
| mistralai/Ministral-3-8B-Instruct-2512 | vllm | 7.29 | 7.95 | 7.30 | 6.75 | 9.15 | 6.25 | 7.95 | 6.60 | 6.35 | 2026-04-20 |
| mistralai/Ministral-3-14B-Instruct-2512 | vllm | 7.34 | 8.15 | 7.35 | 6.05 | 9.70 | 6.15 | 8.15 | 6.45 | 6.70 | 2026-04-20 |
| mistralai/Mistral-7B-v0.3 | vllm | 2.01 | 2.55 | 1.50 | 1.50 | 1.65 | 1.70 | 2.30 | 3.00 | 1.85 | 2026-04-20 |
| deepseek-ai/DeepSeek-R1-0528 | vllm | 5.06 | 7.90 | 7.30 | 3.85 | 2.65 | 1.65 | 5.55 | 5.55 | 6.05 | 2026-04-17 |
| deepseek-ai/DeepSeek-V3-0324 | vllm | 8.07 | 8.20 | 7.95 | 7.35 | 9.50 | 8.20 | 8.30 | 7.00 | 8.05 | 2026-04-17 |
| mistralai/Mistral-Large-3-675B-Instruct-2512 | vllm | 7.86 | 8.05 | 7.65 | 7.75 | 9.05 | 7.90 | 8.50 | 7.25 | 6.70 | 2026-04-17 |
| nvidia/NVIDIA-Nemotron-3-Nano-4B-BF16 | vllm | 6.37 | 6.35 | 5.65 | 6.65 | 9.10 | 5.85 | 7.05 | 6.10 | 4.20 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-Nano-9B-v2 | vllm | 5.54 | 6.05 | 4.50 | 5.65 | 7.45 | 4.20 | 6.75 | 5.25 | 4.50 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-Nano-12B-v2-VL-BF16 | vllm | 7.08 | 7.55 | 6.45 | 6.80 | 9.50 | 6.60 | 6.55 | 6.35 | 6.85 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B-BF16 | vllm | 6.89 | 6.45 | 7.20 | 6.60 | 9.95 | 5.30 | 7.05 | 6.15 | 6.40 | 2026-04-29 |
| nvidia/Nemotron-Cascade-2-30B-A3B | vllm | 5.71 | 3.40 | 6.85 | 6.60 | 9.20 | 5.65 | 4.15 | 4.90 | 4.95 | 2026-04-29 |
| nvidia/NVIDIA-Nemotron-3-Super-120B-A12B-BF16 | vllm | 6.91 | 7.35 | 7.30 | 7.05 | 9.65 | 6.35 | 7.25 | 5.40 | 4.90 | 2026-04-29 |
| Qwen/Qwen3-8B-AWQ | vllm | 3.98 | 6.05 | 6.20 | 2.60 | 1.95 | 1.95 | 5.30 | 3.55 | 4.25 | 2026-04-29 |
| google/gemma-4-31B-it | vllm | 8.56 | 8.20 | 8.50 | 8.70 | 9.90 | 8.35 | 8.35 | 8.35 | 8.10 | 2026-05-04 |
| google/gemma-4-26B-A4B-it | vllm | 8.49 | 8.30 | 8.50 | 8.80 | 10.00 | 8.25 | 8.20 | 8.05 | 7.85 | 2026-05-04 |
| google/gemma-4-E4B-it | vllm | 7.88 | 8.00 | 8.25 | 8.00 | 9.45 | 7.00 | 8.10 | 7.10 | 7.10 | 2026-05-04 |
| google/gemma-4-E2B-it | vllm | 7.30 | 7.50 | 7.25 | 6.50 | 9.45 | 6.35 | 7.95 | 6.60 | 6.80 | 2026-05-04 |
