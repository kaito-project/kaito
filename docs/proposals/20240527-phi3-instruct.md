---
title: Proposal for new model support
authors:
  - "Justin Joy"
reviewers:
  - "KAITO contributor"
creation-date: 2024-05-27
last-updated: 2024-05-27
status: provisional
---

# Title
Add Phi-3 Medium Models to KAITO supported model list

## Glossary
N/A

## Summary
- **Model description**: Phi-3 is a series of SLMs launched this year around April 2024 and is one of the most downloaded and used SLMs in HuggingFace repository. It comes with a series of sizes, Mini(3B), Small (7B), Medium (14B) & Vision (4B). All punching above its Parameter class and benchmarks shows they are better than some of the larger models like GPT3.5, Mistral 8x7B & Llama3. https://huggingface.co/microsoft/Phi-3-medium-128k-instruct . Comes with 4k & 128k context window for its family of models.
- **Model usage statistics**: Phi-3 Mini 4k has about 1.12M Downloads as of 27th May 2024
- **Model license**: MIT License


## Requirements

The following table describes the basic model characteristics and the resource requirements of running it.

| Field | Notes|
|----|----|
| Family name| Phi-3 Medium|
| Type| conversational |
| Download site| https://huggingface.co/microsoft/Phi-3-mini-128k-instruct |
| Version| bbd531db4632bb631b0c44d98172894a0c594dd0 |
| Storage size| 9GB |
| GPU count| 1 GPU |
| Total GPU memory| 10 GB |
| Per GPU memory | N/A |


## Runtimes

This section describes how to configure the runtime framework to support the inference calls.

| Options | Notes|
|----|----|
| Runtime | Huggingface Transformer & onnx |
| Distributed Inference| False |
| Custom configurations| Precision: BF16. Can run on one machine with 10 GB of GPU memory.|

# History

- [x] 05/27/2024: Open proposal PR.
- [x] 06/13/2024: Phi-3 Mini Merged [#469](https://github.com/kaito-project/kaito/pull/469)
