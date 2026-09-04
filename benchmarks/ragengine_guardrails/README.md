# RAGEngine Guardrails Benchmark

Microbenchmark suite for measuring the performance overhead, detection effectiveness, and correctness of RAGEngine output guardrails.

## Quick Start

```bash
# From repo root — install dependencies
pip install datasets httpx

# Microbenchmark: overhead vs output tokens (Figure 1 in the blog)
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.plot_overhead

# Core profiles benchmark (streaming + non-streaming)
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.benchmark \
    --iterations 100 --warmup 10 -v

# Scaling experiments (per-scanner cost, concurrency, holdback)
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_scaling
```

## Reproducing Figure 1

Figure 1 and its absolute timing results were generated with Python 3.12 on
WSL2 Linux 5.15 using an AMD Ryzen 9 8945HS CPU (8 cores, 16 threads) and
24 GB RAM. The benchmark uses output sizes from 1,024 to 16,384 tokens, a
20-character streaming chunk size, 10 warmup iterations, and 100 measured
iterations per configuration.

Run the Figure 1 benchmark from the repository root:

```bash
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.plot_overhead
```

The command writes the raw measurements to
`results/overhead_vs_tokens.json`. Regenerate the chart from those
measurements with:

```bash
python -m benchmarks.ragengine_guardrails.plot_figure
```

## E2E Benchmark with Real Models

Requires Azure AI Foundry serverless API endpoints.

```bash
# Step 1: Download public datasets
pip install datasets
python -m benchmarks.ragengine_guardrails.datasets.download_datasets

# Step 2: Curate benchmark prompts (380 prompts, 63% public datasets)
python -m benchmarks.ragengine_guardrails.datasets.curate_prompts

# Step 3: Record traces from real models (one-time API cost)
python -m benchmarks.ragengine_guardrails.bench_e2e record \
    --model-url <AI_FOUNDRY_ENDPOINT>/v1/chat/completions \
    --api-key <KEY> --model-name <DEPLOYMENT_NAME> \
    --prompts datasets/benchmark_prompts_v2.jsonl \
    --output traces/<model>/

# Step 4: Replay traces through guardrails (no API needed, free)
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_e2e replay \
    --traces traces/<model>/

# Step 5: Live E2E benchmark
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.bench_e2e live \
    --model-url <AI_FOUNDRY_ENDPOINT>/v1/chat/completions \
    --api-key <KEY> --model-name <DEPLOYMENT_NAME> \
    --label <LABEL> --prompts datasets/benchmark_prompts_v2.jsonl
```

## Benchmark Profiles

| Profile | Mode | Guardrails | Policy |
|---------|------|-----------|--------|
| NS0 | Non-streaming | Disabled | — |
| NS1 | Non-streaming | Enabled | redact_block.yaml |
| S0 | Streaming | Disabled | — |
| S1 | Streaming | Block-only | block_only.yaml |
| S2 | Streaming | Redact + Block | redact_block.yaml |

## Metrics

- **Overhead rate**: μs per output token (linear fit across 1K–16K tokens)
- **E2E latency**: P50/P95/P99 end-to-end with real models
- **TTFT delay**: Guardrail-induced first-visible-token delay (streaming)
- **Throughput**: Requests/sec
- **CPU time**: process_time_ns delta per request
- **Memory**: tracemalloc peak allocation per request
- **Detection**: Blocked count, redaction count, block reasons
- **Correctness**: Cross-chunk leakage (0 bytes target), redaction accuracy
- **Reload**: Policy hot-reload time vs scanner count

## Dataset

`benchmark_prompts_v2.jsonl` — 380 prompts, 63% from public datasets:

| Category | Count | Source |
|----------|-------|--------|
| pii | 100 | [ai4privacy/pii-masking-400k](https://huggingface.co/datasets/ai4privacy/pii-masking-400k) |
| toxicity | 80 | [Babelscape/ALERT](https://huggingface.co/datasets/Babelscape/ALERT) |
| refusal | 60 | [LibrAI/do-not-answer](https://huggingface.co/datasets/LibrAI/do-not-answer) |
| clean | 60 | custom |
| secrets | 40 | custom |
| json | 15 | custom |
| reading_time | 15 | custom |
| scanner-targeted | 10 | custom |

Regenerate with a fixed seed: `python -m benchmarks.ragengine_guardrails.datasets.curate_prompts`

## Directory Structure

```
benchmarks/ragengine_guardrails/
├── benchmark.py             # Core profiles microbenchmark
├── bench_scaling.py         # Scaling experiments
├── bench_e2e.py             # E2E with real models (record/replay/live/compare)
├── bench_integration.py     # HTTP round-trip integration benchmark
├── plot_overhead.py         # Overhead vs tokens microbenchmark + figure
├── plot_figure.py           # Generate overhead figure (PNG/SVG)
├── mock_server.py           # Deterministic OpenAI mock
├── workloads.py             # Response generators + cross-chunk corpus
├── metrics.py               # Timing + stats collection
├── validate.py              # Correctness assertions
├── resource_collector.py    # CPU/memory measurement
├── policies/                # Policy YAML files
├── cases/                   # Test case JSON files
├── datasets/                # Prompt datasets + download/curate scripts
│   ├── download_datasets.py # Download public datasets from HuggingFace
│   ├── curate_prompts.py    # Curate benchmark_prompts_v2.jsonl
│   └── *.jsonl              # Raw + curated prompt files
└── results/                 # Output directory
    ├── overhead_vs_tokens.png  # Figure 1 for the blog
    └── *.json               # Raw benchmark results
```
