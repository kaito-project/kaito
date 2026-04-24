# Dynamo vs llm-d: Distributed LLM Inference Orchestration Framework Comparison

Both are **orchestration layers** for LLM inference, sitting above inference engines (vLLM/SGLang/TRT-LLM) to solve multi-GPU/multi-node distributed serving challenges. Core capabilities overlap significantly, but design philosophies and implementation paths differ.

- **Dynamo**:
   - https://github.com/ai-dynamo/dynamo
   - **architecture**: https://github.com/ai-dynamo/dynamo/blob/main/docs/design-docs/architecture.md
- **llm-d**:
   - https://github.com/llm-d/llm-d
   - **architecture**: https://github.com/llm-d/llm-d-inference-scheduler/blob/main/docs/architecture.md

## Overall Comparison

### Similarities

| Capability | Dynamo | llm-d |
|---|---|---|
| Prefill/Decode disaggregation | ✅ | ✅ |
| KV-cache aware routing | ✅ | ✅ (prefix-cache aware) |
| Multi-tier KV offloading (GPU→CPU→SSD→remote) | ✅ (KVBM) | ✅ (vLLM KVConnector + filesystem backend) |
| SLA/SLO-driven autoscaling | ✅ (Planner) | ✅ (Workload Variant Autoscaler) |
| vLLM / SGLang backend support | ✅ Both supported | vLLM primary, SGLang in progress |
| NIXL for KV transfer | ✅ | ✅ |
| OpenAI-compatible API | ✅ | ✅ |

### Key Differences

| Dimension | **Dynamo** (NVIDIA) | **llm-d** (Red Hat / Community) |
|---|---|---|
| **Led by** | NVIDIA | Red Hat + open-source community (IBM, etc.) |
| **Core language** | **Rust** + Python | **Go** (scheduler) + Python (vLLM contributions) |
| **Control plane** | Custom service discovery + etcd/NATS; K8s via Grove operator (optional) | **Kubernetes-native** — built directly on K8s Gateway API / Inference Gateway (IGW) |
| **Deployment model** | Bare metal, container, or K8s (flexible but requires extra config) | **K8s-first**, Helm chart + standard K8s API as control plane |
| **Inference engine coupling** | Deep support for SGLang, TRT-LLM, and vLLM simultaneously | Deeply bound to **vLLM**, scheduler orchestrates P/D via sidecar |
| **Hardware affinity** | Strong NVIDIA optimization (NVLink/NVSwitch/GB200 NVL72) | Hardware-neutral — explicit support for **NVIDIA GPU, Intel XPU, Google TPU** |
| **Routing implementation** | Custom Router component with built-in KV overlap calculation | Reuses **K8s Gateway API extension point** (EPP), scheduler as filter/scorer plugin |
| **P/D coordination** | Built into framework's prefill-decode scheduling pipeline | Decode-side **sidecar** pattern, loosely coupled |
| **Model startup optimization** | **ModelExpress**: GPU-to-GPU weight streaming (7× cold start speedup) | No equivalent; relies on standard model loading |
| **Configuration tuning** | **AIConfigurator**: offline simulation of 10K+ configs to find optimal | No equivalent; manual tuning via benchmark guide |
| **Multimodal/Video** | Native multimodal E/P/D + FastVideo video generation | Focused on LLM text inference |

---

## Router Deep Dive

### I. Architecture Positioning

| | **Dynamo Router** | **llm-d Inference Scheduler** |
|---|---|---|
| **Essence** | Custom standalone routing component, embedded in Dynamo Frontend or independently deployed | K8s Gateway API **EPP (Endpoint Picker) extension**, embedded in Envoy ext-proc callback chain |
| **Language** | **Rust** (core) + Python bindings | **Go** |
| **Data plane** | Dynamo's own HTTP Frontend forwards directly | **Envoy** proxy → ext-proc callback → EPP makes decisions → Envoy executes forwarding |
| **Control plane** | etcd/NATS service discovery, Router maintains global state | **K8s API** (InferencePool / InferenceModel CRD) + GIE framework |
| **Service discovery** | Dynamic registration (`register_model()`), broadcast via etcd | K8s Pod endpoint auto-discovery |

**Core distinction**: Dynamo Router is a **fully custom stateful router**; llm-d is **plugin-style enhancement** on top of standard K8s Gateway.

### II. KV Cache-Aware Routing

This is the most critical capability of both, with completely different implementation paths:

#### Dynamo: Event-Driven Global Radix Tree

```
Worker → KVPublisher → (KV stored/removed events) → KVIndexer (Global Radix Tree) → Router query
```

- Each Worker embeds a **KVPublisher** that emits events on block allocation/eviction
- **KVIndexer** maintains a global prefix tree (RadixTree), each node stores worker ID + block info
- Supports **concurrent RadixTree** (multi-threaded, sticky worker routing ensures per-worker serialization) or single-threaded version
- Blocks identified by token hash (including LoRA adapter name), enabling precise cross-worker overlap matching
- Router calls `find_matches_for_request(tokens)` → returns matched block count per worker

