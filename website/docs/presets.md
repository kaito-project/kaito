---
title: Presets
---

:::info What's NEW!

**Best-effort Hugging Face vLLM model support**

Starting from KAITO v0.9.0, generic Hugging Face models are supported on a best-effort basis. By specifying a Hugging Face model card ID as `inference.preset.name` in the KAITO workspace or InferenceSet configuration, you can run any Hugging Face model with a model architecture supported by vLLM on KAITO. In this process, KAITO retrieves the model metadata from the Hugging Face website and generates model preset configurations by analyzing this data. During the creation of vLLM inference workloads, KAITO downloads the model weights directly from the Hugging Face site. Below is an example illustrating how to create a Hugging Face inference workload using the model card ID `Qwen/Qwen3-0.6B` from https://huggingface.co/Qwen/Qwen3-0.6B:

:::tip

For certain Hugging Face models that require authentication, configure `inference.preset.presetOptions.modelAccessSecret` to reference a Secret containing a Hugging Face access token under the `HF_TOKEN` key.

:::

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: qwen3-06b
resource:
  instanceType: Standard_NC24ads_A100_v4
  labelSelector:
    matchLabels:
      apps: qwen3-06b
inference:
  preset:
    name: Qwen/Qwen3-0.6B
```

:::

The current supported built-in model families with preset configurations are listed below.

| Model Family                                | Compatible KAITO Versions |
|---------------------------------------------|---------------------------|
| [deepseek](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/deepseek)     | v0.6.0+                       |
| [falcon](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/falcon)         | v0.0.1+                   |
| [gemma-3](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/gemma3)        | v0.8.0+                       |
| [gpt-oss](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/gpt)           | v0.7.0+                       |
| [llama](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/llama)           | v0.4.6+                   |
| [mistral](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/mistral)       | v0.2.0+                   |
| [phi-3](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/phi3)            | v0.3.0+                   |
| [phi-4](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/phi4)            | v0.4.5+                   |
| [qwen](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/qwen)             | v0.4.1+                   |


## Validation

Each preset built-in model has its own hardware requirements in terms of GPU count and GPU memory defined in the respective `model.go` file. KAITO controller performs a validation check of whether the specified SKU and node count are sufficient to run the model or not. In case the provided SKU is not in the known list, the controller bypasses the validation check which means users need to ensure the model can run with the provided SKU.

## Distributed inference

For models that support distributed inference, when the node count is larger than one, [Torch Distributed Elastic](https://pytorch.org/docs/stable/distributed.elastic.html) is configured with master/worker pods running in multiple nodes and the service endpoint is the master pod.

The following preset models support multi-node distributed inference:

| Model Family | Models | Multi-Node Support |
|--------------|--------|-------------------|
| [deepseek](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/deepseek) | `deepseek-r1`, `deepseek-v3` | ✅ |
| [llama](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/llama) | `llama-3.3-70b-instruct` | ✅ |

For detailed information on configuring and using multi-node inference, see the [Multi-Node Inference](./multi-node-inference.md) documentation.
