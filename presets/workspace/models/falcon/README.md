## Supported Models
| Model name          |                        Model source                         |                                Sample workspace                                 | Kubernetes Workload | Distributed inference |
|---------------------|:-----------------------------------------------------------:|:-------------------------------------------------------------------------------:|:-------------------:|:---------------------:|
| falcon-7b-instruct  | [tiiuae](https://huggingface.co/tiiuae/falcon-7b-instruct)  | [link](../../../../examples/inference/kaito_workspace_falcon_7b-instruct.yaml)  |     Deployment      |         false         |
| falcon-7b           |      [tiiuae](https://huggingface.co/tiiuae/falcon-7b)      |      [link](../../../../examples/inference/kaito_workspace_falcon_7b.yaml)      |     Deployment      |         false         |
| falcon-40b-instruct | [tiiuae](https://huggingface.co/tiiuae/falcon-40b-instruct) | [link](../../../../examples/inference/kaito_workspace_falcon_40b-instruct.yaml) |     Deployment      |         false         |
| falcon-40b          |     [tiiuae](https://huggingface.co/tiiuae/falcon-40b)      |     [link](../../../../examples/inference/kaito_workspace_falcon_40b.yaml)      |     Deployment      |         false         |

## Image Source
- **Public**: Kaito maintainers manage the lifecycle of the inference service images that contain model weights. The images are available in Microsoft Container Registry (MCR).

## Usage

See [document](../../../../docs/inference/README.md).
