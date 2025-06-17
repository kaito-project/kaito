---
title: Proposal for support of Microsoft's Phi-4
authors:
  - "Suraj Deshmukh"
reviewers:
  - "KAITO contributor"
creation-date: 2024-12-12
last-updated: 2024-12-12
status: provisional
---

# Title

Add official support for Microsoft's Phi-4 models in KAITO.

## Summary

- **Model Description**: The `phi-4` model from Microsoft, released in December 2024, is a state-of-the-art language model designed for advanced reasoning and high-quality text generation. It is built on a blend of synthetic datasets, filtered public domain websites, and academic books, ensuring robust performance in various tasks. With 14.7B parameters, `phi-4` excels in memory and compute-constrained environments, offering precise instruction adherence and strong safety measures through supervised fine-tuning and direct preference optimization. This model is particularly suited for general-purpose AI applications requiring reasoning and logic. The knowledge cut-off date for `phi-4` is set to June 2024.
- **Model Usage Statistics**: As of January 2025, `phi-4` has garnered over 88k downloads.
- **Model License**: MIT License

## Requirements

The following table describes the basic model characteristics and the resource requirements of running it.

| Field            | Notes                                    |
|------------------|------------------------------------------|
| Family name      | Phi-4                                    |
| Type             | conversational                           |
| Download site    | [https://huggingface.co/microsoft/phi-4](https://huggingface.co/microsoft/phi-4) |
| Version          | f957856cd926f9d681b14153374d755dd97e45ed |
| Storage size     | 30 GB                                    |
| GPU count        | 01 GPU                                   |
| Total GPU memory | 30 GB                                    |
| Per GPU memory   | 80 GB                                    |

## Runtimes

This section describes how to configure the runtime framework to support the inference calls.

| Options               | Notes                                                              |
|-----------------------|--------------------------------------------------------------------|
| Runtime               | Huggingface Transformer & VLLM                                     |
| Distributed Inference | False                                                              |
| Custom configurations | Precision: BF16. Can run on one machine with 30+ GB of GPU memory. |

# History

- [x] 01/16/2025: Open proposal PR.
- [ ] xx/xx/2025: Phi-4 Merged TBD
