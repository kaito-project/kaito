# Phase 4 — Methodology

## Model Selection

We selected three models spanning small, medium, and large parameter tiers to measure guardrails overhead across different inference profiles:

| Tier | Model | Parameters | Why |
|------|-------|-----------|-----|
| Small | Phi-4-mini-instruct | 3.8B | Fast inference, low latency baseline; tests whether guardrails overhead is visible on quick-responding models |
| Medium | mistral-small-2503 | ~24B | Mid-range inference time; originally planned Phi-4 (14B) but it does not support Global Standard (serverless) deployment on Azure AI Foundry, so we substituted mistral-small-2503 |
| Large | Mistral-Large-3 | 123B | Slow inference, high latency baseline; tests whether guardrails overhead disappears into model inference time |

All three models were deployed as **Global Standard** (serverless, pay-per-token) on Azure AI Foundry. Azure AI Foundry requires all serverless deployments to have a content filtering configuration — there is no "None" option. We selected **DefaultV2**, the current default Azure AI Content Safety configuration, which filters at medium severity across four categories: hate and fairness, self-harm, sexual content, and violence. This means every experiment includes Azure Content Safety as a baseline layer, and we measure what KAITO guardrails add on top of it.

## Dataset Design

### Goals

1. Maximize use of **public datasets** for reproducibility — anyone can re-run the benchmark without proprietary data.
2. Use **custom prompts only** where no public dataset covers the scanner type.
3. Keep the dataset **small enough** to run in ~3 hours per model (380 prompts × 3 policies × ~10s/prompt), but large enough for stable P95/P99 statistics.

### Public Dataset Selection

We evaluated 12 candidate public datasets and selected three based on relevance to our scanner types:

| Dataset | Source | Full Size | What It Tests | Why Selected |
|---------|--------|----------|---------------|-------------|
| **ai4privacy/pii-masking-400k** | HuggingFace | 406,896 entries across 6 languages | 17 PII classes: email, phone, SSN, credit card, address, etc. | Direct match for our `sensitive` PII redaction scanner — the strongest public dataset for testing PII detection in model output |
| **Babelscape/ALERT** | HuggingFace | 14,763 red-team prompts | Toxicity across 6 categories: discrimination, violence, sexual content, self-harm, crime, substance abuse | Designed to elicit harmful model output — directly tests `ban_substrings` blocking and content filter behavior |
| **LibrAI/do-not-answer** | HuggingFace | 939 safety-boundary prompts | Questions models should refuse: misinformation, discrimination, privacy violations, harmful instructions | Tests the boundary between content that should be blocked vs allowed — validates both content filter and guardrails false-positive rate |

Datasets we considered but did not use:
- **WildGuardMix** (allenai/wildguardmix): Gated dataset requiring access approval — excluded for reproducibility.
- **BeaverTails**, **RealToxicityPrompts**, **ToxicChat**: Would provide additional toxicity coverage but overlap with ALERT; not worth the additional runtime cost.
- No public dataset exists for `secrets`, `json`, `invisible_text`, or `reading_time` scanner types.

### Sampling Strategy

We did not download the full datasets. For ai4privacy (400K entries), downloading the full dataset would be wasteful when we only need 100 samples. Instead, we used **streaming reservoir sampling**: the HuggingFace `datasets` library streams entries one at a time, and we apply reservoir sampling to select a uniform random subset without downloading the entire dataset to disk.

- **ai4privacy**: Streamed the full 400K dataset, filtered to English entries (67,887 candidates), reservoir-sampled 1,000 entries to disk, then sampled 100 for the benchmark. Each entry contains real PII patterns (synthetic, not real personal data) that we wrap in prompts asking the model to "rewrite" or "summarize" the text — this induces the model to reproduce the PII in its output, which the `sensitive` scanner should then redact.
- **ALERT**: Downloaded the full 14,763 test split (small enough), then randomly sampled 80 prompts.
- **Do-Not-Answer**: Downloaded all 939 entries (small), then randomly sampled 60 prompts.

### Sample Size Rationale

| Category | Count | Rationale |
|----------|-------|-----------|
| pii | 100 | `sensitive` scanner is the most expensive scanner (~10-50ms per request); higher sample count needed for stable latency distribution |
| toxicity | 80 | `ban_substrings` is cheap (~3ms); 80 is sufficient for stable P50/P95 |
| refusal | 60 | Primarily tests content filter behavior, not guardrails performance |
| clean | 60 | Validates false-positive rate — guardrails should not block or redact clean content |
| secrets | 40 | No public dataset; `secrets` scanner is very expensive (~1s+); 40 is enough to measure latency and detection |
| json | 15 | Simple scanner, low variance |
| reading_time | 15 | Simple scanner, low variance |
| ban_substrings / secrets_targeted | 10 | Edge cases and boundary conditions |

**Total: 380 prompts** — balances statistical stability against runtime cost (~3 hours per model, ~9 hours total for three models running in parallel).

### Final Dataset Composition

| Source | Count | Percentage |
|--------|-------|-----------|
| ai4privacy/pii-masking-400k (public) | 100 | 26% |
| Babelscape/ALERT (public) | 80 | 21% |
| LibrAI/do-not-answer (public) | 60 | 16% |
| Custom | 140 | 37% |
| **Total** | **380** | **63% public** |

All prompts are curated into `datasets/benchmark_prompts_v2.jsonl` using a fixed random seed (42) for reproducibility. The curate script (`datasets/curate_prompts.py`) can regenerate the exact same dataset from the downloaded sources.