**Cost function**:
```
cost = overlap_score_weight × prefill_blocks + decode_blocks
```
Selects the worker with lowest cost. Configurable `router_temperature` for softmax sampling to avoid hotspots.

#### llm-d: Scraper Pull + Filter/Scorer Plugin Chain

```
Scraper → periodically pull Pod metrics → Datastore → Filter chain → Scorer chain → Pod selection
```

- **Scraper** periodically fetches metrics from vLLM Pods (KV cache hit rate, queue depth, load, etc.)
- **precise-prefix-cache-scorer**: maintains its own block index, scores candidate Pods by prefix matching
  - Configurable `blockSize`, `maxPrefixBlocksToMatch`, etc.
- Orchestrated via **SchedulingProfile** YAML: filter → scorer → picker chain
- Selects highest-scoring Pod after weighted aggregation

| Dimension | Dynamo | llm-d |
|---|---|---|
| KV info acquisition | Worker **pushes** events (real-time) | Scraper **pulls** metrics (periodic) |
| Global index | Custom concurrent RadixTree + per-worker nodes | prefix-cache-scorer built-in block index |
| Precision | Block-level exact overlap (including LoRA hash) | Block-level matching, but affected by pull latency |
| Multi-engine support | SGLang / TRT-LLM / vLLM all have KVPublisher | Primarily targets vLLM |

### III. P/D Disaggregated Routing

| | Dynamo | llm-d |
|---|---|---|
| Routing decision | Router has built-in prefill/decode pool concept, unified cost function allocation | **disagg-profile-handler** plugin: independent SchedulingProfile for prefill and decode |
| Trigger condition | Automatic (based on Planner decisions) | **prefix-based-pd-decider**: triggers P/D disaggregation when uncached tokens > threshold |
| KV transfer coordination | Built into framework | **Decode-side Sidecar** coordinates vLLM P2P KV transfer via NIXL |
| E/P/D three-stage | Supports multimodal Encode/Prefill/Decode | Experimental support (`always-disagg-multimodal-decider`) |

### IV. Extensibility and Plugin Architecture

| | Dynamo | llm-d |
|---|---|---|
| Extension method | Code-level: modify Router's Rust logic or Python bindings | **YAML declarative**: configure filter/scorer/picker plugin combinations |
| Custom routing | Requires writing Rust/Python code | Implement Go interface → register plugin → reference in YAML |
| Profile mechanism | None (single cost function) | **SchedulingProfile**: define different filter/scorer chains for different request types |
| Label filtering | No equivalent | **by-label-selector**: use K8s label selector to filter Pods directly (e.g., `hardware-type: H100`) |

llm-d's plugin architecture is significantly stronger — routing strategies can be assembled purely through YAML without code changes.

### V. Multi-Router Coordination

**Dynamo**: Supports **multi-Router instance** state synchronization via Inter-Router Communication:
- `AddRequest` / `RemoveRequest` / `OverlapUpdate` — three event types
- Ensures routing consistency in distributed environments

**llm-d**: EPP is stateless (or low-state), relying on K8s API + Envoy's load balancing mechanisms for natural horizontal scaling. No explicit inter-router synchronization needed.

### VI. Additional Routing Capabilities

| Capability | Dynamo | llm-d |
|---|---|---|
| Priority scheduling | `nvext.agent_hints.latency_sensitivity` + queue policies (FCFS / WSPT) | Multi-tenant fairness + priority (via scorer weights) |
| Backpressure control | `router_queue_threshold` | Envoy-level flow control + EPP-level filter |
| Session affinity | Implicitly achieved via KV cache overlap | Explicit support |
| LoRA routing | Block hash includes adapter name, automatic routing | Cache-aware LoRA routing (v0.5) |
| Multi-model | Single Router serves single model | **InferencePool CRD** natively supports multi-model cluster sharing |

---

## Summary

- **Dynamo** = NVIDIA's full-stack inference operating system. Rust performance + deep NVIDIA hardware integration + custom control plane — most feature-complete but with stronger ecosystem lock-in. Router is a **high-performance stateful router** with real-time event-driven design, global RadixTree, and precise block overlap calculation — higher performance ceiling.

- **llm-d** = Kubernetes-native inference orchestration. Reuses standard K8s Gateway API, hardware-neutral, loosely coupled components — better suited for multi-cloud/multi-hardware scenarios on existing K8s platforms. Scheduler is a **K8s-native declarative scheduling framework** with plugin filter/scorer chains, YAML orchestration, and Envoy ext-proc integration — superior flexibility and extensibility.

**Engineering recommendation**: Pure NVIDIA GPU clusters pursuing peak performance → Dynamo; K8s platforms requiring multi-hardware, multi-model, and customizable routing strategies → llm-d.

---

## KAITO Integration Recommendation: llm-d Is the Better Fit

