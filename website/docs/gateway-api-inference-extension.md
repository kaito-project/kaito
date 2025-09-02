---
title: Gateway API Inference Extension
---

KAITO integrates with [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/) (GWIE) to provide model-aware routing and optimal endpoint selection for inference. This page covers what it is, prerequisites, how to enable it in KAITO, how it’s wired, and a quickstart.

## What is it

Gateway API Inference Extension extends [Gateway API](https://gateway-api.sigs.k8s.io/) with inference-focused backends and behaviors. It adds:

- [InferencePool](https://gateway-api-inference-extension.sigs.k8s.io/api-types/inferencepool/) CRD to represent model-serving backends
- A reference [Endpoint Picker](https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/pkg/epp) (EPP) that uses inference server metrics and policies to pick the best backend
- Optional [Body-Based Routing](https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/pkg/bbr) (BBR) that extracts model names from OpenAI-style requests and injects a header for routing purposes

KAITO uses GWIE to route requests for models to the right Workspace pods, improving latency and GPU utilization.

## Prerequisites

Before enabling this feature in KAITO, ensure the following are installed in your cluster:

- A Gateway API implementation that supports Envoy ext_proc and the Inference Extension pattern. See available Gateway implementations: https://gateway-api-inference-extension.sigs.k8s.io/implementations/gateways/

## Enable in KAITO

The feature is off by default. Enable it by setting the workspace chart feature gate:

```bash
# Based on https://kaito-project.github.io/kaito/docs/installation
helm upgrade --install kaito-workspace \
  https://github.com/kaito-project/kaito/raw/gh-pages/charts/kaito/workspace-$KAITO_WORKSPACE_VERSION.tgz \
  --namespace kaito-workspace \
  --create-namespace \
  --set featureGates.gatewayAPIInferenceExtension=true \
  --wait
```

## How KAITO wires it

When the feature gate is enabled, [Flux](https://fluxcd.io/) will be installed in the same namespace as the Workspace controller as a Helm dependency. It is used to deploy and manage the GWIE InferencePool Helm chart for each Workspace.

When you create a Workspace, the KAITO Workspace controller will:

1) Dry-run the inference workload to determine whether it’s a Deployment or StatefulSet (important for how endpoints are selected)
2) Create or update two Flux resources in the Workspace namespace:
	 - [OCIRepository](https://fluxcd.io/flux/components/source/ocirepositories/): points to the upstream GWIE inferencepool Helm chart
		 - URL: oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool
		 - Tag/Version: https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/latest
	 - [HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/): references the OCIRepository and applies values to deploy EPP and related resources
3) Wait for Flux resources to become Ready

You can inspect these resources with kubectl in the Workspace namespace. Updates to the Workspace will reconcile these resources.

## Quickstart

In this quickstart example, we will use Istio as the Gateway API provider to handle traffic management and routing, and deploy KAITO Workspaces to serve inference models. The following steps demonstrate how to set up an end-to-end inference gateway that routes requests to model-serving backends managed by KAITO.

### 1. Install Istio and Deploy Gateway

First, install Istio base and control plane components, setting flags that enable Gateway API Inference Extension support in the data plane and pilot:

```bash
TAG=1.28-alpha.89f30b26ba71bf5e538083a4720d0bc2d8c06401
HUB=gcr.io/istio-testing
helm upgrade -i istio-base oci://$HUB/charts/base --version $TAG -n istio-system --create-namespace
helm upgrade -i istiod oci://$HUB/charts/istiod \
  --version $TAG \
  -n istio-system \
  --set meshConfig.defaultConfig.proxyMetadata.ENABLE_GATEWAY_API_INFERENCE_EXTENSION="true" \
  --set pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION="true" \
  --set tag=$TAG \
  --set hub=$HUB \
  --wait
```

Then, create the Gateway resource that will handle incoming requests and integrate with GWIE per the example configuration:

```bash
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/gateway-api-inference-extension/gateway.yaml
```

### 2. Deploy KAITO Workspace

Create a sample KAITO Workspace (using a vLLM preset) that will host the model server behind the inference gateway:

```bash
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/inference/kaito_workspace_phi_4_mini.yaml
```

### 3. Deploy DestinationRule and HTTPRoute

Apply an Istio DestinationRule. Since EPP runs with `--secure-serving=true` by default using a self-signed certificate, and Istio doesn't trust self-signed certificates, this DestinationRule bypasses TLS verification as a temporary workaround:

```bash
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/gateway-api-inference-extension/destinationrule-phi-4-mini-instruct.yaml
```

Create the HTTPRoute that targets the Workspace’s InferencePool (via ExtensionRef) and defines the routing matchers used by the gateway:

```bash
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/gateway-api-inference-extension/httproute.yaml
```

### 4. Test Inference

Verify that the InferencePool is properly configured and ready to accept traffic by checking its status conditions:

```bash
kubectl describe inferencepool workspace-phi-4-mini-inferencepool

...
    Conditions:
      Last Transition Time:  2025-08-26T18:55:13Z
      Message:               Referenced by an HTTPRoute accepted by the parentRef Gateway
      Observed Generation:   1
      Reason:                Accepted
      Status:                True
      Type:                  Accepted
      Last Transition Time:  2025-08-26T18:55:13Z
      Message:               Referenced ExtensionRef resolved successfully
      Observed Generation:   1
      Reason:                ResolvedRefs
      Status:                True
      Type:                  ResolvedRefs
...
```

Get the ClusterIP of the Istio Gateway service to enable internal cluster routing:

```bash
kubectl get service

NAME                      TYPE           CLUSTER-IP     EXTERNAL-IP     PORT(S)                        AGE
inference-gateway-istio   LoadBalancer   10.0.249.124                   15021:31583/TCP,80:30314/TCP   13m
```

Export the ClusterIP for easy access and test the inference endpoint using a temporary curl pod:

```bash
export CLUSTERIP=$(kubectl get svc inference-gateway-istio -o jsonpath='{.spec.clusterIP}')
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -s  http://$CLUSTERIP/v1/models | jq

{
  "data": [
    {
      "created": 1756234889,
      "id": "phi-4-mini-instruct",
      "max_model_len": 131072,
      "object": "model",
      "owned_by": "vllm",
      "parent": null,
      "permission": [
        {
          "allow_create_engine": false,
          "allow_fine_tuning": false,
          "allow_logprobs": true,
          "allow_sampling": true,
          "allow_search_indices": false,
          "allow_view": true,
          "created": 1756234889,
          "group": null,
          "id": "modelperm-de0d47575adf467f8222aac90296aab8",
          "is_blocking": false,
          "object": "model_permission",
          "organization": "*"
        }
      ],
      "root": "/workspace/vllm/weights"
    }
  ],
  "object": "list"
}
```

Send a chat completion request to test the inference endpoint:


```bash
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-4-mini-instruct",
    "messages": [{"role": "user", "content": "What is kubernetes?"}],
    "max_tokens": 50
  }' | jq

{
  "choices": [
    {
      "finish_reason": "length",
      "index": 0,
      "logprobs": null,
      "message": {
        "annotations": null,
        "audio": null,
        "content": "Kubernetes, often abbreviated as K8s, is an open-source platform designed to automate the deployment, scaling, and operation of application containers. It was originally developed by Google and is now maintained by the Cloud Native Computing Foundation (CNCF).",
        "function_call": null,
        "reasoning_content": null,
        "refusal": null,
        "role": "assistant",
        "tool_calls": []
      },
      "stop_reason": null
    }
  ],
  "created": 1756235005,
  "id": "chatcmpl-e0a390b5-3066-4c4c-8087-80528bb5d843",
  "kv_transfer_params": null,
  "model": "phi-4-mini-instruct",
  "object": "chat.completion",
  "prompt_logprobs": null,
  "service_tier": null,
  "system_fingerprint": null,
  "usage": {
    "completion_tokens": 50,
    "prompt_tokens": 17,
    "prompt_tokens_details": null,
    "total_tokens": 67
  }
}
```

### 4. [Optional] Deploy BBR

Deploy a second KAITO Workspace and DestinationRule with a different model to demonstrate multi-model routing. This step uses [`mistral-7b-instruct`](https://github.com/kaito-project/kaito/blob/main/examples/inference/kaito_workspace_mistral_7b-instruct.yaml) as an example:

```bash
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/inference/kaito_workspace_mistral_7b-instruct.yaml
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/gateway-api-inference-extension/destinationrule-mistral-7b-instruct.yaml
```

Install the Body-Based Routing (BBR) Helm chart. BBR automatically extracts model names from OpenAI-style API requests and injects an `X-Gateway-Model-Name` header to the inference request, enabling model routing without modifying client code:

```bash
helm upgrade --install body-based-router oci://registry.k8s.io/gateway-api-inference-extension/charts/body-based-routing \
  --version v0.5.1 \
  --set provider.name=istio \
  --wait
```

Update the HTTPRoute to use header-based matching so requests are routed by the model name found in the request body:

```bash
kubectl apply -f github.com/kaito-project/kaito/blob/main/examples/gateway-api-inference-extension/httproute-bbr.yaml
```

Verify routing with the original model name; the gateway should route to the corresponding Workspace via the InferencePool:

```bash
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-4-mini-instruct",
    "messages": [{"role": "user", "content": "What is kubernetes?"}],
    "max_tokens": 50
  }' | jq

{
  "choices": [
    {
      "finish_reason": "length",
      "index": 0,
      "logprobs": null,
      "message": {
        "annotations": null,
        "audio": null,
        "content": "Kubernetes, often abbreviated as K8s, is an open-source container orchestration platform designed to automate the deployment, scaling, and management of containerized applications. It was originally developed by Google and is now maintained by the Cloud Native Computing Foundation (",
        "function_call": null,
        "reasoning_content": null,
        "refusal": null,
        "role": "assistant",
        "tool_calls": []
      },
      "stop_reason": null
    }
  ],
  "created": 1756237522,
  "id": "chatcmpl-c7aeedbd-50d1-4ac3-9005-ad8dba451e65",
  "kv_transfer_params": null,
  "model": "phi-4-mini-instruct",
  "object": "chat.completion",
  "prompt_logprobs": null,
  "service_tier": null,
  "system_fingerprint": null,
  "usage": {
    "completion_tokens": 50,
    "prompt_tokens": 17,
    "prompt_tokens_details": null,
    "total_tokens": 67
  }
}
```

Now, send the same request but change the model name to `mistral-7b-instruct` to verify BBR-driven model-aware routing across multiple Workspaces:

```bash
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "mistral-7b-instruct",
    "messages": [{"role": "user", "content": "What is kubernetes?"}],
    "max_tokens": 50
  }' | jq

{
  "choices": [
    {
      "finish_reason": "length",
      "index": 0,
      "logprobs": null,
      "message": {
        "annotations": null,
        "audio": null,
        "content": " Kubernetes (also known as K8s) is an open-source platform designed to automate deployment, scaling, and management of containerized applications. It groups containers that make up an application into logical units for easy management, and helps to ensure",
        "function_call": null,
        "reasoning_content": null,
        "refusal": null,
        "role": "assistant",
        "tool_calls": []
      },
      "stop_reason": null
    }
  ],
  "created": 1756237560,
  "id": "chatcmpl-b563a6a5-8009-43e9-aedc-4f4238d8c6b8",
  "kv_transfer_params": null,
  "model": "mistral-7b-instruct",
  "object": "chat.completion",
  "prompt_logprobs": null,
  "service_tier": null,
  "system_fingerprint": null,
  "usage": {
    "completion_tokens": 50,
    "prompt_tokens": 8,
    "prompt_tokens_details": null,
    "total_tokens": 58
  }
}
```
