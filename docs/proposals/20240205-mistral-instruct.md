---
title: Proposal for Mistral model support
authors:
  - "Ishaan Sehgal"
reviewers:
  - "KAITO contributor"
creation-date: 2024-02-02
last-updated: 2024-02-02
status: provisional
---
# Title
Add Mistral-7B-Instruct-v0.2 to KAITO supported model list.

## Glossary
N/A

## Summary

- **Model description**: Launched last September, Mistral-7B has quickly become a standout among large language models (LLMs) with its 7.3B parameters. It surpasses models nearly twice its size in benchmarks, showcasing its efficiency and cost-effectiveness. Mistral has also forged strategic alliances with major cloud platforms including Azure, AWS, and GCP. For more information, refer to the [Mistral-7B Documentation](https://docs.mistral.ai/) and access the model on [Hugging Face](https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.2). The newly introduced Mistral-7B-Instruct-v0.1 is an instruct fine-tuned iteration of the original, optimized for understanding and executing specific instructions. This enhances mistral's utility in conversational applications.
- **Model usage statistics**: In the past month, Mistral-7B-Instruct-v0.1 has garnered 424,580 downloads on Hugging Face, reflecting its widespread popularity. Google Trends data shows a high level of search interest in <!-- markdown-link-check-disable --> ["mistral ai 7b"](https://trends.google.com/trends/explore?q=mistral%20ai%207b), <!-- markdown-link-check-enable --> indicating strong market curiosity.
- **Model license**: Mistral-7B-Instruct-v0.1 is distributed under the Apache 2.0 license, ensuring broad usability and modification rights.

## Requirements

The following table describes the basic model characteristics and the resource requirements of running it.

| Field | Notes|
|----|----|
| Family name| Mistral|
| Type| `text generation` |
| Download site|  https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.2|
| Version| b70aa86578567ba3301b21c8a27bea4e8f6d6d61|
| Storage size| 50GB |
| GPU count| 1 |
| Total GPU memory| 16GB |
| Per GPU memory | `N/A` |

## Runtimes

This section describes how to configure the runtime framework to support the inference calls.

| Options | Notes|
|----|----|
| Runtime | Huggingface Transformer |
| Distributed Inference| False |
| Custom configurations| Precision: BF16. Can run on one machine with total of 16GB of GPU Memory.|

# History

- [x] 02/05/2024: Open proposal PR.
- [x] 02/06/2024: Update proposal PR - Mistral-7B-Instruct-v0.2
