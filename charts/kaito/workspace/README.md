# KAITO Workspace Helm Chart

## Install

```bash
export REGISTRY=mcr.microsoft.com/aks/kaito
export IMG_NAME=workspace
export IMG_TAG=0.10.0
helm install workspace ./charts/kaito/workspace  \
--set image.repository=${REGISTRY}/${IMG_NAME} --set image.tag=${IMG_TAG} \
--namespace kaito-workspace --create-namespace
```

## Values

| Key                                            | Type   | Default                                                  | Description                                                   |
|------------------------------------------------|--------|----------------------------------------------------------|---------------------------------------------------------------|
| affinity                                       | object | `{}`                                                     | Pod affinity rules.                                           |
| image.pullPolicy                               | string | `"IfNotPresent"`                                         | Allowed values: `Always`, `IfNotPresent`, `Never`.            |
| image.repository                               | string | `mcr.microsoft.com/aks/kaito/workspace`                  | Controller image repository.                                  |
| image.tag                                      | string | `"0.10.0"`                                               | Controller image tag.                                         |
| imagePullSecrets                               | list   | `[]`                                                     | List of image pull secret references (`[{name: <secret>}]`).  |
| nodeSelector                                   | object | `{"kubernetes.io/os": "linux"}`                          | Controller pod node selector.                                 |
| podAnnotations                                 | object | `{}`                                                     | Extra annotations added to the controller pod.                |
| podSecurityContext.runAsUser                   | int    | `1000`                                                   | UID the controller runs as. Must be non-zero (runAsNonRoot).  |
| podSecurityContext.runAsGroup                  | int    | `1000`                                                   | GID the controller runs as.                                   |
| podSecurityContext.runAsNonRoot                | bool   | `true`                                                   | Allowed values: `true`, `false`. Must stay `true` on restricted PSS clusters. |
| podSecurityContext.seccompProfile.type         | string | `"RuntimeDefault"`                                       | Allowed values: `RuntimeDefault`, `Localhost`, `Unconfined`.  |
| presetRegistryName                             | string | `"mcr.microsoft.com/aks/kaito"`                          | Registry used to pull preset inference/tuning images.         |
| replicaCount                                   | int    | `1`                                                      | Controller replicas. Non-negative integer; leader election enables HA when >1. |
| deploymentStrategy.rollingUpdate.maxUnavailable| int / string | `1`                                                | Integer or percentage string (e.g. `"50%"`) per Kubernetes rolling update semantics. |
| resources.limits.cpu                           | string | `"500m"`                                                 | Kubernetes CPU quantity.                                      |
| resources.limits.memory                        | string | `"128Mi"`                                                | Kubernetes memory quantity.                                   |
| resources.requests.cpu                         | string | `"10m"`                                                  | Kubernetes CPU quantity.                                      |
| resources.requests.memory                      | string | `"64Mi"`                                                 | Kubernetes memory quantity.                                   |
| securityContext.allowPrivilegeEscalation       | bool   | `false`                                                  | Allowed values: `true`, `false`.                              |
| securityContext.readOnlyRootFilesystem         | bool   | `true`                                                   | Allowed values: `true`, `false`.                              |
| securityContext.capabilities.drop[0]           | string | `"ALL"`                                                  | Linux capability name, or the special value `ALL`.            |
| defaultNodeImageFamily                         | string | `""`                                                     | Default NodeClaim image-family annotation. Allowed values: `""` (treated as `ubuntu`), `ubuntu`, `azurelinux`. Any other value causes controller startup failure. |
| nodeProvisioner                                | string | `""`                                                     | Node provisioner type. Allowed values: `""` (inferred from feature gates for backward compatibility), `azure-gpu-provisioner`, `azure-karpenter`, `byo`. |
| tolerations                                    | list   | `[]`                                                     | Controller pod tolerations.                                   |
| webhook.port                                   | int    | `9443`                                                   | Webhook HTTPS port. Valid TCP port (1–65535); must not conflict with other container ports. |
| logging.level                                  | string | `"error"`                                                | Knative zap logging level. Allowed values: `debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal`. |
| cloudProviderName                              | string | `"azure"`                                                | Cloud provider identifier propagated as the `CLOUD_PROVIDER` env var. Allowed values: `azure`, `aws`, `arc`. |
| clusterName                                    | string | `"kaito"`                                                | Logical Kubernetes cluster name used in controller labels/metrics. |
| spotInstance.enabled                           | bool   | `false`                                                  | Allowed values: `true`, `false`. When `true`, adds the Azure Spot toleration so workloads can be scheduled on Spot GPU node pools. |
| localCSIDriver.useLocalCSIDriver               | bool   | `true`                                                   | Allowed values: `true`, `false`. Enables use of the bundled local CSI driver. |
| nvidiaDevicePlugin.enabled                     | bool   | `true`                                                   | Allowed values: `true`, `false`. Set to `false` if the cluster already has an NVIDIA device plugin (e.g., via GPU Operator). |
| nvidiaDevicePlugin.daemonsetName               | string | `"nvidia-device-plugin-daemonset"`                       | DNS-1123 name of the generated DaemonSet.                     |
| nvidiaDevicePlugin.image                       | string | `"mcr.microsoft.com/oss/v2/nvidia/k8s-device-plugin:v0.18.2-1"` | Full image reference for the device plugin container. |
| nvidiaDevicePlugin.imagePullPolicy             | string | `"IfNotPresent"`                                         | Allowed values: `Always`, `IfNotPresent`, `Never`.            |
| featureGates.vLLM                              | bool   | `true`                                                   | Allowed values: `true`, `false`. Enables the vLLM inference runtime feature gate. |
| featureGates.disableNodeAutoProvisioning       | bool   | `false`                                                  | Allowed values: `true`, `false`. When `true`, disables Node Auto-Provisioning (NAP) and installs the `gpu-feature-discovery` subchart as a standalone replacement. |
| featureGates.gatewayAPIInferenceExtension      | bool   | `false`                                                  | Allowed values: `true`, `false`. Enables the Gateway API Inference Extension (also gates installation of the GAIE subchart). |
| featureGates.enableInferenceSetController      | bool   | `false`                                                  | Allowed values: `true`, `false`. Enables the InferenceSet controller and its RBAC. |
| gpu-feature-discovery.nfd.enabled              | bool   | `true`                                                   | Allowed values: `true`, `false`. Set to `false` if NFD is already installed (e.g., via the NVIDIA GPU Operator) to avoid CRD conflicts. Only applies when the GFD subchart is active (`featureGates.disableNodeAutoProvisioning=true`). |
| gpu-feature-discovery.gfd.enabled              | bool   | `true`                                                   | Allowed values: `true`, `false`. Set to `false` if GFD is already installed (e.g., via the NVIDIA GPU Operator). Only applies when the GFD subchart is active (`featureGates.disableNodeAutoProvisioning=true`). |

