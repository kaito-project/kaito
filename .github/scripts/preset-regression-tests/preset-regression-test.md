# Preset regression tests

Preset regression tests deploy catalog models on representative GPU SKUs and check two independent contracts:

- **Correctness:** bounded GSM8K accuracy through the model's OpenAI-compatible endpoint.
- **Performance:** GuideLLM startup-benchmark TPM, TTFT, and TPOT recorded in Workspace status.

The standard workflow is `.github/workflows/preset-model-regression.yaml`. Models larger than the standard-pool limit run through `.github/workflows/preset-model-regression-8xh100.yaml`.

## How it works

1. `preset-regression-matrix.sh` joins `presets/workspace/models/model_catalog.yaml` with `.github/preset-regression-config.json`. It selects models by profile and GPU pool and maps the requested GPUs per node to an instance type.
2. `preset-regression-run.sh` processes targets serially so one model cannot consume another model's GPU allocation.
3. The runner creates a Workspace and waits for `ResourceReady`, `InferenceReady`, `WorkspaceSucceeded`, and `BenchmarkCompleted`.
4. The inference container's startup probe runs GuideLLM. The runner reads its metrics from Workspace status; it does not launch a second performance test.
5. The runner verifies `/v1/models` and `/v1/chat/completions`, then runs the bounded GSM8K evaluator through a local port-forward.
6. Correctness and performance are compared with the reviewed baselines. Per-model summaries and an aggregate `results-*.json` file are written before the Workspace is removed.

The baseline identity is `(model, instanceType, nodes)`. Its recorded profile must also match the currently resolved profile.

## Configuration

Shared matrix and infrastructure policy lives in `.github/preset-regression-config.json`.

Correctness configuration is in `benchmarks/gsm8k/config.yaml`. It controls the dataset revision, evaluator version, sample selection, generation profiles, concurrency, retries, timeouts, and allowed accuracy regression. Model-specific execution and generation overrides are resolved before evaluation.

Performance configuration is in `benchmarks/guidellm/config.yaml`. The current profile uses a 20-second native GuideLLM warmup followed by a 60-second measurement window. Changing warmup, duration, token counts, or profile description makes existing Workspace metrics incompatible with that profile.

## Run locally

Prerequisites:

- A reachable Kubernetes cluster with KAITO and the configured GPU provisioner installed.
- `kubectl`, `jq`, `yq`, and Python 3.12 or later.
- Permission to create and delete Workspaces and to port-forward their Services.
- Sufficient GPU quota for the selected pool.

Preview the resolved matrix:

```bash
GPU=a100 REGRESSION_PROFILE=standard \
  bash .github/scripts/preset-regression-tests/preset-regression-matrix.sh | jq .
```

Run one model and retain its successful artifacts:

```bash
KUBECONFIG=/path/to/kubeconfig \
GPU=a100 \
REGRESSION_PROFILE=standard \
MODEL_FILTER='Qwen/Qwen3.5-4B' \
KEEP_SUCCESS_ARTIFACTS=true \
RESULTS_FILE=/tmp/preset-regression/results-a100.json \
ARTIFACT_DIR=/tmp/preset-regression/artifacts/a100 \
bash .github/scripts/preset-regression-tests/preset-regression-run.sh
```

`MODEL_FILTER` accepts comma-separated substrings. Important optional controls include `GLOBAL_DEADLINE_EPOCH`, `RESOURCE_READY_TIMEOUT_MINUTES`, `ENDPOINT_TIMEOUT_SECONDS`, `NAMESPACE`, and `BYO_NODE_LABEL`.

The runner deletes each Workspace after its result is recorded. On persistent clusters, an Azure `CanNotDelete` lock can prevent the provisioner from deleting an agent pool. Follow the cluster owner's approved lock procedure and verify that NodeClaims and GPU nodes are gone after a local run.

## Interpret results

The aggregate results file contains one entry per target:

- `passed`: deployment, endpoints, correctness, and performance checks passed.
- `failed`: at least one required stage failed. Read `reason`, then inspect the referenced artifact directory.
- `skipped`: the target was excluded by policy, lacked an instance mapping, or the global run budget expired.
- `actualNodes`: estimator result observed in Workspace status.
- `correctness`: GSM8K comparison summary.
- `performance`: GuideLLM comparison summary.

### GSM8K

The evaluator runs the configured fixed sample set. `accuracy`, `correct`, and `evaluated` describe the result. Empty responses and requests that exceed `requestTimeoutSeconds` are scored as incorrect samples; `emptyResponses` and `requestTimeouts` expose those failure modes. Retryable HTTP and connection failures remain bounded by the configured retry and suite deadlines.

