## Supported Models

| Model name                |                             Model source                              |                                Sample workspace                                | Kubernetes Workload | Distributed inference |
|---------------------------|:----------------------------------------------------------------------:|:------------------------------------------------------------------------------:|:-------------------:|:---------------------:|
| gemma-3-4b-instruct       | [gemma-3-4b-instruct](https://huggingface.co/google/gemma-3-4b-it)                | [link](../../../../examples/inference/kaito_workspace_gemma_3_4b_instruct.yaml) |     Deployment      |         false         |
| gemma-3-27b-instruct      | [gemma-3-27b-instruct](https://huggingface.co/google/gemma-3-27b-it)               | [link](../../../../examples/inference/kaito_workspace_gemma_3_27b_instruct.yaml)|     Deployment      |         false         |

## Image Source

- **Public**: KAITO maintainers manage the lifecycle of the inference service images. Model weights are downloaded directly from HuggingFace at runtime.

## Usage

See [document](../../../../website/docs/inference.md).