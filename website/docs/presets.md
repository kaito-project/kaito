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


## Validation

KAITO controller performs a validation check of whether the specified SKU and node count are sufficient to run the model or not. In case the provided SKU is not in the known list, the controller bypasses the validation check which means users need to ensure the model can run with the provided SKU.

## Distributed inference

Based on the provided GPU instanceType and the model metadata, KATIO controller determines the number of GPU nodes used to run the model. The controller will setup proper health/readiness probes for both leader and worker nodes to handle node failure gracefully. For more details, please check the [design doc](https://github.com/kaito-project/kaito/blob/main/docs/proposals/20250325-distributed-inference.md#liveness-and-readiness-probes).
