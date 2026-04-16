---
title: Memory Estimator
description: How KAITO estimates GPU memory to determine node count and context size for LLM inference.
---

# Memory Estimator

When you deploy an LLM through KAITO, the system must answer two questions before it can start serving:

1. **How many GPU nodes are needed?** — fewer nodes means less inter-node communication and better inference performance.
2. **What is the largest context window the vLLM server can support?** — a longer context window lets the model handle longer conversations and documents, but requires more GPU memory.

Both answers come from the same underlying principle: **GPU memory is a finite budget that must be shared between model weights, the KV cache, and runtime overhead.** KAITO's memory estimator does the math automatically so you don't have to.

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

When tensor parallelism splits the model across N GPUs, each GPU only holds 1/N of the KV cache — the attention heads are partitioned, and each GPU stores the cache for its assigned heads only.

### Runtime Overhead

Activation memory — the intermediate results from each transformer layer during the forward pass — and other runtime allocations (CUDA context, memory allocator fragmentation) also consume VRAM. A common rule of thumb estimates activation memory at around 25% of model weight memory. During inference, however, the activation footprint is much smaller than during training because only one token is processed at a time rather than entire batches. KAITO reserves a small fixed overhead for these costs.

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
2. **Per-GPU overhead** = fixed runtime overhead + KV cache per GPU (= `contextLength × BytesPerToken / totalGPUs`)
3. **Per-GPU memory left for weights** = usable memory − overhead
4. **Minimum GPUs needed** = model weight size ÷ per-GPU memory left for weights (rounded up)
5. **Minimum nodes** = minimum GPUs ÷ GPUs per node (rounded up)

## How KAITO Determines the Context Size

Once the node count is known, a second calculation runs in the opposite direction: instead of asking _"how many nodes for this context length?"_, it asks _"given this many nodes, what is the **longest context window** the vLLM server can support?"_

It takes the leftover GPU memory *after* model weights and the runtime overhead, then divides by the per-token KV cache cost:

```
Max Context Length = (Usable Memory − Model Weights per GPU − Runtime Overhead) / Adjusted Bytes per Token
```

The result is then:

1. **Clamped** to the model's architectural limit (`max_position_embeddings`) — the context length can never exceed what the model was designed for.
2. **Aligned down** to a 256-token boundary for vLLM memory allocation efficiency.

This final value is passed as `--max-model-len` when launching the vLLM server.

### User Override

If you set `max-model-len` explicitly in your inference ConfigMap, that value takes precedence in **node count estimation** — the estimator uses your specified context size to compute nodes, and the exact value is forwarded to vLLM. This is useful when you know the context length your workload requires and want KAITO to provision accordingly.

### The Relationship Between Node Count and Context Size

Node count and context size are two faces of the same memory budget:

- **More nodes** → model weights are distributed across more GPUs → more memory left per GPU for KV cache → **longer context possible**.
- **Longer context** → more KV cache per GPU → less room for model weights → **more nodes needed**.

KAITO resolves this tension by first determining the minimum node count (using a conservative context size), then maximizing the context window within the resulting memory envelope.

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
4. `overhead = fixedOverhead + 0.25 GiB`
5. `memForWeightsPerGPU = 67.2 − overhead ≈ 64.7 GiB`
6. `minGPUs = ⌊7.29 / 64.7⌋ + 1 = 1`
7. `minNodes = ⌈1 / 1⌉ = 1`

**Result: 1 node** (1 GPU) is sufficient — the model is small enough to fit easily.

### Step 2: Determine Context Size

Now compute the longest context window on that 1 GPU:

1. `modelWeightPerGPU = 7.29 GiB` (single GPU, no distribution)
2. `availablePerGPU = 67.2 GiB`
3. `remainingPerGPU = 67.2 − 7.29 − fixedOverhead ≈ 57.61 GiB`
4. `adjustedBytesPerToken = 128 KB` (single GPU)
5. `rawCandidate = 57.61 GiB / 128 KB ≈ 471941 tokens`
6. Clamp to model limit: `min(471941, 131072) = 131,072`
7. Align to 256: `131,072` (already aligned)

**Result: vLLM launches with `--max-model-len 131072`** — the full 128K context the model architecture supports.

### What If We Used a Smaller GPU?

On a `Standard_NV36ads_A10_v5` node (1× A10 24 GiB GPU):

1. `availablePerGPU = 24 × 0.84 ≈ 20.16 GiB`
2. `modelSize = 7.29 GiB` (actual SafeTensor size × 1.02)
3. `remainingPerGPU = 20.16 − 7.29 − fixedOverhead ≈ 10.57 GiB`
4. `rawCandidate = 10.57 GiB / 128 KB ≈ 86,589 tokens`
5. Clamp to model limit: `min(86589, 131072) = 86,589`
6. Align to 256: `86,528`

The model still fits on 1 node, but the context window drops to ~77K tokens — well below the model's 128K architectural limit. To get the full context, you'd need a GPU with more memory.

</details>
