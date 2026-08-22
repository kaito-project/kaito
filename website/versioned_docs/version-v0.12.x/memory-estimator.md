---
title: Memory Estimator
description: How KAITO estimates GPU memory to determine node count and context size for LLM inference.
---

# Memory Estimator

When you deploy an LLM through KAITO, the system must answer two questions before it can start serving:

1. **How many GPU nodes are needed?** — fewer nodes means less inter-node communication and better inference performance. KAITO's memory estimator computes this up front.
2. **What is the largest context window the vLLM server can support?** — a longer context window lets the model handle longer conversations and documents, but requires more GPU memory. KAITO delegates this to vLLM's runtime auto-fit (`--max-model-len=auto`).

Both answers come from the same underlying principle: **GPU memory is a finite budget that must be shared between model weights, the KV cache, and runtime overhead.** KAITO estimates this budget to size the cluster, and vLLM measures it precisely at startup to size the context window.

## The GPU Memory Budget

The total VRAM consumed during LLM inference can be broken down into three components:

```
Total VRAM = Model Weights + KV Cache + Runtime Overhead

┌─────────────────────────────────────────┐
│              Total GPU VRAM             │
├─────────────────────────────────────────┤
│  Model Weights (static, largest chunk)  │
├─────────────────────────────────────────┤
│  KV Cache (grows with context length)   │
├─────────────────────────────────────────┤
│  Runtime Overhead (CUDA, activations…)  │
└─────────────────────────────────────────┘
```

### Model Weights

Model weights are the learned parameters that define the network. Their memory footprint is determined by a simple formula:

```
Model Memory = Parameter Count × Bytes per Parameter
```

Where bytes per parameter depends on the precision: 2 bytes for FP16/BF16, 1 byte for FP8, 0.5 bytes for INT4. For example, a 70B-parameter model in FP16 needs 70 × 10⁹ × 2 = 140 GiB. In practice, the vLLM runtime representation is about **2% larger** than the raw checkpoint size.

### KV Cache

During autoregressive generation, the model caches the **Key** and **Value** vectors from every previous token so it doesn't recompute attention from scratch. This KV cache is the largest *dynamic* memory consumer and the direct reason context length is bounded by GPU memory.

The per-token KV cache cost is:

```
KV Cache per Token = 2 × Precision × Layers × Hidden Dimension
```

More specifically, KAITO computes it as:

```
BytesPerToken = 2 × numLayers × numKVHeads × headDim × dtypeSize
```

| Factor | Meaning |
|--------|---------|
| 2 | One **Key** tensor + one **Value** tensor per layer |
| numLayers | Number of transformer layers |
| numKVHeads | Key-value attention heads (may be fewer than query heads in GQA/MQA) |
| headDim | Per-head dimension (`hidden_size / num_attention_heads`) |
| dtypeSize | Bytes per element (2 for FP16/BF16, 1 for FP8) |

The total KV cache scales linearly with both context length and concurrency:

```
Total KV Cache = BytesPerToken × Context Length × Concurrent Requests
```

When the model is distributed across multiple GPUs, each GPU holds only a fraction of the KV cache. How the cache is partitioned depends on the parallelism strategy and the attention type:

- **Tensor parallelism (within a node):** the attention heads are partitioned across the GPUs, so each of the `N` GPUs stores the KV cache for only its assigned heads — `1/N` of the per-token cost.
- **Pipeline parallelism (across nodes):** the transformer *layers* are split across the pipeline stages (one per node), so each GPU holds the KV cache for only its share of the layers — an additional `1/nodes` factor.
- **Multi-head Latent Attention (MLA):** models such as DeepSeek and Kimi compress the KV cache into a single shared latent vector per token. This latent cache is **replicated on every tensor-parallel rank** rather than partitioned, so it is *not* divided by the tensor-parallel size (pipeline parallelism still splits it by layer across nodes).

KAITO folds these into a single **adjusted bytes per token** that mirrors how the model weights are distributed.

### Runtime Overhead

Activation memory — the intermediate results from each transformer layer during the forward pass — together with CUDA graph capture buffers and other runtime allocations (CUDA context, memory allocator fragmentation) also consume VRAM. vLLM measures these empirically at startup; KAITO has to estimate them ahead of time.

Because activation and CUDA graph memory grow with model size (more layers and a wider hidden dimension produce larger intermediate tensors), KAITO does not use a single flat number. Instead it reserves a **base overhead plus a term that scales with the per-GPU model weight share**:

```
Runtime Overhead per GPU = baseOverhead + scaleFactor × (model weight share per GPU)
```

