---
title: Gateway API Inference Extension
---

KAITO integrates with [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/) (GWIE) and [llm-d](https://github.com/llm-d/llm-d-inference-scheduler) to provide model-aware routing and optimal endpoint selection for inference. This page covers what it is, prerequisites, how to enable it in KAITO, how it's wired, and a quickstart.

## What is it

Gateway API Inference Extension extends [Gateway API](https://gateway-api.sigs.k8s.io/) with inference-focused backends and behaviors. It adds:

- [InferencePool](https://gateway-api-inference-extension.sigs.k8s.io/api-types/inferencepool/) CRD to represent model-serving backends
- A reference Endpoint Picker Plugin (EPP) that uses inference server metrics and policies to pick the best backend. In KAITO, the EPP image is overridden to use the [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler), which builds on the GWIE EPP with advanced scheduling plugins including KV cache-aware routing, prefill/decode (P/D) disaggregation, and pluggable filters/scorers.
- Optional [Body-Based Routing](https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/pkg/bbr) (BBR) that extracts model names from OpenAI-style requests and injects a header for routing purposes
- **Prefill/Decode (P/D) disaggregation** via MultiRoleInference (MRI), enabling separation of compute-intensive prefill and memory-bandwidth-intensive decode phases into independently scalable pod groups

KAITO uses GWIE to route requests for models to the right Workspace pods, improving latency and GPU utilization.

## Prerequisites

Before enabling this feature in KAITO, ensure the following are installed in your cluster:

- A Gateway API implementation that supports Envoy ext_proc and the Inference Extension pattern. See available Gateway implementations: https://gateway-api-inference-extension.sigs.k8s.io/implementations/gateways/

## Enable this feature

This feature is supported from KAITO v0.8.0. Starting from **KAITO v0.11.0**, the InferenceSet feature has been promoted to **beta** and the `enableInferenceSetController` feature gate is enabled by default — you only need to enable the `gatewayAPIInferenceExtension` feature gate.

### KAITO v0.11.0+

```bash
export CLUSTER_NAME=kaito

helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set featureGates.gatewayAPIInferenceExtension=true \
  --wait \
  --take-ownership
```

### KAITO v0.8.0 – v0.10.x

For older versions, you need to explicitly enable both feature gates:

> **Note:** MultiRoleInference (MRI) also requires the same feature gates. No additional flags are needed beyond what is shown below.

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
  --wait \
  --take-ownership
```

## How KAITO wires it

### InferenceSet

When the feature gate is enabled, [Flux](https://fluxcd.io/) will be installed in the same namespace as the InferenceSet controller as a Helm dependency. It is used to deploy and manage the GWIE InferencePool Helm chart for each InferenceSet.

When you create an InferenceSet, the KAITO InferenceSet controller will:

1) Create or update two Flux resources in the InferenceSet namespace:
   - [OCIRepository](https://fluxcd.io/flux/components/source/ocirepositories/): points to the upstream GWIE inferencepool Helm chart
     - URL: oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool
     - Tag/Version: https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/latest
   - [HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/): references the OCIRepository and applies values to deploy the InferencePool and EPP. The EPP image is overridden to use the [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler) (`mcr.microsoft.com/oss/v2/llm-d/llm-d-inference-scheduler:v0.7.1`) instead of the default GWIE EPP. This tag is pinned by KAITO and may be updated or made configurable in future releases.
2) Wait for Flux resources to become Ready

You can inspect these resources with kubectl in the InferenceSet namespace. Updates to the InferenceSet will reconcile these resources.

### MultiRoleInference (MRI)

MultiRoleInference (MRI) enables **prefill/decode (P/D) disaggregation** for large models where separating the two inference phases improves throughput and latency. Instead of a single set of pods handling both phases, MRI creates separate workloads for each role:

- **Prefill pods** handle the initial prompt processing — the compute-intensive phase where all input tokens are processed in parallel to build the KV cache.
- **Decode pods** handle autoregressive token generation — the memory-bandwidth-intensive phase where tokens are generated one at a time using the KV cache.

When you create a MultiRoleInference resource, KAITO will:

1) Provision separate pod groups for each role (prefill and decode) via child InferenceSets, each with their own instance types optimized for their workload characteristics.
2) Create a single InferencePool with an EPP (Endpoint Picker) deployment that uses llm-d plugins to handle P/D-aware routing. The pool selects all MRI pods (both prefill and decode), and the EPP internally uses label-based filters to route requests to the correct role.
3) The EPP uses scheduling profiles (`prefill` and `decode`) with a `prefix-based-pd-decider` plugin to decide which role handles each request. Decode pods communicate directly with prefill pods for KV cache transfer, bypassing the gateway.

This separation allows:

- **Independent scaling** — scale prefill and decode replicas separately based on workload patterns
- **GPU optimization** — use compute-optimized instances for prefill and memory-bandwidth-optimized instances for decode
- **Improved throughput** — prefill pods can process new prompts while decode pods generate tokens for previous requests

MRI is ideal for models where the compute profiles of prefill and decode differ significantly.

## Quickstart

### Option A: InferenceSet (Single-role)

In this quickstart example, we will use Istio as the Gateway API provider to handle traffic management and routing, and deploy KAITO InferenceSet to serve inference models. The following steps demonstrate how to set up an end-to-end inference gateway that routes requests to model-serving backends managed by KAITO.

#### 1. Install Istio and Deploy Gateway

First, install Istio base and control plane components, setting flags that enable Gateway API Inference Extension support in the data plane and pilot:

```bash
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
helm upgrade -i istio-base istio/base \
  --version 1.28.3 \
  --namespace istio-system \
  --create-namespace
helm upgrade -i istiod istio/istiod \
  --version 1.28.3 \
  --namespace istio-system \
  --set pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION="true" \
  --wait
```

Then, deploy Gateway API CRDs and create the Gateway resource that will handle incoming requests and integrate with GWIE per the example configuration:

```bash
kubectl apply -k "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v1.4.1"
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/gateway.yaml
```

#### 2. Deploy InferenceSet

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

Verify that the Endpoint Picker (EPP) Pod is running in the InferenceSet namespace:

```bash
kubectl get pod -l inferencepool=phi-4-mini-inferencepool-epp

NAME                                           READY   STATUS    RESTARTS   AGE
phi-4-mini-inferencepool-epp-b74f8994b-s9kkt   1/1     Running   0          87s
```

Confirm the EPP is using the llm-d inference scheduler image:

```bash
kubectl get pod -l inferencepool=phi-4-mini-inferencepool-epp -o jsonpath='{.items[0].spec.containers[0].image}'
# Expected: mcr.microsoft.com/oss/v2/llm-d/llm-d-inference-scheduler:v0.7.1
```

#### 3. Deploy DestinationRule and HTTPRoute

Apply an Istio DestinationRule. Since EPP runs with `--secure-serving=true` by default using a self-signed certificate, and Istio doesn't trust self-signed certificates, this DestinationRule bypasses TLS verification as a temporary workaround:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/destinationrule-phi-4-mini-instruct.yaml
```

Create the HTTPRoute that targets the InferenceSet's InferencePool (via `.spec.endpointPickerRef`) and defines the routing matchers used by the Gateway:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/httproute.yaml
```

#### 4. Test Inference

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

### Option B: MultiRoleInference (Prefill/Decode Disaggregation)

This quickstart demonstrates deploying a large model with prefill/decode disaggregation using MultiRoleInference (MRI). With MRI, prefill pods handle the compute-intensive initial prompt processing while decode pods handle memory-bandwidth-intensive autoregressive token generation, with the llm-d EPP using scheduling profiles to route requests to the correct role and decode pods communicating directly with prefill pods for KV cache transfer.

#### 1. Install Istio and Deploy Gateway

Follow the same Istio and Gateway setup as [Option A, Step 1](#1-install-istio-and-deploy-gateway). If you've already completed Option A, skip this step.

#### 2. Deploy MultiRoleInference

Create a MultiRoleInference resource for phi-4-mini with prefill/decode disaggregation:

```bash
kubectl apply -f - <<EOF
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: phi-4-mini
  namespace: kaito-workspace
spec:
  labelSelector:
    matchLabels:
      apps: phi-4-mini
  model:
    name: phi-4-mini-instruct
  roles:
  - type: prefill
    replicas: 1
    instanceType: Standard_NC24ads_A100_v4
  - type: decode
    replicas: 1
    instanceType: Standard_NC24ads_A100_v4
EOF
```

#### 3. Verify Prefill and Decode Pods

Verify that both prefill and decode pods are running:

```bash
kubectl get pods -l multiroleinference.kaito.sh/created-by=phi-4-mini

NAME                       READY   STATUS    RESTARTS   AGE
phi-4-mini-decode-25x54-0       2/2     Running   0          10h
phi-4-mini-prefill-qkrzk-0      1/1     Running   0          10h
```

Verify the InferencePool is created (a single pool covers both prefill and decode roles):

```bash
kubectl get inferencepool

NAME                    AGE
phi-4-mini-inferencepool     9h
```

Verify the EPP pod is running for the pool:

```bash
kubectl get pods -l inferencepool=phi-4-mini-inferencepool-epp

NAME                                          READY   STATUS    RESTARTS   AGE
phi-4-mini-inferencepool-epp-5d994d5ff-6bmzj       1/1     Running   0          9h
```

#### 4. Deploy DestinationRule and HTTPRoute

With MRI, the KAITO operator creates a **single InferencePool** that selects all MRI pods (both prefill and decode). The EPP service requires a DestinationRule for TLS bypass (since EPP uses `--secure-serving=true` with a self-signed certificate).

The HTTPRoute targets the InferencePool as the entry point. The llm-d EPP uses scheduling profiles to route requests to the correct role internally.

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/destinationrule-phi-4-mini-instruct.yaml
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/httproute-mri.yaml
```

Verify the HTTPRoute is accepted and references are resolved:

```bash
kubectl describe httproute phi-4-mini-mri
```

Expected conditions:
```
Status:
  Parents:
    Conditions:
      Message:  Route was valid
      Reason:   Accepted
      Status:   True
      Type:     Accepted
      Message:  All references resolved
      Reason:   ResolvedRefs
      Status:   True
      Type:     ResolvedRefs
```

Verify the InferencePool status shows it is referenced by the HTTPRoute:

```bash
kubectl describe inferencepool phi-4-mini-inferencepool
```

Expected conditions:
```
Status:
  Parents:
    Conditions:
      Message:  Referenced by an HTTPRoute accepted by the parentRef Gateway
      Reason:   Accepted
      Status:   True
      Type:     Accepted
      Message:  Referenced ExtensionRef resolved successfully
      Reason:   ResolvedRefs
      Status:   True
      Type:     ResolvedRefs
```

#### 5. Test Inference

Export the Gateway ClusterIP and send a request:

```bash
export CLUSTERIP=$(kubectl get svc inference-gateway-istio -o jsonpath='{.spec.clusterIP}')
kubectl run -it --rm --restart=Never curl --image=curlimages/curl -- curl -X POST http://$CLUSTERIP/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-4-mini-instruct",
    "messages": [{"role": "user", "content": "What is kubernetes?"}],
    "max_tokens": 100
  }' | jq
```

With P/D disaggregation active, the request flow is:

1. The Gateway routes the request to the llm-d EPP scheduler
2. The EPP's `prefill` scheduling profile routes the request to a **prefill pod**, which processes all input tokens in parallel and builds the KV cache
3. The **decode pod** communicates directly with the prefill pod to transfer the KV cache (bypassing the gateway)
4. The decode pod performs autoregressive token generation using the transferred KV cache
5. The response streams back through the Gateway to the client

This separation means prefill pods are always available for new requests while decode pods focus on generating tokens — improving overall throughput for large models.

### Body-Based Routing (BBR)

> BBR works with both InferenceSet and MultiRoleInference deployments.

Deploy a second KAITO InferenceSet and DestinationRule with a different model to demonstrate multi-model routing. This step uses [`mistral-7b-instruct`](https://github.com/kaito-project/kaito/blob/main/examples/inference/kaito_inferenceset_mistral_7b-instruct.yaml) as an example:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/inference/kaito_inferenceset_mistral_7b-instruct.yaml
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/destinationrule-mistral-7b-instruct.yaml
```

Install the Body-Based Routing (BBR) Helm chart. BBR automatically extracts model names from OpenAI-style API requests and injects an `X-Gateway-Model-Name` header to the inference request, enabling model routing without modifying client code:

```bash
helm upgrade --install body-based-router oci://registry.k8s.io/gateway-api-inference-extension/charts/body-based-routing \
  --version v1.3.0 \
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
