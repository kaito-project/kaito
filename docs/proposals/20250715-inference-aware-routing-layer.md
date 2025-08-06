---
title: Inference-Aware Routing Layer
authors:
  - "@chewong"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2025-07-15
last-updated: 2025-07-15
status: provisional
see-also:
---

## Goals

Our goal is to make KAITO clusters fully compatible with the [Kubernetes AI Conformance](https://docs.google.com/document/d/1hXoSdh9FEs13Yde8DivCYjjXyxa7j4J8erjZPEGWuzc/edit?tab=t.0#heading=h.9j85ih1tpsk) profile, enabling seamless operation within AI-conformant Kubernetes environments. This integration leverages the [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/) to provide intelligent, inference-aware routing that optimally selects pods for serving each request. The implementation is structured in two phases:

- Phase 1: Integrate Gateway API Inference Extension (GWIE) components with KAITO inference workloads to enable inference-aware routing.

- Phase 2: (Required > 1 replica of inference pods) Enable and validate prompt-aware and metrics-aware routing using Endpoint Picker (EPP) to select the optimal pod in an `InferencePool` to serve the request.

## Non-Goals

- Integrate with a specific Gateway implementation.

## Gateway API Inference Extension Components

This section dives into the core components that comprise GWIE. Understanding these individual parts and their interactions is important to understand how GWIE extends the Gateway API to provide inference-specific traffic management and intelligent routing for inference workloads.

- Gateway: Must conform to upstream criteria. Currently, kgateway and istio are recommended for GWIE integration.

- Extproc: An Envoy filter designed to intercept and modify inference requests by connecting to an external gRPC server. This allows for dynamic examination and alteration of headers, body, and trailers, enabling advanced routing logic like Endpoint Picker (EPP) and Body-Based Routing (BBR) that are implemented as these external gRPC servers.

- `InferencePool`: A Kubernetes CRD that defines a specialized group of model servers (pods) and the associated endpoint picker (EPP) inference extension responsible for scheduling requests among them. It functions similarly to a standard Kubernetes Service.

- `InferenceModel`: A Kubernetes CRD that defines a specific model or adapter and its configuration, allowing for granular management of individual model versions or fine-tuning.

- Body-Based Routing (BBR): An Extproc implementation that analyzes the HTTP request body to extract the intended model name, which it then adds to a new header (X-Gateway-Model-Name=model-name). This empowers the Gateway's HTTPRoute to make informed routing decisions based on the dynamically injected model name. For more details, refer to this guide.

- Endpoint Picker (EPP): Another Extproc implementation responsible for inference pod selection. Once the HTTPRoute has directed a request to an `InferencePool` backend, EPP (as part of the Envoy filter chain) performs the following steps to choose the best pod:
  1. It lists all pods selected by the `InferencePool`'s pod label selector.

  2. It can optionally filter this list.

  3. It scores each pod based on several criteria, such as longest matching prefix-cache, vLLM queue size, and KV cache size. This is very similar to kube-scheduler where it filters and scores each node when scheduling pending pods. Based on the scores, it picks the pod with the maximum score for the request.

  4. Finally, it adds an `x-gateway-destination-endpoint: <pod-ip>:<port>` header to the request, which Envoy then uses as a hint to route the request to the chosen pod. Note that there is a strict one-to-one relationship between an EPP service and an `InferencePool`. For further implementation details, refer to this proposal.

## Integration Points

This section will outline the key areas where KAITO will interact and integrate with the Gateway API Inference Extension and its various components.

### CRDs Installation

For `InferencePool` and `InferenceModel` CRDs, we can use the upstream version from https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/config/crd/bases.

For Gateway API CRDs, we have two options:

Upstream: https://github.com/kubernetes-sigs/gateway-api/tree/main/config/crd/standard

For Gateway API implementation-specific CRDs, users will install them during installation.

For KAITO integration, we will only include `InferencePool` and `InferenceModel` CRDs, as these are the ones KAITO will integrate with.

### Gateway

Use upstream-compliant Gateways such as `Istio` and `kgateway` for initial integration.

### `InferencePool`

Create one `InferencePool` per KAITO Workspace to encapsulate the set of model servers managed by that Workspace. Consider the following KAITO Workspace:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini-instruct
  namespace: default
resource:
  count: 1
  instanceType: Standard_NC80adis_H100_v5
  labelSelector:
    matchLabels:
      accelerator: nvidia
inference:
  preset:
    name: phi-4-mini-instruct
```

KAITO Workspace controller will create an `InferencePool` for this workspace, which will look like this:

```yaml
apiVersion: inference.networking.x-k8s.io/v1alpha2
kind: InferencePool
metadata:
  name: workspace-phi-4-mini-instruct
  namespace: default
spec:
  extensionRef:
    name: workspace-phi-4-mini-instruct-epp # service is defined in the section below
  selector:
    kaito.sh/workspace: workspace-phi-4-mini-instruct
  targetPortNumber: 5000
```


### `InferenceModel`

Define one `InferenceModel` for each available model in a KAITO workspace. This includes one for the base model and one for each adapter.

Consider the following KAITO Workspace with one adapter:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini-instruct
  namespace: default
resource:
  count: 1
  instanceType: Standard_NC80adis_H100_v5
  labelSelector:
    matchLabels:
      accelerator: nvidia
inference:
  preset:
    name: phi-4-mini-instruct
  adapters:
    - source:
        name: "phi-4-mini-instruct-adapter"
        image:  "<YOUR_IMAGE>"
      strength: "0.2"
```

KAITO Workspace controller will create an `InferenceModel` for the base model and each adapter, which will look like this:

```yaml
# Base model
apiVersion: inference.networking.x-k8s.io/v1alpha2
kind: InferenceModel
metadata:
  name: phi-4-mini-instruct
  namespace: default
spec:
  modelName: phi-4-mini-instruct
  poolRef:
    name: workspace-phi-4-mini-instruct
---
# Adapter model
apiVersion: inference.networking.x-k8s.io/v1alpha2
kind: InferenceModel
metadata:
  name: phi-4-mini-instruct-adapter
  namespace: default
spec:
  modelName: phi-4-mini-instruct-adapter
  poolRef:
    name: workspace-phi-4-mini-instruct
```

### Body-Based Routing (BBR)

BBR is not a core part of KAITO integration but is recommended for deployment when there are multiple `InferencePools` (for example, one llm-d `InferencePool` and one KAITO Workspace `InferencePool`) under the same gateway. We will provide sample manifests in the new KAITO documentation website for deploying BBR, configuring the gateway to use this extproc, and updating HTTPRoutes to route based on the header set by BBR.

Below is a sample HTTPRoute for the Gateway that uses X-Gateway-Model-Name header injected by BBR to route to the correct `InferencePool` backend:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: kaito-llm-d-route
spec:
  parentRefs:
  - name: inference-gateway
  rules:
  - matches:
    - headers:
      - type: Exact
        name: X-Gateway-Model-Name
        value: kaito-model
      path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: workspace-phi-4-mini-instruct
      kind: InferencePool
  - matches:
    - headers:
      - type: Exact
        name: X-Gateway-Model-Name
        value: llm-d-model
      path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: llm-d-inference-pool
      kind: InferencePool
```

### Endpoint Picker (EPP)

EPP is a core component of the integration, deployed as a gRPC server within the cluster and configured as an Envoy extproc service. The EPP service is referenced from the `InferencePool` custom resource and in compatible Gateway implementations, incorporated into the Envoy filter chain, enabling inference-aware routing. EPP is responsible for intelligently selecting the optimal pod for each inference request based on real-time performance metrics, ensuring efficient load balancing and resource utilization.

Using the above `InferencePool` example, the EPP deployment will look like this:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: workspace-phi-4-mini-instruct-epp
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      inferencepool: workspace-phi-4-mini-instruct-epp
  template:
    metadata:
      labels:
        inferencepool: workspace-phi-4-mini-instruct-epp
    spec:
      containers:
      - args:
        - -poolName
        - workspace-phi-4-mini-instruct
        - -poolNamespace
        - default
        - -v
        - "3"
        - -grpcPort
        - "9002"
        - -grpcHealthPort
        - "9003"
        - -metricsPort
        - "9090"
        image: us-central1-docker.pkg.dev/k8s-staging-images/gateway-api-inference-extension/epp:main
        name: epp
        ports:
        - containerPort: 9002
          name: grpc
          protocol: TCP
        - containerPort: 9003
          name: grpc-health
          protocol: TCP
        - containerPort: 9090
          name: metrics
          protocol: TCP
---
apiVersion: v1
kind: Service
metadata:
  name: workspace-phi-4-mini-instruct-epp # used in InferencePool
  namespace: default
spec:
  ports:
  - name: grpc-ext-proc
    port: 9002
  - name: http-metrics
    port: 9090
  selector:
    inferencepool: workspace-phi-4-mini-instruct-epp
```

## Summary

The following diagram represents a successful integration:

![Inference-Aware Routing Layer Integration](../../website/static/img/gateway-api-inference-extension.png)
