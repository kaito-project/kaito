---
title: FAQ
---

### How do I use existing GPUs in the cluster for my inference workload?

Regardless of whether the GPUs are in cloud provider or on-prem clusters, make sure each node has the label specified in the `resource.labelSelector` field of the workspace. For example, if your labelSelector in the workspace is:

```yaml
resource:
  labelSelector:
    matchLabels:
      apps: falcon-7b
```

Then the node should have the label: `apps=falcon-7b`. In addition, if the GPU nodes are provisioned by cloud providers, make sure the `resource.instanceType` field matches the value of the label `node.kubernetes.io/instance-type` in the node.

### Will KAITO controller upgrade affect existing inference workload?

By default, no. Upgrading the KAITO controller does not change existing `Workspace` inference workloads — they keep running their current base image until recreated.

The exception is `InferenceSet` with **automatic base image upgrades** enabled. When you upgrade the controller to a release that bundles a newer base image, an `InferenceSet` with `spec.autoUpgrade.enabled: true` (and the `enableBaseImageAutoUpgrade` feature gate turned on) detects the version drift and rolls its replicas onto the new image one at a time, keeping the service available throughout. See [Automatic base image upgrades](./inference.md#automatic-base-image-upgrades) for details.

### How to upgrade the existing workload to use the latest model configuration?
- Option 1 (recommended): **Use an `InferenceSet` with auto-upgrade**. Enable `spec.autoUpgrade.enabled: true` so the controller automatically performs a rolling, one-replica-at-a-time upgrade onto the latest base image after a controller upgrade. See [Automatic base image upgrades](./inference.md#automatic-base-image-upgrades).
- Option 2: **Delete and recreate**. You can delete the existing inference workload (`StatefulSet`) manually, and the workspace controller will create a new one with the latest preset configuration (e.g., the latest base image) defined in the latest release.


### How to update model/inference parameters to override the KAITO Preset Configuration?

KAITO provides an option to use a custom configmap to override the preset configurations set by the controller. Check out this [example](https://github.com/kaito-project/kaito/blob/main/examples/inference/kaito_workspace_custom_config.yaml).
