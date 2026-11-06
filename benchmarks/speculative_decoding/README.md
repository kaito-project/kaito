# Speculative decoding benefit under load

`specdec_bench.py` measures how much KAITO's preset-driven speculative decoding (the `kaito.sh/enable-speculative-decoding` annotation) helps a preset at different load levels. Speculative decoding usually helps most at low concurrency and less as batches fill the GPU, so the tool reports the benefit per concurrency level:

- **Throughput gain**: speculative ÷ baseline output tokens per second.
- **Decode speedup**: baseline ÷ speculative mean time per output token (TPOT), which is how much faster each user sees tokens stream.

## How it works

1. **Deploy.** Creates a minimal Workspace for the preset: just the preset, label selector and optional instance type. Model weights are downloaded from Hugging Face.
2. **Benchmark each variant.** For the annotation set to `"false"`, then `"true"`:
   1. Waits until the serving pods run that variant, and records the `--speculative-config` KAITO injected (`mtp`, `dspark`, or the `ngram` fallback).
   2. Starts a client pod on the server's node with the server's image. It requests no GPU.
   3. Runs an unmeasured warm-up at the highest concurrency, so every engine replica pays one-time compilation costs before measurement.
   4. Runs `vllm bench serve` once per dataset and concurrency level, with `max(16, 4 × concurrency)` requests.
3. **Report.** Writes the comparison table and plot, then deletes the Workspace and client pod.

`vllm bench serve` runs with its defaults wherever possible. It takes the model name and tokenizer from the server's `/v1/models`, and uses its own output length and sampling. Each annotation change rolls the serving pods, so expect one model reload per variant.

## Requirements

- `kubectl` access to a cluster running KAITO, and Python 3.11+. Install `matplotlib` to also get the plot.
- GPU capacity for the preset. On clusters without node auto-provisioning, pass `--label-selector` to match a node, and make sure the Workspace's storage can bind.
- Internet access from the cluster to Hugging Face, for the model weights, the tokenizer and the datasets.

## Datasets

Acceptance, and so the benefit, depends on the traffic, so pick datasets close to yours with `--datasets`, a comma-separated list:

| Name | Hugging Face dataset | Traffic |
| --- | --- | --- |
| `mtbench` (default) | [`philschmid/mt-bench`](https://huggingface.co/datasets/philschmid/mt-bench) | Multi-turn chat across 8 categories |
| `instructcoder` | [`likaixin/InstructCoder`](https://huggingface.co/datasets/likaixin/InstructCoder) | Code editing; usually more draft-friendly |

## Usage

```bash
python3 benchmarks/speculative_decoding/specdec_bench.py \
  --preset deepseek-ai/DeepSeek-V4.1-Flash-1017 \
  --instance-type Standard_ND96isr_H100_v5 \
  --label-selector kubernetes.io/hostname=my-gpu-node \
  --datasets mtbench,instructcoder \
  --concurrency 1,4,16,64,256
```

Run `--help` for the remaining options (`--context`, `--namespace`, `--workspace`, `--timeout`, `--output-dir`).

## Output

`--output-dir` (default `specdec-results`) contains:

- `REPORT.md`: one table per dataset with throughput gain, decode speedup, median TTFT and draft acceptance length per concurrency level.
- `speedup.png` (with `matplotlib`): one row of three panels per dataset.
  - Aggregate throughput vs concurrency, with the throughput gain on a second axis.
  - Per-user decode speed (1000 / TPOT) vs concurrency.
  - Throughput vs interactivity, each point labeled with its concurrency.
- `summary.json`: the same comparison in machine-readable form.
- `<variant>/<dataset>/c<N>.json` and `.log`: raw `vllm bench serve` results and logs, plus `<variant>/warmup.log`.

## Tests

```bash
pytest benchmarks/speculative_decoding
```
