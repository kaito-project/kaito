## Supported Models

| Model name           | Model source                                                                 | Sample workspace                                                                              | Kubernetes Workload | Distributed inference |
|----------------------|------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------|---------------------|-----------------------|
| llama-3.1-8b-instruct | [llama-3.1-8b-instruct](https://huggingface.co/meta-llama/Llama-3.1-8B-Instruct) | [link](../../../../examples/inference/kaito_workspace_llama-3.1_8b_instruct.yaml)           | Deployment          | false                 |
| llama-3.3-70b-instruct | [llama-3.3-70b-instruct](https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct) | [link](../../../../examples/inference/kaito_workspace_llama-3.3_70b_instruct.yaml)           | Statefulset          | True                 |


## Image Source

- **Public**: KAITO maintainers manage the lifecycle of the inference service images that contain model weights. The images are available in Microsoft Container Registry (MCR).

## Usage

See [document](../../../../docs/inference/README.md).
