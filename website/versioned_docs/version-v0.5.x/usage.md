---
title: Usage
---

The detailed usage for Kaito supported models can be found in [**HERE**](./presets.md). In case users want to deploy their own containerized models, they can provide the pod template in the `inference` field of the workspace custom resource (please see [API definitions](../../../api/v1alpha1/workspace_types.go) for details). The controller will create a deployment workload using all provisioned GPU nodes. Note that currently the controller does **NOT** handle automatic model upgrade. It only creates inference workloads based on the preset configurations if the workloads do not exist.

The number of the supported models in Kaito is growing! Please check [this](./preset-onboarding.md) document to see how to add a new supported model.

Starting with version v0.3.0, Kaito supports model fine-tuning and using fine-tuned adapters in the inference service. Refer to the [tuning document](./tuning.md) and [inference document](./inference.md) for more information.
