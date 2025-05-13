## Supported Models
| Model name          |                        Model source                         |                                Sample workspace                                 | Kubernetes Workload | Distributed inference |
|---------------------|:-----------------------------------------------------------:|:-------------------------------------------------------------------------------:|:-------------------:|:---------------------:|
| falcon-7b-instruct  | [tiiuae](https://huggingface.co/tiiuae/falcon-7b-instruct)  | [link](../../../../examples/inference/kaito_workspace_falcon_7b-instruct.yaml)  |     Deployment      |         false         |
| falcon-7b           |      [tiiuae](https://huggingface.co/tiiuae/falcon-7b)      |      [link](../../../../examples/inference/kaito_workspace_falcon_7b.yaml)      |     Deployment      |         false         |
| falcon-40b-instruct | [tiiuae](https://huggingface.co/tiiuae/falcon-40b-instruct) | [link](../../../../examples/inference/kaito_workspace_falcon_40b-instruct.yaml) |     Deployment      |         false         |
| falcon-40b          |     [tiiuae](https://huggingface.co/tiiuae/falcon-40b)      |     [link](../../../../examples/inference/kaito_workspace_falcon_40b.yaml)      |     Deployment      |         false         |

## Image Source
- **Public**: KAITO maintainers manage the lifecycle of the inference service images that contain model weights. The images are available in Microsoft Container Registry (MCR).

## Usage

See [document](../../../../docs/inference/README.md).

## Fine-tuning Configuration

When fine-tuning Falcon models, you must specify a chat template in your configuration. Use the following steps:

1. Create a ConfigMap with your fine-tuning configuration that includes the `chat_template` parameter in the `ModelConfig` section:

```yaml
ModelConfig:
  # other model parameters...
  chat_template: "/workspace/chat_templates/falcon-instruct.jinja"
```

You can see complete example configurations in the default templates:
- [LoRA configuration template](../../../../charts/kaito/workspace/templates/lora-params.yaml)
- [QLoRA configuration template](../../../../charts/kaito/workspace/templates/qlora-params.yaml)

2. Reference this ConfigMap in your Workspace by adding the `Config` field:

```yaml
kind: Workspace
metadata:
  name: workspace-tuning-falcon-7b
spec:
  tuning:
    method: lora  # or qlora
    preset:
      name: falcon-7b  # or falcon-7b-instruct, falcon-40b, etc.
    config: your-config-map-name  # Reference to your ConfigMap
```

The falcon-instruct.jinja chat template ensures proper formatting of conversation data during fine-tuning.
