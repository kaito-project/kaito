# RAGEngine Guardrails Benchmark — Execution Plan

## Current Status

### Phase 1: Unit-Level & Scaling Benchmark (DONE)

- [x] Core profiles (NS0/NS1/S0/S1/S2), 100 iterations — `results/results.json`
- [x] Scaling: response length / chunk size / scanner count / concurrency / holdback — `results/scaling_results.json`
- [x] Per-scanner type cost (8 scanners isolated) — in `benchmark_report.md`
- [x] Integration benchmark (HTTP round-trip) — `results/integration_results.json`
- [x] Correctness validation: 12/12 PASS, 0 leakage bytes

### Phase 2: E2E with Real Models — Record & Replay (DONE)

- [x] Record traces: Phi-4-mini (225), Phi-4 (225), Mistral-Large-3 (225)
- [x] Replay through all scanners — `results/e2e_replay_all_scanners_v2.json`
- [x] Per-scanner performance with real model output
- [x] Detection effectiveness and PII redaction verification

### Phase 3: E2E Live Benchmark — Raw Baseline (DONE)

- [x] Phi-4-mini-instruct (95 prompts, content filter OFF) — `results/e2e_live_results_phi4mini-raw.json`
- [x] Phi-4 (95 prompts, content filter OFF) — `results/e2e_live_results_phi4-raw.json`
- [x] Mistral-Large-3 (95 prompts, content filter OFF) — `results/e2e_live_results_mistral-raw.json`

### Phase 4: Azure Content Safety Comparison (DONE)

Models deployed with DefaultV2 content filter:

| Tier | Model | Endpoint | Status |
|------|-------|----------|--------|
| Small | Phi-4-mini-instruct | `.../openai/v1/chat/completions` | Done |
| Medium | mistral-small-2503 | `.../openai/v1/chat/completions` | Done |
| Large | Mistral-Large-3 | `.../openai/v1/chat/completions` | Done |

Results:
- [x] Run `--label azure-cs-phi4mini` (content filter ON) — `results/e2e_live_results_azure-cs-phi4mini.json`
- [x] Run `--label azure-cs-mistral-small` (content filter ON) — `results/e2e_live_results_azure-cs-mistral-small.json`
- [x] Run `--label azure-cs-mistral-large` (content filter ON) — `results/e2e_live_results_azure-cs-mistral-large.json`
- [x] Results report — `results/phase4_results.md`
- [ ] Update benchmark_report.md with Phase 4 data

Note: Content filter cannot be toggled OFF on existing deployments (no "None" option in Foundry portal).
Experiment design changed to: Azure CS baseline vs Azure CS + KAITO guardrails (both with content filter ON).
Medium model changed from Phi-4 (no serverless support) to mistral-small-2503.
Dataset: benchmark_prompts_v2.jsonl (380 prompts, 63% public datasets).

---

## Phase 5: Dataset Expansion (DONE)

### Goal

Maximize use of public datasets for reproducibility; custom only where no public dataset exists.

### Original Plan vs Actual

Originally planned ~970 prompts, but reduced to 380 to keep runtime reasonable (~3 hours/model instead of ~8 hours/model). Statistical stability at P95/P99 is sufficient with 380 prompts.

### Final Dataset (benchmark_prompts_v2.jsonl)

| Scanner | Dataset | Source | Count |
|---------|---------|--------|-------|
| `sensitive` / PII | ai4privacy/pii-masking-400k | HuggingFace (public) | 100 |
| `ban_substrings` / toxicity | Babelscape/ALERT | HuggingFace (public) | 80 |
| refusal | LibrAI/do-not-answer | HuggingFace (public) | 60 |
| clean (false positive) | custom | — | 60 |
| `secrets` | custom | — | 40 |
| `json` | custom | — | 15 |
| `reading_time` | custom | — | 15 |
| scanner-targeted | custom | — | 10 |
| **Total** | | | **380** |

Public: 240/380 (63%). Custom: 140/380 (37%).

WildGuardMix (allenai/wildguardmix) was excluded — gated dataset requiring access approval, not suitable for reproducibility.

### Sampling Method

- ai4privacy: streaming reservoir sampling (1,000 English entries from 67,887 candidates, then sampled 100)
- ALERT: random sample from 14,763 test split
- Do-Not-Answer: random sample from 939 entries
- Fixed random seed (42) for reproducibility

---

## Phase 6: Blog & Report Finalization (PLANNED)

- [ ] Update benchmark_report.md with Phase 4 (content filter comparison) data
- [ ] Update benchmark_report.md with Phase 5 (expanded dataset) results
- [ ] Finalize engineering blog (docs/aks-engineering-blog-ragengine-guardrails.md)
- [ ] Clean up repo root temporary files (stream-*.txt, vllm-*.txt, etc.)
