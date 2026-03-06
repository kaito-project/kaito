---
title: Inference Traffic Routing
---

In this conceptual guide, you'll learn how KAITO integrates with the Gateway API Inference Extension (GAIE) to improve how traffic is routed to LLM-serving pods and how it enables smarter, more scalable inference across multi-model environments.

## Motivation

In KAITO, inference traffic is typically routed directly to model-serving pods deployed through workspaces, instead of LLM-aware routing based on server load or priority, which may lead to performance bottlenecks as usage scales up. The [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/) (GAIE) enhances this experience by adding intelligent traffic routing, scheduling, and load balancing, all based on model metadata, system load, and request priority.

### Core concepts

| Concept | Purpose |
|--|--|
| `InferencePool` | A logical backend abstraction representing a group of model-serving pods. Functions similarly to a Kubernetes service, but with inference-specific semantics such as model identity, queuing, and performance metrics. |
| `InferenceModel` | Maps public-facing model identifiers to one or more `InferencePools`. Carries metadata such as version, fine-tuned adapter name, and criticality for prioritization and routing. |
| Gateway / `HTTPRoute` |	Kubernetes-native Gateway API resources. When GAIE is enabled, `HTTPRoute` can reference an `InferencePool` directly. Requests are routed via GAIE’s intelligent endpoint selection mechanism. |
| Endpoint Picker | Selects the optimal backend pod within an `InferencePool` to handle an inference request. May consider system metrics (e.g. GPU utilization, memory), model metadata, fine-tuned adapters, and criticality for efficient routing. |

### GAIE Integration with KAITO

KAITO orchestrates and manages model-serving workloads using its Workspace CRD, which defines model runtimes (e.g., vLLM, HuggingFace Transformers), fine-tuned adapters, and inference defaults. GAIE extends KAITO by enabling inference-aware traffic routing to these workloads through Kubernetes Gateway APIs, and when enabled, KAITO:

* Dynamically provisions InferencePool and Endpoint Picker resources per Workspace
* Integrates with **any** Gateway controller that supports GAIE (e.g. [Istio](https://istio.io/latest/docs/reference/config/networking/gateway/), [Envoy](https://gateway.envoyproxy.io/docs/tasks/quickstart/))
* Enables inference-aware traffic routing using model metadata and runtime metrics
* Delivers improved latency, rollout flexibility, and multi-model serving at scale

The following diagram illustrates how KAITO functions as a first-class model backend in modern, Gateway-driven AI/ML platforms:

```bash
+-------------------+
|  KAITO Controller |
+-------------------+
          |
          v
+------------------------+            +-----------------------------+
|   Workspace CRDs       |            |  FluxCD (per workspace)     |
|   Presets, resources   |            |                             |
+------------------------+            |  +----------------------+   |
          |                           |  |     OCIRepository    |   |
          |                           |  +----------------------+   |
          |                           |  +----------------------+   |
          v                           |  |     HelmRelease      |   |
+----------------------------+        |  +----------------------+   |
| Model server pod(s)        | <------+-----------------------------+
| - vLLM / Transformers      |
| - LoRA adapters (optional) |
+----------------------------+
          |
          |  (matched via labels)
          v
+------------------------+
|     InferencePool      |  <----------------------+
+------------------------+                         |
          |                                        |
          v                                        |
+----------------------- -+                        |
|   Endpoint Picker Pod   |                        |
+---------------------- --+                        |
          |                                        |
          v                                        |
+------------------------+           +--------------------------------+
|     InferenceModel     | ------->  |  HTTPRoute (Gateway API)       |
+------------------------+           |  - Route rules                 |
                                     |  - BackendRef (points to) Pool |
                                     +--------------------------------+
                                                 |
                                                 v
                                 +-------------------------------+
                                 | Gateway (Istio, Envoy, etc.)  |
                                 +-------------------------------+
```

* The deployed KAITO workspace defines the model, compute resources, and inference settings.
* KAITO spins up the model server pod(s) for the defined workspace.
* If GAIE is enabled, KAITO also sets up an automation tool (in this case, FluxCD) to install GAIE’s components like `InferencePools` and `Endpoint Pickers` that will help route traffic.
* Your LLM inference pod gets added to an `InferencePool`, which groups similar pods (e.g., same model and version).
* An `InferenceModel` is created to represent the specific variant of the model that clients can query (e.g. LLM + version + criticality).
* These resources plug into your Gateway API (like Istio or Envoy), which connects client requests to the right model pod using this GAIE logic.

### Inference Request Flow

The following diagram walks through what happens when a user (or your application) requests an LLM response, like calling `/v1/chat/completions` OpenAI-compatible inference endpoint:

```bash
+-----------------+
|  Client / App   |
+-----------------+
         |
         |  HTTP request: POST /v1/chat/completions
         |  Headers: model=phi-4-mini-instruct
         v
+------------------+
|     Gateway      |
+------------------+
         |
         | HTTPRoute match
         v
+--------------------------------------+
| BackendRef (points to) InferencePool | 
+--------------------------------------+
         |
         v
+---------------------------+
| GAIE Controller           |
|                           |
| - InferenceModel lookup   |
| - Model metadata          |  
| - Criticality             |
| - Pod metrics             |
+---------------------------+
         |
         | Endpoint picker selects pod
         v
+--------------------------+
|   KAITO LLM server pod   |
|   (e.g., phi-4-mini)     |
+--------------------------+
         |
         | Inference response
         v
+--------------------------+
| Gateway forwards result  |
+--------------------------+
         |
         v
+------------------+
|     Client       |
+------------------+
```

In this sample request flow:
1. A client (like your application frontend or a script) sends an inference request, specifying which LLM it wants.
2. The Gateway receives the request and uses configured rules to match the model name to a backend.
3. Instead of going directly to a random pod, the request is passed through GAIE’s InferencePool logic.
4. GAIE checks the InferenceModel for the requested model name and uses the Endpoint Picker to choose the best pod based on (1) model version and adapter, (2) load and/or GPU usage, (3) priority.
5. The request is sent to the selected KAITO model pod, which processes it and returns a result.
6. The result flows back through the Gateway to the client.

Perk: You don’t have to configure any of this logic manually. Once GAIE is enabled and your KAITO workspace is deployed, your inference requests are automatically routed to the right inference pod in an optimized manner.

### Get Started

Learn how to enable GAIE and get started with sample deployments on your Kubernetes cluster using [Gateway API Inference Extension with KAITO guide](./gateway-api-inference-extension.md).

### Advanced Use Cases

1. Versioned Rollouts
    * Deploy new versions as separate `InferenceModels` with traffic splitting.
    * Update routing logic to gradually shift traffic or support A/B testing.

2. Criticality-Based Scheduling
    * Assign higher criticality to latency-sensitive models (like `chat`).
    * GAIE endpoint picker prioritizes requests accordingly.

3. Multi-Tenant Model Serving
    * Use label-based separation in `InferencePools`.
    * Tenants define their own `InferenceModels` while platform operators manage pool capacity.
