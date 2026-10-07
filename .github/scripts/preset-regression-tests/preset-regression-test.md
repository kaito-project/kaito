# Preset regression tests

Preset regression tests deploy catalog models on representative GPU SKUs and check bounded GSM8K accuracy through each model's OpenAI-compatible endpoint.

The standard workflow is `.github/workflows/preset-model-regression.yaml`. Models larger than the standard-pool limit run through `.github/workflows/preset-model-regression-8xh100.yaml`.

## How it works

1. `preset-regression-matrix.sh` joins `presets/workspace/models/model_catalog.yaml` with `.github/preset-regression-config.json`. It selects models by profile and GPU pool, estimates the node count for each allowed `gpusPerNodeOptions` shape, and chooses the shape requiring the fewest nodes. Ties use the smaller shape.
2. `preset-regression-run.sh` processes targets serially so one model cannot consume another model's GPU allocation.
3. The runner creates a Workspace and waits for `ResourceReady`, `InferenceReady`, `WorkspaceSucceeded`, and `BenchmarkCompleted`.
4. The existing startup benchmark must complete as part of normal Workspace readiness, but its values are not compared with regression baselines.
5. The runner verifies `/v1/models` and `/v1/chat/completions`, then runs the bounded GSM8K evaluator through a local port-forward.
6. Correctness is compared with the reviewed baseline. Per-model summaries and an aggregate `results-*.json` file are written before the Workspace is removed.

The baseline identity is `(model, instanceType, nodes)`. Its recorded profile must also match the currently resolved profile.

## Configuration

Shared matrix and infrastructure policy lives in `.github/preset-regression-config.json`.

Correctness configuration is in `benchmarks/gsm8k/config.yaml`. It controls the dataset revision, evaluator version, sample selection, generation profiles, concurrency, retries, timeouts, and allowed accuracy regression. Model-specific execution and generation overrides are resolved before evaluation.

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

- `passed`: deployment, endpoints, and correctness checks passed.
- `failed`: at least one required stage failed. Read `reason`, then inspect the referenced artifact directory.
- `skipped`: the target was excluded by policy, lacked an instance mapping, or the global run budget expired.
- `actualNodes`: estimator result observed in Workspace status.
- `correctness`: GSM8K comparison summary.

### GSM8K

The evaluator runs the configured fixed sample set. `accuracy`, `correct`, and `evaluated` describe the result. Empty responses and requests that exceed `requestTimeoutSeconds` are scored as incorrect samples; `emptyResponses` and `requestTimeouts` expose those failure modes. Retryable HTTP and connection failures remain bounded by the configured retry and suite deadlines.

A correctness run passes when:

$$
\text{observed accuracy} \geq \text{baseline accuracy} - \text{allowed regression}
$$

## Update baselines

Update baselines only after reviewing successful artifacts and confirming that the environment and runtime represent the intended release. Recollect affected correctness baselines after changing the dataset revision, evaluator, sample selection, prompt/generation profile, or model behavior.

The promotion CLI accepts artifact directories, individual `gsm8k-summary.json` files, and aggregate results JSON files. The baseline path defaults to `benchmarks/gsm8k/baselines.yaml`.

Preview all candidates without modifying files:

```bash
python3 .github/scripts/preset-regression-tests/update_preset_regression_baselines.py \
  --artifacts /tmp/preset-regression/artifacts/a10 \
  --artifacts /tmp/preset-regression/results-a100.json \
  --dry-run
```

Promote reviewed correctness results from downloaded workflow artifacts:

```bash
python3 .github/scripts/preset-regression-tests/update_preset_regression_baselines.py \
  --artifacts /path/to/preset-regression-a10 \
  --artifacts /path/to/preset-regression-a100 \
  --artifacts /path/to/preset-regression-h100
```

The CLI ignores failed summaries, validates metric ranges, deduplicates by deployment identity, preserves unaffected targets, and sorts the resulting manifest. Always inspect `git diff -- benchmarks/gsm8k/baselines.yaml` before committing.

## Troubleshooting

Start with the aggregate entry's `reason`. Each retained model artifact directory may contain:

- `workspace-applied.yaml`
- `workspace.json` and condition snapshots
- pod descriptions and current or previous container logs
- `gsm8k-summary.json` and evaluator logs

Common interpretations:

- `ResourceReady=False`: inspect NodeClaims and GPU provisioner events for quota, capacity, or lock failures.
- `InferenceReady=False`: inspect model download, CUDA, vLLM, and container restart logs.
- `BenchmarkFailed`: inspect `KAITO_BENCHMARK_CONFIG` and `KAITO_BENCHMARK_RESULT` in inference logs.
- `baseline-config-mismatch`: regenerate the baseline for the observed node count and active profile rather than weakening validation.
