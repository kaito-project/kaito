# KAITO Workspace Helm Chart

## Install

Install DACS separately first as described below, or disable its integration
with `--set cache.providers.dacs.enabled=false` when it is not needed.

```bash
export REGISTRY=mcr.microsoft.com/aks/kaito
export IMG_NAME=workspace
export IMG_TAG=0.13.0
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
| image.tag                                      | string | `"0.13.0"`                                               | Controller image tag.                                         |
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
| defaultNodeImageFamily                         | string | `""`                                                     | Default NodeClaim image-family annotation. Only used by the GPU provisioner path (not karpenter). Allowed values: `""` (treated as `ubuntu`), `ubuntu`, `azurelinux`. Any other value causes controller startup failure. |
| nodeProvisioner                                | string | `"azure-gpu-provisioner"`                                | Node provisioner type. Allowed values: `azure-gpu-provisioner`, `karpenter`, `byo`. |
| karpenterProvider                              | string | `"azure"`                                                | Selects which provider block under `karpenterProviders` to use. Only used when `nodeProvisioner=karpenter`. |
| karpenterProviders.azure.group                 | string | `"karpenter.azure.com"`                                  | Karpenter NodeClass API group. |
| karpenterProviders.azure.kind                  | string | `"AKSNodeClass"`                                         | Karpenter NodeClass API kind. |
| karpenterProviders.azure.version               | string | `"v1beta1"`                                              | Karpenter NodeClass API version. |
| karpenterProviders.azure.resourceName          | string | `"aksnodeclasses"`                                       | Plural resource name for the NodeClass CRD. Combined with `group` to form the full CRD name. |
| karpenterProviders.azure.nodeClasses           | list   | (see values.yaml)                                        | NodeClass definitions to reconcile at startup, and the allowlist for the `kaito.sh/node-class-name` annotation. Exactly one entry must have `default: true`. Each entry has `name`, `spec`, and optionally `default: true`. Passed to the controller as `--karpenter-node-classes`, so changing it rolls the Deployment. |
| tolerations                                    | list   | `[]`                                                     | Controller pod tolerations.                                   |
| webhook.port                                   | int    | `9443`                                                   | Webhook HTTPS port. Valid TCP port (1–65535); must not conflict with other container ports. |
| logging.level                                  | string | `"error"`                                                | Knative zap logging level. Allowed values: `debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal`. |
| cloudProviderName                              | string | `"azure"`                                                | Cloud provider identifier propagated as the `CLOUD_PROVIDER` env var. Allowed values: `azure`, `aws`, `arc`. |
| clusterName                                    | string | `"kaito"`                                                | Logical Kubernetes cluster name used in controller labels/metrics. |
| spotInstance.enabled                           | bool   | `false`                                                  | Allowed values: `true`, `false`. When `true`, adds the Azure Spot toleration so workloads can be scheduled on Spot GPU node pools. |
| localCSIDriver.useLocalCSIDriver               | bool   | `true`                                                   | Enables local CSI for downloaded model weights. Its node affinity excludes DACS cache nodes, so both providers can be installed. |
| local-csi-driver.daemonset.nodeAffinity        | object | (see values.yaml)                                        | Required affinity excludes nodes labeled `dacs.azure.com/processing-unit=cpu`. The subchart also requires Linux amd64/arm64. |
| nvidiaDevicePlugin.enabled                     | bool   | `true`                                                   | Allowed values: `true`, `false`. Set to `false` if the cluster already has an NVIDIA device plugin (e.g., via GPU Operator). |
| nvidiaDevicePlugin.daemonsetName               | string | `"nvidia-device-plugin-daemonset"`                       | DNS-1123 name of the generated DaemonSet.                     |
| nvidiaDevicePlugin.image                       | string | `"mcr.microsoft.com/oss/v2/nvidia/k8s-device-plugin:v0.20.1-5"` | Full image reference for the device plugin container. |
| nvidiaDevicePlugin.imagePullPolicy             | string | `"IfNotPresent"`                                         | Allowed values: `Always`, `IfNotPresent`, `Never`.            |
| featureGates.vLLM                              | bool   | `true`                                                   | Allowed values: `true`, `false`. Enables the vLLM inference runtime feature gate. |
| featureGates.disableNodeAutoProvisioning       | bool   | `false`                                                  | Allowed values: `true`, `false`. When `true`, disables Node Auto-Provisioning (NAP) and installs the `gpu-feature-discovery` subchart as a standalone replacement. |
| featureGates.gatewayAPIInferenceExtension      | bool   | `false`                                                  | Allowed values: `true`, `false`. Enables the Gateway API Inference Extension (also gates installation of the GAIE subchart). |
| featureGates.enableInferenceSetController      | bool   | `true`                                                  | Allowed values: `true`, `false`. Enables the InferenceSet controller and its RBAC. |
| featureGates.distributedCache                  | bool   | `false`                                                  | Allowed values: `true`, `false`. Enables the distributed cache feature (model weights and KV cache injection). |
| cache.providers.dacs.enabled                   | bool   | `true`                                                   | Configures the DACS runtime provider; does not install DACS. Injection additionally requires `featureGates.distributedCache=true` and an applicable workspace cache attachment. |
| cache.providers.dacs.discoveryEndpoint         | string | `""`                                                     | DACS cache server discovery URL. Auto-discovered from the Cache CR status when empty. |
| cache.providers.dacs.clientImage               | string | `"mcr.microsoft.com/dacs/runai-dacs-client:v0.1.0"` | OCI image bundling the DACS client libraries, mounted into inference pods as an ImageVolume. Requires glibc >= 2.35 (Ubuntu 22.04+). |
| cache.providers.dacs.cache.enabled             | bool   | `true` | Creates the KAITO-owned Cache resource when the DACS provider is enabled. Set to `false` to use an existing externally managed Cache. |
| cache.providers.dacs.cache.name                | string | `"cache-sample"` | Name of the Cache resource. |
| cache.providers.dacs.cache.namespace           | string | `"kube-system"` | Namespace of the Cache resource; empty uses the Helm release namespace. |
| cache.providers.dacs.cache.spec                | object | (see values.yaml) | Cache CR spec, including node selection, dynamic/static scaling, capacity, and storage. |
| gpu-feature-discovery.nfd.enabled              | bool   | `true`                                                   | Allowed values: `true`, `false`. Set to `false` if NFD is already installed (e.g., via the NVIDIA GPU Operator) to avoid CRD conflicts. Only applies when the GFD subchart is active (`featureGates.disableNodeAutoProvisioning=true`). |
| gpu-feature-discovery.gfd.enabled              | bool   | `true`                                                   | Allowed values: `true`, `false`. Set to `false` if GFD is already installed (e.g., via the NVIDIA GPU Operator). Only applies when the GFD subchart is active (`featureGates.disableNodeAutoProvisioning=true`). |

## DACS Cache configuration

DACS must be installed as a **separate Helm release**, or supplied by an existing
managed installation. Do not install a second controller over managed DACS.
Separate releases avoid global Helm helper-name collisions with local CSI.
KAITO renders the Cache resource from
`cache.providers.dacs.cache`, keeping node selection and scaling configurable
through this chart:

```bash
# Install/upgrade the CRD before installing KAITO or upgrading DACS.
helm show crds oci://mcr.microsoft.com/dacs/helmchart/dacs --version 0.1.0 |
  kubectl apply -f -
