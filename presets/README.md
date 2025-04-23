# Kaito Preset Configurations
The current supported model families with preset configurations are listed below.

| Model Family                                | Compatible Kaito Versions |
|---------------------------------------------|---------------------------|
| [falcon](./workspace/models/falcon)         | v0.0.1+                   |
| [llama2](./workspace/models/llama2)         | v0.0.1+                   |
| [llama2chat](./workspace/models/llama2chat) | v0.0.1+                   |
| [mistral](./workspace/models/mistral)       | v0.2.0+                   |
| [phi2](./workspace/models/phi2)             | v0.2.0+                   |
| [phi3](./workspace/models/phi3)             | v0.3.0+                   |
| [phi4](./workspace/models/phi4)             | v0.4.5+                   |
| [qwen7b](./workspace/models/qwen)           | v0.4.1+                   |
| [qwen32b](./workspace/models/qwen)          | v0.4.5+                   |


## Validation
Each preset model has its own hardware requirements in terms of GPU count and GPU memory defined in the respective `model.go` file. Kaito controller performs a validation check of whether the specified SKU and node count are sufficient to run the model or not. In case the provided SKU is not in the known list, the controller bypasses the validation check which means users need to ensure the model can run with the provided SKU. 

## Distributed inference

For models that support distributed inference, when the node count is larger than one, [torch distributed elastic](https://pytorch.org/docs/stable/distributed.elastic.html) is configured with master/worker pods running in multiple nodes and the service endpoint is the master pod.
