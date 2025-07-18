## Supported Models
| Model name          |                        Model source                         |                                      | Kubernetes Workload | Distributed inference |
|---------------------|:-----------------------------------------------------------:|:-------------------------------------------------------------------------------:|:-------------------:|:---------------------:|
| deepseek-r1-distill-llama-8b  | [deepseek-r1-distill-llama-8b](https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Llama-8B)  | [link](../../../../examples/inference/kaito_workspace_deepseek_r1_distill_llama_8b.yaml)  |     Deployment      |         false         |
| deepseek-r1-distill-qwen-14b  | [deepseek-r1-distill-qwen-14b](https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Qwen-14B)  | [link](../../../../examples/inference/kaito_workspace_deepseek_r1_distill_qwen_14b.yaml)  |     Deployment      |         false         |
| deepseek-r1-0528  | [deepseek-r1-0528](https://huggingface.co/deepseek-ai/DeepSeek-R1-0528)  | [link](../../../../examples/inference/kaito_workspace_deepseek_r1.yaml)  |     Statefulset      |         true         |
| deepseek-v3-0324  | [deepseek-v3-0324](https://huggingface.co/deepseek-ai/DeepSeek-V3-0324)  | [link](../../../../examples/inference/kaito_workspace_deepseek_v3.yaml)  |     Statefulset      |         true         |


## Image Source
- **Public**: KAITO maintainers manage the lifecycle of the inference service images that contain model weights. The images are available in Microsoft Container Registry (MCR).

## Usage

See [document](../../../../docs/inference/README.md).