kubectl wait --for=condition=Established --timeout=120s crd/caches.storage.azure.com

# Requires dedicated, unused NVMe nodes labeled dacs.azure.com/processing-unit=cpu.
helm upgrade --install dacs oci://mcr.microsoft.com/dacs/helmchart/dacs \
  --version 0.1.0 --namespace kube-system --create-namespace \
  --set cacheServerPrereq.enabled=true --wait --timeout 15m
```

These are DACS release values, not workspace chart values. The prerequisite's
default selector is `dacs.azure.com/processing-unit=cpu`; the workspace chart
uses the same label for its Cache and excludes these nodes from local CSI.

```yaml
cache:
  providers:
    dacs:
      cache:
        enabled: true
        name: cache-sample
        namespace: kube-system
        spec:
          nodeSelectorKey: dacs.azure.com/processing-unit
          nodeSelectorValue: cpu
          scaling:
            dynamic:
              percentageOfNodesToUse: 100
              minServers: 1
              maxServers: 100
```

Local CSI and DACS can coexist on **separate storage nodes**:

- Inference nodes without `dacs.azure.com/processing-unit=cpu` can run local CSI
  and store non-streamed weights in local NVMe PVCs.
- Dedicated NVMe cache nodes labeled `dacs.azure.com/processing-unit=cpu` run
  DACS disk preparation and cache servers; required node affinity excludes local CSI.
- Streaming workspaces with a DACS model-weight cache attachment read through
  Run:ai and DACS; enabling DACS does not force all workspaces to stream.

Use node-pool labels so placement survives node replacement. The label is a
storage-ownership designation, not automatic CPU/GPU detection. If changing it,
update the Cache selector, prerequisite selector and local-CSI exclusion together.
Do not relabel nodes with existing local-CSI volumes or DACS data: both components
manage local disks, and `requiredDuringSchedulingIgnoredDuringExecution` does not
evict an already-running driver. Prepare separate unused nodes instead.

Provision the labeled cache nodes before installing the DACS prerequisite:
its rollout hook requires a targeted node. Disk preparation can format unused NVMe
devices, so review node ownership before enabling it. An alternative is to disable
`cacheServerPrereq.enabled` in the **DACS release** and supply already-prepared
cache storage. Azure CI installs the controller separately with this prerequisite
disabled; that provides the API, not a functioning cache data plane.
Installing local CSI requires chart deployment; changing these values alone does
not repair an existing cluster with a stale StorageClass.

For static scaling, set `spec.scaling.dynamic: null` and
`spec.scaling.static.numServers` to the desired count. To use an externally
managed Cache, set `cache.providers.dacs.cache.enabled=false`. The former
`dacs.cache.*` and `dacs.cacheServer.*` values do not configure Cache resources
in this version; `dacs.cache.enabled=true` is rejected upstream.

When upgrading an older release that owns a Cache, preserve its name, namespace,
and spec in `cache.providers.dacs.cache` so Helm retains the same resource.
Simply disabling the old Cache template without a replacement can cause Helm to
delete it. An existing Cache owned by a different Helm release must be migrated
before enabling KAITO management. Apply the target DACS CRD before upgrading an
existing installation, since Helm does not upgrade CRDs automatically.

Upgrades from a KAITO release that bundled DACS also need an ownership migration:
removing a subchart can delete its old controller, webhook and RBAC resources.
Do not upgrade such a release in place without arranging that migration first.
The CI path installs on disposable fresh clusters and does not perform it.

## Karpenter NodeClass Updates

When `nodeProvisioner=karpenter`, KAITO reconciles the configured NodeClasses at
controller startup. Missing resources are created. Existing resources must have
`karpenter.kaito.sh/managed-by: kaito`; otherwise startup fails without adopting
them.

The configured `spec` is authoritative: omitted fields are removed, subject to
defaults applied by the provider API. KAITO also synchronizes its default label,
while preserving unrelated metadata and status. Changing the chart values rolls
the controller Deployment and applies the new configuration. Manual edits are
not continuously reconciled, but may be overwritten on the next restart.

Removing an entry does not delete its NodeClass; it only removes the name from
the Workspace selection allowlist. Spec updates may trigger Karpenter drift and
node replacement according to the NodePool disruption policies.

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
