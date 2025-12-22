# Model SKU Requirements Calculation Guide

## Overview

This document provides detailed guidance on how to calculate the required SKU count and final max_model_length based on a given model and SKU requirements.

**Important Prerequisites:**
1. All experimental data is measured based on vLLM v0.10.1
2. In practice, VRAM calculations are per GPU basis

## Table of Contents

- [Model Configuration Analysis](#model-configuration-analysis)
- [Node Calculation](#node-calculation)
- [Final Max Token Length Calculation](#final-max-token-length-calculation)

## Model Configuration Analysis

### GPU Memory Requirements

In vLLM inference, vRAM consumption consists of four main components:

1. **Model Parameter Space** (Model Weight)
2. **Key-Value Cache During Inference** (KV-Cache)
3. **Non-PyTorch Memory Usage**
4. **Activations**

This can be expressed mathematically as (per GPU):

```
VRAM₍total_per_gpu₎ = VRAM₍model_per_gpu₎ + VRAM₍kv_cache₎ + VRAM₍non_pytorch₎ + VRAM₍activations₎
```

Where:
- `VRAM₍total_per_gpu₎`: Total GPU memory consumption per GPU
- `VRAM₍model_per_gpu₎`: Memory required for model parameters per GPU
- `VRAM₍kv_cache₎`: Memory allocated for key-value cache during inference
- `VRAM₍non_pytorch₎`: Non-PyTorch memory overhead (maximum observed: 0.6 GiB)
- `VRAM₍activations₎`: Intermediate computation memory (maximum observed: 1.7 GiB)

#### Model Parameter Space

The model weight size can be calculated using the model configuration methods implemented in the [`preset_generator.py`](./preset_generator.py) file. This model weight size corresponds to the total size of all safetensors files from the model's Hugging Face repository. This represents the static memory footprint required to load the model parameters into GPU memory. In experimental observations, the actual VRAM usage is approximately 102% of the model weight size.

**Multi-GPU Distribution:**
When using multiple GPUs with tensor parallelism, the model weight is evenly distributed across all GPUs. Therefore, each GPU receives:

```
VRAM₍model_per_gpu₎ = VRAM₍total_model_size₎ / tensor_parallel_size
```

*Note: Falcon-related models do not support multi-GPU deployment.*

#### Activations

Through extensive experimental analysis, the maximum activations memory consumption has been determined to be **1.7 GiB per GPU**. This represents the memory required for intermediate computations during inference, and each GPU requires this amount regardless of the tensor parallelism configuration.

#### Non-PyTorch Memory Usage

Based on comprehensive testing and profiling, the maximum Non-PyTorch memory usage has been measured at **0.6 GiB per GPU**. This includes memory overhead from CUDA contexts, driver allocations, and other system-level memory requirements that are not directly managed by PyTorch, and each GPU requires this amount regardless of the tensor parallelism configuration.

#### KV-Cache

The KV-Cache VRAM consumption can be calculated using the following formula:

```
VRAM₍kv_cache_per_gpu₎ = (max_token_length × Bytes_per_token) / tensor_parallel_size
```

Where:
```
Bytes_per_token = 2 × hidden_layers × kv_heads × head_dim × dtype_size
```

The `max_token_length` can be configured in the configMap. If not specified, the default value is 2048.

The `Bytes_per_token` can also be calculated using the [`preset_generator.py`](./preset_generator.py) file.

The `tensor_parallel_size` is the number of effective GPU nodes.

Where these parameters are obtained from the model's `config.json` file on Hugging Face:

- `hidden_layers`: Number of hidden layers (`num_hidden_layers` in config.json)
- `kv_heads`: Number of key-value attention heads
- `head_dim`: Dimension per attention head, calculated as `hidden_size / num_attention_heads`
- `dtype_size`: Data type size in bytes (see table below)

**Common PyTorch Data Types:**

| Data Type | Size (bytes) |
|-----------|--------------|
| float32   | 4            |
| float16   | 2            |
| bfloat16  | 2            |
| float64   | 8            |

**KV-Heads Determination:**
- If `num_key_value_heads` exists in config.json, use that value
- If `multi_query` is `true`, then `kv_heads = 1`
- Otherwise, `kv_heads = num_attention_heads`

## Node Calculation

The final GPU count required can be calculated using the following formula:

```
n × VRAM₍per_gpu₎ × utilization_rate = VRAM₍model_size_total₎ × 1.02 + 1.7×n + 0.6×n + Bytes_per_token × max_token_length
```

Where:
- `n`: Number of GPUs required
- `VRAM_per_gpu`: GPU memory capacity per GPU
- `utilization_rate`: GPU memory utilization rate (typically 0.85-0.95)
- `VRAM_model_size_total`: Total model weight size
- `1.7×n`: Total activations memory (1.7 GiB per GPU)
- `0.6×n`: Total non-PyTorch memory (0.6 GiB per GPU)
- `Bytes_per_token × max_token_length`: Total KV-Cache memory

**Node Count Calculation:**
The required number of nodes can be calculated as:

```
Required_nodes = ⌈n / GPUs_per_node⌉
```

Where `⌈⌉` represents the ceiling function, and `GPUs_per_node` is the number of GPUs available per node in the selected SKU.

## Final Max Token Length Calculation

To calculate the maximum token length that can be supported with the available resources, we can rearrange the node calculation formula:

```
Node_count × GPUs_per_node × VRAM₍per_gpu₎ × utilization_rate = VRAM₍model_size_total₎ × 1.02 + 1.7×Node_count × GPUs_per_node + 0.6×Node_count × GPUs_per_node + Bytes_per_token × max_token_length
```

Solving for `max_token_length`:

```
max_token_length = (Node_count × GPUs_per_node × VRAM₍per_gpu₎ × utilization_rate - VRAM₍model_size_total₎ × 1.02 - 1.7×Node_count × GPUs_per_node - 0.6×Node_count × GPUs_per_node) / Bytes_per_token
```

**Model Constraint:**
The calculated `max_token_length` must be compared with the model's `max_position_embeddings` value from `config.json`. The final maximum token length is:

```
final_max_token_length = min(calculated_max_token_length, max_position_embeddings)
```

This ensures that the token length does not exceed the model's architectural limitations.

### Understanding max_position_embeddings

The `max_position_embeddings` parameter is a critical architectural constraint that defines the maximum sequence length a model can handle during training and inference.

**What is max_position_embeddings:**
- **Definition**: The maximum number of tokens that the model's positional encoding can handle
- **Source**: Found in the model's `config.json` file in the Hugging Face repository
- **Purpose**: Defines the upper limit for input sequence length during model training

**Why this constraint matters:**
1. **Positional Encoding Limitation**: Transformer models use positional encodings to understand token positions in a sequence. The model is only trained with positions up to `max_position_embeddings`
2. **Training Context**: Models are trained with specific maximum sequence lengths, and exceeding this limit can lead to:
   - Degraded model performance
   - Unexpected behavior or errors
   - Potential inference failures

**Common max_position_embeddings values:**
- **2048**: Traditional limit for many early transformer models (GPT-2, early LLaMA variants)
- **4096**: Common in modern models (LLaMA-2, some Mistral variants)
- **8192**: Extended context models (some Code Llama variants)
- **32768**: Long context models (LLaMA-2-32k, some specialized variants)
- **131072**: Very long context models (specialized research models)

**Example from config.json:**
```json
{
  "architectures": ["LlamaForCausalLM"],
  "hidden_size": 4096,
  "num_attention_heads": 32,
  "num_hidden_layers": 32,
  "max_position_embeddings": 4096,
  ...
}
```

**Practical implications:**
- If your GPU memory allows for `max_token_length = 8192` but the model's `max_position_embeddings = 4096`, the effective limit is 4096
- Users can configure higher values in their ConfigMap, but the system will automatically cap it at the model's architectural limit
- This prevents runtime errors and ensures optimal model performance

**KAITO Implementation:**
In KAITO's estimator logic, this constraint is enforced by:
1. Reading the model's `max_position_embeddings` from the model metadata
2. Calculating the maximum possible token length based on available GPU memory
3. Taking the minimum of these two values as the final limit
4. Using this final value for both node estimation and runtime configuration