The base covers model-independent costs (CUDA context, NCCL buffers, and a small-model activation/CUDA-graph baseline); the weight-scaled term approximates the larger activation and CUDA graph footprints of bigger models. Because activations and CUDA graphs are sharded across tensor-parallel ranks the same way weights are, the per-GPU weight share is a good proxy.

Beyond this, not all of a GPU's physical VRAM is available to the application. The GPU driver, CUDA runtime, and memory allocator fragmentation all claim a portion. KAITO accounts for this by applying a **GPU utilization factor** — treating only a fraction of the advertised VRAM as usable.

:::tip
The exact overhead and utilization constants are tuned for vLLM's inference engine. If you observe OOM errors, consider selecting a larger GPU SKU or reducing the context length.
:::

## Why Fewer Nodes Is Better

Tensor parallelism — splitting a model across GPUs — introduces inter-GPU (and potentially inter-node) communication at every transformer layer. Within a single node, GPUs communicate over high-bandwidth NVLink or PCIe. Across nodes, they communicate over the network, which is orders of magnitude slower.

Therefore, **the minimum node count that can physically hold the model yields the best inference latency and throughput.** KAITO's estimator always targets this minimum rather than spreading the model across more nodes than necessary.

## How KAITO Determines the Minimum Node Count

The estimator runs at Workspace creation time, before any GPU nodes are provisioned. Given:

- The model's weight size and per-token KV cache cost (from the preset model registry)
- The GPU SKU (memory per GPU, number of GPUs per node)
- A context size (user-specified or a conservative default of 2048 tokens)

…it answers: _"What is the smallest number of nodes that can hold the model weights, the KV cache for this context length, and overhead?"_

The logic, in plain terms:

1. **Per-GPU usable memory** = physical VRAM × utilization factor
2. **Fixed per-GPU reserve** = base runtime overhead + KV cache per GPU (= `contextLength × BytesPerToken / totalGPUs`)
3. **Per-GPU memory left for weights** = (usable memory − fixed reserve) ÷ (1 + weightOverheadFactor). The weight-scaled part of the runtime overhead (activations, CUDA graphs) is proportional to the weight that lands on each GPU, so it folds into a `(1 + factor)` divisor rather than a fixed subtraction — this keeps the solve non-circular when the GPU count is still unknown.
4. **Minimum GPUs needed** = model weight size ÷ per-GPU memory left for weights (rounded up)
5. **Minimum nodes** = minimum GPUs ÷ GPUs per node (rounded up)

## How the Context Size Is Determined

KAITO does **not** pin a fixed `--max-model-len` for the server. Instead it launches vLLM with **`--max-model-len=auto`** and lets vLLM's native [auto-fit](https://docs.vllm.ai/en/latest/configuration/engine_args/#-max-model-len) logic choose the context window.

The reason is accuracy. vLLM measures the **real** KV-cache budget on the GPU at startup — after the weights are actually loaded and the runtime overhead (activations, CUDA graphs, allocator fragmentation) is measured empirically — and then selects the largest context window that fits. Estimating these quantities ahead of time is inherently approximate; an over-estimate would launch the server with a context window that does not fit and crash-loop at startup. Delegating to auto-fit removes that risk.

Conceptually, vLLM solves the same equation the estimator uses for node count, but in the opposite direction and with **measured** rather than predicted values:

```
Max Context Length = (Measured Free Memory after weights + overhead) / Adjusted Bytes per Token
```

The result is then **clamped** to the model's architectural limit (`max_position_embeddings`) — the context length can never exceed what the model was designed for — and reduced further if needed so the KV cache fits the measured budget.

You can see vLLM's chosen value in the server logs, for example:

```
Auto-fit max_model_len: reduced from 262144 to 117264 to fit in available GPU memory
```

### User Override

If you set `max-model-len` explicitly in your inference ConfigMap, that value is used in two places:

- **Node count estimation:** the estimator uses your specified context size (instead of the conservative 2048-token default) to reserve KV-cache memory when computing how many nodes are needed.
- **vLLM launch:** your explicit value is appended after `--max-model-len=auto` on the vLLM command line, so it takes precedence over auto-fit. This is useful when you know the exact context length your workload requires and want KAITO to provision accordingly.

### The Relationship Between Node Count and Context Size

Node count and context size are two faces of the same memory budget:

- **More nodes** → model weights are distributed across more GPUs → more memory left per GPU for KV cache → **longer context possible**.
- **Longer context** → more KV cache per GPU → less room for model weights → **more nodes needed**.