A correctness run passes when:

$$
\text{observed accuracy} \geq \text{baseline accuracy} - \text{allowed regression}
$$

### GuideLLM

Workspace status contains:

- `peakTokensPerMinute`: total-token throughput. The historical field name says "peak"; the current implementation is GuideLLM's mean active-window token rate converted to tokens per minute.
- `averageTimeToFirstToken`: average TTFT in milliseconds.
- `averageTimePerOutputToken`: average TPOT in milliseconds.

GuideLLM excludes the native warmup phase from these metrics. The comparator also verifies `warmupSec`, `durationSec`, `inputTokens`, `outputTokens`, and the profile description before comparing values.

For a higher-is-better metric such as TPM, the minimum accepted value is:

$$
\text{minimum} = \text{baseline} \times (1 - \text{regression ratio})
$$

For lower-is-better latency metrics, the maximum is:

$$
\text{maximum} = \text{baseline} \times (1 + \text{regression ratio})
$$

`baseline-missing` passes only when `requireBaselines` is false. `baseline-config-mismatch` means the topology or profile is not comparable and must not be interpreted as a performance regression.

## Update baselines

Update baselines only after reviewing successful artifacts and confirming that the environment and runtime represent the intended release. Recollect affected performance baselines after changing GuideLLM, warmup, duration, token sizes, metric calculations, model runtime, or GPU topology. Recollect affected correctness baselines after changing the dataset revision, evaluator, sample selection, prompt/generation profile, or model behavior.

The promotion CLI accepts artifact directories, individual `gsm8k-summary.json` or `guidellm-summary.json` files, and aggregate results JSON files. Baseline paths default to the repository manifests.

Preview all candidates without modifying files:

```bash
python3 .github/scripts/preset-regression-tests/preset_regression_test_cli.py \
  --artifacts /tmp/preset-regression/artifacts/a10 \
  --artifacts /tmp/preset-regression/results-a100.json \
  --dry-run
```

Promote only performance baselines:

```bash
python3 .github/scripts/preset-regression-tests/preset_regression_test_cli.py \
  --artifacts /tmp/preset-regression/artifacts/a100 \
  --suite guidellm
```

When a deliberate benchmark or runtime change makes the old performance baselines
incomparable, review the complete run and explicitly include its regressed
measurements:

```bash
python3 .github/scripts/preset-regression-tests/preset_regression_test_cli.py \
  --artifacts /path/to/preset-regression-a10 \
  --artifacts /path/to/preset-regression-a100 \
  --artifacts /path/to/preset-regression-h100 \
  --artifacts /path/to/preset-regression-8xh100 \
  --suite guidellm \
  --include-regressions \
  --dry-run
```

Remove `--dry-run` only after confirming the candidate count and values. Without
`--include-regressions`, performance-regressed measurements are never promoted.

Promote both suites from downloaded workflow artifacts:

```bash
python3 .github/scripts/preset-regression-tests/preset_regression_test_cli.py \
  --artifacts /path/to/preset-regression-a10 \
  --artifacts /path/to/preset-regression-a100 \
  --artifacts /path/to/preset-regression-h100
```

The CLI ignores failed summaries by default, validates metric ranges, deduplicates by deployment identity, preserves unaffected targets, and sorts the resulting manifests. Always inspect `git diff -- benchmarks/gsm8k/baselines.yaml benchmarks/guidellm/baselines.yaml` before committing.

## Troubleshooting

Start with the aggregate entry's `reason`. Each retained model artifact directory may contain:

- `workspace-applied.yaml`
- `workspace.json` and condition snapshots
- pod descriptions and current or previous container logs
- `gsm8k-summary.json` and evaluator logs
- `guidellm-summary.json`

Common interpretations:

- `ResourceReady=False`: inspect NodeClaims and GPU provisioner events for quota, capacity, or lock failures.
- `InferenceReady=False`: inspect model download, CUDA, vLLM, and container restart logs.
- `BenchmarkFailed`: inspect `KAITO_BENCHMARK_CONFIG` and `KAITO_BENCHMARK_RESULT` in inference logs.
- `baseline-config-mismatch`: regenerate the baseline for the observed node count and active profile rather than weakening validation.
- `performance-regressed`: the failure names every regressed metric and reports its observed value, allowed limit, baseline, and percentage beyond the limit. Rerun on equivalent hardware before concluding that the runtime regressed.
