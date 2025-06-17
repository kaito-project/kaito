## Supported Models
| Model name |                    Model source                     |                         Sample workspace                          | Kubernetes Workload | Distributed inference |
|------------|:---------------------------------------------------:|:-----------------------------------------------------------------:|:-------------------:|:---------------------:|
| phi-2      | [microsoft](https://huggingface.co/microsoft/phi-2) | [link](../../../../examples/inference/kaito_workspace_phi_2.yaml) |     Deployment      |         false         |


## Image Source
- **Public**: KAITO maintainers manage the lifecycle of the inference service images that contain model weights. The images are available in Microsoft Container Registry (MCR).

## Usage

See [document](../../../../docs/inference/README.md).

## Fine-tuning Configuration

When fine-tuning Phi-2 models, you must specify a chat template in your configuration. Use the following steps:

1. Create a ConfigMap with your fine-tuning configuration that includes the `chat_template` parameter in the `ModelConfig` section:

```yaml
ModelConfig:
  # other model parameters...
  chat_template: "/workspace/chat_templates/phi-3.jinja"
```

You can see complete example configurations in the default templates:
- [LoRA configuration template](../../../../charts/kaito/workspace/templates/lora-params.yaml)
- [QLoRA configuration template](../../../../charts/kaito/workspace/templates/qlora-params.yaml)

2. Reference this ConfigMap in your Workspace by adding the `Config` field:

```yaml
kind: Workspace
metadata:
  name: workspace-tuning-phi-2
spec:
  tuning:
    method: lora  # or qlora
    preset:
      name: phi-2
    config: your-config-map-name  # Reference to your ConfigMap
```

The phi-3.jinja chat template is compatible with phi-2 models and ensures proper formatting of conversation data during fine-tuning.
