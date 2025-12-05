# KAITO RAGEngine Helm Chart

KAITO RAGEngine provides Retrieval-Augmented Generation (RAG) capabilities for AI workloads in Kubernetes. This Helm chart installs the RAGEngine controller that manages RAGEngine custom resources, enabling you to deploy and manage RAG services with embedding models and vector stores.

## Install

```bash
helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito/ragengine \
  --namespace kaito-ragengine \
  --create-namespace
```

## Prerequisites

- Kubernetes 1.20+
- Helm 3.0+
- KAITO Workspace controller (if using with workspace resources)

## Usage

After installing the RAGEngine Helm chart, you can create RAGEngine resources to deploy RAG services:

```yaml
apiVersion: kaito.sh/v1alpha1
kind: RAGEngine
metadata:
  name: ragengine-example
spec:
  compute:
    instanceType: "Standard_NC6s_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
```

### Using Persistent Storage

To enable persistent storage for vector indexes, add a `storage` specification:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-ragengine-vector-db
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: managed-csi-premium
  resources:
    requests:
      storage: 50Gi
---
apiVersion: kaito.sh/v1alpha1
kind: RAGEngine
metadata:
  name: ragengine-with-storage
spec:
  compute:
    instanceType: "Standard_NC6s_v3"
    labelSelector:
      matchLabels:
        apps: ragengine-example
  storage:
    persistentVolumeClaim: pvc-ragengine-vector-db
    mountPath: /mnt/vector-db
  embedding:
    local:
      modelID: "BAAI/bge-small-en-v1.5"
  inferenceService:
    url: "<inference-url>/v1/completions"
```

With persistent storage configured, vector indexes are automatically saved during pod termination and restored on startup.

## Values

| Key                          | Type   | Default                                      | Description                                                   |
|------------------------------|--------|----------------------------------------------|---------------------------------------------------------------|
| affinity                     | object | `{}`                                         | Pod affinity settings                                         |
| cloudProviderName            | string | `"azure"`                                    | Karpenter cloud provider name. Values can be "azure" or "aws" |
| image.pullPolicy             | string | `"IfNotPresent"`                             | Image pull policy                                             |
| image.repository             | string | `"mcr.microsoft.com/aks/kaito/ragengine"`    | RAGEngine controller image repository                         |
| image.tag                    | string | `"0.0.1"`                                    | RAGEngine controller image tag                                |
| imagePullSecrets             | list   | `[]`                                         | Image pull secrets                                            |
| nodeSelector                 | object | `{}`                                         | Node selector for pod assignment                              |
| podAnnotations               | object | `{}`                                         | Pod annotations                                               |
| podSecurityContext.runAsNonRoot | bool | `true`                                       | Run container as non-root user                                |
| presetRagRegistryName        | string | `"aimodelsregistrytest.azurecr.io"`          | Registry for preset RAG service images                        |
| presetRagImageName           | string | `"kaito-rag-service"`                        | Name of the preset RAG service image                          |
| presetRagImageTag            | string | `"0.3.2"`                                    | Tag of the preset RAG service image                           |
| replicaCount                 | int    | `1`                                          | Number of replicas for the RAGEngine controller              |
| resources.limits.cpu         | string | `"500m"`                                     | CPU resource limits                                           |
| resources.limits.memory      | string | `"128Mi"`                                    | Memory resource limits                                        |
| resources.requests.cpu       | string | `"10m"`                                      | CPU resource requests                                         |
| resources.requests.memory    | string | `"64Mi"`                                     | Memory resource requests                                      |
| securityContext.allowPrivilegeEscalation | bool | `false`                           | Allow privilege escalation                                    |
| securityContext.capabilities.drop[0] | string | `"ALL"`                               | Capabilities to drop                                          |
| tolerations                  | list   | `[]`                                         | Pod tolerations                                               |
| webhook.port                 | int    | `9443`                                       | Webhook server port                                           |

## Contributing

Please refer to the [KAITO project contribution guidelines](https://github.com/kaito-project/kaito/blob/main/CONTRIBUTING.md) for information on how to contribute to this project.