KAITO resolves this tension by determining the minimum node count using a conservative context size, then delegating context-window maximization to vLLM's auto-fit, which fills the resulting memory envelope at runtime.

## Worked Example

<details>
<summary>Phi-4-mini-instruct on A100 vs A10</summary>

We'll use **Phi-4-mini-instruct** as an example. Running the preset generator gives us the model parameters KAITO needs:

```bash
❯ go run ./cmd/preset-generator microsoft/Phi-4-mini-instruct
attn_type: GQA
name: phi-4-mini-instruct
architectures:
- Phi3ForCausalLM
type: tfs
version: https://huggingface.co/microsoft/Phi-4-mini-instruct
download_at_runtime: true
download_auth_required: false
disk_storage_requirement: 87Gi
model_file_size_gb: 7.15
bytes_per_token: 131072
model_token_limit: 131072
reasoning_parser: ""
tool_call_parser: ""
vllm:
  model_name: phi-4-mini-instruct
  model_run_params:
    load_format: auto
    config_format: auto
    tokenizer_mode: auto
  disallow_lora: false
```

### Setup

Deploy **Phi-4-mini-instruct** on Azure `Standard_NC24ads_A100_v4` nodes (1× A100 80 GiB GPU per node).

| Parameter | Value |
|-----------|-------|
| Model weight size (`model_file_size_gb`) | 7.15 GiB |
| `bytes_per_token` | 131,072 bytes (128 KB) |
| `model_token_limit` | 131,072 tokens (128K) |
| GPUs per node | 1 |
| GPU memory | 80 GiB |

### Step 1: Determine Minimum Node Count

Using the default context size (2,048 tokens) for initial estimation:

1. `modelSize = 7.15 × 1.02 = 7.29 GiB`
2. `availablePerGPU = 80 × 0.84 = 67.2 GiB`
3. `kvCachePerGPU = 2048 × 128 KB / 1 = 0.25 GiB`
4. `fixedReserve = 2.3 (base) + 0.25 (KV) = 2.55 GiB`
5. `memForWeightsPerGPU = (67.2 − 2.55) / (1 + 0.05) = 64.65 / 1.05 = 61.57 GiB` — the `(1 + 0.05)` divisor accounts for the weight-scaled runtime overhead (activations + CUDA graphs)
6. `minGPUs = ⌈7.29 / 61.57⌉ = 1`
7. `minNodes = ⌈1 / 1⌉ = 1`

**Result: 1 node** (1 GPU) is sufficient — the model is small enough to fit easily.

### Step 2: Determine Context Size

KAITO launches vLLM with `--max-model-len=auto`, so the context window is chosen by vLLM's auto-fit at runtime rather than computed by KAITO. The calculation below mirrors what auto-fit does — using measured memory instead of these estimates — to show why the model gets its full context on this GPU:

1. `modelWeightPerGPU = 7.29 GiB` (single GPU, no distribution)
2. `availablePerGPU = 67.2 GiB`
3. `overhead = 2.3 + 0.05 × 7.29 = 2.66 GiB`
4. `remainingPerGPU = 67.2 − 7.29 − 2.66 = 57.25 GiB`
5. `adjustedBytesPerToken = 128 KB` (single GPU, GQA)
6. `rawCandidate = 57.25 GiB × 8,192 tokens/GiB ≈ 469,000 tokens`
7. Clamp to model limit: `min(469000, 131072) = 131,072`

**Result: vLLM's auto-fit selects `131072`** — the full 128K context the model architecture supports, since there is far more KV-cache room than the model can use.

### What If We Used a Smaller GPU?

On a `Standard_NV36ads_A10_v5` node (1× A10 24 GiB GPU):

1. `availablePerGPU = 24 × 0.84 ≈ 20.16 GiB`
2. `modelSize = 7.29 GiB` (actual SafeTensor size × 1.02)
3. `overhead = 2.3 + 0.05 × 7.29 = 2.66 GiB` (base + weight-scaled term)
4. `remainingPerGPU = 20.16 − 7.29 − 2.66 = 10.21 GiB`
5. `rawCandidate = 10.21 GiB × 8,192 tokens/GiB ≈ 83,640 tokens`
6. Clamp to model limit: `min(83640, 131072) = 83,640`

The model still fits on 1 node, but vLLM's auto-fit selects a context window of only ~83K tokens — well below the model's 128K architectural limit — because the smaller GPU leaves less room for the KV cache. To get the full context, you'd need a GPU with more memory.

</details>
