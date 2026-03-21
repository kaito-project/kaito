---
title: Gateway API Inference Extension E2E Test
authors:
  - "@rambohe-ch"
reviewers:
  - "@Fei-Guo"
  - "@techworldhello"
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

- **Part 1**: Build the complete test environment, including an AKS cluster, KAITO components, GPU node mocker, Istio Gateway, BBR, and model inference instances.
- **Part 2**: Execute test cases against the deployed environment to validate correct end-to-end behavior.

## Motivation

GWIE integration is a key capability for making KAITO clusters conformant with the [Kubernetes AI Conformance](https://docs.google.com/document/d/1hXoSdh9FEs13Yde8DivCYjjXyxa7j4J8erjZPEGWuzc) profile. To confidently ship this integration, we need a reproducible, automated E2E test suite that:

- Validates the full request lifecycle: client → Gateway → BBR (model name extraction) → EPP (pod selection) → inference pod → response.
- Exercises multi-model routing (multiple `InferenceSet` / `InferencePool` instances co-existing on the same Gateway).
- Confirms that KAITO-managed `InferencePool` resources report the expected Kubernetes conditions (e.g., `Accepted=True`).
- Can run without real GPU hardware by using a lightweight GPU-node mocker instead of an actual GPU provisioner, while still running on a real AKS cluster to exercise the full network stack.

Related issues: the inference-aware routing layer proposal [20250715-inference-aware-routing-layer.md](20250715-inference-aware-routing-layer.md) covers the feature design; this proposal covers its E2E validation.

### Goals

- Define a fully automated, reproducible test environment using AKS (no real GPU nodes required).
- Validate that KAITO's `InferenceSet` controller correctly creates `InferencePool` resources and that the `Accepted` condition becomes `True`.
- Validate that the Gateway correctly routes requests to the intended model backend via BBR + EPP.
- Validate correct handling of unknown-model requests (e.g., proper 4xx error response).
- Provide a reusable environment setup that can be extended with additional test scenarios.

### Non-Goals/Future Work

- Testing with real GPU hardware (the GPU node mocker handles GPU-less validation; real GPU testing is covered by the existing cloud E2E pipeline).
- Performance or load testing of the inference models themselves.
- Testing Gateway implementations other than Istio (e.g., kgateway) — that can be added later.
- Performance or stress testing of the EPP prefix-cache scoring algorithm itself.

## Proposal

### High-Level Overview

The test plan is organized into two sequential parts: **environment setup** (Part 1) and **test case execution** (Part 2). The overall test architecture is shown below.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         E2E Test Architecture                                │
│                                                                              │
│  ┌──────────┐   POST /v1/chat   ┌───────────┐   ext_proc   ┌─────────────┐  │
│  │  E2E     │ ────────────────► │  Istio    │ ────────────► │    BBR      │  │
│  │  Test    │                   │  Gateway  │               │ (extracts   │  │
│  └──────────┘                   └─────┬─────┘               │  model name)│  │
│                                       │                     └──────┬──────┘  │
│                              HTTPRoute│matching                    │injects   │
│                              X-Gateway│-Model-Name                │X-Gateway-│
│                                       ▼                           │Model-Name│
│                              ┌─────────────────┐  ◄──────────────┘          │
│                              │  InferencePool  │                              │
│                              │  (per model)    │  ext_proc                    │
│                              └────────┬────────┘ ────────► EPP               │
│                                       │                    (selects pod,      │
│                                       │                     injects           │
│                                       │                     X-Gateway-        │
│                                       │                     Destination-      │
│                                       │                     Endpoint)         │
│                                       │ routes to pod IP                      │
│                                       ▼                                       │
│                              ┌─────────────────┐                              │
│                              │  Shadow Pod      │  ← real pod IP (AKS CNI)    │
│                              │  (LLM Mocker)    │                              │
│                              │  /v1/chat/compl. │                              │
│                              │  /metrics        │                              │
│                              │  _debug headers  │                              │
│                              └─────────────────┘                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key design decisions:**

| Concern | Decision | Rationale |
|---|---|---|
| **Cluster** | AKS (non-GPU SKU) | Real AKS CNI networking, real LB, matches production topology. No GPU quota needed. |
| **GPU node provisioning** | GPU Node Mocker (replaces `gpu-provisioner`) | Intercepts `NodeClaim`, creates a fake `Node` and keeps it `Ready` via Lease heartbeat, without launching a real VM. |
| **Inference pod** | Shadow Pod on real AKS node | The pending pod (on the fake node, created by KAITO from the standard preset) has its `status.podIP` patched to the shadow pod's real CNI IP. The preset image never runs. Gateway/EPP route to the real IP and hit the LLM Mocker. |
| **LLM simulation** | LLM Mocker HTTP server (`kaito/llm-mocker`) | Exposes `/v1/chat/completions` and `/metrics`. Echoes BBR/EPP-injected headers in the response body so tests can assert routing correctness without log scraping. |
| **KAITO conditions** | Fake Node → `ResourceReady=True`; Shadow Pod patch → `InferenceReady=True` | KAITO's two readiness conditions are satisfied through the two-phase mocker design. |

**End-to-end flow for a single inference request:**

1. E2E test sends `POST /v1/chat/completions` with `{"model": "falcon-7b-instruct", ...}` to the Gateway external IP.
2. **BBR** reads the request body, extracts `model`, injects `X-Gateway-Model-Name: falcon-7b-instruct`.
3. **HTTPRoute** matches the header and forwards to `falcon-7b-instruct-inferencepool`.
4. **EPP** selects an inference pod (by KV-cache score), injects `X-Gateway-Destination-Endpoint: <shadow-pod-IP>:5000`.
5. Envoy forwards the request to the shadow pod IP. The **LLM Mocker** responds with an OpenAI-compatible JSON body that includes `_debug.gateway_model_name` and `_debug.destination_endpoint`.
6. The E2E test asserts on these `_debug` fields to verify correct routing.

---

### Detailed Specification

#### Part 1: Test Environment Setup

##### 1.1 Kubernetes Cluster, KAITO, and GPU Node Mocker

**AKS Cluster**

Create an AKS cluster with 2 worker nodes. These nodes host system add-ons (KAITO controllers, Istio, BBR, GPU node mocker, shadow pods, etc.) and do not require GPU SKUs.

```bash
az group create --name kaito-gwie-e2e --location eastus

az aks create \
  --resource-group kaito-gwie-e2e \
  --name kaito-gwie-e2e \
  --node-count 2 \
  --node-vm-size Standard_D4s_v3 \
  --enable-managed-identity \
  --generate-ssh-keys

az aks get-credentials \
  --resource-group kaito-gwie-e2e \
  --name kaito-gwie-e2e
```

**KAITO Workspace Component**

Install KAITO workspace controller manually at version **v0.9.1** with the following feature gates enabled. The `gpu-provisioner` is **not** installed — its role (responding to `NodeClaim` resources) is taken over entirely by the GPU node mocker described below.

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

The GPU node mocker is a new test-only component deployed inside the AKS cluster. It replaces `gpu-provisioner` by intercepting `NodeClaim` resources and simulating successful GPU node provisioning — without launching any real VM or GPU node.

The mocker uses a **two-phase** design to make both `ResourceReady` and `InferenceReady` conditions on the KAITO `Workspace`/`InferenceSet` reach `True`:

```
┌────────────────────────────────────────────────────────────────────────┐
│                          AKS Cluster                                    │
│                                                                        │
│  ┌──────────────────┐  NodeClaim  ┌──────────────────────────────────┐ │
│  │  KAITO Workspace │ ──────────► │         GPU Node Mocker          │ │
│  │  Controller      │             │                                  │ │
│  └──────────────────┘             │  Phase 1: Fake Node              │ │
│          │                        │  • Create Node resource          │ │
│          │                        │  • Add workspace labels          │ │
│          │                        │  • Goroutine: renew Lease /10 s  │ │
│          │  create pod            │    → Node stays Ready            │ │
│          ▼                        └────────────────┬─────────────────┘ │
│  ┌───────────────────┐                             │                   │
│  │   Pending Pod     │ ◄── Phase 2: detect ────────┘                   │
│  │  (on fake node)   │     Pending pod                                 │
│  └───────────────────┘                                                 │
│          │                        ┌──────────────────────────────────┐ │
│          │                        │         Shadow Pod               │ │
│          │                        │  • Scheduled on real AKS node    │ │
│          │                        │  • Runs LLM Mocker container     │ │
│          │    patch podIP=        │  • Gets real CNI IP from AKS     │ │
│          └──── shadow pod IP ─────┤  • Exposes /v1/chat/completions  │ │
│               patch status=Ready  │    and /metrics                  │ │
│                                   └──────────────────────────────────┘ │
│                                                                        │
│  Gateway ──► routes to patched pod IP ──► hits Shadow Pod (LLM Mocker) │
└────────────────────────────────────────────────────────────────────────┘
```

**Phase 1 — Fake Node (makes `ResourceReady=True`)**

When the mocker detects a new `NodeClaim`:

1. Creates a `Node` resource with a non-cloud-parseable `spec.providerID` (e.g. `fake://<node-name>`) so the Azure Cloud Controller Manager skips deletion (CCM calls `InstanceExistsByProviderID`, receives a parse error, and skips the node rather than deleting it).
2. Adds the label `node.kubernetes.io/exclude-from-external-load-balancers: "true"` to avoid CCM LB reconciliation errors.
3. Patches the node's `status.conditions` to `Ready=True` and sets `status.allocatable` / `status.capacity`.
4. Creates a `Lease` in `kube-node-lease` for this node.
5. Starts a background goroutine that renews the `Lease.spec.renewTime` every 10 seconds, keeping the node-lifecycle-controller from marking the node `Unknown`.
6. Adds the workspace-specific label (`kaito.sh/workspace: <name>`) and any other labels required by the `InferenceSet`'s `labelSelector`, so the KAITO workspace controller flips `ResourceReady=True`.

**Phase 2 — Shadow Pod (makes `InferenceReady=True`)**

When KAITO creates a pod and binds it (`spec.nodeName`) to the fake node, the pod stays `Pending` indefinitely — there is no kubelet. The mocker:

1. Detects the `Pending` pod assigned to the fake node.
2. Creates a **shadow pod** on a real AKS worker node. The shadow pod runs the LLM Mocker container (see below) and is assigned a real CNI IP by AKS.
3. Waits for the shadow pod to reach `Running` and records its `status.podIP`.
4. Patches the original pending pod's `status` via `--subresource=status`:
   - `status.phase = Running`
   - `status.podIP` / `status.podIPs` = shadow pod's IP
   - `status.conditions[Ready] = True`
   - `status.containerStatuses[*].ready = true`

From KAITO's perspective the pending pod is now `Running/Ready`, so `InferenceReady` flips to `True`. From the Gateway/EPP's perspective, the pod IP it receives is the shadow pod's real IP — meaning inference traffic is actually served by the LLM Mocker.

**LLM Mocker**

The LLM mocker is a new lightweight HTTP server image (`kaito/llm-mocker`) that simulates a vLLM-compatible OpenAI inference endpoint. It runs inside **shadow pods** on real AKS worker nodes (scheduled by the GPU node mocker in Phase 2), making the E2E tests GPU-free while still exercising real AKS networking and the full Gateway → EPP routing path.

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

# AKS cluster nodes are ready
kubectl get nodes
```

All pods must reach `Running` state and all AKS nodes must reach `Ready` state before proceeding.

---

##### 1.2 Gateway API Inference Extension Components

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

##### 1.3 Model Resource Deployment

**InferenceSet Resources**

Deploy the following two `InferenceSet` resources using their standard `preset` configurations (i.e., the existing YAML files under `examples/inference/`). No custom container spec is needed: KAITO creates the inference pods normally using the preset image, but the pods are assigned to fake nodes and therefore stay `Pending` indefinitely. The GPU node mocker's Phase 2 then patches each pending pod's `status.podIP` to the corresponding shadow pod's real CNI IP, making KAITO treat them as `Running/Ready`. The preset image itself **never executes** — all actual inference traffic is served by the shadow pod running the LLM Mocker.

| Model | File | Replicas |
|---|---|---|
| `falcon-7b-instruct` | `examples/inference/kaito_inferenceset_falcon_7b-instruct.yaml` | 2 |
| `ministral-3-3b-instruct` | `examples/inference/kaito_inferenceset_ministral_3_3b-instruct.yaml` | 1 |

```bash
kubectl apply -f examples/inference/kaito_inferenceset_falcon_7b-instruct.yaml
kubectl apply -f examples/inference/kaito_inferenceset_ministral_3_3b-instruct.yaml
```

The resulting logical topology (fake nodes host the pending pods; shadow pods on real AKS nodes serve actual traffic):

```
┌────────────────────────────────────────────────────────────────────┐
│  InferencePool: falcon-7b-instruct-inferencepool                     │
│    ├── falcon-7b-instruct-pod-0  (fake-node-0, IP=shadow-pod-0-IP)   │
│    └── falcon-7b-instruct-pod-1  (fake-node-1, IP=shadow-pod-1-IP)   │
│                                                                      │
│  InferencePool: ministral-3-3b-instruct-inferencepool                │
│    └── ministral-3-3b-instruct-pod-0 (fake-node-2, IP=shadow-pod-2-IP)│
│                                                                      │
│  Shadow Pods (on real AKS nodes, running LLM Mocker):                │
│    shadow-pod-0  10.244.x.x  ← falcon pod-0 traffic lands here      │
│    shadow-pod-1  10.244.x.x  ← falcon pod-1 traffic lands here      │
│    shadow-pod-2  10.244.x.x  ← ministral pod-0 traffic lands here   │
└────────────────────────────────────────────────────────────────────┘
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

#### Part 2: Test Cases

> **Note**: The detailed test case specifications will be defined in a follow-up iteration of this proposal. The categories below outline the planned coverage.

##### 2.1 Inference-Aware Routing

Verify that requests containing a valid model name in the JSON body are correctly routed to the corresponding `InferencePool`.

**Test steps:**

1. Send a POST request with `"model": "falcon-7b-instruct"` and verify the response comes back from the correct pool.
2. Send a POST request with `"model": "ministral-3-3b-instruct"` and verify routing to the other pool.

**Assertions** (using the `_debug` fields echoed by the LLM mocker):

- `response._debug.gateway_model_name` equals the requested model name — confirms BBR correctly extracted the model from the request body and injected `X-Gateway-Model-Name`.
- `response._debug.destination_endpoint` is non-empty and matches an IP of a pod belonging to the expected `InferencePool` — confirms EPP selected a pod and injected `X-Gateway-Destination-Endpoint`.
- HTTP status is `200`.

##### 2.2 Prefix Cache Affinity — Multi-Turn Conversation with Tool Calls

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

##### 2.3 Load Distribution Across Multiple Replicas

With 2 `falcon-7b-instruct` replicas, issue multiple concurrent **independent** requests (distinct messages, no shared prefix) and verify that:

- `response._debug.destination_endpoint` varies across responses — both pods receive traffic.
- No single pod's endpoint appears in 100% of responses under normal conditions.

##### 2.4 Unknown Model Error Handling

Request with an unrecognized model name (e.g., `"model": "unknown-model"`) must return a well-formed error response:

- HTTP status `404`.
- Response body conforms to the OpenAI error schema: `{"error": {"message": "...", "type": "invalid_request_error", "code": "model_not_found"}}`.

##### 2.5 InferenceSet Scaling

Scale the `falcon-7b-instruct` `InferenceSet` from 2 to 3 replicas and verify:

- A new Kind worker node is provisioned by the GPU node mocker.
- A new inference pod is scheduled on the new node.
- The `InferencePool` updates its endpoint list to include the new pod.
- Traffic is distributed across all 3 replicas.

## EPP Solution Comparison

This section surveys the Endpoint Picker (EPP) / inference routing implementations across the ecosystem to justify why KAITO's E2E tests use the upstream [Gateway API Inference Extension (GAIE)](https://github.com/kubernetes-sigs/gateway-api-inference-extension) EPP. The comparison covers six projects and evaluates them across multiple dimensions.

### Evaluated Projects

| # | Project | Repository |
|---|---------|------------|
| 1 | **Gateway API Inference Extension (GAIE)** | [kubernetes-sigs/gateway-api-inference-extension/pkg/epp](https://github.com/kubernetes-sigs/gateway-api-inference-extension/tree/main/pkg/epp) |
| 2 | **llm-d Inference Scheduler** | [llm-d/llm-d-inference-scheduler](https://github.com/llm-d/llm-d-inference-scheduler/tree/main/pkg/plugins) |
| 3 | **NVIDIA Dynamo** | [ai-dynamo/dynamo/deploy/inference-gateway/epp](https://github.com/ai-dynamo/dynamo/tree/main/deploy/inference-gateway/epp) |
| 4 | **KubeAI** | [kubeai-project/kubeai/internal/loadbalancer](https://github.com/kubeai-project/kubeai/tree/main/internal/loadbalancer) |
| 5 | **Kthena (Volcano)** | [volcano-sh/kthena/pkg/kthena-router](https://github.com/volcano-sh/kthena/tree/main/pkg/kthena-router) |
| 6 | **vLLM Production Stack** | [vllm-project/production-stack/src/vllm_router](https://github.com/vllm-project/production-stack/tree/main/src/vllm_router) |

### Comparison Matrix

| Dimension | GAIE (upstream) | llm-d Inference Scheduler | NVIDIA Dynamo | KubeAI | Kthena | vLLM Production Stack |
|---|---|---|---|---|---|---|
| **GAIE API support (`InferencePool`, `InferenceModel`, etc.)** | ✅ Reference implementation; defines and implements all GAIE CRDs | ✅ Full support; directly extends GAIE EPP and uses `InferencePool`/`InferenceModel` APIs | ✅ Full support; Dynamo EPP directly extends GAIE EPP and uses `InferencePool`/`InferenceModel` APIs | ❌ Does not use GAIE APIs; uses its own `Model` CRD and built-in proxy | ✅ Partial; Kthena router supports Gateway API and Inference Extension (`InferencePool`, HTTPRoute) alongside its own `ModelRoute`/`ModelServing` CRDs | ❌ Does not use GAIE APIs; uses its own Python-based router with K8s service discovery |
| **KV-cache aware routing** | ✅ Built-in `prefix-cache-scorer` estimates overlap from scheduling history; pluggable framework supports custom scorers | ✅ Advanced; adds `precise-prefix-cache-scorer` with real-time KV-cache tracking via event subscriptions from vLLM, speculative indexing, and `NoHitLRUScorer` for cold requests | ✅ Advanced; Dynamo EPP embeds Dynamo's Rust-based Router library (linked as a static library via CGo) which runs the model's tokenizer inline for token-aware block hash computation; subscribes to real-time per-worker KV-cache events via NATS/ZMQ event plane and builds a KVIndexer (RadixTree) for prefix-overlap scoring; configurable `overlap_score_weight` and `router_temperature` for balancing cache reuse vs load distribution | ✅ Prefix-aware load balancing (PrefixHash / CHWBL); published paper showing dramatic TTFT improvements at scale | ✅ KV-cache aware routing as a pluggable load-balancing strategy; claims model-load-aware and KV-cache aware strategies | 🚧 WIP; roadmap includes prefix-aware routing; currently supports round-robin and session-ID-based routing only |
| **Envoy ext_proc support** | ✅ Core mechanism; EPP runs as an Envoy ext_proc filter that intercepts requests and injects endpoint selection headers | ✅ Same as GAIE; inherits ext_proc integration from upstream EPP | ✅ Same as GAIE; Dynamo EPP inherits ext_proc integration from upstream EPP | ❌ Does not use ext_proc; built-in Go proxy handles routing directly without Envoy | ❌ Does not use ext_proc; Kthena router is a standalone HTTP reverse-proxy that routes requests directly to inference backends | ❌ Does not use ext_proc; Python-based router acts as a standalone HTTP proxy |
| **Extended from GAIE EPP** | — (is the upstream) | ✅ Directly extends GAIE EPP with custom scheduler plugins (filters, scorers, scrapers); imports `pkg/epp` from GAIE as a Go dependency | ✅ Directly extends GAIE EPP with a custom scorer plugin; imports `pkg/epp` from GAIE as a Go dependency | ❌ Independent implementation; no GAIE dependency | ❌ Independent implementation; Kthena router is its own component that optionally consumes GAIE CRDs but does not extend the GAIE EPP codebase | ❌ Independent implementation; Python-based router with no GAIE dependency |
| **External dependencies** | Requires an ext-proc-capable Gateway (Envoy Gateway, Istio, GKE Gateway, kgateway) and optionally BBR for body-based routing | Same as GAIE (ext-proc gateway + optional BBR); additionally depends on `llm-d-kv-cache` library for precise prefix cache scoring and vLLM KV event publishing; P/D mode additionally requires `llm-d-routing-sidecar` on decode worker pods for prefill/decode orchestration | Same as GAIE (ext-proc gateway + optional BBR); additionally requires Dynamo FrontEnd sidecar on each worker pod (running `--router-mode direct`) to translate `x-worker-instance-id` headers into actual worker routing, and NATS/ZMQ event plane for the scorer plugin to subscribe to per-worker KV-cache events | Minimal; zero external dependencies (no Istio, Knative, Prometheus adapter); self-contained operator + proxy | Moderate; depends on Volcano scheduler for gang scheduling and topology-aware placement; router is self-contained but benefits from Volcano integration | Minimal; depends only on Kubernetes API for service discovery; optional Prometheus + Grafana for monitoring |
| **Production readiness** | ✅ GA (v1.3.1); used in production by multiple organizations; 148+ contributors, 600+ stars | ✅ Production-ready (v0.6.0); used as the inference scheduler for the llm-d platform; 63+ contributors | ✅ Production-ready (v1.0.1); backed by NVIDIA with production deployments at scale (Baseten, Moonshot AI, Mistral AI, etc.); 6.3k+ stars | ✅ Production-ready (v0.23.1); adopted by Google Cloud, Lambda, Vultr, Telescope, Arcee, Seeweb; 1.2k+ stars | 🚧 Early stage (v0.3.0); active development with 265+ stars; routing features under iteration; Gateway API integration partially in draft PRs | ✅ Production-ready (v0.1.10); backed by vLLM project; 2.2k+ stars; used in production deployments on multiple clouds |
| **Disaggregated Prefill/Decode (P/D)** | 🚧 Roadmap item; architecture supports it but no built-in P/D profiles | ✅ First-class support; EPP uses `PdProfileHandler`, `PrefillFilter`, `DecodeFilter` to select separate prefill and decode pods; requires a routing sidecar (`llm-d-routing-sidecar`) on each **decode** worker pod — the sidecar reads `x-prefiller-host-port` header injected by EPP, forwards prefill to the selected prefill pod, then runs decode locally; prefill pods do not need a sidecar | 🚧 Dynamo EPP itself does not add P/D scheduling profiles; disaggregated P/D is handled by Dynamo's native Router outside the GAIE EPP path | ❌ Not supported | ✅ Supports xPyD disaggregated prefill/decode patterns with PD-group-aware routing | 🚧 Proposal stage; disaggregated prefill routing proposed but not yet shipped |
| **Multi-model routing** | ✅ Multiple `InferencePool`/`InferenceModel` per Gateway with HTTPRoute header-based matching | ✅ Multiple `InferencePool` instances supported; each EPP handles one pool | ✅ Same as GAIE; multiple `InferencePool` instances supported via standard HTTPRoute header-based matching | ✅ Built-in multi-model proxy; routes by model name in request | ✅ ModelRoute CRDs and HTTPRoute header-based matching for multi-model routing | ✅ Routes to different model backends by model name |
| **Gateway implementation support** | Envoy Gateway, Istio, GKE Gateway, kgateway | Same as GAIE | Same as GAIE | N/A (self-contained proxy, no external gateway required) | Any Gateway API-compatible gateway; router also works standalone behind any API gateway | N/A (self-contained router, no external gateway required) |
| **Scheduling extensibility** | ✅ Pluggable framework with ProfileHandler, Filters, Scorers, Pickers; configuration via `SchedulerConfig` profiles | ✅ Highly extensible; inherits GAIE plugin framework and adds custom filter/scorer types (ByLabel, SessionAffinity, ActiveRequestScorer) | ✅ Inherits GAIE plugin framework; adds a Dynamo-specific scorer plugin for KV-cache-aware scoring | Limited; PrefixHash is the primary algorithm; no pluggable scorer framework | Pluggable load-balancing algorithms; supports custom strategies; architecture designed for extensibility | Limited; round-robin and session-ID are the only built-in algorithms; prefix-aware is WIP |
| **Implementation language** | Go | Go | Go (EPP plugin); depends on Dynamo Router in Rust for KV-cache state | Go | Go (router) + Python (utilities) | Python |

### Analysis

**1. Gateway API Inference Extension (GAIE) — upstream reference EPP**

GAIE is the Kubernetes-native standard for inference-aware routing. It defines the `InferencePool` and `InferenceModel` CRDs and provides the reference EPP implementation. The EPP operates as an Envoy `ext_proc` filter, selecting backend pods via a pluggable scheduling framework (ProfileHandler → Filter → Scorer → Picker). The built-in `prefix-cache-scorer` estimates KV-cache overlap from scheduling history — it hashes raw prompt text without tokenization and tracks assignments in a local LRU per server. GAIE is GA (v1.3.1), supports multiple Gateway implementations (Envoy Gateway, Istio, GKE Gateway, kgateway), and has the broadest ecosystem support with 148+ contributors and 600+ stars.

**2. llm-d Inference Scheduler — GAIE-extended with advanced KV-cache scoring and P/D**

llm-d directly extends the GAIE EPP codebase (imports `pkg/epp` as a Go module dependency) and adds advanced scheduling plugins. Its `precise-prefix-cache-scorer` subscribes to real-time KV-cache events from vLLM via `llm-d-kv-cache`, uses a HuggingFace tokenizer to compute accurate block hashes, and tracks actual per-pod KV block states — a significant upgrade over GAIE's estimation-based approach. llm-d also supports speculative indexing and `NoHitLRUScorer` for cold-request distribution. For disaggregated P/D, `PdProfileHandler` selects separate prefill and decode pods via label-based filtering (`PrefillFilter`, `DecodeFilter`), with a routing sidecar (`llm-d-routing-sidecar`) required on each decode worker pod to orchestrate the prefill→decode handoff via `x-prefiller-host-port` headers. Prefill pods do not need a sidecar.

**3. NVIDIA Dynamo EPP — GAIE-extended with token-aware Rust-based KV-cache scoring**

Dynamo's EPP component directly extends the GAIE EPP codebase (imports `pkg/epp` as a Go module dependency) and embeds Dynamo's Rust-based Router library (linked as a static library via CGo) for token-aware KV-cache scoring. Unlike the standard GAIE EPP which sets `X-Gateway-Destination-Endpoint` to a pod IP directly, Dynamo's EPP scorer selects workers by `worker-instance-id` and injects `x-worker-instance-id` / `x-prefill-instance-id` custom headers. Each worker pod must run a Dynamo FrontEnd sidecar (`--router-mode direct`) that reads these headers and forwards the request to the correct worker. The EPP subscribes to per-worker KV-cache events via NATS/ZMQ event plane and builds a KVIndexer (RadixTree) for prefix-overlap scoring, with configurable `overlap_score_weight` and `router_temperature`. Disaggregated P/D scheduling is not handled within the EPP itself — that capability lives in Dynamo's native Router path.

**4. KubeAI — self-contained operator with prefix-aware proxy**

KubeAI is an inference operator with a built-in Go proxy that implements prefix-aware load balancing (PrefixHash / CHWBL). It does not use GAIE APIs, ext-proc, or any external gateway. Its strength is simplicity: zero external dependencies (no Istio, Knative, or Prometheus adapter), with multi-model routing and scale-from-zero built in. However, it has no GAIE compatibility, no disaggregated P/D support, and limited scheduling extensibility beyond the built-in PrefixHash algorithm.

**5. Kthena (Volcano) — Kubernetes LLM platform with evolving GAIE support**

Kthena is a Kubernetes-native LLM serving platform from the Volcano project. Its router supports both its own `ModelRoute`/`ModelServing` CRDs and Gateway API Inference Extension CRDs (InferencePool, HTTPRoute). However, Kthena's GAIE integration is at the API consumption level — the router reads GAIE CRDs for configuration — rather than extending the GAIE EPP codebase, and does not use Envoy ext_proc. The router is a standalone HTTP reverse-proxy with pluggable load-balancing strategies including KV-cache aware routing and PD-group-aware routing for xPyD disaggregated patterns. Key differentiators include Volcano scheduler integration for gang scheduling and network topology-aware placement. The project is at an early stage (v0.3.0) with some GAIE features still in draft PRs.

**6. vLLM Production Stack — lightweight Python router**

The vLLM Production Stack provides a Python-based request router with K8s service discovery, round-robin and session-ID-based routing, and observability via Prometheus + Grafana. It has no GAIE API support, no ext-proc integration, and prefix-aware routing is listed as WIP. Disaggregated P/D is at the proposal stage. The stack is production-ready (v0.1.10, 2.2k+ stars) for simple routing scenarios but lacks the inference-aware scheduling capabilities and GAIE API compliance needed for the KAITO GWIE E2E tests.

### Decision Rationale

For KAITO's E2E tests, we use the **upstream GAIE EPP** because:

1. **Standards alignment**: KAITO's `InferenceSet` controller creates `InferencePool` resources — the EPP that consumes them should be the reference implementation that defines the API. Among the six evaluated projects, only GAIE, llm-d, and Dynamo EPP directly implement the `InferencePool`/`InferenceModel` APIs via ext_proc. KubeAI and vLLM Production Stack use independent routing mechanisms without GAIE compatibility. Kthena consumes GAIE CRDs but does not use ext_proc.
2. **Minimal dependencies**: The upstream EPP requires only an ext-proc-capable gateway (Istio in our case) and optionally BBR. In contrast, llm-d additionally requires `llm-d-kv-cache` and a routing sidecar on decode pods for P/D mode; Dynamo EPP requires a FrontEnd sidecar on every worker pod plus NATS/ZMQ event plane for KV-cache scoring.
3. **Test fidelity**: The E2E tests validate KAITO ↔ GAIE integration correctness (CRD creation, condition propagation, routing behavior). Using the reference EPP ensures we test against the canonical implementation without introducing platform-specific behaviors (e.g., Dynamo's `x-worker-instance-id` header routing or llm-d's sidecar-based P/D orchestration).
4. **Production readiness**: GAIE is GA (v1.3.1) with broad production adoption and active maintenance. llm-d (v0.6.0) and Dynamo EPP (v1.0.1) are also production-ready but carry heavier dependency stacks.
5. **Simplicity**: llm-d and Dynamo offer more advanced KV-cache scoring (real-time KV events, token-aware hashing), but GAIE's estimation-based `prefix-cache-scorer` is sufficient for validating routing correctness in the E2E environment with LLM mockers that do not have real KV caches.

## Alternatives

### Using a Kind Cluster Instead of AKS

A Kind cluster would avoid cloud costs entirely, but introduces its own problems:

- **Azure CCM**: Kind has no Azure Cloud Controller Manager, so the fake node providerID trick is unnecessary — however this also means the test does not exercise real AKS CNI networking, load balancer integration, or node conditions as seen in production.
- **Network fidelity**: Kind uses `kindnet` rather than Azure CNI. Shadow pod IPs are in a different CIDR and routing may differ from real deployments.
- **Istio support**: Istio's Gateway pod requires LoadBalancer service support; Kind requires `MetalLB` or similar, adding complexity.

AKS is preferred because it provides a realistic network environment that matches production KAITO deployments with minimal extra setup.

### Using Real GPU Nodes

Running the full GWIE E2E against real GPU nodes provides the highest fidelity (real vLLM metrics, real KV-cache prefix scoring) but introduces:

- **Cost**: GPU node provisioning is expensive (10–20 min per node, $$$).
- **Quota limits**: Cloud GPU quota can cause flakiness unrelated to code changes.
- **CI unsuitability**: Impractical for open PR checks from external contributors.

The GPU node mocker + shadow pod approach decouples Gateway/routing correctness from GPU availability, making tests fast and cost-effective.

### Using Existing `preset_test.go` Infrastructure

The existing `test/e2e/preset_test.go` tests validate Workspace-based inference against real cloud GPU nodes. We could extend this file with GWIE-specific cases. However:

- Those tests require GPU nodes and real vLLM.
- GWIE routing behavior (BBR, EPP, `InferencePool`) requires a Gateway, which is not part of the existing test infrastructure.

A dedicated test file (e.g., `test/e2e/gateway_inference_test.go`) with its own environment setup is preferred to keep concerns separated.

## Implementation History

- 2026-03-12: Initial proposal drafted
