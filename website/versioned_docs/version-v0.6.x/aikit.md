# AIKit Integration with KAITO

[AIKit](https://github.com/sozercan/aikit/) provides a streamlined way to package and deploy large language models (LLMs) as container images.

This document demonstrates how to integrate AIKit-built models with KAITO workspaces for efficient AI model deployment on Kubernetes, including CPU-based inference and custom model creation with a variety of supported formats, such as GGUF, GPTQ, EXL2, and more.

For more detailed information about AIKit, please refer to the [AIKit documentation](https://sozercan.github.io/aikit/docs/). For any AIKit-related issues, please open an issue in the [AIKit repository](https://github.com/sozercan/aikit/issues).

## Overview

AIKit enables you to:

- 📦 [Package AI models](https://sozercan.github.io/aikit/docs/create-images) as OCI container images with minimal configuration
- 🤏 Minimal image size, resulting in less vulnerabilities and smaller attack surface with a custom distroless-based image
- 🏃 Run models with a variety of inference backends, such as text or image generation
- 🖥️ Supports [AMD64 and ARM64 CPUs](https://sozercan.github.io/aikit/docs/create-images#multi-platform-support) and [GPU-accelerated inferencing with NVIDIA GPUs](https://sozercan.github.io/aikit/docs/gpu)
- 🪄 Integrate seamlessly with KAITO's infrastructure management and deployment workflows

:::note

While AIKit and KAITO integrate well, they are separate projects. AIKit focuses on model packaging and deployment, while KAITO provides infrastructure management and Kubernetes deployment workflows via controllers. There may be differences in what model formats are supported by each project.

:::

## Deploying AIKit Models to KAITO

### Cluster Setup

This guide will provide instructions using a [kind](https://kind.sigs.k8s.io/) cluster for local development and testing so it's easy to get started.

Please note that if you already have a Kubernetes cluster set up, you can skip the cluster setup section.

- Download and install [kind](https://kind.sigs.k8s.io/docs/user/quick-start/)

- Create a kind cluster:

```bash
kind create cluster --name kaito
```

- Install [KAITO workspace controller](installation.md#install-kaito-workspace-controller) on your cluster

### KAITO Workspace Configuration

Create a KAITO workspace configuration file to deploy your model. Here's a complete example:

```yaml title="aikit-workspace.yaml"
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-llama-3point2-3b
resource:
  labelSelector:
    matchLabels:
      apps: llama-3point2-3b
  preferredNodes:
    - kaito-control-plane
inference:
  template:
    spec:
      containers:
        - name: llama-3point2-3b
          image: ghcr.io/sozercan/llama3.2:3b
          args:
            - "run"
            - "--address=:5000"
```

:::info Memory Requirements
Before deploying models, check the model's memory requirements to avoid Out of Memory (OOM) errors. Add appropriate `resources.requests.memory` and `resources.limits.memory` to your container spec based on the model requirements.

For GGUF models:

- 7B models generally require at least 8GB of RAM
- 13B models generally require at least 16GB of RAM
- 70B models generally require at least 64GB of RAM

You can use [gguf-parser-go](https://github.com/gpustack/gguf-parser-go) to get a better estimate for the memory requirements for a given GGUF model, and quantization.
:::

Label the nodes with the applicable label to ensure the workspace can schedule pods on them.

```bash
kubectl label nodes kaito-control-plane apps=llama-3point2-3b
```

Deploy the workspace using:

```bash
kubectl apply -f aikit-workspace.yaml
```

AIKit provides a number of pre-built and curated models that can be used directly. Please refer to [Pre-made Models](https://sozercan.github.io/aikit/docs/premade-models) for available options.

:::tip

Alternatively, if you are on a supported cloud provider and want the cloud provider to auto-provision the nodes for you, you can define an `instanceType` for KAITO to autoprovision nodes, including CPU and GPU nodes.

You can specify the instance type based on your cloud provider's offerings. For example, for Azure, you can specify a `Standard_D2ads_v5`, which is a CPU SKU like this:

```yaml
resource:
  instanceType: "Standard_D2ads_v5"
  labelSelector:
    matchLabels:
      apps: llama-3point2-3b
```

:::

After workspace deployment succeeds, please refer to [Quick Start](quick-start.mdx#monitor-deployment) for monitoring the workspace and testing model inference.

#### Custom Model Creation and Integration

AIKit provides a simple way to create custom models without additional tools except for [Docker](https://docs.docker.com/desktop/install/linux-install/)!

Here's an example on how to create a custom model and integrate it with KAITO:

```bash
export IMAGE_NAME="your-registry/your-model:latest"

docker buildx build -t $IMAGE_NAME --push \
    --build-arg="model=huggingface://TheBloke/Llama-2-7B-Chat-GGUF/llama-2-7b-chat.Q4_K_M.gguf" \
    "https://raw.githubusercontent.com/sozercan/aikit/main/models/aikitfile.yaml"
```

After building the image, you can use it in your KAITO workspace configuration by updating the `image` field.

For more information on creating custom models, refer to the [AIKit documentation](https://sozercan.github.io/aikit/docs/create-images).

:::info
AIKit supports a subset of backends, (such as [`llama.cpp`](https://sozercan.github.io/aikit/docs/llama-cpp), [`diffusers`](https://sozercan.github.io/aikit/docs/diffusion), [`exllamav2`](https://sozercan.github.io/aikit/docs/exllama2), and others) from [LocalAI](https://localai.io/) at this time. Please see [Inference Supported Backends](https://sozercan.github.io/aikit/docs/) section for more details, and updates.
:::
