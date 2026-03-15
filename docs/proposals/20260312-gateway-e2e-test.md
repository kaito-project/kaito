---
title: Gateway API Inference Extension E2E Test
authors:
  - "@rambohe-ch"
reviewers:
  - "@Fei-Guo"
  - "@helayoty"
  - "@zhuangqh"
creation-date: 2026-03-12
last-updated: 2026-03-12
status: provisional
see-also:
  - docs/proposals/20250715-inference-aware-routing-layer.md
---

# Gateway API Inference Extension E2E Test

## Summary

This proposal describes the end-to-end (E2E) test plan for validating KAITO's integration with the [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/) (GWIE). The tests verify that inference requests are correctly routed through the Gateway → Body-Based Router (BBR) → Endpoint Picker (EPP) → model inference pods managed by KAITO's `InferenceSet` resources.

The test plan is divided into two parts:

- **Part 1**: Build the complete test environment, including a Kind cluster, KAITO components, GPU node mocker, Istio Gateway, BBR, and model inference instances.
- **Part 2**: Execute test cases against the deployed environment to validate correct end-to-end behavior.

## Motivation

GWIE integration is a key capability for making KAITO clusters conformant with the [Kubernetes AI Conformance](https://docs.google.com/document/d/1hXoSdh9FEs13Yde8DivCYjjXyxa7j4J8erjZPEGWuzc) profile. To confidently ship this integration, we need a reproducible, automated E2E test suite that:

- Validates the full request lifecycle: client → Gateway → BBR (model name extraction) → EPP (pod selection) → inference pod → response.
- Exercises multi-model routing (multiple `InferenceSet` / `InferencePool` instances co-existing on the same Gateway).
- Confirms that KAITO-managed `InferencePool` resources report the expected Kubernetes conditions (e.g., `Accepted=True`).
- Can run in CI without real GPU hardware by using a lightweight GPU-node mocker instead of an actual GPU provisioner.

Related issues: the inference-aware routing layer proposal [20250715-inference-aware-routing-layer.md](20250715-inference-aware-routing-layer.md) covers the feature design; this proposal covers its E2E validation.

### Goals

- Define a fully automated, reproducible test environment that can run in CI using Kind (no real GPU nodes required).
- Validate that KAITO's `InferenceSet` controller correctly creates `InferencePool` resources and that the `Accepted` condition becomes `True`.
- Validate that the Gateway correctly routes requests to the intended model backend via BBR + EPP.
- Validate correct handling of unknown-model requests (e.g., proper 4xx error response).
- Provide a reusable environment setup that can be extended with additional test scenarios.

### Non-Goals/Future Work

- Testing with real GPU hardware or cloud-provider GPU nodes (that is covered by the existing cloud E2E pipeline).
- Performance or load testing of the inference models themselves.
- Testing Gateway implementations other than Istio (e.g., kgateway) — that can be added later.
- Performance or stress testing of the EPP prefix-cache scoring algorithm itself.

## Proposal

The test plan is organized into two sequential parts. Part 1 builds the environment; Part 2 runs test cases against it.

### Part 1: Test Environment Setup

#### 1.1 Kubernetes Cluster, KAITO, and GPU Node Mocker

**Kind Cluster**

Create a Kind cluster with 2 initial worker nodes. These nodes are used for system add-ons (KAITO controllers, Istio, BBR, etc.) and do not need GPU labels. The cluster must support enough resources for all add-on pods.

```yaml
# kind-cluster.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
```

**KAITO Workspace Component**

Install KAITO workspace controller at version **v0.9.1** with the following feature gates enabled:

| Feature Gate | Value | Purpose |
|---|---|---|
| `enableInferenceSetController` | `true` | Enables the `InferenceSet` CRD and controller |
| `gatewayAPIInferenceExtension` | `true` | Enables automatic `InferencePool` creation from `InferenceSet` |

```bash
helm install kaito kaito/workspace \
  --version v0.9.1 \
  --namespace kaito-system \
  --create-namespace \
  --set featureGates.enableInferenceSetController=true \
  --set featureGates.gatewayAPIInferenceExtension=true
```

**GPU Node Mocker**

The GPU node mocker is a new test-only component that eliminates the need for real GPU hardware in E2E tests. It watches `NodeClaim` resources (produced by the KAITO workspace controller when it needs to provision GPU nodes) and, instead of calling a cloud provider API, directly adds a new Kind worker node to the cluster with the required labels and taints.

```
┌─────────────────────────────────────────────────────────┐
│                    Kind Cluster                          │
│                                                          │
│  ┌─────────────────┐      NodeClaim      ┌────────────┐ │
│  │  KAITO Workspace│ ──────────────────► │  GPU Node  │ │
│  │  Controller     │                     │  Mocker    │ │
│  └─────────────────┘                     └─────┬──────┘ │
│                                                │        │
│                                  kind node add │        │
│                                                ▼        │
│                                   ┌────────────────────┐│
│                                   │ New Kind Worker     ││
│                                   │ labels:             ││
│                                   │  kaito.sh/workspace ││
│                                   │    : <name>         ││
│                                   └────────────────────┘│
└─────────────────────────────────────────────────────────┘
```

Key behaviors of the GPU node mocker:

- **Trigger**: Watches `NodeClaim` resources in all namespaces. When a new `NodeClaim` appears, the mocker creates a Kind worker node and updates the `NodeClaim` status to simulate successful provisioning.
- **Labels**: The newly created Kind worker node carries the label `kaito.sh/workspace: <workspace-name>` derived from the `NodeClaim`'s owner reference or annotations, so that the KAITO workspace controller can select it.
- **No real GPU**: The node has no GPU device plugin. Test inference pods run the **LLM Mocker** image (described below) instead of a real vLLM process.

**LLM Mocker**

The LLM mocker is a new lightweight HTTP server image (`kaito/llm-mocker`) that simulates a vLLM-compatible OpenAI inference endpoint. It is used in place of a real LLM on the Kind worker nodes, making the E2E tests self-contained and GPU-free.

Key features:

| Feature | Behaviour |
|---|---|
| **`/v1/chat/completions`** | Accepts OpenAI-compatible chat requests and returns a well-formed JSON response. |
| **`/metrics`** | Exposes Prometheus-compatible vLLM metrics (e.g., `vllm:num_requests_waiting`) so EPP can score pods. |
| **Tool call priority** | When the request contains a `tools` array, the response always includes a `tool_calls` assistant turn, enabling multi-turn tool-call test flows. |
| **Header echo** | Reads the `X-Gateway-Model-Name` and `X-Gateway-Destination-Endpoint` request headers (injected by BBR and EPP respectively) and echoes them back in the response body under `_debug.gateway_model_name` and `_debug.destination_endpoint`. This lets E2E tests assert routing correctness without requiring log scraping. |
| **Model name parameter** | The model name is passed as a CLI argument (`--model-name=<name>`), so the same image can stand in for any model. |

Example LLM mocker response for a chat request:

```json
{
  "id": "chatcmpl-mock-001",
  "object": "chat.completion",
  "model": "falcon-7b-instruct",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "[mock response]"},
      "finish_reason": "stop"
    }
  ],
  "_debug": {
    "gateway_model_name": "falcon-7b-instruct",
    "destination_endpoint": "10.244.1.5:5000"
  }
}
```

When a `tools` array is present the `choices[0].message` is replaced with a `tool_calls` list instead of a plain content string, producing a valid tool-call turn for multi-turn conversation tests.

**Verification**

After installing the above components, validate:

```bash
# KAITO controller is running
kubectl -n kaito-system get pods -l app=kaito-workspace

# GPU node mocker is running
kubectl -n kaito-system get pods -l app=gpunode-mocker

# Kind cluster has the base worker nodes
kubectl get nodes
```

All pods must reach `Running` state and all nodes must reach `Ready` state before proceeding.

---

#### 1.2 Gateway API Inference Extension Components

**Istio**

Install Istio at version **v1.29** as the Gateway implementation. This includes the Istio control plane (`istiod`), the `IstioOperator` CRD, and the standard Gateway API CRDs.

```bash
# Install Gateway API CRDs
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml

# Install Istio v1.29 with minimal profile
istioctl install --set profile=minimal --set hub=docker.io/istio --set tag=1.29.0 -y

# Install GWIE CRDs (InferencePool, InferenceModel)
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/latest/download/manifests.yaml
```

**Body-Based Router (BBR)**

Install BBR at version **v1.3.1**. BBR runs as a deployment and is registered as an Envoy `ext_proc` filter via an Istio `EnvoyFilter` resource. It intercepts incoming requests, reads the `model` field from the JSON request body, and injects an `X-Gateway-Model-Name` header so the `HTTPRoute` can perform header-based routing to the correct `InferencePool`.

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api-inference-extension/releases/download/v1.3.1/body-based-router.yaml
```

**Gateway**

Deploy the Istio `Gateway` resource that exposes port 80 for incoming inference traffic. Istio's Gateway controller will automatically create the corresponding `Deployment`, `ReplicaSet`, and `Pod` (named `inference-gateway-istio-*`) from this resource.

```bash
kubectl apply -f examples/gateway-api-inference-extension/gateway.yaml
```

The `gateway.yaml` used:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: inference-gateway
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    port: 80
    protocol: HTTP
```

**Verification**

```bash
# Istio control plane is running
kubectl -n istio-system get pods -l app=istiod

# BBR deployment is running
kubectl get pods -l app=body-based-router

# Gateway pod is running
kubectl get pods -l gateway.networking.k8s.io/gateway-name=inference-gateway

# Gateway API CRDs are installed
kubectl get crd gateways.gateway.networking.k8s.io
kubectl get crd inferencepools.inference.networking.k8s.io
```

All components must be `Running` and `Ready` before proceeding.

---

#### 1.3 Model Resource Deployment

**InferenceSet Resources**

Deploy the following two `InferenceSet` resources. In the E2E environment the GPU node mocker fulfills the `NodeClaim` requests and schedules pods on the new Kind worker nodes. Because the nodes have no GPU, the `InferenceSpec.template` field is used instead of `InferenceSpec.preset` — this bypasses KAITO's preset image lookup and directly specifies the LLM mocker container with the model name passed as a CLI argument.

| Model | Replicas | LLM mocker arg |
|---|---|---|
| `falcon-7b-instruct` | 2 | `--model-name=falcon-7b-instruct` |
| `ministral-3-3b-instruct` | 1 | `--model-name=ministral-3-3b-instruct` |

The `InferenceSet` YAML for `falcon-7b-instruct` (the `ministral-3-3b-instruct` one follows the same pattern with `replicas: 1`):

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  name: falcon-7b-instruct
  annotations:
    scaledobject.kaito.sh/auto-provision: "true"
    scaledobject.kaito.sh/metricName: "vllm:num_requests_waiting"
    scaledobject.kaito.sh/threshold: "10"
spec:
  replicas: 2
  nodeCountLimit: 3
  labelSelector:
    matchLabels:
      apps: falcon-7b-instruct
  template:
    resource:
      instanceType: "Standard_NV36ads_A10_v5"
    inference:
      # Use template (not preset) so the LLM mocker image is used instead of the
      # real vLLM preset image, which cannot run on GPU-less Kind worker nodes.
      template:
        spec:
          containers:
          - name: llm-mocker
            image: kaito/llm-mocker:latest
            args:
            - --model-name=falcon-7b-instruct
            ports:
            - containerPort: 5000
              name: http
            - containerPort: 5001
              name: metrics
            readinessProbe:
              httpGet:
                path: /health
                port: 5000
              initialDelaySeconds: 5
              periodSeconds: 5
```

```bash
kubectl apply -f examples/inference/kaito_inferenceset_falcon_7b-instruct.yaml
kubectl apply -f examples/inference/kaito_inferenceset_ministral_3_3b-instruct.yaml
```

The resulting cluster topology:

```
┌──────────────────────────────────────────────────────────┐
│  InferencePool: falcon-7b-instruct-inferencepool          │
│    ├── inference-pod-falcon-7b-instruct-0 (node-1)       │
│    └── inference-pod-falcon-7b-instruct-1 (node-2)       │
│                                                           │
│  InferencePool: ministral-3-3b-instruct-inferencepool     │
│    └── inference-pod-ministral-3-3b-instruct-0 (node-3)  │
└──────────────────────────────────────────────────────────┘
```

**HTTPRoute and DestinationRule Resources**

Once the `InferenceSet` instances and their `InferencePool` resources are ready, deploy the routing layer that ties the Gateway to each `InferencePool`.

`HTTPRoute` — routes requests to the correct `InferencePool` based on the `X-Gateway-Model-Name` header injected by BBR. A catch-all rule at the end returns an OpenAI-compatible 404 for unrecognized model names:

```bash
kubectl apply -f examples/gateway-api-inference-extension/httproute-bbr-aimanager.yaml
```

`DestinationRule` — configures Istio's TLS policy for the EPP sidecar of each `InferencePool`. Two rules are required, one per model:

```bash
kubectl apply -f examples/gateway-api-inference-extension/destinationrule-falcon-7b-instruct.yaml
kubectl apply -f examples/gateway-api-inference-extension/destinationrule-ministral-3-3b-instruct.yaml
```

The two `DestinationRule` resources configure `SIMPLE` TLS with `insecureSkipVerify: true` for the EPP services:

```yaml
# destinationrule-falcon-7b-instruct.yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: falcon-7b-instruct-inferencepool-epp
spec:
  host: falcon-7b-instruct-inferencepool-epp
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
---
# destinationrule-ministral-3-3b-instruct.yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: ministral-3-3b-instruct-inferencepool-epp
spec:
  host: ministral-3-3b-instruct-inferencepool-epp
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
```

**Verification**

The environment is considered fully ready when **all** of the following conditions are satisfied:

1. All inference pods are in `Running` state:

   ```bash
   kubectl get pods -l apps=falcon-7b-instruct
   kubectl get pods -l apps=ministral-3-3b-instruct
   ```

2. The `InferencePool` resources exist and show `Accepted=True`:

   ```bash
   kubectl get inferencepool falcon-7b-instruct-inferencepool \
     -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'
   # Expected: True

   kubectl get inferencepool ministral-3-3b-instruct-inferencepool \
     -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'
   # Expected: True
   ```

3. The `InferenceSet` resources show the desired replica count is met:

   ```bash
   kubectl get inferenceset falcon-7b-instruct
   # READY should equal 2/2

   kubectl get inferenceset ministral-3-3b-instruct
   # READY should equal 1/1
   ```

4. The `HTTPRoute` is accepted by the Gateway:

   ```bash
   kubectl get httproute llm-route \
     -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'
   # Expected: True
   ```

5. The `DestinationRule` resources exist:

   ```bash
   kubectl get destinationrule falcon-7b-instruct-inferencepool-epp
   kubectl get destinationrule ministral-3-3b-instruct-inferencepool-epp
   ```

Only after all checks pass should Part 2 test cases be executed.

---

### Part 2: Test Cases

> **Note**: The detailed test case specifications will be defined in a follow-up iteration of this proposal. The categories below outline the planned coverage.

#### 2.1 Inference-Aware Routing

Verify that requests containing a valid model name in the JSON body are correctly routed to the corresponding `InferencePool`.

**Test steps:**

1. Send a POST request with `"model": "falcon-7b-instruct"` and verify the response comes back from the correct pool.
2. Send a POST request with `"model": "ministral-3-3b-instruct"` and verify routing to the other pool.

**Assertions** (using the `_debug` fields echoed by the LLM mocker):

- `response._debug.gateway_model_name` equals the requested model name — confirms BBR correctly extracted the model from the request body and injected `X-Gateway-Model-Name`.
- `response._debug.destination_endpoint` is non-empty and matches an IP of a pod belonging to the expected `InferencePool` — confirms EPP selected a pod and injected `X-Gateway-Destination-Endpoint`.
- HTTP status is `200`.

#### 2.2 Prefix Cache Affinity — Multi-Turn Conversation with Tool Calls

EPP implements prefix-cache-aware pod selection: when a follow-up request shares a long common prefix with a previous request (e.g., same system prompt, same conversation history, same tool definitions), EPP should prefer the pod that already has that prefix cached in its KV-cache, minimizing recomputation and latency.

**Test scenario — multi-turn conversation with tool calls:**

1. Send an initial request to `falcon-7b-instruct` that includes a system prompt, a tool schema (`tools` array), and a first user message. Record the inference pod selected by EPP (via the `X-Gateway-Destination-Endpoint` response header or server-side logs).

   ```json
   {
     "model": "falcon-7b-instruct",
     "messages": [
       {"role": "system", "content": "You are a helpful assistant with access to tools."},
       {"role": "user", "content": "What is the weather in Seattle?"}
     ],
     "tools": [
       {
         "type": "function",
         "function": {
           "name": "get_weather",
           "description": "Get current weather for a city",
           "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
         }
       }
     ]
   }
   ```

2. Append the assistant's tool-call response and a follow-up user message (same system prompt + tool schema prefix), then send the second turn. Record which pod EPP selects.

   ```json
   {
     "model": "falcon-7b-instruct",
     "messages": [
       {"role": "system", "content": "You are a helpful assistant with access to tools."},
       {"role": "user", "content": "What is the weather in Seattle?"},
       {"role": "assistant", "tool_calls": [{"id": "call_1", "function": {"name": "get_weather", "arguments": "{\"city\": \"Seattle\"}"}}]},
       {"role": "tool", "tool_call_id": "call_1", "content": "Rainy, 12°C"},
       {"role": "user", "content": "What about tomorrow?"}
     ],
     "tools": [ /* same tool schema */ ]
   }
   ```

3. Repeat turn 2 several more times, extending the conversation history each time.

**Assertions** (using the `_debug` fields echoed by the LLM mocker):

- `response._debug.destination_endpoint` is **identical** across all turns of the same conversation — EPP selected the same pod each time because the long shared prefix (system prompt + tool schema + conversation history) causes that pod to score highest on prefix-cache affinity.
- The LLM mocker returns a `tool_calls` turn for turn 1 (because `tools` was present), enabling the test to construct a valid turn-2 message with `role: tool`.
- If the pinned pod is deleted mid-conversation, EPP falls back to another pod with no error returned to the client (the `destination_endpoint` changes to a new pod IP).

#### 2.3 Load Distribution Across Multiple Replicas

With 2 `falcon-7b-instruct` replicas, issue multiple concurrent **independent** requests (distinct messages, no shared prefix) and verify that:

- `response._debug.destination_endpoint` varies across responses — both pods receive traffic.
- No single pod's endpoint appears in 100% of responses under normal conditions.

#### 2.4 Unknown Model Error Handling

Request with an unrecognized model name (e.g., `"model": "unknown-model"`) must return a well-formed error response:

- HTTP status `404`.
- Response body conforms to the OpenAI error schema: `{"error": {"message": "...", "type": "invalid_request_error", "code": "model_not_found"}}`.

#### 2.5 InferenceSet Scaling

Scale the `falcon-7b-instruct` `InferenceSet` from 2 to 3 replicas and verify:

- A new Kind worker node is provisioned by the GPU node mocker.
- A new inference pod is scheduled on the new node.
- The `InferencePool` updates its endpoint list to include the new pod.
- Traffic is distributed across all 3 replicas.

## Alternatives

### Using a Real Cloud Cluster for E2E Tests

The simplest alternative is to run E2E tests against a real AKS or other cloud cluster with actual GPU nodes, relying on the existing `gpu-provisioner`. This approach provides the highest fidelity but introduces:

- **Cost**: GPU node provisioning is expensive and slow (10–20 minutes per node).
- **Flakiness**: Cloud-provider quota limits and network issues can cause test failures unrelated to the code under test.
- **CI constraints**: Requires cloud credentials and is unsuitable for open PR checks from external contributors.

The GPU node mocker approach proposed here decouples test correctness (routing, scheduling, condition reporting) from cloud infrastructure, making the tests fast, cheap, and safe to run on every PR.

### Using Existing `preset_test.go` Infrastructure

The existing `test/e2e/preset_test.go` tests validate Workspace-based inference against real cloud nodes. We could extend this file with GWIE-specific cases. However:

- Those tests are gated behind cloud credentials.
- GWIE routing behavior (BBR, EPP, `InferencePool`) requires a Gateway, which is not part of the existing test infrastructure.

A dedicated test file (e.g., `test/e2e/gateway_inference_test.go`) with its own environment setup is preferred to keep concerns separated and avoid polluting the existing workspace E2E flow.

## Implementation History

- 2026-03-12: Initial proposal drafted
