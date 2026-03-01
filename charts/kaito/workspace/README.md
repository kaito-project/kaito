# KAITO Workspace Helm Chart

## Install

```bash
export REGISTRY=mcr.microsoft.com/aks/kaito
export IMG_NAME=workspace
export IMG_TAG=0.9.0
helm install workspace ./charts/kaito/workspace  \
--set image.repository=${REGISTRY}/${IMG_NAME} --set image.tag=${IMG_TAG} \
--namespace kaito-workspace --create-namespace
```

## Values

| Key                                      | Type   | Default                                 | Description                                                   |
|------------------------------------------|--------|-----------------------------------------|---------------------------------------------------------------|
| affinity                                 | object | `{}`                                    |                                                               |
| image.pullPolicy                         | string | `"IfNotPresent"`                        |                                                               |
| image.repository                         | string | `mcr.microsoft.com/aks/kaito/workspace` |                                                               |
| image.tag                                | string | `"0.3.0"`                               |                                                               |
| imagePullSecrets                         | list   | `[]`                                    |                                                               |
| nodeSelector                             | object | `{}`                                    |                                                               |
| podAnnotations                           | object | `{}`                                    |                                                               |
| podSecurityContext.runAsNonRoot          | bool   | `true`                                  |                                                               |
| presetRegistryName                       | string | `"mcr.microsoft.com/aks/kaito"`         |                                                               |
| replicaCount                             | int    | `1`                                     |                                                               |
| resources.limits.cpu                     | string | `"500m"`                                |                                                               |
| resources.limits.memory                  | string | `"128Mi"`                               |                                                               |
| resources.requests.cpu                   | string | `"10m"`                                 |                                                               |
| resources.requests.memory                | string | `"64Mi"`                                |                                                               |
| securityContext.allowPrivilegeEscalation | bool   | `false`                                 |                                                               |
| securityContext.capabilities.drop[0]     | string | `"ALL"`                                 |                                                               |
| defaultNodeImageFamily                   | string | `""`                                    | Default NodeClaim image-family annotation value. Supported values: `azurelinux`, `ubuntu`. Empty means `ubuntu`. Unsupported values cause workspace controller startup failure. |
| tolerations                              | list   | `[]`                                    |                                                               |
| webhook.port                             | int    | `9443`                                  |                                                               |
| cloudProviderName                        | string | `"azure"`                               | Karpenter cloud provider name. Values can be "azure" or "aws" |
| nvidiaDevicePlugin.enabled               | bool   | `true`                                  | Enable deployment of NVIDIA device plugin DaemonSet. Set to false if your cluster already has the NVIDIA device plugin installed (e.g., via GPU Operator). |
| gpu-feature-discovery.nfd.enabled        | bool   | `true`                                  | Enable Node Feature Discovery (NFD) deployment. Set to `false` if NFD is already installed (e.g., via the NVIDIA GPU Operator) to avoid CRD conflicts. |
| gpu-feature-discovery.gfd.enabled        | bool   | `true`                                  | Enable GPU Feature Discovery (GFD). Set to `false` if GFD is already installed (e.g., via the NVIDIA GPU Operator). |

## NVIDIA GPU Operator Coexistence

If your cluster already has the [NVIDIA GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/index.html) installed, it provides its own Node Feature Discovery (NFD) and GPU Feature Discovery (GFD) components. Installing KAITO's bundled copies will cause **CRD and resource conflicts**.

To avoid these conflicts, disable the overlapping components when installing KAITO:

```bash
helm install workspace ./charts/kaito/workspace \
  --namespace kaito-workspace --create-namespace \
  --set gpu-feature-discovery.nfd.enabled=false \
  --set gpu-feature-discovery.gfd.enabled=false
```

If you also want to skip the bundled NVIDIA device plugin (because the GPU Operator already provides one):

```bash
helm install workspace ./charts/kaito/workspace \
  --namespace kaito-workspace --create-namespace \
  --set gpu-feature-discovery.nfd.enabled=false \
  --set gpu-feature-discovery.gfd.enabled=false \
  --set nvidiaDevicePlugin.enabled=false
```

Alternatively, if you don't need GPU auto-provisioning at all, you can disable the entire `gpu-feature-discovery` subchart:

```bash
helm install workspace ./charts/kaito/workspace \
  --namespace kaito-workspace --create-namespace \
  --set featureGates.disableNodeAutoProvisioning=true
```

> **Note:** The `featureGates.disableNodeAutoProvisioning` flag controls whether the `gpu-feature-discovery` subchart is installed. When set to `true`, no GFD or NFD components are deployed by KAITO.
