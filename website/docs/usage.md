---
title: Usage Guide
---

This guide covers the essential usage patterns and configurations for Kaito.

## Model Presets

Kaito supports a wide variety of popular open-source models through preset configurations. For more information about supported models, see the [installation documentation](./installation.md).

## Custom Models

In case you want to deploy your own containerized models, you can provide the pod template in the `inference` field of the workspace custom resource. See the [API definitions](https://github.com/kaito-project/kaito/blob/main/api/v1alpha1/workspace_types.go) for details.

The controller will create a deployment workload using all provisioned GPU nodes.

:::note
Currently the controller does **NOT** handle automatic model upgrade. It only creates inference workloads based on the preset configurations if the workloads do not exist.
:::

## Model Capabilities

Starting with version v0.3.0, Kaito supports:

- **Model fine-tuning**: Train models with your specific data
- **Fine-tuned adapters**: Use fine-tuned adapters in the inference service
- **RAG (Retrieval-Augmented Generation)**: Coming in v0.5.0 with LlamaIndex orchestration and Faiss vectorDB

Refer to the [tuning documentation](./tuning.md) and [inference documentation](./inference.md) for more information.

## Adding New Models

The number of supported models in Kaito is constantly growing! Check the [preset onboarding guide](./preset-onboarding.md) to see how to contribute new model support.

## Frequently Asked Questions

### How do I ensure preferred nodes are correctly labeled for use in my workspace?

For using preferred nodes, make sure the node has the label specified in the labelSelector under matchLabels. For example, if your labelSelector is:

```yaml
labelSelector:
  matchLabels:
    apps: falcon-7b
```

Then the node should have the label: `apps=falcon-7b`.

### How to upgrade the existing deployment to use the latest model configuration?

When using hosted public models, you can delete the existing inference workload (`Deployment` or `StatefulSet`) manually, and the workspace controller will create a new one with the latest preset configuration (e.g., the image version) defined in the current release.

For private models, it is recommended to create a new workspace with a new image version in the Spec.

### How to update model/inference parameters to override the Kaito Preset Configuration?

Kaito provides a limited capability to override preset configurations for models that use `transformer` runtime manually.

To update parameters for a deployed model, perform `kubectl edit` against the workload, which could be either a `StatefulSet` or `Deployment`.

For example, to enable 4-bit quantization on a `falcon-7b-instruct` deployment:

```bash
kubectl edit deployment workspace-falcon-7b-instruct
```

Within the deployment specification, locate and modify the command field.

**Original:**
```bash
accelerate launch --num_processes 1 --num_machines 1 --machine_rank 0 --gpu_ids all inference_api.py --pipeline text-generation --torch_dtype bfloat16
```

**Modified to enable 4-bit Quantization:**
```bash
accelerate launch --num_processes 1 --num_machines 1 --machine_rank 0 --gpu_ids all inference_api.py --pipeline text-generation --torch_dtype bfloat16 --load_in_4bit
```

Currently, we allow users to change the following parameters manually:

- `pipeline`: For text-generation models this can be either `text-generation` or `conversational`.
- `load_in_4bit` or `load_in_8bit`: Model quantization resolution.

Should you need to customize other parameters, kindly file an issue for potential future inclusion.

### What is the difference between instruct and non-instruct models?

The main distinction lies in their intended use cases:

- **Instruct models**: Fine-tuned versions optimized for interactive chat applications. They are typically the preferred choice for most implementations due to their enhanced performance in conversational contexts.
- **Non-instruct (raw) models**: Designed for further fine-tuning with your own data.