## NVIDIA GPU Operator Coexistence

If your cluster already has the [NVIDIA GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/index.html) installed, it provides its own Node Feature Discovery (NFD) and GPU Feature Discovery (GFD) components. Installing KAITO's bundled copies will cause **CRD and resource conflicts**.

### How the GFD subchart is controlled

The `gpu-feature-discovery` subchart is **only installed** when `featureGates.disableNodeAutoProvisioning=true`. By default, this value is `false`, so the GFD subchart is not deployed and no NFD/GFD conflicts will occur.

If you have disabled Node Auto-Provisioning (NAP) and the GFD subchart is being installed, you can selectively disable its NFD and GFD components to avoid conflicts with the GPU Operator:

```bash
helm install workspace ./charts/kaito/workspace \
  --namespace kaito-workspace --create-namespace \
  --set featureGates.disableNodeAutoProvisioning=true \
  --set gpu-feature-discovery.nfd.enabled=false \
  --set gpu-feature-discovery.gfd.enabled=false
```

If you also want to skip the bundled NVIDIA device plugin (because the GPU Operator already provides one):

```bash
helm install workspace ./charts/kaito/workspace \
  --namespace kaito-workspace --create-namespace \
  --set featureGates.disableNodeAutoProvisioning=true \
  --set gpu-feature-discovery.nfd.enabled=false \
  --set gpu-feature-discovery.gfd.enabled=false \
  --set nvidiaDevicePlugin.enabled=false
```

> **Note:** If you are using the default configuration (`featureGates.disableNodeAutoProvisioning=false`), the `gpu-feature-discovery` subchart is not installed at all, and no NFD/GFD overrides are needed.
