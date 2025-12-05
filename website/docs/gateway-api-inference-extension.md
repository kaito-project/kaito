---
title: Gateway API Inference Extension
---

KAITO integrates with [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/) (GWIE) to provide model-aware routing and optimal endpoint selection for inference. This page covers what it is, prerequisites, how to enable it in KAITO, how it's wired, and a quickstart.

## What is it

Gateway API Inference Extension extends [Gateway API](https://gateway-api.sigs.k8s.io/) with inference-focused backends and behaviors. It adds:

- [InferencePool](https://gateway-api-inference-extension.sigs.k8s.io/api-types/inferencepool/) CRD to represent model-serving backends
- A reference [Endpoint Picker](https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/pkg/epp) (EPP) that uses inference server metrics and policies to pick the best backend
- Optional [Body-Based Routing](https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/pkg/bbr) (BBR) that extracts model names from OpenAI-style requests and injects a header for routing purposes

KAITO uses GWIE to route requests for models to the right Workspace pods, improving latency and GPU utilization.

## Prerequisites

Before enabling this feature in KAITO, ensure the following are installed in your cluster:

- A Gateway API implementation that supports Envoy ext_proc and the Inference Extension pattern. See available Gateway implementations: https://gateway-api-inference-extension.sigs.k8s.io/implementations/gateways/

## Enable this feature

This feature is supported from KAITO v0.8.0, to enable this feature in KAITO helm chart install, you need to enable both InferenceSet Controller and `gatewayAPIInferenceExtension`.

```bash
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set featureGates.gatewayAPIInferenceExtension=true \
  --set featureGates.enableInferenceSetController=true \
  --wait
```

## How KAITO wires it

When the feature gate is enabled, [Flux](https://fluxcd.io/) will be installed in the same namespace as the InferenceSet controller as a Helm dependency. It is used to deploy and manage the GWIE InferencePool Helm chart for each InferenceSet.

When you create a InferenceSet, the KAITO InferenceSet controller will:

1) Dry-run the inference workload to determine whether it's a Deployment or StatefulSet (important for how endpoints are selected)
2) Create or update two Flux resources in the InferenceSet namespace:
	 - [OCIRepository](https://fluxcd.io/flux/components/source/ocirepositories/): points to the upstream GWIE inferencepool Helm chart
		 - URL: oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool
		 - Tag/Version: https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/latest
	 - [HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/): references the OCIRepository and applies values to deploy EPP and related resources
3) Wait for Flux resources to become Ready

You can inspect these resources with kubectl in the InferenceSet namespace. Updates to the InferenceSet will reconcile these resources.

## Quickstart

In this quickstart example, we will use Istio as the Gateway API provider to handle traffic management and routing, and deploy KAITO InferenceSet to serve inference models. The following steps demonstrate how to set up an end-to-end inference gateway that routes requests to model-serving backends managed by KAITO.

### 1. Install Istio and Deploy Gateway

First, install Istio base and control plane components, setting flags that enable Gateway API Inference Extension support in the data plane and pilot:

```bash
# Based on https://github.com/istio/istio/commit/2d5fc65b386ac3c3eff28aee4040dce37923b9b7
TAG=1.28-alpha.2d5fc65b386ac3c3eff28aee4040dce37923b9b7
HUB=gcr.io/istio-testing
helm upgrade -i istio-base oci://$HUB/charts/base --version $TAG -n istio-system --create-namespace
helm upgrade -i istiod oci://$HUB/charts/istiod \
  --version $TAG \
  -n istio-system \
  --set pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION="true" \
  --set tag=$TAG \
  --set hub=$HUB \
  --wait
```

Then, deploy Gateway API CRDs and create the Gateway resource that will handle incoming requests and integrate with GWIE per the example configuration:

```bash
kubectl apply -k https://github.com/kubernetes-sigs/gateway-api/config/crd?ref=v1.3.0
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/gateway.yaml
```

### 2. Deploy InferenceSet

Create a sample KAITO InferenceSet (using a vLLM preset) that will host the model server behind the inference gateway:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/inference/kaito_inferenceset_phi_4_mini.yaml
```

Once the InferenceSet is created, verify that Flux's OCIRepository and HelmRelease resources are ready in the InferenceSet namespace:

```bash
kubectl get ocirepository,helmrelease

NAME                                                              URL                                                                          READY   STATUS                                                                                                        AGE
ocirepository.source.toolkit.fluxcd.io/phi-4-mini-inferencepool   oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool   True    stored artifact for digest 'v1.0.1@sha256:301b913dbff1d75017db0962b621e6780777dcb658475df60d1c6b5b84ee1635'   33s

helmrelease.helm.toolkit.fluxcd.io/phi-4-mini-inferencepool   32s   True    Helm install succeeded for release default/phi-4-mini-inferencepool.v1 with chart inferencepool@1.0.1+301b913dbff1
```

Verify that the InferencePool resource is created:

```bash
kubectl get inferencepool

NAME                       AGE
phi-4-mini-inferencepool   69s
```

Verify that the Endpoint Picker Pod is running in the InferenceSet namespace:

```bash
kubectl get pod -l inferencepool=phi-4-mini-inferencepool-epp

NAME                                           READY   STATUS    RESTARTS   AGE
phi-4-mini-inferencepool-epp-b74f8994b-s9kkt   1/1     Running   0          87s
```

### 3. Deploy DestinationRule and HTTPRoute

Apply an Istio DestinationRule. Since EPP runs with `--secure-serving=true` by default using a self-signed certificate, and Istio doesn't trust self-signed certificates, this DestinationRule bypasses TLS verification as a temporary workaround:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/destinationrule-phi-4-mini-instruct.yaml
```

Create the HTTPRoute that targets the InferenceSet's InferencePool (via `.spec.endpointPickerRef`) and defines the routing matchers used by the Gateway:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/httproute.yaml
```

### 4. Test Inference

Verify that the HTTPRoute is properly configured and accepted by the Gateway:

```bash
kubectl describe httproute llm-route

...
Status:
  Parents:
    Conditions:
      Last Transition Time:  2025-12-02T08:59:58Z
      Message:               Route was valid
      Observed Generation:   2
      Reason:                Accepted
      Status:                True
      Type:                  Accepted
      Last Transition Time:  2025-12-03T13:35:59Z
      Message:               All references resolved
      Observed Generation:   2
      Reason:                ResolvedRefs
      Status:                True
      Type:                  ResolvedRefs
    Controller Name:         istio.io/gateway-controller
    Parent Ref:
      Group:  gateway.networking.k8s.io
      Kind:   Gateway
      Name:   inference-gateway
...
```

Verify that the InferencePool is properly configured and ready to accept traffic by checking its status conditions:

```bash
kubectl describe inferencepool phi-4-mini-inferencepool

...
    Conditions:
      Last Transition Time:  2025-12-03T13:35:59Z
      Message:               Referenced by an HTTPRoute accepted by the parentRef Gateway
      Observed Generation:   1
      Reason:                Accepted
      Status:                True
      Type:                  Accepted
      Last Transition Time:  2025-12-03T13:35:59Z
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
inference-gateway-istio   ClusterIP      10.0.249.124                   15021:31583/TCP,80:30314/TCP   13m
```

Export the ClusterIP for easy access and test the inference endpoint using a temporary curl pod:

```bash
export CLUSTERIP=$(kubectl get svc inference-gateway-istio -o jsonpath='{.spec.clusterIP}')
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -s  http://$CLUSTERIP/v1/models | jq

{
  "data": [
    {
      "created": 1764772194,
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
          "created": 1764772194,
          "group": null,
          "id": "modelperm-c535582bbc454bbd93cc3cf370318635",
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
        "content": "Kubernetes, often abbreviated as K8s, is an open-source platform designed to automate the deployment, scaling, and operations of containerized applications. Developed by Google and donated to the Cloud Native Computing Foundation (CNCF), Kubernetes provides an orches",
        "function_call": null,
        "reasoning_content": null,
        "refusal": null,
        "role": "assistant",
        "tool_calls": []
      },
      "stop_reason": null
    }
  ],
  "created": 1764815768,
  "id": "chatcmpl-d503646c-154d-4413-ba1c-cccba73effa7",
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

Deploy a second KAITO InferenceSet and DestinationRule with a different model to demonstrate multi-model routing. This step uses [`mistral-7b-instruct`](https://github.com/kaito-project/kaito/blob/main/examples/inference/kaito_inferenceset_mistral_7b-instruct.yaml) as an example:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/inference/kaito_inferenceset_mistral_7b-instruct.yaml
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/destinationrule-mistral-7b-instruct.yaml
```

Install the Body-Based Routing (BBR) Helm chart. BBR automatically extracts model names from OpenAI-style API requests and injects an `X-Gateway-Model-Name` header to the inference request, enabling model routing without modifying client code:

```bash
helm upgrade --install body-based-router oci://registry.k8s.io/gateway-api-inference-extension/charts/body-based-routing \
  --version v1.0.0 \
  --set provider.name=istio \
  --wait
```

Update the HTTPRoute to use header-based matching so requests are routed by the model name found in the request body:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/httproute-bbr.yaml
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
  "created": 1764818961,
  "id": "chatcmpl-e344b96b-94d2-4e6b-b922-05b312dd02bd",
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

Now, send the same request but change the model name to `mistral-7b-instruct` to verify BBR-driven model-aware routing across multiple InferenceSets:

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
