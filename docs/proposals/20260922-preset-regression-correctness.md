---
title: Preset Regression Correctness Verification
authors:
  - "@zhehli688"
reviewers:
  - "@KAITO contributors"
creation-date: 2026-09-22
last-updated: 2026-09-28
status: provisional
---

# Preset Regression Correctness Verification

## Summary

Extend the preset model regression workflows from deployment validation into model correctness regression testing.

After each model reaches `WorkspaceSucceeded=True` and `BenchmarkCompleted=True`, the runner executes a bounded GSM8K evaluation against the deployed OpenAI-compatible endpoint and compares exact-match accuracy with a reviewed baseline for the same model, GPU SKU, node count, and evaluation profile.

Benchmark definitions and measured results remain separate:

- `benchmarks/gsm8k/config.yaml` stores benchmark identity, profiles, execution limits, and comparison policy.
- `benchmarks/gsm8k/baselines.yaml` stores reviewed correctness results by deployment target.

The existing startup benchmark remains part of normal Workspace readiness.

## Motivation

The existing preset workflows validate that a model can deploy and serve requests on supported GPU pools. They check:

- resource and inference readiness;
- successful completion of the startup benchmark;
- `/v1/models`; and
- `/v1/chat/completions`.

Those checks do not detect a model that returns HTTP 200 but produces wrong or empty output because of a parser, chat template, quantization kernel, dtype, runtime flag, or model implementation regression.

## Why GSM8K

GSM8K provides deterministic, self-scored answers without an external judge. A fixed 128-question subset with greedy decoding is bounded enough for a regression workflow while exercising multi-step generation, reasoning output, chat formatting, and final-answer extraction.

MMLU measures broader factual knowledge but exercises less of the generated-answer path. MT-Bench measures conversational quality but requires a judge model and introduces scoring variability. GSM8K is used here as a reproducible correctness canary, not as a comprehensive capability score.

## Repository layout

```text
benchmarks/
  gsm8k/
    README.md
    config.yaml
    baselines.yaml
```

`.github/preset-regression-config.json` continues to own infrastructure and target-matrix policy. Correctness identity, execution limits, and tolerances belong in `benchmarks/gsm8k/config.yaml`.

## Configuration

The config pins the dataset and evaluator and defines reusable generation profiles:

```yaml
schemaVersion: 1
benchmark:
  dataset: openai/gsm8k
  datasetRevision: <pinned-revision>
  split: test
  evaluator: lm-eval
  evaluatorVersion: 0.4.13
  task: gsm8k
  metric: exact_match,flexible-extract
  sampleSelection:
    strategy: first
    count: 128
  defaultProfile: chat-thinking-v1
profiles:
  chat-thinking-v1:
    applyChatTemplate: true
    fewshotAsMultiturn: true
    systemInstruction: null
    maxGenTokens: 8192
    temperature: 0
    chatTemplateKwargs:
      enable_thinking: true
execution:
  numConcurrent: 4
  requestTimeoutSeconds: 300
  timeoutSeconds: 1200
  maxRetries: 2
comparison:
  defaultMaxRegression: 0.05
  maxRegressionOverrides: {}
  requireBaselines: false
```

Model-specific profile and execution overrides are sparse. Profiles that require native request fields, such as Mistral reasoning effort, use `requestKwargs` rather than tokenizer-specific chat-template arguments.

## Baseline identity

Each baseline entry is keyed by the deployed target:

```yaml
schemaVersion: 1
targets:
  - model: Qwen/Qwen3.5-4B
    instanceType: Standard_NC24ads_A100_v4
    nodes: 1
    profile: chat-thinking-v1
    accuracy: 0.8359375
    correct: 107
    evaluated: 128
    emptyResponses: 18
    measuredAt: "2026-09-27"
```

The comparison identity is `(model, instanceType, nodes, profile)`. A topology or profile mismatch is not comparable and must be reported separately from a correctness regression.

## Evaluation behavior

The runner:

1. deploys one Workspace at a time;
2. waits for normal Workspace and startup-benchmark readiness;
3. verifies model discovery and chat completion endpoints;
4. port-forwards the Workspace Service;
5. runs the pinned GSM8K sample set through `lm-eval`; and
6. records raw evaluator output, failed samples, and a compact summary before teardown.

Request execution is bounded:

- HTTP 429, HTTP 5xx, and connection failures are retryable up to `maxRetries`.
- Individual request timeouts become empty responses and are scored incorrect.
- The suite deadline terminates the evaluation rather than being converted to a sample failure.
- Empty responses remain visible through `emptyResponses`; request timeouts remain visible through `requestTimeouts`.

## Comparison

A result passes when:

$$
\text{observed accuracy} \geq \text{baseline accuracy} - \text{allowed regression}
$$

The default tolerance is an absolute accuracy difference. Target-specific overrides require measured evidence in review.

Missing baselines are reported as `baseline-missing`. Topology or profile mismatches are reported as `baseline-config-mismatch`. Neither should be mislabeled as a model correctness regression.

## Baseline updates

Baseline promotion is explicit and reviewable. The CLI accepts per-model `gsm8k-summary.json` artifacts or aggregate `results-*.json` files, validates values, deduplicates deployment identities, preserves unaffected entries, and sorts the manifest.

Changing the dataset revision, evaluator version, sample selection, generation profile, request semantics, or model behavior requires recollecting affected baselines.

## Alternatives considered

- **HTTP-only smoke tests:** too weak; malformed or empty model output can still return HTTP 200.
- **MMLU:** broader knowledge coverage but less sensitive to generated-answer and reasoning-path regressions.
- **MT-Bench:** useful for deeper quality evaluation but too slow and variable for this gate.
