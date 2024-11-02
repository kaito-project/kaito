# Kubernetes AI Toolchain Operator (Kaito)

![GitHub Release](https://img.shields.io/github/v/release/Azure/kaito)
[![Go Report Card](https://goreportcard.com/badge/github.com/kaito-project/kaito)](https://goreportcard.com/report/github.com/kaito-project/kaito)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/Azure/kaito)
[![codecov](https://codecov.io/gh/Azure/kaito/graph/badge.svg?token=XAQLLPB2AR)](https://codecov.io/gh/Azure/kaito)

| ![notification](docs/img/bell.svg) What is NEW! |
|-------------------------------------------------|
| Latest Release: Sep 9th, 2024. Kaito v0.3.1.    |
| First Release: Nov 15th, 2023. Kaito v0.1.0.    |

Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster.
The target models are popular open-sourced large models such as [falcon](https://huggingface.co/tiiuae) and [phi-3](https://huggingface.co/docs/transformers/main/en/model_doc/phi3).
Kaito has the following key differentiations compared to most of the mainstream model deployment methodologies built on top of virtual machine infrastructures:

- Manage large model files using container images. A http server is provided to perform inference calls using the model library.
- Provide preset configurations to avoid adjusting workload parameters based on GPU hardware.
- Auto-provision GPU nodes based on model requirements.
- Host large model images in the public Microsoft Container Registry (MCR) if the license allows.

Using Kaito, the workflow of onboarding large AI inference models in Kubernetes is largely simplified.

## Architecture

Kaito follows the classic Kubernetes Custom Resource Definition(CRD)/controller design pattern. User manages a `workspace` custom resource which describes the GPU requirements and the inference or tuning specification. Kaito controllers will automate the deployment by reconciling the `workspace` custom resource.
<div align="left">
  <img src="docs/img/arch.png" width=80% title="Kaito architecture" alt="Kaito architecture">
</div>

The above figure presents the Kaito architecture overview. Its major components consist of:

- **Workspace controller**: It reconciles the `workspace` custom resource, creates `machine` (explained below) custom resources to trigger node auto provisioning, and creates the inference or tuning workload (`deployment`, `statefulset` or `job`) based on the model preset configurations.
- **Node provisioner controller**: The controller's name is *gpu-provisioner* in [gpu-provisioner helm chart](https://github.com/Azure/gpu-provisioner/tree/main/charts/gpu-provisioner). It uses the `machine` CRD originated from [Karpenter](https://sigs.k8s.io/karpenter) to interact with the workspace controller. It integrates with Azure Resource Manager REST APIs to add new GPU nodes to the AKS cluster.
> Note: The [*gpu-provisioner*](https://github.com/Azure/gpu-provisioner) is an open sourced component. It can be replaced by other controllers if they support [Karpenter-core](https://sigs.k8s.io/karpenter) APIs.

## Installation

Please check the installation guidance [here](./docs/installation.md) for deployment using Azure CLI and [here](./terraform/README.md) for deployment using Terraform.

## Quick start

After installing Kaito, one can try following commands to start a falcon-7b inference service.

```sh
$ cat examples/inference/kaito_workspace_falcon_7b.yaml
apiVersion: kaito.sh/v1alpha1
kind: Workspace
metadata:
  name: workspace-falcon-7b
resource:
  instanceType: "Standard_NC12s_v3"
  labelSelector:
    matchLabels:
      apps: falcon-7b
inference:
  preset:
    name: "falcon-7b"

$ kubectl apply -f examples/inference/kaito_workspace_falcon_7b.yaml
```

The workspace status can be tracked by running the following command. When the WORKSPACEREADY column becomes `True`, the model has been deployed successfully.

```sh
$ kubectl get workspace workspace-falcon-7b
NAME                  INSTANCE            RESOURCEREADY   INFERENCEREADY   WORKSPACEREADY   AGE
workspace-falcon-7b   Standard_NC12s_v3   True            True             True             10m
```

Next, one can find the inference service's cluster ip and use a temporal `curl` pod to test the service endpoint in the cluster.

```sh
$ kubectl get svc workspace-falcon-7b
NAME                  TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)            AGE
workspace-falcon-7b   ClusterIP   <CLUSTERIP>  <none>        80/TCP,29500/TCP   10m

export CLUSTERIP=$(kubectl get svc workspace-falcon-7b -o jsonpath="{.spec.clusterIPs[0]}") 
$ kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/chat -H "accept: application/json" -H "Content-Type: application/json" -d "{\"prompt\":\"YOUR QUESTION HERE\"}"
```

## Usage

The detailed usage for Kaito supported models can be found in [**HERE**](presets/README.md). In case users want to deploy their own containerized models, they can provide the pod template in the `inference` field of the workspace custom resource (please see [API definitions](api/v1alpha1/workspace_types.go) for details). The controller will create a deployment workload using all provisioned GPU nodes. Note that currently the controller does **NOT** handle automatic model upgrade. It only creates inference workloads based on the preset configurations if the workloads do not exist.

The number of the supported models in Kaito is growing! Please check [this](./docs/How-to-add-new-models.md) document to see how to add a new supported model.

Starting with version v0.3.0, Kaito supports model fine-tuning and using fine-tuned adapters in the inference service. Refer to the [tuning document](./docs/tuning/README.md) and [inference document](./docs/inference/README.md) for more information.

## FAQ

### How do I ensure preferred nodes are correctly labeled for use in my workspace?

For using preferred nodes, make sure the node has the label specified in the labelSelector 
under matchLabels. For example, if your labelSelector is:
```
  labelSelector:
    matchLabels:
      apps: falcon-7b
```
Then the node should have the label: `apps=falcon-7b`.

### How to upgrade the existing deployment to use the latest model configuration?

When using hosted public models, a user can delete the existing inference workload (`Deployment` of `StatefulSet`) manually, and the workspace controller will create a new one with the latest preset configuration (e.g., the image version) defined in the current release. For private models, it is recommended to create a new workspace with a new image version in the Spec.

### How to update model/inference parameters to override the Kaito Preset Configuration?

Kaito provides a limited capability to override preset configurations for models that use `transformer` runtime manually.
To update parameters for a deployed model, perform `kubectl edit` against the workload, which could be either a `StatefulSet` or `Deployment`.
For example, to enable 4-bit quantization on a `falcon-7b-instruct` deployment, you would execute:

```sh
kubectl edit deployment workspace-falcon-7b-instruct
```

Within the deployment specification, locate and modify the command field.

#### Original

```sh
accelerate launch --num_processes 1 --num_machines 1 --machine_rank 0 --gpu_ids all inference_api.py --pipeline text-generation --torch_dtype bfloat16
```

#### Modify to enable 4-bit Quantization

```sh
accelerate launch --num_processes 1 --num_machines 1 --machine_rank 0 --gpu_ids all inference_api.py --pipeline text-generation --torch_dtype bfloat16 --load_in_4bit
```

Currently, we allow users to change the following paramenters manually:

- `pipeline`: For text-generation models this can be either `text-generation` or `conversational`.
- `load_in_4bit` or `load_in_8bit`: Model quantization resolution.

Should you need to customize other parameters, kindly file an issue for potential future inclusion.

### What is the difference between instruct and non-instruct models?

The main distinction lies in their intended use cases. Instruct models are fine-tuned versions optimized
for interactive chat applications. They are typically the preferred choice for most implementations due to their enhanced performance in
conversational contexts.
On the other hand, non-instruct, or raw models, are designed for further fine-tuning. 

## Contributing

[Read more](docs/contributing/readme.md)
<!-- markdown-link-check-disable -->
This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit <https://cla.opensource.microsoft.com>.

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft
trademarks or logos is subject to and must follow [Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/legal/intellectualproperty/trademarks/usage/general).
Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship.
Any use of third-party trademarks or logos are subject to those third-party's policies.

## License

See [LICENSE](LICENSE).

## Code of Conduct

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/). For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

<!-- markdown-link-check-enable -->
## Contact

"Kaito devs" <kaito-dev@microsoft.com>
