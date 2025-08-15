---
title: Presets
---

The current supported model families with preset configurations are listed below.

| Model Family                                | Compatible KAITO Versions |
|---------------------------------------------|---------------------------|
| [falcon](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/falcon)         | v0.0.1+                   |
| [mistral](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/mistral)       | v0.2.0+                   |
| [phi2](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/phi2)             | v0.2.0+                   |
| [phi3](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/phi3)             | v0.3.0+                   |
| [phi4](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/phi4)             | v0.4.5+                   |
| [qwen7b](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/qwen)           | v0.4.1+                   |
| [qwen32b](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/qwen)          | v0.4.5+                   |
| [llama3](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/llama3)         | v0.4.6+                   |

## Validation

Each preset model has its own hardware requirements in terms of GPU count and GPU memory defined in the respective `model.go` file. KAITO controller performs a validation check of whether the specified SKU and node count are sufficient to run the model or not. In case the provided SKU is not in the known list, the controller bypasses the validation check which means users need to ensure the model can run with the provided SKU.

## Distributed inference

For models that support distributed inference, when the node count is larger than one, [Torch Distributed Elastic](https://pytorch.org/docs/stable/distributed.elastic.html) is configured with master/worker pods running in multiple nodes and the service endpoint is the master pod.

The following preset models support multi-node distributed inference:

| Model Family | Models | Multi-Node Support |
|--------------|--------|-------------------|
| [llama3](https://github.com/kaito-project/kaito/tree/main/presets/workspace/models/llama3) | `llama-3.3-70b-instruct` | ✅ |

For detailed information on configuring and using multi-node inference, see the [Multi-Node Inference](./multi-node-inference.md) documentation.
