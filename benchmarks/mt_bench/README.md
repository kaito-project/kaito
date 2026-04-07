# MT-Bench Evaluation for Kaito

Evaluate models deployed through [Kaito](https://github.com/kaito-project/kaito) using [MT-Bench](https://arxiv.org/abs/2306.05685), a multi-turn conversation benchmark that scores model quality on a 1-10 scale across 8 categories.

## How It Works

1. **Generate Answers** — Sends 80 multi-turn questions (2 turns each) to a Kaito workspace's `/v1/chat/completions` endpoint
2. **Judge with a Strong LLM** — An strong LLM model (e.g. GPT-5.4) scores each response (1-10 scale, single-answer grading)
3. **Aggregate** — Computes per-category averages (writing, roleplay, reasoning, math, coding, extraction, STEM, humanities) and an overall score

## Prerequisites

- A running Kaito workspace with inference ready
- A deployed LLM for judging
- A Kubernetes Secret with the judge API key

### Create the Judge API Secret

```bash
kubectl create secret generic judge-api-secret \
  --from-literal=api-key=YOUR_API_KEY \
  -n YOUR_NAMESPACE
```

## Quick Start

Edit `job.yaml` with your values, then:

```bash
kubectl apply -n my-namespace -f job.yaml
kubectl wait --for=condition=complete --timeout=60m job/mt-bench-eval -n my-namespace
kubectl logs job/mt-bench-eval -n my-namespace
```

## Configuration

| Environment Variable | Required | Default | Description |
|---|---|---|---|
| `KAITO_ENDPOINT` | Yes | — | Workspace inference endpoint URL |
| `MODEL_NAME` | Yes | — | Model name for `/v1/chat/completions` |
| `JUDGE_SOURCE` | Yes | — | Source of LLM (e.g. `MICROSOFT_AI_FOUNDRY`) |
| `JUDGE_ENDPOINT` | Yes | — | Judge LLM endpoint URL |
| `JUDGE_API_KEY` | Yes | — | Judge LLM API key (via K8s Secret) |
| `AZURE_OPENAI_DEPLOYMENT` | When `MICROSOFT_AI_FOUNDRY` | — | Azure deployment name (e.g., `gpt-5.4`) |
| `AZURE_OPENAI_API_VERSION` | When `MICROSOFT_AI_FOUNDRY` | — | Azure API version |
| `PARALLEL` | No | `2` | Concurrent judge API calls |
| `MAX_TOKENS` | No | `1024` | Max tokens for model responses |
| `TEMPERATURE` | No | `0.0` | Model response temperature |
| `DRY_RUN` | No | `false` | Skip judging (answer generation only) |
| `RESULTS_DIR` | No | `/results` | Output directory for detailed files |

## Output

### Structured log line (for automation)

```
KAITO_MTBENCH_RESULT 2026-04-06T10:30:45Z {"model_id":"llama-3.1-8b-instruct","overall_score":7.85,"turn1_score":8.12,"turn2_score":7.58,"categories":{"writing":8.5,"roleplay":7.9,"reasoning":7.2,"math":6.8,"coding":7.1,"extraction":8.9,"stem":7.5,"humanities":8.3},"num_questions":80,"num_scored":160,"judge_model":"gpt-4o"}
```

### Human-readable summary

```
============================================================
MT-Bench Results for: llama-3.1-8b-instruct
Judge: gpt-4o
============================================================
  Overall Score:  7.85
  Turn 1 Average: 8.12
  Turn 2 Average: 7.58
  Questions: 80 | Scored: 160
------------------------------------------------------------
  Category Scores:
    writing       8.50  ########
    roleplay      7.90  #######
    reasoning     7.20  #######
    math          6.80  ######
    coding        7.10  #######
    extraction    8.90  ########
    stem          7.50  #######
    humanities    8.30  ########
============================================================
```

### Detailed output files

Files are written to `RESULTS_DIR` (default `/results`):
- `mt_bench_answers.jsonl` — Model answers for all 80 questions
- `mt_bench_judgments.jsonl` — Judge scores and reasoning for each turn
- `mt_bench_report.json` — Aggregated results

Extract from a completed Job pod:
```bash
kubectl cp my-namespace/mt-bench-eval-xxxxx:/results ./mt_bench_results
```

## Building the Container Image

```bash
# From repo root — builds and pushes to $(REGISTRY) in one step
make docker-build-mt-bench
```

## Dry Run Mode

Generate answers without calling the judge:

```bash
export DRY_RUN=true
./run.sh my-workspace my-namespace --dry-run
```

## Data Source & Attribution

Benchmark data is sourced from HuggingFace repo [lmsys/mt-bench](https://huggingface.co/spaces/lmsys/mt-bench). We thank the authors for creating this benchmark.
