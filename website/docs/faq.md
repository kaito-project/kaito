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

No. KAITO controller does not update existing inference workload when controller is upgraded. 

### How to upgrade the existing workload to use the latest model configuration?

You can delete the existing inference workload (`StatefulSet`) manually, and the workspace controller will create a new one with the latest preset configuration (e.g., the latest base image) defined in the latest release.


### How to update model/inference parameters to override the KAITO Preset Configuration?

KAITO provides an option to use a custom configmap to override the preset configurations set by the controller. Check out this [example](https://github.com/kaito-project/kaito/blob/main/examples/inference/kaito_workspace_custom_config.yaml).
