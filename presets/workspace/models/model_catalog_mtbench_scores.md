# MT-Bench Scores for KAITO Preset Models

## Overview

This file records [MT-Bench](https://arxiv.org/abs/2306.05685) evaluation scores for KAITO's built-in preset models. MT-Bench is a multi-turn benchmark consisting of 80 questions across 8 categories (Writing, Roleplay, Reasoning, Math, Coding, Extraction, STEM, Humanities). Each response is scored 1–10 by a GPT judge model, and the overall score is the average across all categories.

All models were deployed as KAITO Workspace CRs on AKS and evaluated using the vLLM runtime with GPT-5.4 as the judge.

## Scores

| Model | Runtime | Overall | Writing | Roleplay | Reasoning | Math | Coding | Extraction | STEM | Humanities | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|
| microsoft/Phi-3-mini-4k-instruct | vllm | 6.12 | 7.00 | 6.20 | 5.10 | 6.25 | 5.20 | 6.40 | 6.35 | 6.50 | 2026-04-15 |
| microsoft/Phi-3-mini-128k-instruct | vllm | 5.52 | 6.65 | 6.05 | 4.15 | 5.10 | 4.40 | 5.80 | 6.20 | 5.80 | 2026-04-15 |
| microsoft/Phi-3-medium-4k-instruct | vllm | 6.88 | 7.30 | 6.45 | 6.05 | 8.00 | 6.55 | 7.50 | 6.20 | 6.95 | 2026-04-15 |
| microsoft/Phi-3-medium-128k-instruct | vllm | 6.46 | 7.25 | 5.90 | 6.10 | 7.50 | 5.30 | 7.50 | 6.05 | 6.10 | 2026-04-15 |
| microsoft/Phi-3.5-mini-instruct | vllm | 6.27 | 7.30 | 6.35 | 4.85 | 6.40 | 5.35 | 6.70 | 6.45 | 6.75 | 2026-04-15 |
| meta-llama/Llama-3.1-8B-Instruct | vllm | 5.92 | 7.15 | 6.05 | 3.45 | 6.35 | 5.15 | 7.00 | 5.80 | 6.45 | 2026-04-09 |
| deepseek-ai/DeepSeek-R1-Distill-Llama-8B | vllm | 4.58 | 5.50 | 4.65 | 4.85 | 4.85 | 1.85 | 6.45 | 4.35 | 4.15 | 2026-04-15 |
| deepseek-ai/DeepSeek-R1-Distill-Qwen-14B | vllm | 5.17 | 5.50 | 5.60 | 6.35 | 5.05 | 3.50 | 6.90 | 4.55 | 3.90 | 2026-04-15 |
| Qwen/Qwen2.5-Coder-7B-Instruct | vllm | 4.98 | 5.75 | 4.15 | 2.90 | 6.45 | 5.50 | 5.40 | 5.00 | 4.65 | 2026-04-15 |
| Qwen/Qwen2.5-Coder-32B-Instruct | vllm | 5.81 | 5.35 | 5.50 | 5.45 | 5.85 | 7.00 | 4.80 | 6.30 | 6.25 | 2026-04-15 |
| openai/gpt-oss-20b | vllm | 5.84 | 6.10 | 3.90 | 5.10 | 10.00 | 6.05 | 7.45 | 3.95 | 4.20 | 2026-04-09 |
| meta-llama/Llama-3.3-70B-Instruct | vllm | 7.53 | 7.45 | 7.25 | 8.05 | 8.90 | 6.40 | 8.40 | 6.70 | 7.05 | 2026-04-10 |
| openai/gpt-oss-120b | vllm | 7.16 | 7.80 | 6.70 | 6.60 | 9.55 | 6.35 | 7.75 | 6.15 | 6.40 | 2026-04-10 |
| microsoft/phi-4 | vllm | 7.64 | 7.75 | 7.70 | 7.95 | 9.05 | 6.60 | 8.05 | 6.85 | 7.15 | 2026-04-15 |
| microsoft/Phi-4-mini-instruct | vllm | 6.22 | 6.55 | 6.15 | 4.60 | 7.45 | 5.15 | 6.85 | 6.75 | 6.25 | 2026-04-15 |
| google/gemma-3-4b-it | vllm | 6.77 | 7.55 | 7.30 | 5.05 | 8.70 | 6.10 | 6.60 | 6.05 | 6.80 | 2026-04-10 |
| google/gemma-3-27b-it | vllm | 7.69 | 7.70 | 8.05 | 7.10 | 9.45 | 6.55 | 8.70 | 7.05 | 6.95 | 2026-04-10 |
| mistralai/Mistral-7B-Instruct-v0.3 | vllm | 5.43 | 6.80 | 5.90 | 4.05 | 3.60 | 4.50 | 6.50 | 5.55 | 6.55 | 2026-04-15 |
| mistralai/Ministral-3-3B-Instruct-2512 | vllm | 6.73 | 7.75 | 6.00 | 5.90 | 7.90 | 6.55 | 7.85 | 6.20 | 5.70 | 2026-04-15 |
| mistralai/Ministral-3-8B-Instruct-2512 | vllm | 7.26 | 7.95 | 7.10 | 6.50 | 8.70 | 6.70 | 8.00 | 6.65 | 6.50 | 2026-04-15 |
| mistralai/Ministral-3-14B-Instruct-2512 | vllm | 7.45 | 7.95 | 7.35 | 6.40 | 9.90 | 6.20 | 8.40 | 6.80 | 6.60 | 2026-04-15 |
| mistralai/Mistral-7B-v0.3 | vllm | 1.91 | 2.20 | 1.30 | 1.50 | 1.60 | 1.60 | 2.05 | 3.20 | 1.80 | 2026-04-15 |