[KAITO](https://github.com/kaito-project/kaito), as a Kubernetes-native AI inference operator, should integrate with **llm-d** to implement Prefill/Decode disaggregation.

### Core Reasons for Choosing llm-d

1. **Kubernetes-native architecture alignment**: KAITO is a K8s operator; llm-d is built directly on K8s Gateway API / InferencePool CRD, sharing the same control plane (K8s API) with no additional service discovery components required. Dynamo's custom etcd/NATS control plane conflicts architecturally with KAITO's K8s operator model.

2. **Loosely coupled P/D disaggregation model**: llm-d's P/D coordination via decode-side sidecar is naturally compatible with KAITO's Pod orchestration model — KAITO manages Pod lifecycle and GPU allocation, llm-d sidecar manages P/D scheduling and KV transfer. Dynamo's P/D is built into the framework, requiring the entire Dynamo runtime to be pulled in.

3. **Hardware neutrality**: KAITO supports multiple GPU SKUs; llm-d explicitly supports NVIDIA GPU, Intel XPU, and Google TPU. Dynamo is tightly bound to NVIDIA hardware optimizations (NVLink/NVSwitch).

4. **Declarative extensibility**: llm-d's SchedulingProfile orchestrates filter/scorer chains via YAML; KAITO can map CRD parameters to different scheduling strategies without code changes. Dynamo requires modifying Rust logic.

5. **Community direction alignment**: llm-d is led by Red Hat + IBM community, aligned with KAITO's open-source K8s ecosystem positioning. Gateway API + Inference Extension (IGW) is already the K8s SIG direction.

### Recommended Integration Path

```
KAITO Workspace CRD (extend with P/D mode declaration)
  ├── Prefill Pool (KAITO orchestrates prefill Pods + GPU allocation)
  ├── Decode Pool (KAITO orchestrates decode Pods + GPU allocation)
  ├── llm-d EPP (routing layer, integrated via InferencePool CRD)
  └── llm-d Sidecar (injected into decode Pods for NIXL KV transfer)
```

- KAITO Workspace CRD extended with P/D mode declaration (prefill pool / decode pool)
- KAITO operator handles Pod orchestration + GPU allocation
- llm-d EPP serves as routing layer, integrated via InferencePool CRD
- llm-d sidecar injected into decode Pods for KV transfer

### Additional Findings from Dynamo Deep-Dive Walkthrough

After hands-on walkthrough, Dynamo revealed additional practical concerns:

#### 1. Dynamo Is Essentially a Showcase Platform for NVIDIA's Full-Stack Software

Dynamo is not just an inference orchestration layer — it's an integrated showcase platform for NVIDIA's full-stack OSS software. Using a Python web framework analogy: **Dynamo is like Django (batteries-included, heavyweight), while llm-d is like Flask (lightweight, flexible, easy to integrate)**. For a project like KAITO that needs to embed into an existing K8s platform, lightweight integration is far superior to adopting a full-stack framework.

#### 2. Runtime Wrapper Complexity Exceeds Expectations

Dynamo's runtime wrapper doesn't just handle inference forwarding — it also includes built-in reasoning and tool parsing logic. This means:
- **KAITO's existing model presets will fail** — Dynamo's wrapper has its own assumptions about model input/output
- **Extended version dependency chain**: Each new model release requires waiting for ① a vLLM release ② a Dynamo release before support is available
- The wrapper is written in **Python + Rust** hybrid, making debugging and maintenance difficult

#### 3. Analysis of Dynamo's Two Deployment Modes

**Traditional deployment mode (Central Router)**:
- Frontend serves as central router, forwarding requests to Worker Pods
- Depends on **NATS + etcd** as infrastructure

**EPP deployment mode**:
- Dynamo EPP only supports P/D disaggregation with KV-cache awareness
- Frontend serves as sidecar, forwarding prompt requests to prefill/decode instances
- Depends on **NATS** for KV-events information

**Key conclusion**: Dynamo's EPP mode is functionally nearly identical to llm-d, but with an additional NATS dependency for KV event transport. Furthermore, the EPP is written in **Go + Rust** hybrid, making maintenance and troubleshooting difficult. In contrast, llm-d's EPP is pure Go, consistent with the K8s ecosystem toolchain and much more maintainable.

### Final Conclusion

Based on comprehensive architecture evaluation and hands-on walkthrough, **llm-d is the clear choice for KAITO's P/D disaggregation implementation**:

| Dimension | Dynamo | llm-d |
|---|---|---|
| Integration complexity | High (full-stack framework, NATS/etcd dependencies) | Low (K8s-native, no extra dependencies) |
| Version dependency chain | vLLM → Dynamo → KAITO (three-tier) | vLLM → KAITO (two-tier) |
| Code maintainability | Python + Rust + Go hybrid | Pure Go (EPP) + Python (vLLM) |
| P/D disaggregation capability | EPP mode ≈ llm-d, but with extra NATS layer | Native support, sidecar pattern |
| KAITO preset compatibility | Requires adapting to Dynamo wrapper | Directly compatible |
| Troubleshooting difficulty | High (multi-language hybrid, NVIDIA proprietary components) | Low (standard K8s toolchain) |
