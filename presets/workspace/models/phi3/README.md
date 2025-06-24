## Supported Models
| Model name                 |                               Model source                               |                               Sample workspace                                | Kubernetes Workload | Distributed inference |
|----------------------------|:------------------------------------------------------------------------:|:-----------------------------------------------------------------------------:|:-------------------:|:---------------------:|
| phi-3-mini-4k-instruct     |   [microsoft](https://huggingface.co/microsoft/Phi-3-mini-4k-instruct)   |   [link](../../../../examples/inference/kaito_workspace_phi_3_mini_4k.yaml)   |     Deployment      |         false         |
| phi-3-mini-128k-instruct   |  [microsoft](https://huggingface.co/microsoft/Phi-3-mini-128k-instruct)  |  [link](../../../../examples/inference/kaito_workspace_phi_3_mini_128k.yaml)  |     Deployment      |         false         |
| phi-3-medium-4k-instruct   |  [microsoft](https://huggingface.co/microsoft/Phi-3-medium-4k-instruct)  |  [link](../../../../examples/inference/kaito_workspace_phi_3_medium_4k.yaml)  |     Deployment      |         false         |
| phi-3-medium-128k-instruct | [microsoft](https://huggingface.co/microsoft/Phi-3-medium-128k-instruct) | [link](../../../../examples/inference/kaito_workspace_phi_3_medium_128k.yaml) |     Deployment      |         false         |
| phi-3.5-mini-instruct      | [microsoft](https://huggingface.co/microsoft/Phi-3.5-mini-instruct)      | [link](../../../../examples/inference/kaito_workspace_phi_3.5-instruct.yaml)  |     Deployment      |         false         |

## Image Source
- **Public**: KAITO maintainers manage the lifecycle of the inference service images that contain model weights. The images are available in Microsoft Container Registry (MCR).

## Usage

See [document](../../../../docs/inference/README.md).
