# MT-Bench Scores for KAITO Preset Models

## Overview

This file records [MT-Bench](https://arxiv.org/abs/2306.05685) evaluation scores for KAITO's built-in preset models. MT-Bench is a multi-turn benchmark consisting of 80 questions across 8 categories (Writing, Roleplay, Reasoning, Math, Coding, Extraction, STEM, Humanities). Each response is scored 1–10 by a GPT judge model, and the overall score is the average across all categories.

All models were deployed as KAITO Workspace CRs on AKS and evaluated using the vLLM runtime with GPT-5.4 as the judge.

## Scores

| Model | Runtime | Overall | Writing | Roleplay | Reasoning | Math | Coding | Extraction | STEM | Humanities | Date |
|---|---|---|---|---|---|---|---|---|---|---|---|
| phi-3-mini-4k-instruct | vllm | 6.21 | 6.55 | 6.25 | 4.85 | 6.75 | 5.75 | 6.75 | 6.10 | 6.65 | 2026-04-09 |
| phi-3-mini-128k-instruct | vllm | 5.44 | 6.35 | 6.00 | 4.30 | 5.10 | 4.20 | 5.55 | 6.25 | 5.75 | 2026-04-09 |
| phi-3-medium-4k-instruct | vllm | 6.91 | 7.45 | 6.30 | 6.35 | 8.00 | 6.35 | 7.55 | 6.35 | 6.95 | 2026-04-09 |
| phi-3-medium-128k-instruct | vllm | 6.45 | 7.25 | 5.80 | 6.15 | 7.65 | 5.20 | 7.45 | 6.00 | 6.10 | 2026-04-09 |
| phi-3.5-mini-instruct | vllm | 6.19 | 7.00 | 6.45 | 4.85 | 6.35 | 5.40 | 6.50 | 6.35 | 6.65 | 2026-04-09 |
| llama-3.1-8b-instruct | vllm | 5.92 | 7.15 | 6.05 | 3.45 | 6.35 | 5.15 | 7.00 | 5.80 | 6.45 | 2026-04-09 |
| deepseek-r1-distill-llama-8b | vllm az acr login -n| 4.58 | 5.45 | 4.70 | 5.00 | 4.75 | 1.85 | 6.30 | 4.35 | 4.20 | 2026-04-09 |
| deepseek-r1-distill-qwen-14b | vllm | 5.13 | 5.40 | 5.55 | 6.25 | 5.00 | 3.40 | 6.95 | 4.55 | 3.95 | 2026-04-09 |
| qwen2.5-coder-7b-instruct | vllm | 4.96 | 5.70 | 4.15 | 2.90 | 6.40 | 5.30 | 5.40 | 5.05 | 4.75 | 2026-04-09 |
| qwen2.5-coder-32b-instruct | vllm | 5.78 | 5.55 | 5.50 | 5.15 | 5.60 | 7.30 | 4.70 | 6.20 | 6.20 | 2026-04-09 |
| gpt-oss-20b | vllm | 5.84 | 6.10 | 3.90 | 5.10 | 10.00 | 6.05 | 7.45 | 3.95 | 4.20 | 2026-04-09 |
| llama-3.3-70b-instruct | vllm | 7.53 | 7.45 | 7.25 | 8.05 | 8.90 | 6.40 | 8.40 | 6.70 | 7.05 | 2026-04-10 |
| gpt-oss-120b | vllm | 7.16 | 7.80 | 6.70 | 6.60 | 9.55 | 6.35 | 7.75 | 6.15 | 6.40 | 2026-04-10 |
| phi-4 | vllm | 7.60 | 7.45 | 7.60 | 7.90 | 9.20 | 6.55 | 8.15 | 6.85 | 7.10 | 2026-04-10 |
| phi-4-mini-instruct | vllm | 6.26 | 6.70 | 6.10 | 4.55 | 7.65 | 4.90 | 6.90 | 6.95 | 6.30 | 2026-04-10 |
