---
title: Preset-Driven Speculative Decoding Toggle
authors:
  - "@andyzhangx"
reviewers:
  - "@Fei-Guo"
  - "@zhuangqh"
creation-date: 2026-08-27
last-updated: 2026-08-27
status: provisional
see-also:
  - "https://github.com/kaito-project/kaito/issues/2286"
---
# Preset-Driven Speculative Decoding Toggle

## Summary

This proposal introduces a preset-driven speculative decoding toggle for KAITO. Users enable speculative decoding with a single annotation (`kaito.sh/enable-speculative-decoding: "true"`) on a Workspace, InferenceSet, or MultiRoleInference. The per-preset configuration (method, parameters) is maintained by KAITO preset maintainers in the model catalog, not exposed as a user knob. The preset controller injects the appropriate vLLM `--speculative-config` flag automatically, and the admission webhook rejects unsupported preset + annotation combinations at `kubectl apply` time. When the annotation is set on an InferenceSet or MultiRoleInference, it propagates down the resource chain (`MRI → InferenceSet → Workspace`) so a single opt-in reaches every generated pod without any change to the runtime injection code path.

## Motivation

LLM decoding is fundamentally token-by-token: each step produces one token and the GPU is heavily under-utilized. **Speculative decoding** is a **theoretically lossless** speedup:

1. **Draft** - cheaply guess the next N tokens (small model / n-gram lookup / MTP head bundled in the checkpoint).
2. **Verify** - run the target model **once**, in parallel, over those N candidates.
3. Accept the matching prefix, resample at the first mismatch.

Net: multiple tokens per GPU forward pass; end-to-end tok/s goes up. The output distribution is intended to match normal decoding, but as [vLLM's own docs note](https://docs.vllm.ai/en/latest/features/spec_decode.html) the guarantee is only up to hardware/precision effects - batching and numerical differences can shift log probabilities and, in edge cases, individual token choices. In practice the outputs are indistinguishable for user-facing chat/agent workloads.

Today, enabling speculative decoding in KAITO requires users to write a ConfigMap with raw vLLM JSON passthrough - they must know the vLLM speculative decoding API, method taxonomy, and parameter tuning. Every misspelled field, wrong method, or wrong parameter causes pod startup failure or silent inference degradation.

### Evidence

- DeepSeek DSpark paper (arXiv:2607.05147): **60-85% faster per-user generation** vs the prior MTP baseline.
- vLLM's MTP benchmark on DeepSeek-R1 (vllm-project/vllm#12755): **1.63×-1.69× speedup at QPS = 1**, 1.18×-1.32× at QPS = 2-4, and regressing below 1× at QPS ≥ 6 on TP=8. The benchmark uses MTP depth = 1 (see "Default value" note below).
- `deepseek-v3-0324` and `deepseek-r1-0528` (existing KAITO presets) already support `mtp` at zero extra memory / download cost.

### Goals

- Define a one-annotation toggle (`kaito.sh/enable-speculative-decoding: "true"`) for users to enable speculative decoding on supported presets
- Add a typed `SpeculativeDecoding` field to `model.PresetParam`, populated from a KAITO-authored `speculativeDecodingByPreset` Go map in `presets/workspace/generator/generator.go` (colocated with the existing per-preset maintainer maps such as `reasoningParserModeNamePrefixMap`), propagated through `Generator` / `vLLMCompatibleModel` so both the controller and the admission webhook can read it. `model_catalog.yaml` is intentionally not extended: it mirrors HuggingFace `config.json` and is not the right home for a KAITO-authored operational knob. `supported_models.yaml` is also not extended (it is being deprecated - only the `base` image entry remains actively used).
- Preset controller reads the field **off the already-resolved `model.Model`** - via `GetInferenceParameters()` on the model object the controller already resolved through `models.GetModelByName` (which calls `GetModelByNameWithToken` internally) - and injects the vLLM `--speculative-config` flag automatically. The controller must not re-look up the preset by its short name at reconcile time: `GetModelByNameWithToken` rewrites short aliases like `deepseek-r1-0528` to `deepseek-ai/deepseek-r1-0528` before registering them (`presets/workspace/models/vllm_model.go`), so a direct `plugin.KaitoModelRegister.MustGet` keyed on the raw annotation'd preset name is not guaranteed to hit.
- Admission webhook rejects unsupported preset + annotation combinations at `kubectl apply` time - using `models.GetModelByName(ws.Inference.Preset.Name, accessSecret)` which performs the same alias rewrite and lazy model generation as the controller; the webhook then reads `GetInferenceParameters().SpeculativeDecoding` from the resolved model.
- Initial preset coverage: `mtp` on the two DeepSeek presets that are catalog-present today and vLLM-verified against KAITO's pinned vLLM at merge time - `deepseek-r1-0528` and `deepseek-v3-0324`. `deepseek-v3.2` is a Free-to-onboard-next candidate (same `mtp` path, no new type work) but is not in the initial ship list. DeepSeek-V4-Flash (the dated releases `DeepSeek-V4-Flash-0731` and `DeepSeek-V4-Flash-DSpark`) lands in **Ready to Onboard (`dspark`, fused)** below: the vLLM V4-Flash recipe classifies both as fused served-checkpoint DSpark, not assistant-checkpoint - see the corrected DeepSeek-V4 section for details. DeepSeek-V4-Pro-0813 is **deferred**, not fused-ready - its fused-vs-assistant shape is not pinned in an upstream vLLM recipe yet, so it stays out of the initial `dspark`-fused claim until the recipe lands (see "Deferred (V4-Pro, pending recipe)" in Model Coverage).
- Support InferenceSet via `spec.template.metadata.annotations` propagation
- Support MultiRoleInference via `metadata.annotations` propagation (annotation is cloned onto each child InferenceSet's `Spec.Template.Annotations` by the MRI controller, then onto each child Workspace by the InferenceSet controller)

### Non-Goals/Future Work

- EAGLE / Medusa separate-draft-model methods (need checkpoint sourcing design)
- Typed override field on `InferenceSpec` for power users to tune speculative decoding parameters
- Custom scheduling algorithms for speculative decoding
- Automatically enabling speculative decoding by default

## Proposal

### Background - Speculative Decoding Methods

vLLM 0.10 collapsed the older `--speculative-model` / `--num-speculative-tokens` CLI flags into a single JSON blob passed to `--speculative-config`:

```bash
# MTP - DeepSeek-R1, no extra download, no extra memory
vllm serve deepseek-ai/DeepSeek-R1 \
  --speculative-config '{"method":"mtp","num_speculative_tokens":3}'

# ngram - zero-cost across any preset that opts in
--speculative-config '{"method":"ngram","num_speculative_tokens":5,"prompt_lookup_max":4}'
```

#### The Four Methods (vLLM `speculative_config.method`)

| Method | How it drafts | Extra GPU memory? | Best-fit workload |
|---|---|---|---|
| `mtp` | Multi-Token Prediction head bundled in the checkpoint (DeepSeek-V3 / R1, etc.) | No | Any workload on that model family |
| `dspark` | DeepSeek-V4's own semi-autoregressive block drafting. Ships two ways: **fused** (module baked into the served checkpoint - `DeepSeek-V4-Flash-0731`, `DeepSeek-V4-Flash-DSpark`; no separate download) or **assistant** (a separately sourced draft checkpoint loaded alongside a distinct base). `DSparkConfig.Variant` discriminates the two at catalog time. V4-Pro-0813 is deferred until its upstream vLLM recipe pins which shape applies. | Fused: no extra GPU memory; Assistant: yes (draft checkpoint loaded alongside target - requires node-estimator budgeting) | Any workload on DeepSeek-V4 |
| `eagle` / `eagle3` | Separate draft model trained to mimic the target | Yes (separate checkpoint loaded alongside target) | General-purpose; mainstream across vLLM/SGLang/TensorRT-LLM |
| `ngram` / `suffix` | Pure lookup against prompt + generation history | No | Code completion, RAG, summarization, translation, agent tool-call echo |

#### Method Deep Dive

The rest of this section explains each method in more depth: what the draft actually is, how vLLM verifies it, why the memory/latency cost lands where it does, and which KAITO presets it maps to. This is the reasoning that drives the per-preset `SpeculativeDecoding` map in [Per-Preset Config Ownership](#per-preset-config-ownership).

##### `mtp` — Multi-Token Prediction

**How it drafts.** The base checkpoint ships extra transformer heads (the "MTP heads") trained to predict the *next N* tokens in parallel from the *same* hidden state as the normal LM head. At serve time, vLLM asks the MTP head for K speculative continuations, then verifies them against the true LM head in a single batched forward pass. Accepted tokens are committed; the first rejected token triggers a re-decode from that point.

**Why it's free.** The MTP heads are already inside the served weights — no separate draft checkpoint, no additional GPU memory, no extra HuggingFace download. The only cost is a bit more compute per verification step, which is usually more than repaid by 1.5–2× fewer autoregressive steps.

**Where it applies.** DeepSeek-V3 / R1 family (`deepseek-v3-0324`, `deepseek-r1-0528`). Upstream vLLM benchmark: [vllm-project/vllm#12755](https://github.com/vllm-project/vllm/pull/12755). Tuned config in this PR: `{"method":"mtp","num_speculative_tokens":1}` — conservative because the DeepSeek recipe is only trained to depth 1.

**Caveats.** Requires an MTP-head-bearing checkpoint. Not every model family trains one — e.g. Llama / Qwen 2.5 don't ship MTP heads today, so they use `ngram` (universal fallback) instead.

##### `dspark` — DeepSeek-V4 Semi-Autoregressive Block Drafting

**How it drafts.** DeepSeek's own successor to MTP for the V4 family. Instead of predicting a fixed number of next tokens, the DSpark module drafts a *variable-length block* per step using semi-autoregressive attention, then vLLM verifies the block in one shot. Longer accepted runs on high-agreement prefixes; shorter (or empty) drafts when the model is uncertain.

**Two ship shapes.** vLLM exposes DSpark in two flavors, discriminated by `DSparkConfig.Variant` at catalog time:
- **Fused** (`DeepSeek-V4-Flash-0731`, `DeepSeek-V4-Flash-DSpark`): DSpark module is baked into the served checkpoint. No extra download, no extra GPU memory — the same free-lunch shape as `mtp`.
- **Assistant** (`DeepSeek-V4-Pro-0813` — pending upstream recipe): DSpark module lives in a separately sourced draft checkpoint, loaded alongside the base. Needs draft weight download and its own KV cache budget — the node-estimator has to account for it (see [Deferred Follow-Ups](#deferred-followups)).

**Where it applies.** DeepSeek-V4 family. Not shipped in this PR; tracked in *Ready to Onboard (`dspark`, DeepSeek-V4 Family)*.

**Caveats.** `V4-Pro-0813` is deferred until upstream vLLM pins the recipe (fused vs assistant, which draft checkpoint, which verification depth).

##### `eagle` / `eagle3` — Trained Draft Model

**How it drafts.** A *separate*, smaller draft model is trained to mimic the target model's hidden-state trajectory (EAGLE-1 conditions on the target's last hidden state; EAGLE-3 adds multi-layer feature reuse). The draft proposes K tokens; the target verifies them in one batched forward, rewinding on the first rejection.

**Why it costs GPU memory.** The draft checkpoint is loaded alongside the target on the same GPU (or the same node in TP setups) and needs its own KV cache slice. Depending on draft-to-target size ratio, this can eat 3–10% of GPU memory. Node-estimator has to know about it before scheduling.

**Why we still care.** EAGLE/EAGLE-3 is the most portable of the four — it's the mainstream path in vLLM, SGLang, and TensorRT-LLM, and draft checkpoints exist (or can be trained) for any target family. For general-purpose (non-code, non-lookup-friendly) workloads it's often the highest-quality speculative option.

**Where it applies.** Any preset where a compatible EAGLE draft checkpoint is available. Not shipped in this PR — tracked in *Deferred - EAGLE / EAGLE-3 (Separate Draft Checkpoint)* because it needs new catalog schema for the draft-checkpoint reference, node-estimator memory budgeting, and a per-target compatibility matrix.

**Caveats.** Draft-target compatibility is *not* automatic; you need the specific EAGLE checkpoint that was trained against that target. Version drift between the two breaks acceptance rates silently.

##### `ngram` / `suffix` — Universal Prompt Lookup

**How it drafts.** Zero neural draft. On each step, vLLM scans the prompt + generation-history window for the longest n-gram suffix (up to `prompt_lookup_max`) that matches the last few emitted tokens, and proposes the next `num_speculative_tokens` tokens from that match. Verification is the normal target forward pass.

**Why it's universal.** No draft checkpoint, no MTP head, no extra GPU memory, no catalog-schema changes. It works on *any* preset that vLLM can serve. This is why it's the fallback path when the toggle is on but the preset has no per-preset config — see the current PR (#2312).

**Where it wins.** Any workload with high self-repetition:
- code completion (repeated identifiers, imports, boilerplate)
- RAG (the answer often quotes the context back)
- summarization / translation (source-token re-emission)
- agent tool-call echo (the model repeats tool arguments)

**Where it doesn't help.** Free-form creative generation with low prompt overlap; there's simply nothing to look up. Acceptance rate collapses to near zero and you just pay the (small) lookup cost.

**Why the KAITO default is `{num_speculative_tokens:5, prompt_lookup_max:4}`.** These match vLLM's documented `ngram` baseline. `num_speculative_tokens=5` is aggressive enough to see gains on repetition-heavy workloads but not so large that verification cost dominates on more open-ended generation. `prompt_lookup_max=4` caps the lookup window so the scan stays `O(prompt_len * 4)` even on very long contexts.

#### Method → Preset Selection Rule

At annotation-opt-in time, KAITO picks the method for the user:

1. If the preset has an entry in [`speculativeDecodingByPreset`](#per-preset-config-ownership) (`mtp` today, `dspark` after DeepSeek-V4 lands) → use the preset-tuned config.
2. Otherwise → use the universal `ngram` default.

`eagle` / `eagle3` never selects itself automatically — it needs schema work first (draft-checkpoint reference in `model_catalog.yaml`, node-estimator memory budgeting) and is intentionally left out of the automatic selection until that lands.

#### Pipeline Parallelism Compatibility

Speculative decoding and pipeline parallelism (PP) have a fundamental tension: the spec-decoding loop is `draft k → verify k → accept/reject → repeat`, which is strongly serial across iterations, while PP extracts throughput by keeping the pipeline full with many in-flight micro-batches. Under single-request spec-decoding there is effectively one in-flight micro-batch, so PP bubbles are not hidden, and every iteration pays for one full pipeline round-trip to move the accept/reject decision from the last stage back to stage 0. As a result, whether a given drafting method composes with PP is method-specific, not global:

| Method | TP | PP (target Count > 1) | Notes |
|---|---|---|---|
| `ngram` | ✅ | ✅ (reduced speedup) | Drafter is CPU-side string matching over the context; it does not touch the target model's execution graph. Runs correctly under PP; combined speedup is smaller than TP-only + `ngram`, but stable. |
| `mtp` | ✅ | ✅ | MTP heads are baked into the DeepSeek-V3 / R1 checkpoint and are sharded together with the model; vLLM supports MTP under PP. This matters because DeepSeek-V3/R1 (671B) physically require multi-node PP to serve, so blanket-rejecting PP would make the `mtp` presets unreachable. |
| `dspark` | ✅ | ⚠️ deferred | Fused vs assistant variants place the drafter differently; PP behaviour is not yet validated against a pinned vLLM. Treat as unsupported until V4 recipes land. |
| `eagle` / `eagle3` | ✅ | ❌ | vLLM's EAGLE implementation assumes the draft head co-locates with a TP-sharded target on the same set of GPUs. EAGLE-3 additionally fuses shallow / middle / deep target features that would now live on different PP stages. Gathering them per iteration is prohibitively expensive. Known upstream limitation. |

Because vLLM does not fail loudly for the unsupported combinations, KAITO must own this validation. The rule is method-aware, not blanket:

1. Resolve the effective method at admission time using the same source of truth as pod-spec generation: look up `speculativeDecodingByPreset[strings.ToLower(preset)]` and fall back to `"ngram"` when the preset is not registered.
2. If the resolved method is `ngram` or `mtp` and `Resource.Count > 1` → allow. Optionally emit a `Warning` event noting that PP reduces spec-decoding speedup for `ngram`.
3. If the resolved method is `eagle` / `eagle3` (or any future method flagged as PP-incompatible) and `Resource.Count > 1` → reject with a message that names the resolved method and points at this limitation.
4. Same check applies to the child-Workspace layer for InferenceSet and MultiRoleInference; the parent-level validators do not need their own PP check because the propagated child Workspace's `Resource.Count` is what actually determines PP.

The shared truth table lives with the `speculativeDecodingByPreset` map (`ResolveSpeculativeDecodingMethod` + `SpeculativeDecodingMethodSupportsPipelineParallelism` in `presets/workspace/generator/generator.go`) so the admission webhook and the pod-spec injection helper cannot drift.

#### Why It Isn't Always On

Throughput can *regress* at high QPS (draft is wasted work when the batch is already saturated). It has to stay **opt-in**, never default. Several vLLM compatibility caveats also exist (pipeline parallelism as covered above, prefix caching, chunked prefill, logprob stability, LoRA/tool-calling) that need per-preset re-verification against KAITO's pinned vLLM version.

### User Experience

#### Today (Before This Proposal) - Painful

To turn speculative decoding on for DeepSeek-R1, the user has to write a ConfigMap that forwards raw JSON to vLLM via the `vllm:` passthrough:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-inference-config
data:
  inference_config.yaml: |
    vllm:
      # The user has to know:
      #  - that this JSON key exists
      #  - that their model supports mtp
      #  - what num_speculative_tokens value is reasonable
      speculative-config: '{"method":"mtp","num_speculative_tokens":1}'
---
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-r1
inference:
  preset:
    name: deepseek-r1-0528     # a committed MTP preset (see Model Coverage)
  config: my-inference-config
resource:
  instanceType: Standard_ND96isr_H200_v5
  labelSelector:
    matchLabels:
      apps: workspace-r1
```

Every misspelled field / wrong method / wrong param → pod fails to start or silently produces garbage.

#### After - One Annotation

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-r1
  annotations:
    kaito.sh/enable-speculative-decoding: "true"     # ← that's it
inference:
  preset:
    name: deepseek-r1-0528     # a committed MTP preset
resource:
  instanceType: Standard_ND96isr_H200_v5
  labelSelector:
    matchLabels:
      apps: workspace-r1
```

`kubectl apply -f` and KAITO:

1. Looks up the preset's `SpeculativeDecoding` config (validated in advance by preset maintainers).
2. Injects `--speculative-config '{"method":"mtp","num_speculative_tokens":1}'` into the vLLM command.
3. Pod comes up with speculative decoding enabled.

The user does **not** need to know what `mtp` / `eagle` / `ngram` are, what the parameters mean, or what the JSON schema looks like.

#### Side-by-Side

| Aspect | Before | After |
|---|---|---|
| Steps | Write ConfigMap + reference from Workspace | Add 1 annotation |
| Required knowledge | vLLM speculative decoding API, method taxonomy, parameter tuning | How to write `annotations:` |
| Error probability | High (JSON typo, wrong field, method/model mismatch) | Very low |
| Switching preset | Rewrite the JSON | Annotation unchanged; KAITO picks up new preset's config |
| Unsupported preset | Pod starts, inference errors out | Rejected at `kubectl apply` by admission webhook |

Expected latency win for interactive chat / agent workloads on `deepseek-r1-*`, per the vLLM MTP benchmark (vllm-project/vllm#12755):

| Regime | Speedup |
|---|---|
| Interactive (QPS ≈ 1) | 1.63×-1.69× |
| Low QPS (QPS 2-4) | 1.18×-1.32× |
| Saturated (QPS ≥ 6, TP=8) | below 1× |

Speculative decoding is therefore most valuable for interactive chat/agent workloads; the annotation is opt-in precisely so operators do not enable it under saturated batch traffic where it regresses.

#### What Happens on an Unsupported Preset

```yaml
metadata:
  annotations:
    kaito.sh/enable-speculative-decoding: "true"
inference:
  preset:
    name: llama-3.1-8b-instruct    # no SpeculativeDecoding entry
```

`kubectl apply` is rejected by the admission webhook:

```
Error from server (Forbidden): admission webhook "validation.workspace.kaito.sh"
denied the request: preset "llama-3.1-8b-instruct" does not have a validated
speculative decoding configuration; remove kaito.sh/enable-speculative-decoding
annotation or choose a supported preset (currently: deepseek-r1-0528,
deepseek-v3-0324).
```

### Per-Preset Config Ownership

**The per-preset config is baked in by KAITO maintainers, not a user knob.**

| Who | Controls |
|---|---|
| **KAITO maintainers** (code) | Which method (`mtp` / `dspark` / `ngram` / ...), the parameters (`num_speculative_tokens`, `prompt_lookup_max` ...), which presets are enabled |
| **User** (annotation) | On / off |

#### Escape Hatch for Power Users

The existing `inference_config.yaml` ConfigMap `vllm:` passthrough is **not going away** - a researcher who wants to sweep `num_speculative_tokens` or try a different method (e.g. `ngram`) can still write raw `--speculative-config` themselves. A typed override field on `InferenceSpec` is called out as **out of scope** for this proposal.

> **Caveat on the passthrough for methods that need a separate draft model (`eagle` / `medusa` / assistant-checkpoint `mtp` / `dspark`).** The passthrough gets vLLM the `speculative_config` JSON, but the KAITO node estimator (`workspace_controller.go`, resource-planning path) sizes the pod GPU memory from the **target** model only. A separate draft checkpoint of non-trivial size can silently push the pod OOM at load, or evict KV cache under load. So `inference_config.yaml` passthrough is safe for zero-extra-weight methods (`mtp` with self-contained head, `ngram`, `suffix`), but for draft-model methods the estimator has to be updated in the same PR that ships the config. Step 1 covers the DSpark assistant-checkpoint case as first-class typed config (`DSparkConfig.Model`). Assistant-checkpoint `mtp` (Gemma 4 IT family) is **not** first-class in Step 1 - Step 1's `MTPConfig` is intentionally a self-contained-head-only shape (see the type below). Extending `MTPConfig` with a `Model` field and updating the node estimator are called out as **future work** in the Gemma 4 IT onboarding section below, alongside assistant-checkpoint sourcing.

### Where the Per-Preset Config Lives in Code

**The config lives in a Go map (`speculativeDecodingByPreset`) in `presets/workspace/generator/generator.go`, next to the existing per-preset maintainer-authored maps such as `reasoningParserModeNamePrefixMap` and `reasoningParserArchMap`. It is NOT added to `model_catalog.yaml`, and `catalogOverrides` / `CatalogEntry` are NOT extended with a `SpeculativeDecoding` field. Every step in this proposal - catalog-time validation, runtime injection, admission-webhook validation, unit tests, and preset-maintainer workflow - reads from and writes to `speculativeDecodingByPreset` only.**

Status quo:

- `presets/workspace/models/model_catalog.yaml` is a local mirror of HuggingFace `config.json` for preset models. It is generated from HuggingFace `config.json` by `presets/workspace/generator/update_model_catalog/main.go`. `catalogOverrides` in `presets/workspace/generator/generator.go` today only patches fields that HF `config.json` omits (Gemma 3 context length, Mistral Large 3 architecture). Speculative-decoding config is a KAITO-authored runtime knob - nothing on HuggingFace corresponds to it - so it does not belong in a file whose purpose is to mirror HF metadata.
- The codebase already has precedent for KAITO-authored, per-preset config kept in a Go map keyed by preset/arch: `reasoningParserModeNamePrefixMap` and `reasoningParserArchMap` at `generator.go:46` / `generator.go:67`. Same shape as what this proposal needs.
- `presets/workspace/models/supported_models.yaml` is being deprecated (only the `base` image entry is still actively consumed) and is not a candidate either.
- Today `CatalogEntry` is parsed into `model.PresetParam` by `GeneratePreset`; `vLLMCompatibleModel` (the object registered in `plugin.KaitoModelRegister`) currently retains only the fields it uses at command-build time. `ModelRegister` also has no `Get` API that exposes richer metadata to callers such as the admission webhook.

The plumbing change this proposal adds:

1. Extend `model.PresetParam` (the type served by `plugin.KaitoModelRegister`) with a `SpeculativeDecoding *SpeculativeDecodingConfig` field (Step 1 below).
2. Add a package-level `speculativeDecodingByPreset map[string]*SpeculativeDecodingConfig` in `presets/workspace/generator/generator.go` (colocated with `catalogOverrides` and the `reasoningParser*` maps). Keys are lowercased HuggingFace repo names, matching the key convention `catalogOverrides` already uses, so entries look up cleanly with `strings.ToLower(g.ModelRepo)`.
3. In `Generator.loadFromCatalog` (`generator.go:785`), after the existing explicit field copies, add a single map lookup and assign: `if sd, ok := speculativeDecodingByPreset[strings.ToLower(g.ModelRepo)]; ok { g.Param.SpeculativeDecoding = sd }`. Same pattern the reasoning-parser lookup uses later in this file.
4. Extend `vLLMCompatibleModel` in `presets/workspace/models/vllm_model.go` to hold the field, and make `GetInferenceParameters()` return it on the copy it hands back to the controller - this is what makes the value visible to the reconcile-time injection site in Step 3.
5. Use `models.GetModelByName(name, accessSecret)` in the admission webhook to resolve the preset. This function calls `GetModelByNameWithToken` internally, which normalizes aliases (`deepseek-r1-0528` → `deepseek-ai/deepseek-r1-0528`) BEFORE the `plugin.KaitoModelRegister.MustGet` lookup (`presets/workspace/models/vllm_model.go:108-122`). Registered models were populated at init time from their full HF repo name, and the map key convention is the same lowercased HF repo name, so the alias rewrite lands on the same object whether the user typed the short alias or the full name. Both the controller and the webhook thus resolve the same object for the same annotation-facing preset name.

After these changes, both the controller and the webhook read the same `SpeculativeDecoding` value from one source of truth (`speculativeDecodingByPreset` in `generator.go`) - both go through `models.GetModelByName(ws.Inference.Preset.Name, accessSecret)` → `GetModelByNameWithToken` → `GetInferenceParameters()`. `model_catalog.yaml` and `supported_models.yaml` are not touched by this proposal.

> **Why not `model_catalog.yaml` / `catalogOverrides`?** Reviewer feedback ([#2303 review comment](https://github.com/kaito-project/kaito/pull/2303#discussion_r3883244980)): `model_catalog.yaml` mirrors HuggingFace `config.json`; `catalogOverrides` today only patches HF-missing fields. Storing an operational KAITO knob there would mix responsibilities and force three coordinated updates in `update_model_catalog/main.go` (change-detection map, `FetchCatalogEntry` explicit copy, refresh preservation). A dedicated map next to the existing `reasoningParser*` maps has none of that friction, keeps `model_catalog.yaml` as an HF mirror, and matches an established codebase pattern for maintainer-authored per-preset config.

### Implementation

#### Step 1 - Extend the model metadata type

In the `model` package (whichever file defines `Metadata` / `PresetParam` served by `plugin.KaitoModelRegister`):

```go
type SpeculativeDecodingConfig struct {
    Method string       `yaml:"method"`         // "mtp" / "ngram" / "dspark" / ...
    MTP    *MTPConfig   `yaml:"mtp,omitempty"`
    NGram  *NGramConfig `yaml:"ngram,omitempty"`
    DSpark *DSparkConfig `yaml:"dspark,omitempty"`
    // future: EAGLE *EAGLEConfig
}

// MTPConfig covers the self-contained-head case only (DeepSeek R1/V3
// family), where the MTP head is bundled in the served checkpoint and
// vLLM emits a method-only-plus-depth blob. Assistant-checkpoint MTP
// (Gemma 4 IT family) is future work and will extend this struct with
// a `Model` field of the same shape as `DSparkConfig.Model` - see the
// Gemma 4 IT onboarding section under Model Coverage.
type MTPConfig struct {
    NumSpeculativeTokens int `yaml:"numSpeculativeTokens"`
}

type NGramConfig struct {
    NumSpeculativeTokens int `yaml:"numSpeculativeTokens"`
    PromptLookupMax      int `yaml:"promptLookupMax"`
}

type DSparkConfig struct {
    // Variant is the draft-source discriminator: "fused" (default) or
    // "assistant". Fused variants (V4-Flash-0731, V4-Flash-DSpark)
    // ship the DSpark module inside the served checkpoint and emit a
    // method-only-plus-parameters vLLM blob (no `model` field).
    // Assistant variants load a separately sourced DSpark checkpoint
    // alongside the base model. Catalog validation keys off this
    // field: assistant REQUIRES Model non-empty; fused REQUIRES Model
    // empty. Unset (zero value) is treated as "fused" for backwards
    // compatibility with the initial fused-only shipping list.
    Variant              string `yaml:"variant,omitempty"` // "" | "fused" | "assistant"
    // Model is the assistant/draft checkpoint reference (e.g.
    // "deepseek-ai/DeepSeek-V4-Flash-DSpark") serialized as
    // speculative_config.model in the vLLM JSON blob. REQUIRED when
    // Variant == "assistant"; MUST be empty when Variant == "fused"
    // (or unset). Fused served-checkpoint identity is tracked on the
    // preset (`CatalogEntry.Name` / preset image), not here.
    Model                string `yaml:"model,omitempty"`
    NumSpeculativeTokens int    `yaml:"numSpeculativeTokens"`
    // DraftSampleMethod matches speculative_config.draft_sample_method
    // in the vLLM recipe (e.g. "probabilistic"). Optional; when unset
    // the vLLM default for the served DSpark checkpoint applies.
    DraftSampleMethod    string `yaml:"draftSampleMethod,omitempty"`
    // AttentionBackend is an optional hardware-specific override
    // (e.g. Blackwell FP4 indexer cache) mirrored into
    // speculative_config.attention_backend. Left empty when the
    // recipe does not require it.
    AttentionBackend     string `yaml:"attentionBackend,omitempty"`
}

// Added to model.PresetParam (the type served by plugin.KaitoModelRegister).
type PresetParam struct {
    // ... existing fields ...
    SpeculativeDecoding *SpeculativeDecodingConfig `yaml:"speculativeDecoding,omitempty"`
}
```

**Implementation note on propagation.** `vLLMCompatibleModel` currently stores only `Metadata` and generated run params (`presets/workspace/models/vllm_model.go:173-176`); `registerModel` discards the rest of `PresetParam`. The implementation must:

1. Add a `SpeculativeDecoding *SpeculativeDecodingConfig` field to `vLLMCompatibleModel` and populate it in `registerModel`.
2. Extend `PresetParam.DeepCopy` to deep-copy the `SpeculativeDecoding` pointer and its nested config structs (MTP/NGram/DSpark sub-configs each contain value fields, but the outer pointers must be cloned to avoid aliasing across concurrent reconciles).
3. Return the field through `GetInferenceParameters()` so the admission webhook and injection function can read it.

The typed sub-structs constrain the shape, but they do not by themselves prevent an author from pairing `Method: "mtp"` with a nil `MTP` or a populated `NGram`. Validation happens in two places:

- **Catalog-time validation** (unit test over the `speculativeDecodingByPreset` map): asserts method / sub-struct exclusivity (exactly one non-nil sub-config, matching `method`), required positive fields (`numSpeculativeTokens > 0`, `promptLookupMax > 0` for `ngram`), and supported methods. For `dspark`, a typed `DSparkConfig.Variant` discriminator (`"fused"` | `"assistant"`, defaulting to `"fused"` when unset for backwards compatibility) decides whether `Model` is required: `Variant: "assistant"` requires a non-empty `Model`; `Variant: "fused"` requires `Model` to be empty. Any `MTPConfig` in Step 1 is implicitly self-contained-head; a parallel `MTPConfig.Variant` discriminator is deferred to the Gemma 4 IT assistant-checkpoint follow-up (see the caveat in [Per-Preset Config Ownership](#per-preset-config-ownership)). Fails the build if a maintainer authors a broken `speculativeDecodingByPreset` entry.
- **Runtime** (`vllmFormat`, Step 3): returns a typed error rather than dereferencing a possibly-nil pointer.

`DSpark` is included even though DeepSeek-V4 onboarding is a follow-up, so the type surface is stable when the next preset lights up.

#### Step 2 - Declare per-preset config via `speculativeDecodingByPreset`

Add entries to the `speculativeDecodingByPreset` map in `presets/workspace/generator/generator.go`, colocated with the existing per-preset maintainer-authored maps (`reasoningParserModeNamePrefixMap` at `generator.go:46`, `reasoningParserArchMap` at `generator.go:67`, and `catalogOverrides` at `generator.go:283` - this proposal adds a sibling map, not a new field on `CatalogEntry`). Keys are lowercased HuggingFace repo names, matching the key convention `catalogOverrides` already uses. The map is looked up once in `Generator.loadFromCatalog` and copied onto `PresetParam.SpeculativeDecoding` for the presets that opt in; nothing is written into `model_catalog.yaml`, and `catalogOverrides` / `CatalogEntry` are untouched.

`presets/workspace/generator/generator.go`:

```go
// specDecoEntry pairs the user-facing preset alias (what a user writes in
// `Workspace.Inference.Preset.Name`) with the KAITO-authored config. Storing
// the alias inline avoids a reverse repo->alias lookup, which would either
// require calling into `presets/workspace/models` (impossible: `models`
// already imports `generator` via `vllm_model.go:30`, so the reverse import
// would create a Go cycle) or duplicating `plugin.LegacyBuiltinToCatalog`.
type specDecoEntry struct {
    UserFacing string                          // e.g. "deepseek-r1-0528"
    Config     *model.SpeculativeDecodingConfig
}

// New package-level map, sibling to catalogOverrides / reasoningParser*.
// NOT a field on CatalogEntry, and NOT surfaced in model_catalog.yaml.
// Keys are lowercased HuggingFace repo names, matching the key convention
// `catalogOverrides` uses (so `Generator.loadFromCatalog` can look up by
// `strings.ToLower(g.ModelRepo)` without any translation).
var speculativeDecodingByPreset = map[string]specDecoEntry{
    "deepseek-ai/deepseek-r1-0528": {
        UserFacing: "deepseek-r1-0528",
        Config: &model.SpeculativeDecodingConfig{
            Method: "mtp",
            MTP:    &model.MTPConfig{NumSpeculativeTokens: 1},
        },
    },
    "deepseek-ai/deepseek-v3-0324": {
        UserFacing: "deepseek-v3-0324",
        Config: &model.SpeculativeDecodingConfig{
            Method: "mtp",
            MTP:    &model.MTPConfig{NumSpeculativeTokens: 1},
        },
    },
    // See the "Model Coverage" section for the full initial-ship list.
    // A build-time unit test (see Step 2 validation) asserts, for each entry,
    // one of the following (in order):
    //   1. `plugin.LegacyBuiltinToCatalog[UserFacing]` equals the map key
    //      (covers legacy short aliases like `deepseek-r1-0528` →
    //      `deepseek-ai/deepseek-r1-0528`), OR
    //   2. `UserFacing` equals the map key (covers catalog-native presets
    //      that ship the full HuggingFace ID as their user-facing name, e.g.
    //      `deepseek-ai/deepseek-v3.2`, since `LegacyBuiltinToCatalog` is
    //      frozen and cannot take new entries — see
    //      `pkg/utils/plugin/plugin.go:46` "Please don't introduce new
    //      entries to LegacyBuiltinToCatalog").
    // Either match catches maintainer typos in the key or `UserFacing`
    // without importing `models`.
}

// SupportedSpeculativeDecodingPresets returns the sorted, user-facing preset
// names that currently carry a validated SpeculativeDecoding entry. It is the
// single exported accessor for `speculativeDecodingByPreset` and is what the
// admission webhook (in a different package) MUST call - the map itself stays
// lowercase and package-private so tests and generator internals are the only
// direct readers. Returned slice is a fresh copy; callers may mutate it.
func SupportedSpeculativeDecodingPresets() []string {
    out := make([]string, 0, len(speculativeDecodingByPreset))
    for _, entry := range speculativeDecodingByPreset {
        out = append(out, entry.UserFacing)
    }
    sort.Strings(out)
    return out
}
```

In `Generator.loadFromCatalog`, after the existing field copies (same shape as the `reasoningParser*` lookups later in the file):

```go
if entry, ok := speculativeDecodingByPreset[strings.ToLower(g.ModelRepo)]; ok {
    g.Param.SpeculativeDecoding = entry.Config
}
```

`model_catalog.yaml` gains NO new fields. Runtime callers reach the config through `plugin.KaitoModelRegister → vLLMCompatibleModel → GetInferenceParameters().SpeculativeDecoding`, which is populated by the map lookup above during preset generation - the value lives in `PresetParam`, not on disk.

> **Why `numSpeculativeTokens: 1`?** Matches the evidence: vllm-project/vllm#12755 explicitly benchmarks MTP depth = 1, and current upstream vLLM guidance calls 1 a good starting default. Larger depths materially change acceptance ratios and load behavior, so lifting to 2/3 requires a KAITO-pinned benchmark before it can ship as "pre-validated." Committed as a follow-up.

#### Step 3 - Preset controller reads the field and injects the vLLM flag

The existing runtime command is assembled as a shell string via `BuildCmdStr` and executed with `/bin/sh -c`. `ModelRunParams` appends `--key=value` **verbatim** to that string - it does **not** shell-quote the value, and today `kv-events-config` works around this by storing the value with literal single quotes already embedded. Handing a raw `json.Marshal` result to `ModelRunParams` would let `/bin/sh` strip the JSON's double quotes and mis-tokenize the braces/commas.

Two acceptable options; the design commits to option (a) because it is smaller and localized:

**(a) Explicit shell-escape at the injection site.** Wrap the JSON in single quotes and escape any embedded single quotes with the standard `'\''` sequence before assigning to `ModelRunParams`. A unit test asserts the final `/bin/sh -c` command line by round-tripping through `sh -c 'echo $1'` and re-parsing as JSON.

**(b) Move `--speculative-config` off the shell-string path.** Emit it directly into `container.Args` (argv), bypassing `BuildCmdStr` for this one flag. Larger change; called out as a follow-up if more flags need JSON payloads.

> **On per-preset parameter defaults.** For presets whose vLLM recipe upstream gives concrete recommended values (e.g. DeepSeek-V4-Flash's [vLLM recipe](https://recipes.vllm.ai/deepseek-ai/DeepSeek-V4-Flash) covers `num_speculative_tokens`, tensor-parallel size, and KV-cache dtype), the `speculativeDecodingByPreset` entry for that preset carries those values so the injected `--speculative-config` matches the upstream-recommended profile out of the box. When the recipe is silent, `numSpeculativeTokens: 1` is used as a safe default (see "Why 1?" in Step 2).

**Where the injection lives.** `GenerateInferencePodSpec` (`pkg/workspace/inference/preset_inferences.go:522`) is the code that builds the workload command. On line 578 it calls `ctx.Model.GetInferenceParameters().DeepCopy()` to obtain a fresh `PresetParam` for **this** pod; on line 579 it computes `runtimeName := v1beta1.GetWorkspaceRuntimeName(ctx.Workspace)`; then on line 600 it calls `inferenceParam.GetInferenceCommand(...)` which walks `inferenceParam.VLLM.ModelRunParams` to build the shell string. `GetInferenceParameters` on `vLLMCompatibleModel` allocates a new `PresetParam` on every call, so **mutating any earlier copy is lost**. The injection therefore has to run **after** the line-578 `DeepCopy()` (and after the line-579 runtime-name computation) and **before** the line-600 `GetInferenceCommand(...)`, and it has to mutate `inferenceParam.VLLM.ModelRunParams` on that exact copy - no free-floating `runParams` map, no earlier `PresetParam` snapshot. `RuntimeParam.VLLM` is a value of type `VLLMParam`, not a pointer (see `pkg/model/interface.go:233-260`), so the runtime gate is `runtimeName == pkgmodel.RuntimeNameVLLM`, not a nil check on `inferenceParam.VLLM`:

```go
// pkg/workspace/inference/preset_inferences.go, inside GenerateInferencePodSpec's
// returned func, immediately after:
//     inferenceParam := ctx.Model.GetInferenceParameters().DeepCopy()
//     runtimeName := v1beta1.GetWorkspaceRuntimeName(ctx.Workspace)
// and BEFORE:
//     commands := inferenceParam.GetInferenceCommand(...)
//
// NOTE: the WorkspaceGeneratorContext closure exposes only `ctx` and
// `spec`; there is no `ws` in scope. Bind it once up-front so every
// reference below reads off the same object the caller resolved.
ws := ctx.Workspace

// Declared here (outside the annotation guard) so both the injection
// block below and the tail of the modifier can read it. Every branch
// that decides "do not inject" sets this to true; every other branch
// leaves it false and falls through to the injection code.
skipInjection := false

if runtimeName == pkgmodel.RuntimeNameVLLM &&
    ws.Annotations["kaito.sh/enable-speculative-decoding"] == "true" {
    // Reconcile-time guard: refuse to inject when the resolved node count
    // > 1 (pipeline parallelism, which is incompatible with speculative
    // decoding). The admission webhook checks this too but estimation can
    // change between admission and reconcile (e.g. SKU availability), so
    // we re-check status.targetNodeCount here before mutating
    // ModelRunParams.
    //
    // IMPORTANT: WorkspaceGeneratorContext (pkg/utils/generator/generator.go:30)
    // has no EventRecorder and no status writer, and any mutation of `ws`
    // performed here is on the in-memory copy used to build this manifest
    // - the controller's next status update loads status fresh from the
    // API server. So we do NOT set a Workspace condition or emit an Event
    // from inside GenerateInferencePodSpec. This site only decides whether
    // to inject; the reconciler owns condition/Event emission (see Step 4).
    // A skip is silent here on purpose; the reconciler translates it into
    // status once the injection decision is available (see "Reconciler
    // wiring for the skip signal" below).
    if ws.Status.TargetNodeCount > 1 {
        // Pipeline-parallelism guard is method-aware. ngram (universal
        // fallback) and mtp (DeepSeek-V3/R1 baked-in heads) compose with
        // PP; eagle / eagle3 in vLLM do not. Resolve the method from the
        // already-resolved PresetParam (falling back to the ngram
        // constant used by defaultFallbackNGramConfig) and only skip
        // injection when the method actually can't run under PP.
        method := generator.SpeculativeDecodingFallbackMethod
        if inferenceParam.SpeculativeDecoding != nil && inferenceParam.SpeculativeDecoding.Method != "" {
            method = inferenceParam.SpeculativeDecoding.Method
        }
        if !generator.SpeculativeDecodingMethodSupportsPipelineParallelism(method) {
            // Reconciler translates this into
            // ConditionSpeculativeDecodingDisabled with
            // reason=PipelineParallelism.
            if outDecision != nil {
                *outDecision = SpecDecoPipelineParallelism
            }
            skipInjection = true
        }
    }
    if !skipInjection {
        // Extract SpeculativeDecoding from the already-resolved model.
        // ctx.Model was resolved by the caller via models.GetModelByName
        // (which triggers GetModelByNameWithToken internally and rewrites
        // short aliases like deepseek-r1-0528 -> deepseek-ai/deepseek-r1-0528).
        // Read the field off inferenceParam (the DeepCopy above), not off
        // ctx.Model.GetInferenceParameters() again - the latter would allocate
        // yet another PresetParam and any prior edits to inferenceParam would
        // not appear on it.
        //
        // NOTE: inferenceParam.VLLM is VLLMParam (a value, not a pointer;
        // see pkg/model/interface.go:233-260, RuntimeParam.VLLM VLLMParam).
        // Do NOT gate on `inferenceParam.VLLM != nil` - that wouldn't compile.
        // The `runtimeName == RuntimeNameVLLM` check above is the correct gate.
        if inferenceParam.SpeculativeDecoding == nil {
            // Annotation is set to "true" but the resolved model has no
            // built-in SpeculativeDecoding preset. Admission catches this
            // for freshly-created / freshly-updated Workspaces, but it
            // does NOT protect two important cases:
            //   1. Pre-existing Workspaces created before the annotation
            //      was introduced that carry the annotation via an out-of-
            //      band edit or CRD-level default.
            //   2. Workspaces that survive a controller upgrade in which a
            //      catalog entry lost its SpeculativeDecoding config
            //      (config removed from the model registry).
            //
            // Signal the reconciler via SpecDecoUnsupportedPreset so it
            // persists ConditionSpeculativeDecodingDisabled with
            // reason=UnsupportedPreset. Do NOT return an error: this is a
            // permanent configuration mismatch that a retry cannot resolve,
            // and a retryable error would create a rate-limited
            // reconcile/log loop. Self-healing happens naturally through
            // the existing watches: removing the annotation, changing
            // preset, or a catalog update that re-adds the config all
            // enqueue a new reconcile that will run this branch again with
            // a different decision.
            if outDecision != nil {
                *outDecision = SpecDecoUnsupportedPreset
            }
            // IMPORTANT: do NOT `return nil` here. This code lives inside
            // the `GenerateInferencePodSpec` modifier closure, so a bare
            // `return nil` would exit the entire modifier before
            // `GetInferenceCommand`, `setInferenceContainers`, and any
            // downstream modifiers run — producing an incomplete pod spec
            // (later modifiers would then index a missing container).
            // Instead, the injection code below is guarded by
            // `if !skipInjection { ... }` so this branch falls through
            // to the modifier's tail (which returns nil to continue the
            // normal build); command generation and container assembly
            // still run afterwards under the generator. The other
            // decision-recording branches (ConfigMap override, pipeline
            // parallelism, ...) do the same — they record the decision
            // and let the injection block no-op via `skipInjection`,
            // leaving the rest of the pod spec intact.
            skipInjection = true
        }
        // inferenceParam.SpeculativeDecoding is guaranteed non-nil below
        // when skipInjection is false.
        if !skipInjection {
            // Load and parse the user-provided inference ConfigMap so we can
        // honor the escape hatch ("ConfigMap wins", Step 5). This site
        // is downstream of the pod-spec builder that already mounts the
        // ConfigMap by name but does NOT parse its contents, so we do the
        // parse here explicitly. Failure semantics are conservative: if
        // ConfigMap probe: distinguish transient errors from "user
        // did not specify speculative-config". A transient failure
        // (API error, network glitch) must NOT be treated as absence,
        // because that would cause injection to proceed, revision hash
        // to advance, and subsequent reconciles to return early
        // (StatefulSet up-to-date). The override would then remain
        // shadowed until another revision change. Return the probe
        // error so the reconciler retries.
        userSpecified, cfgErr := loadUserSpeculativeConfig(ctx.Ctx, ctx.KubeClient, ws)
        if cfgErr != nil {
            if apierrors.IsNotFound(cfgErr) {
                // ConfigMap intentionally absent -> no user override.
                userSpecified = false
            } else {
                // Transient error: propagate so reconciler retries.
                // Do NOT collapse to userSpecified=false, or a durable
                // duplicate-config state can emerge.
                return fmt.Errorf("speculative decoding: probing ConfigMap for %s/%s: %w",
                    ws.Namespace, ws.Name, cfgErr)
            }
        }
        if userSpecified {
            // ConfigMap already carries vllm.speculative-config: do NOT
            // overwrite. Reconciler emits SpeculativeDecodingConfigMapOverride
            // Event based on this decision (Step 5).
            if outDecision != nil {
                *outDecision = SpecDecoConfigMapOverride
            }
        } else {
            blob, err := vllmFormat(inferenceParam.SpeculativeDecoding)
            if err != nil {
                return fmt.Errorf("speculative decoding: %w", err)
            }
            // ModelRunParams is walked by GetInferenceCommand below to
            // build the shell string. It does NOT shell-quote values, so
            // wrap the JSON in single quotes and escape embedded single
            // quotes so /bin/sh sees exactly one argv token.
            if inferenceParam.VLLM.ModelRunParams == nil {
                inferenceParam.VLLM.ModelRunParams = map[string]string{}
            }
            inferenceParam.VLLM.ModelRunParams["speculative-config"] = shellSingleQuote(blob)
            // Fall-through injection path completed.
            if outDecision != nil {
                *outDecision = SpecDecoInjected
            }
        }
        } // end of `if !skipInjection`
    }
}
```

`loadUserSpeculativeConfig` is a small helper collocated with the injection
site: it fetches the ConfigMap named by `ws.Inference.Config` (empty name =>
`(false, nil)`), YAML-parses the `inference_config.yaml` key into a shape
that exposes `vllm.speculative-config`, and returns `(true, nil)` iff the
`vllm.speculative-config` **key is present in the ConfigMap** — regardless
of whether the value is empty.

**Why key-presence, not non-empty value.** The vLLM runtime wrapper
(`presets/workspace/inference/vllm/inference_api.py`) forwards every
`vllm.*` key/value pair from the ConfigMap onto the container's argv. If
we treated an existing but empty `speculative-config` value as "not
specified" and injected our own, `inference_api.py` would still append
`--speculative-config` (with empty value) from the ConfigMap side. vLLM
would then see two `--speculative-config` flags and fail on invalid
startup arguments. Presence is therefore the override signal here; the
value itself only matters at admission.

**Admission webhook rejects empty `speculative-config` value.** To keep
the key-presence rule from letting users silently disable the feature by
setting an empty string, the existing Workspace ConfigMap validator
(`api/v1alpha1/inference_config_validation.go` and
`api/v1beta1/inference_config_validation.go`, which already parses
`inference_config.yaml` for `max-model-len` — see the shared
`BuildNodeEstimateInputs` in Step 4) rejects any `vllm.speculative-config`
key whose value is empty or unparsable as JSON with a clear
`spec.inference.config` field error. Users who want to disable speculative
decoding remove the workspace annotation
`kaito.sh/enable-speculative-decoding` instead of blanking the ConfigMap
key; users who want a custom config supply a non-empty JSON body that the
validator checks before reconcile ever runs.

**Invoke the new check on updates, not just create.** Today
`validateInferenceConfig` is wired into the create-time admission path
only, but `Workspace.Spec.Inference.Config` is mutable — an operator can
point an annotated Workspace at a fresh ConfigMap after creation.
Without the new `vllm.speculative-config` JSON/empty-value check running
on updates, a subsequent edit could switch the Workspace to an invalid
ConfigMap that bypasses admission entirely and fails only when vLLM
starts. This proposal therefore extends the v1alpha1 and v1beta1
validators to invoke the same check from `ValidateUpdate` in addition to
`ValidateCreate`, so any change to `spec.inference.config` — annotation
flip or ConfigMap swap — is rejected at admission if the resulting
`vllm.speculative-config` value is empty or unparsable.

This helper uses the same client, namespace, and ConfigMap-name
conventions as the existing `UpdateWorkspaceTargetNodeCount` context-window
path (`workspace_controller.go:1584-1595`) so the two readers never
disagree on which file inside the ConfigMap is authoritative.

#### Reconciler wiring for the skip signal

`GenerateInferencePodSpec` returns a manifest, not a status decision. To
keep `WorkspaceCondition` / Kubernetes Event emission on the reconciler
(which owns both the status writer and the `EventRecorder`), the
`GeneratePresetInference` function is extended to return an explicit
result struct alongside the `client.Object`:

```go
type PresetInferenceResult struct {
	Workload                    client.Object
	SpeculativeDecodingDecision SpecDecoDecision // skip | injected | configmap-override | pipeline-parallelism | unsupported-preset
}
```

The reconciler reads the decision from the returned result after
`GeneratePresetInference` returns and translates it into:

- `ConditionSpeculativeDecodingDisabled` with reason=`PipelineParallelism`
  when the multi-node guard fired, using the existing
  `meta.SetStatusCondition` path in `workspace_controller.go` (line 823 /
  1313 / 1448) so it lands on the persisted `WorkspaceStatus`.

- `ConditionSpeculativeDecodingDisabled` with reason=`UnsupportedPreset`
  when `GeneratePresetInference` returned `SpecDecoUnsupportedPreset`.
  This branch does **not** propagate a reconcile error — the mismatch is
  a permanent configuration state that only spec / annotation / catalog
  changes can resolve, and those changes already enqueue new reconciles
  via the standard watches. Retrying on a config-error return would
  produce an endless rate-limited reconcile/log loop with no possibility
  of self-healing.

  **Condition cleanup on transitions.** For every other decision value
  (`SpecDecoSkip`, `SpecDecoInjected`, `SpecDecoConfigMapOverride`) **that
  was actually recorded by the modifier** (see `SpecDecoNotEvaluated`
  below), the reconciler MUST call `meta.RemoveStatusCondition(&ws.Status.Conditions,
  ConditionSpeculativeDecodingDisabled)` (or set it to
  `Status=False, Reason=NotApplicable` with the current
  `ObservedGeneration`). Otherwise a stale
  `True/PipelineParallelism` or `True/UnsupportedPreset` condition
  survives when: (a) the annotation is removed, (b) the SKU changes so
  `targetNodes==1`, (c) `nodeCountPerReplica` shrinks, or (d) a catalog
  update re-introduces the SpeculativeDecoding config — all valid
  transitions after which the disabled reason no longer holds. The
  cleanup must run unconditionally on every reconcile that produces a
  non-PP / non-UnsupportedPreset **recorded** decision, not only on the
  transition itself, because the reconciler is edge-triggered on spec
  changes and may replay the same decision multiple times.

  **`SpecDecoNotEvaluated` — don’t conflate “closure never ran” with
  “closure recorded Skip”.** `GeneratePresetInference` allocates the
  decision variable **before** `GenerateManifest` runs the modifier
  closure. If a failure occurs *upstream* of the modifier — GPU-config
  resolution, streaming resolution, image resolution, or any other
  pre-`GenerateManifest` return — the closure never executes and the
  decision variable keeps its zero value. If the zero value is
  `SpecDecoSkip`, the cleanup rule above would incorrectly clear an
  existing `True/PipelineParallelism` or `True/UnsupportedPreset`
  condition on the next reconcile that hits an unrelated GPU / streaming
  error, based on a decision the modifier never actually made.

  Fix: define `SpecDecoNotEvaluated` as the **zero value** of
  `SpecDecoDecision` (integer 0) so the uninitialized state is textually
  distinct from `SpecDecoSkip`. The reconciler's translation table then
  becomes:

  | Decision | Reconciler action on `ConditionSpeculativeDecodingDisabled` |
  |---|---|
  | `SpecDecoNotEvaluated` (zero value; closure never ran) | **leave existing condition untouched** |
  | `SpecDecoSkip` (closure ran, annotation absent or not vLLM) | remove |
  | `SpecDecoInjected` | remove |
  | `SpecDecoConfigMapOverride` | remove + emit `SpeculativeDecodingConfigMapOverride` Event |
  | `SpecDecoPipelineParallelism` | set `True` / reason=`PipelineParallelism` |
  | `SpecDecoUnsupportedPreset` | set `True` / reason=`UnsupportedPreset` |

  The modifier closure is responsible for writing **exactly one**
  non-`NotEvaluated` value on every path it runs; the fall-through
  "annotation absent / not vLLM" branch writes `SpecDecoSkip` explicitly
  instead of relying on the zero value. Test coverage:
  `TestPresetInferenceResult_UpstreamErrorPreservesPriorCondition`
  asserts that a GPU-config resolution failure leaves a pre-existing
  `PipelineParallelism` condition in place across the failed reconcile.
- A `SpeculativeDecodingConfigMapOverride` Event on the reconciler's
  `EventRecorder` when injection was skipped because the ConfigMap already
  carried `vllm.speculative-config` (Step 5). Emission is on the reconciler
  - not the validating webhook - because the webhook is `sideEffects: None`
  and can be invoked for dry-run.

**Why not a `TypedManifestModifier`.** An earlier revision of this proposal
sketched a `newSpeculativeDecodingModifier` factory that returned a
`TypedManifestModifier[WorkspaceGeneratorContext, corev1.PodSpec]`. That
approach cannot work: `TypedManifestModifier` receives `(ctx, *PodSpec)`
and runs **after** `GenerateInferencePodSpec` has already called
`inferenceParam.GetInferenceCommand(...)` and materialized the container's
`Command`/`Args`. A modifier that mutates `inferenceParam.VLLM.ModelRunParams`
at that point is writing to a `PresetParam` copy whose command string has
already been rendered — the injection never reaches the container. Even a
modifier that rewrites `spec.Containers[0].Args` directly would have to
re-implement the shell-quoting done by `GetInferenceCommand`, and would
still lose access to the typed `PresetParam.SpeculativeDecoding` source
of truth.

**Where the decision is set.** Because injection has to live inline in
`GenerateInferencePodSpec` (between the line-578 `DeepCopy()` and the
line-600 `GetInferenceCommand`, per Step 3), the decision-plumbing lives
there too. `GenerateInferencePodSpec` takes an `outDecision *SpecDecoDecision`
argument; `GeneratePresetInference` allocates the decision, threads the
pointer through, and copies the final value into
`PresetInferenceResult.SpeculativeDecodingDecision` after the call.
Each of the `SpecDecoDecision` values — the zero value
`SpecDecoNotEvaluated` plus `SpecDecoSkip`,
`SpecDecoPipelineParallelism`, `SpecDecoConfigMapOverride`,
`SpecDecoInjected`, `SpecDecoUnsupportedPreset` —
is written by exactly one branch of the inline block shown in Step 3.
All writes are guarded by `outDecision != nil` so tuning-path callers and
tests can pass `nil`.

Inline decision-write sites (annotations on the Step 3 code block above):

- `runtimeName != RuntimeNameVLLM` **or** annotation absent: closure
  falls through and writes `*outDecision = SpecDecoSkip` **explicitly**.
  It does NOT rely on the zero value — the zero value
  (`SpecDecoNotEvaluated`) is reserved for "closure never ran" so the
  reconciler can distinguish upstream failures from a recorded skip
  (see "`SpecDecoNotEvaluated`" note above).
- `ws.Status.TargetNodeCount > 1` guard fires: `*outDecision = SpecDecoPipelineParallelism`;
  return without injection. Reconciler translates this into
  `ConditionSpeculativeDecodingDisabled` with reason=`PipelineParallelism`.
- `loadUserSpeculativeConfig` returns `userSpecified == true`:
  `*outDecision = SpecDecoConfigMapOverride`; return without injection.
  Reconciler emits the `SpeculativeDecodingConfigMapOverride` Event.
- Fall-through injection path (writes `ModelRunParams["speculative-config"]`):
  `*outDecision = SpecDecoInjected`.
- Annotation `"true"` but `inferenceParam.SpeculativeDecoding == nil`
  (pre-existing Workspace or catalog-entry removed after upgrade;
  admission does not protect this case):
  `*outDecision = SpecDecoUnsupportedPreset`; return **`nil`** (skip
  injection). The reconciler surfaces `ConditionSpeculativeDecodingDisabled`
  with reason=`UnsupportedPreset`. This is a permanent configuration
  mismatch — a retryable error would create a rate-limited reconcile/log
  loop with no path to self-heal. Recovery happens through the existing
  watches: removing the annotation, changing the preset, or a catalog
  update re-adding the config all enqueue a new reconcile that lands on
  a different branch.

At the call site in `GeneratePresetInference`, the decision must reach the
reconciler on both the success and error paths — `SpecDecoConfigMapOverride`
and `SpecDecoPipelineParallelism` produce the decision alongside a nil
error, and although `SpecDecoUnsupportedPreset` also returns nil (see the
"no-retry" rationale in Step 3), other unrelated errors from later
injection steps (e.g. `vllmFormat` failure) can still leave a valid
decision that the reconciler must translate. Return the result (with the
current decision) alongside the error and let the reconciler translate
the decision **before** propagating the error up:

`GenerateInferencePodSpec` is a modifier **factory** — it does not build
the pod spec directly; it returns a closure that `generator.GenerateManifest`
invokes through `podOpts`
(`pkg/workspace/inference/preset_inferences.go:221-222,284-291`). The
decision therefore has to be threaded through the factory so that the
closure can write into it while running under the generator; the
call site in `GeneratePresetInference` reads the decision **after**
`GenerateManifest` returns and packages it alongside the existing
result/error pair:

```go
// Inside GeneratePresetInference (pkg/workspace/inference/preset_inferences.go).
var decision SpecDecoDecision
podOpts := []generator.PodOption{
    // Pass &decision into the factory so the modifier closure can
    // record which branch it took while GenerateManifest runs it.
    GenerateInferencePodSpec(ctx, wObj, model, /* existing args */, &decision),
    // ... other existing modifiers, unchanged ...
}
workload, err := generator.GenerateManifest(ctx, wObj, podOpts, stsOpts /* unchanged */)
// Always carry the decision back to the caller so the reconciler can
// translate SpecDecoUnsupportedPreset / SpecDecoPipelineParallelism into
// a Workspace condition (and SpecDecoConfigMapOverride into an Event)
// even when GenerateManifest returned an error (e.g. StatefulSet
// generation failure downstream of the modifier that already recorded
// a decision).
result := &PresetInferenceResult{
    Workload:                    workload, // nil on error, non-nil on success
    SpeculativeDecodingDecision: decision,
}
return result, err
```

The existing pod-spec / StatefulSet error returns inside
`GeneratePresetInference` are restructured to preserve `decision` on
both paths (currently they `return nil, err` and would drop the decision
recorded before the failure). Concretely, `GeneratePresetInference`
allocates `result := &PresetInferenceResult{}` at function entry and
every early return — GPU-config resolution, streaming resolution, and
any other pre-`GenerateManifest` failure — returns `result, err` instead
of `nil, err`. This guarantees the reconciler never receives a nil
`result` on the error path.

The reconciler side (`WorkspaceReconciler.applyInference` /
`preparePresetInference`, `workspace_controller.go`) defensively
nil-checks the result before dereferencing (`if result != nil {
... translate result.SpeculativeDecodingDecision ... }`), then
propagates `err` up the reconcile chain (which for
`SpecDecoUnsupportedPreset` is `nil`, so no retry loop; for other
errors, the standard reconcile retry applies). The nil-check is a
belt-and-suspenders guard against future callers that skip the
entry-point allocation; the entry-point allocation itself is what
actually preserves the decision on today's error paths.
As an alternative, a typed error
(`type SpecDecoError struct { Decision SpecDecoDecision; Err error }`)
can carry the decision inside the returned error and satisfy `errors.As`;
the pair-return form above is preferred because it keeps the happy path
symmetric with the failure path.

See the "Reconciler status / event translation" note at the end of Step 3
for the exact status/event translation patch.

```go
commands := inferenceParam.GetInferenceCommand(pkgmodel.RuntimeContext{ /* ... */ })
```

```go
// shellSingleQuote wraps s in single quotes, escaping any embedded
// single quote as '\''. Safe for /bin/sh -c "cmd --key=<value>".
func shellSingleQuote(s string) string {
    return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

Where `vllmFormat` converts the typed Go struct into the JSON shape vLLM expects and fails loud on unknown methods:

```go
func vllmFormat(sd *pkgmodel.SpeculativeDecodingConfig) (string, error) {
    // Nil guard: `SpeculativeDecodingConfig` is declared in `pkg/model`,
    // and this helper lives in `pkg/workspace/inference`. Direct callers
    // could pass nil (e.g. before the runtime gate is evaluated); return
    // a typed error rather than panicking on the `sd.Method` dereference
    // below, matching the "malformed configuration returns a typed error"
    // guarantee documented above.
    if sd == nil {
        return "", fmt.Errorf("vllmFormat: SpeculativeDecodingConfig is nil")
    }
    m := map[string]any{"method": sd.Method}
    switch sd.Method {
    case "mtp":
        if sd.MTP == nil {
            return "", fmt.Errorf("method=mtp requires mtp config")
        }
        m["num_speculative_tokens"] = sd.MTP.NumSpeculativeTokens
    case "ngram":
        if sd.NGram == nil {
            return "", fmt.Errorf("method=ngram requires ngram config")
        }
        m["num_speculative_tokens"] = sd.NGram.NumSpeculativeTokens
        m["prompt_lookup_max"]      = sd.NGram.PromptLookupMax
    case "dspark":
        if sd.DSpark == nil {
            return "", fmt.Errorf("method=dspark requires dspark config")
        }
        // dspark ships two ways, discriminated by DSpark.Variant:
        //  1. "fused" (default): the DSpark module is baked into the
        //     served checkpoint (e.g. DeepSeek-V4-Flash-0731,
        //     DeepSeek-V4-Flash-DSpark). vLLM's recipe emits a
        //     method-only blob (`{"method":"dspark", ...}`) with no
        //     `model` field. Served-checkpoint identity is tracked on
        //     the preset (`CatalogEntry.Name` / preset image), not on
        //     the speculative-config blob. Catalog validation requires
        //     `Model == ""` for this variant.
        //  2. "assistant": a separate DSpark assistant loaded alongside
        //     a distinct base model. Catalog validation requires
        //     `Model != ""`; runtime emits the `model` field.
        // The Variant discriminator is authoritative; do NOT branch on
        // `Model` alone at runtime because "" is a valid fused value.
        if sd.DSpark != nil && sd.DSpark.Variant == "assistant" {
            m["model"] = sd.DSpark.Model
        }
        if sd.DSpark != nil && sd.DSpark.NumSpeculativeTokens > 0 {
            m["num_speculative_tokens"] = sd.DSpark.NumSpeculativeTokens
        }
        if sd.DSpark != nil && sd.DSpark.DraftSampleMethod != "" {
            m["draft_sample_method"] = sd.DSpark.DraftSampleMethod
        }
        if sd.DSpark != nil && sd.DSpark.AttentionBackend != "" {
            m["attention_backend"] = sd.DSpark.AttentionBackend
        }
    default:
        // Belt-and-suspenders: caught earlier by the catalog-time
        // validation over `speculativeDecodingByPreset`
        // (the source of truth per Per-Preset Config Ownership), but
        // never silently emit a method-only blob at runtime.
        return "", fmt.Errorf("unsupported speculative decoding method %q", sd.Method)
    }
    b, err := json.Marshal(m)
    if err != nil {
        return "", err
    }
    return string(b), nil
}
```

#### Step 4 - Admission webhook validates the annotation

Wired into both `v1alpha1` and `v1beta1` webhooks, on **both create and update** - `v1beta1.ValidateCreate` / `validateAnnotations` currently skips the update branch, so a Workspace could otherwise be created without the annotation and then have it added later, bypassing all of these checks. The design requires the check to run on `ValidateCreate` **and** `ValidateUpdate` for both served versions.

```go
var validAnnotationValues = map[string]bool{"true": true, "false": true}

func validateSpeculativeDecoding(ws *Workspace) error {
    val, present := ws.Annotations["kaito.sh/enable-speculative-decoding"]
    if !present || val == "false" {
        return nil
    }
    if !validAnnotationValues[val] {
        // Non-boolean values (typos, "1", "yes", etc.) must not be silently treated as
        // disabled - that would defeat the admission-time feedback promise.
        return fmt.Errorf(
            "annotation kaito.sh/enable-speculative-decoding has invalid value %q; "+
            "expected \"true\" or \"false\"",
            val,
        )
    }

    // (a) Workspace must actually run a preset inference. The webhook
    //     aggregates errors and can invoke annotation checks even for
    //     tuning-only or malformed objects, so guard the pointer chain
    //     before dereferencing.
    if ws.Inference == nil || ws.Inference.Preset == nil || ws.Inference.Preset.Name == "" {
        return fmt.Errorf(
            "kaito.sh/enable-speculative-decoding requires a preset inference; "+
            "remove the annotation or set inference.preset.name",
        )
    }

    // (b) Preset must have a validated config in the catalog.
    //
    //     Resolve membership in the local supported-preset map BEFORE calling
    //     `models.GetModelByName`. Rationale: for any preset that is not a
    //     legacy alias and not already registered, `GetModelByName` falls
    //     through to `generateHuggingFaceModel`, which performs outbound
    //     HuggingFace requests with 30-second timeouts and registers the
    //     result (`presets/workspace/models/vllm_model.go:129-166`,
    //     `presets/workspace/generator/generator.go:351-365`). Doing that
    //     inside the admission webhook would (i) tie admission latency and
    //     availability to huggingface.co for an object we will ultimately
    //     reject, and (ii) let unauthenticated Workspace CREATE traffic
    //     trigger unbounded catalog generation inside the webhook process
    //     just by supplying arbitrary HF IDs with the annotation set.
    //
    //     `SupportedSpeculativeDecodingPresets()` returns the sorted
    //     user-facing names as stored in the `speculativeDecodingByPreset`
    //     map. Because the initial entries key on legacy short aliases
    //     (`deepseek-r1-0528`) while future catalog-native entries key
    //     on full HF IDs (`deepseek-ai/deepseek-v3.2`), a naive string
    //     equality check would reject the *canonical* form of legacy
    //     presets (e.g. a user who spells out `deepseek-ai/deepseek-r1-0528`
    //     verbatim) even though that name would resolve fine through
    //     `models.GetModelByName`. It would also reject mixed-case
    //     variants of the full ID.
    //
    //     Normalize both sides before comparing:
    //       - lowercase the user-supplied preset name
    //       - for each supported name S, compare against both
    //         `strings.ToLower(S)` AND
    //         `strings.ToLower(plugin.LegacyBuiltinToCatalog[S])`
    //         (the latter is the canonical full HF ID when S is a legacy
    //         alias; empty string otherwise, which naturally never matches).
    //     This keeps the accessor's return shape untouched (still the
    //     sorted user-facing names, used verbatim in error messages) and
    //     costs one map lookup per entry.
    supported := generator.SupportedSpeculativeDecodingPresets()
    presetName := ws.Inference.Preset.Name
    presetLower := strings.ToLower(presetName)
    presetSupported := false
    for _, s := range supported {
        if strings.ToLower(s) == presetLower {
            presetSupported = true
            break
        }
        // Canonical full-HF-ID form for legacy short aliases. When s is
        // itself a full HF ID (catalog-native entry), the map lookup
        // returns "" and the comparison is a safe no-op.
        if canonical := plugin.LegacyBuiltinToCatalog[s]; canonical != "" &&
            strings.ToLower(canonical) == presetLower {
            presetSupported = true
            break
        }
    }
    if !presetSupported {
        return fmt.Errorf(
            "preset %q does not have a validated speculative decoding configuration; "+
            "remove kaito.sh/enable-speculative-decoding annotation or choose a "+
            "supported preset (currently: %s)",
            presetName, strings.Join(supported, ", "),
        )
    }

    //     Only after the preset is known-supported do we call
    //     `models.GetModelByName` — supported presets are shipped in the
    //     catalog and resolve without any HuggingFace round-trip. The
    //     result is still used to double-check `SpeculativeDecoding` on
    //     the resolved `PresetParam` (defence-in-depth against a supported
    //     entry whose catalog config was pruned).
    //
    //     A direct KaitoModelRegister.Get/MustGet would bypass alias
    //     normalization and fail on short preset names, so we still route
    //     through `models.GetModelByName` here.
    //
    //     The repository API is:
    //       models.GetModelByName(ctx, modelName, secretName, secretNamespace, client)
    //     `ModelAccessSecret` is a v1beta1-only field on
    //     `v1beta1.PresetOptions`; v1alpha1's `PresetOptions` does not
    //     have it (see `api/v1alpha1/workspace_types.go`). To keep the
    //     helper version-neutral, take the secret name as an explicit
    //     input parameter rather than dereferencing a preset-typed
    //     field. Callers pass:
    //       - v1beta1: ws.Inference.Preset.PresetOptions.ModelAccessSecret
    //       - v1alpha1: "" (empty string - no per-preset secret)
    //     Both variants normalize alias rewrites via GetModelByNameWithToken
    //     internally.
    resolved, err := models.GetModelByName(
        ctx,
        presetName,
        accessSecret, // explicit input, see helper signature comment above
        ws.Namespace,
        webhookClient,
    )
    if err != nil || resolved == nil {
        return fmt.Errorf(
            "preset %q could not be resolved; "+
            "remove kaito.sh/enable-speculative-decoding annotation or choose a supported preset",
            presetName,
        )
    }
    params := resolved.GetInferenceParameters()
    if params == nil || params.SpeculativeDecoding == nil {
        // Supported list said yes but the resolved catalog entry has no
        // SpeculativeDecoding config; treat as an internal catalog drift
        // and fail closed so the mismatch is caught at admission time
        // rather than at reconcile.
        return fmt.Errorf(
            "preset %q is listed as supporting speculative decoding but the resolved "+
            "catalog entry has no SpeculativeDecoding config; "+
            "this is a KAITO catalog inconsistency, please file an issue",
            presetName,
        )
    }

    // (c) Runtime must resolve to vLLM. A supported preset can explicitly
    //     select the Transformers runtime (or run with the vLLM feature
    //     gate disabled), in which case --speculative-config would never
    //     be honored.
    if GetWorkspaceRuntimeName(ws) != model.RuntimeNameVLLM {
        return fmt.Errorf(
            "kaito.sh/enable-speculative-decoding requires the vLLM runtime; "+
            "preset %q is configured for a different runtime",
            ws.Inference.Preset.Name,
        )
    }

    // (d) Reject pipeline parallelism only for methods that don't
    //     compose with PP in vLLM.
    //     Rationale: KAITO's cross-node runtime today is pipeline
    //     parallelism (see workspace_controller.go lines 1578-1621, where
    //     multi-node presets are resolved and PP is what gets configured
    //     when the resolved node count > 1). Whether spec-decoding can
    //     ride on top of PP is method-specific:
    //
    //       * ngram: CPU-side prompt lookup, does not touch the target
    //         model's execution graph, composes with PP (reduced speedup).
    //       * mtp: MTP heads are baked into the DeepSeek-V3/R1 checkpoint
    //         and sharded together with the model; vLLM supports MTP
    //         under PP. This is required for correctness because
    //         DeepSeek-V3 / R1 (671B) physically need multi-node PP to
    //         serve at all - a blanket rejection would make those
    //         `speculativeDecodingByPreset` entries unreachable.
    //       * eagle / eagle3: vLLM's implementation assumes the draft
    //         head co-locates with a TP-sharded target; EAGLE-3 also
    //         fuses shallow/middle/deep target features that would
    //         cross PP stages. Not supported upstream, so we reject.
    //
    //     The gate is a per-method decision, not a blanket count-based
    //     one. The truth table lives in one place
    //     (generator.SpeculativeDecodingMethodSupportsPipelineParallelism)
    //     so admission and pod-spec injection cannot drift.
    //
    //     resource.count is NOT the resolved vLLM node count - the
    //     controller computes status.targetNodeCount from model size and
    //     SKU AFTER admission. A nil or 1 count can still become
    //     multiple nodes and pull in PP. So we enforce PP compatibility
    //     using the same estimator the controller uses, and add a
    //     reconcile-time guard before injection.
    //
    //     Admission time (this webhook): resolve the method that the
    //     pod-spec layer would inject (via
    //     generator.ResolveSpeculativeDecodingMethod, falling back to
    //     "ngram" when the preset is not in speculativeDecodingByPreset)
    //     and reject if the resource estimator says the preset will run
    //     under PP for the resolved SKU AND the resolved method is not
    //     PP-compatible.
    //     Reconcile time (Step 3 injection site): re-check
    //     status.targetNodeCount against the same method-aware truth
    //     table and refuse to inject if the current method is not
    //     PP-compatible, emitting a Workspace condition explaining why.
    //
    //     The admission webhook cannot import
    //     `pkg/workspace/estimator/nodesestimator` directly - that package
    //     already imports `api/v1beta1`, so importing it from the v1beta1
    //     admission webhook would create an import cycle. Follow the same
    //     wiring pattern the codebase already uses for
    //     `ValidateInferenceSetWorkspace`: expose an injected callback
    //     (`EstimateTargetNodeCountFn func(ws *v1beta1.Workspace) (int, error)`)
    //     that the controller sets at startup to a thin adapter over the
    //     real estimator. The webhook holds the func pointer and calls
    //     it here as `estimateTargetNodeCount(ws)`; the v1alpha1 webhook
    //     wires the same func through the shared version-neutral helper.
    //     If future refactoring moves the estimator into an
    //     API-independent package, the callback can be replaced with a
    //     direct import - the webhook site does not change.
    //
    //     The callback MUST perform the SAME request+profile construction
    //     the reconciler does in `UpdateWorkspaceTargetNodeCount`
    //     (`workspace_controller.go:1580-1600`), specifically the
    //     `RuntimeProfile{ContextSize: ...}` derived from the inference
    //     ConfigMap's `max-model-len`. If the admission callback only
    //     built a bare estimator request while the reconciler layered the
    //     ConfigMap-derived context size on top, they would disagree:
    //     admission would accept a single-node deployment while the
    //     reconciler recomputed multiple nodes at a smaller context and
    //     silently disabled the requested feature.
    //
    //     To eliminate that skew, factor the request/profile construction
    //     into a shared helper used by both call sites:
    //
    //     ```go
    //     // pkg/workspace/estimator/estimator.go (or a sibling package the
    //     // reconciler already imports - must NOT import api/v1beta1 to
    //     // avoid the same cycle the webhook faces).
    //     func BuildNodeEstimateInputs(
    //         ctx context.Context,
    //         c client.Client,
    //         wObj *WorkspaceLike, // narrow interface exposing Namespace,
    //                              // Preset name, Resource.InstanceType,
    //                              // Resource.Count, Inference.Config
    //     ) (Request, RuntimeProfile, error)
    //     ```
    //
    //     Both `UpdateWorkspaceTargetNodeCount` and the injected
    //     `EstimateTargetNodeCountFn` adapter call `BuildNodeEstimateInputs`
    //     before invoking the estimator, so the ConfigMap `max-model-len`
    //     parse and any future runtime-profile fields land in one place.
    //     `WorkspaceLike` is a thin interface (not a v1beta1 import) so the
    //     estimator package continues to avoid `api/v1beta1`; the webhook
    //     adapter converts `*v1beta1.Workspace` to `WorkspaceLike` before
    //     calling in.
    targetNodes, err := estimateTargetNodeCount(ws)
    if err != nil {
        return fmt.Errorf(
            "kaito.sh/enable-speculative-decoding: failed to estimate node count "+
            "for preset %q on SKU %q: %w; cannot validate pipeline-parallelism "+
            "compatibility - resolve the estimation error or remove the annotation",
            ws.Inference.Preset.Name, ws.Resource.InstanceType, err,
        )
    }
    if targetNodes > 1 {
        method := generator.ResolveSpeculativeDecodingMethod(string(ws.Inference.Preset.Name))
        if !generator.SpeculativeDecodingMethodSupportsPipelineParallelism(method) {
            return fmt.Errorf(
                "kaito.sh/enable-speculative-decoding method %q is incompatible with "+
                "multi-node distributed inference (pipeline parallelism); "+
                "preset %q on SKU %q resolves to %d nodes",
                method, ws.Inference.Preset.Name, ws.Resource.InstanceType, targetNodes,
            )
        }
        // ngram / mtp fall through: reduced speedup for ngram (optional
        // Warning event may be emitted by the reconciler), full support
        // for mtp because MTP heads are sharded with the target.
    }

    return nil
}
```

#### Step 4.5 - Rollout detection must include the annotation

Admission-time validation on `ValidateUpdate` catches invalid values, but is not sufficient to actually apply the change to an existing Workspace. The reconciler decides whether to re-roll the StatefulSet by comparing a revision hash, and the current implementation ignores annotations entirely:

- `ComputeHash(ws)` (`pkg/utils/consts/workspace_controller.go:566-572`) hashes only `ws.Spec.Resource`, `ws.Spec.Inference`, and `ws.Spec.Tuning`. Annotations are not part of the input.
- `addOrUpdatePresetInference` (`pkg/utils/consts/workspace_controller.go:714-720`) reads the previous revision hash off the StatefulSet, compares it to the freshly computed one, and returns early with no update when they match.

Without further changes, flipping `kaito.sh/enable-speculative-decoding` from `"false"` (or absent) to `"true"` on a running Workspace would (a) pass admission, (b) leave `ComputeHash` unchanged, (c) short-circuit `addOrUpdatePresetInference`, and (d) never regenerate the StatefulSet command - so `--speculative-config` would never be added to the vLLM args, and the annotation would appear to have taken effect while doing nothing.

Two acceptable fixes; this proposal commits to (a) because it is smaller and localized:

**(a) Fold the annotation into `ComputeHash`.** Extend the hashed struct with a `SpecDecoAnnotation string` field populated from `ws.Annotations["kaito.sh/enable-speculative-decoding"]`. Any flip — present↔absent, `"true"`↔`"false"` — changes the revision hash, so `addOrUpdatePresetInference` proceeds with a normal StatefulSet update via the standard rollout path (the existing `updateStrategy` still governs pod replacement; no bespoke rollout code).

**InferenceSet annotation propagation on update.** `NewWorkspaceForInferenceSet` copies `spec.template.metadata.annotations` only at child Workspace creation time. The current InferenceSet reconciler updates only labels on existing children (`pkg/inferenceset/inferenceset_controller.go:389-425`) and does not propagate template annotation changes. This proposal extends the InferenceSet reconciler’s update path to reconcile **only the `kaito.sh/enable-speculative-decoding` key** on existing child Workspaces — not replace the full annotation map, which would erase controller-owned annotations (e.g. revision hash, workspace-revision). Specifically:

- If the key is present in `spec.template.metadata.annotations`, set it on the child.
- If the key is absent (or removed), delete only that key from the child.
- All other annotations on the child Workspace are left untouched.

**Preset changes must be reconciled before (or atomically with) the annotation.** The InferenceSet template preset is currently mutable (`api/v1beta1/inferenceset_validation.go:75-82` allows it), but existing children retain the old preset until the reconciler rolls them forward. If the same update flips both `spec.template.spec.inference.preset` **and** the annotation to `"true"` — e.g. moving to an unsupported preset while enabling the annotation — selectively syncing the annotation onto an old child whose preset is still unsupported would trip the child Workspace webhook (which validates the preset/annotation pair) and stall reconciliation on that child. Two acceptable ways to close this gap; this proposal commits to (i):

  (i) **Recreate the child Workspace when the template preset changes, one at a time.**
  `InferenceSpec.Preset` is enforced immutable on the child by
  `api/v1beta1/workspace_validation.go:895-897` (`if !reflect.DeepEqual(i.Preset, old.Preset) { errs = errs.Also(apis.ErrGeneric("field is immutable", "preset")) }`), so an in-place patch that mutates both preset and annotation cannot succeed on the child — even atomically. The InferenceSet reconciler therefore treats a template preset change as a child-lifecycle event.

  **Sequencing (new invariant — not reusing the surplus-replica bulk path).** `InferenceSet.spec.updateStrategy` is declared (`api/v1beta1/inferenceset_types.go:140-145`) but not consumed by the controller today, and the existing surplus-replica path (`pkg/inferenceset/inferenceset_controller.go:326-357`) computes the full excess count and iterates *every* selected Workspace in a single reconcile — it does **not** enforce one-at-a-time deletion. Preset migration therefore introduces a **new** invariant that this proposal owns: the reconciler deletes exactly one preset-mismatched child per reconcile, requeues, and refuses to delete a second one until the previously deleted child's replacement (created on the next reconcile via `NewWorkspaceForInferenceSet` with the new preset + annotation copied together) reaches `Ready`. Concretely:

  1. Enumerate children whose `spec.inference.preset` does not match the template. Call this set `M`.
  2. Count children currently in `Deleting` due to a prior preset migration (identified by a controller-owned annotation, e.g. `kaito.sh/inferenceset-preset-migration=<template-hash>`, set on the child immediately before deletion). If ≥ 1, requeue and stop — do not delete another.
  3. If 0 are deleting, pick the first (stable order) child in `M`, annotate it with `kaito.sh/inferenceset-preset-migration=<template-hash>`, delete it, requeue.
  4. On subsequent reconciles, only advance to the next `M` member once the replacement child (matched by owner ref + template preset + `Ready` condition) exists. Requeue in between.

  This is deliberately **not** reusing the surplus-deletion loop — that loop batches deletions and would take multiple replicas down together on a preset flip. If future work wires `updateStrategy.maxUnavailable` / `maxSurge` through the controller, this recreate path becomes the surge=0 / maxUnavailable=1 special case of the general strategy.

  The selective annotation-only sync path documented above continues to run only when the preset is unchanged. Test coverage: `TestInferenceSetReconcile_PresetChange_RecreatesChildrenSerially` asserts (1) at most one child is in `Deleting` at a time across a preset flip that affects N replicas, (2) a new child is only created after the previous one is `Ready`, (3) the new child carries both the new preset and the current template annotation, and (4) the surplus-replica code path is not invoked during preset migration.

  (ii) **Immutability at the parent.** Alternatively, tighten `api/v1beta1/inferenceset_validation.go` to forbid preset changes on `spec.template.spec.inference.preset` and require a fresh InferenceSet. This is more restrictive than current behavior and is called out only as a fallback if the recreate-on-preset-change path in (i) proves too disruptive to running inference.

Test coverage: `TestInferenceSetReconcile_AnnotationFlip_PropagatesToExistingChildren` (verifies the key is synced) and `TestInferenceSetReconcile_AnnotationSync_PreservesControllerAnnotations` (verifies revision/hash annotations survive).

**The selective update path must also copy `Command`.** The revision-mismatch branch in `applyInference` (`workspace_controller.go:733–740`) currently copies only `Env`, `VolumeMounts`, `InitContainers`, and `Volumes` from the desired pod spec — it does not copy `Containers[0].Command`, where `--speculative-config` is rendered. Without this change, the hash would differ (triggering an update), but the StatefulSet pod template would retain the old command and pods would never receive the new flag. The fix extends the selective update to also set `spec.Containers[0].Command = desiredPodSpec.Containers[0].Command` (and `Args` for completeness), ensuring the rendered vLLM flags are applied alongside the other mutable fields.

**(b) Compare the annotation explicitly at update time.** Load the current StatefulSet, parse the `--speculative-config` flag out of its command, and force an update when it disagrees with what the current Workspace would render. Larger change (introduces bespoke parsing of the runtime command); called out as an alternative if a future feature needs to trigger a rollout without changing the hash.

**Selected-revision data should include the annotation for audit completeness.** `addOrUpdatePresetInference` (and the reconciler's selected-revision bookkeeping) stores the rendered revision alongside the hash so operators can inspect what a given revision represented. Note that this is **not** required for correctness of the rollout comparison itself: `syncControllerRevision` computes the hash and revision name from the live Workspace object each reconcile, and the rollout-comparison path reads the revision-name annotation, not `ControllerRevision.Data`. Folding the annotation into `ComputeHash` (part (a) above) is what actually prevents a spurious re-roll after controller restart or hash re-computation — the stored revision struct is never fed back into the hash. Nevertheless, adding the same `SpecDecoAnnotation` field to the revision struct keeps `kubectl describe controllerrevision` self-explanatory ("why did this revision exist?") and matches how other flag-affecting fields are recorded; the reconciler writes both together.

**Test coverage for the rollout path** (added at the same time as the admission tests in Step 6):

- `TestReconcile_SpecDecoAnnotation_TriggersRollout`: create Workspace without the annotation, reconcile to a stable StatefulSet, add `kaito.sh/enable-speculative-decoding: "true"`, reconcile, assert the StatefulSet's `pod-template-hash`/revision annotation changed and the container command now contains `--speculative-config=`.
- `TestReconcile_SpecDecoAnnotation_FlipToFalse_TriggersRollout`: symmetric case, `"true"` → `"false"` (or removed) drops the flag.
- `TestReconcile_SpecDecoAnnotation_NoOp_WhenUnchanged`: repeated reconciles with the same annotation value do NOT trigger extra rollouts (hash still stable).
- `TestNewWorkspaceForInferenceSet_PropagatesSpecDecoAnnotation`: annotation on `InferenceSet.spec.template.metadata.annotations` lands on child Workspace and participates in `ComputeHash`.

#### Step 5 - Precedence when the user also sets `speculative-config` in a ConfigMap

The existing `inference_config.yaml` passthrough (`vllm.speculative-config: '...'`) stays. When both are present:

- **ConfigMap wins at render time.** The preset controller skips its own `--speculative-config` injection if the user's ConfigMap already contains a `speculative-config` key under `vllm:`. This preserves the power-user escape hatch (sweep `num_speculative_tokens`, try `eagle`) without producing two conflicting `--speculative-config` flags on the vLLM command line.

  **Limitation: in-place ConfigMap edits are not automatically detected.** The Workspace controller does not watch referenced ConfigMaps today, and editing a ConfigMap in place changes neither the Workspace revision hash nor the config name, so even an incidental reconcile returns early for the unchanged revision. This is a pre-existing limitation that affects all ConfigMap-driven overrides, not just speculative decoding. Two mitigations are proposed:

  1. **Short-term (this proposal):** Document that users must trigger a rollout explicitly after editing the ConfigMap (e.g., by toggling the annotation or updating the ConfigMap reference name). The `SpeculativeDecodingConfigMapOverride` Event is emitted at render time — if the user adds `vllm.speculative-config` to an existing ConfigMap without triggering a re-render, the annotation-injected flag remains in effect until the next rollout.
  2. **Follow-up (out of scope):** Add a ConfigMap watch to the Workspace controller so that data changes on referenced ConfigMaps trigger a reconcile. This benefits all ConfigMap-driven overrides and is tracked separately.
- The admission webhook still enforces (b) - the annotation still requires a supported preset. This keeps the failure mode consistent regardless of ConfigMap contents.
- When both sources are present, the **reconciler** emits a Kubernetes Event (`SpeculativeDecodingConfigMapOverride`) on the Workspace. The Event is emitted from `WorkspaceReconciler`, which is the type that holds the `record.EventRecorder` — not from inside `GenerateInferencePodSpec`, whose `WorkspaceGeneratorContext` (`pkg/utils/generator/generator.go:30`) has only `Ctx / Workspace / Model / KubeClient / NodeProvisioner` and no recorder. The injection function signals its decision on the `PresetInferenceResult` struct returned by `GeneratePresetInference` (see "Reconciler wiring for the skip signal" at the end of Step 3); the reconciler translates that signal into the Event using its own recorder. Events are only emitted from the reconcile path — not the validating webhook — because the webhook is declared with `sideEffects: None` (`charts/kaito/workspace/templates/webhooks.yaml`) and can be invoked for dry-run or retried admission requests. Emitting an Event from admission would violate the webhook contract and could produce duplicate or spurious events; the reconcile-time signal may fire on repeated reconciliations of the same generation (status updates, child-resource watches, retries). Kubernetes Events are inherently aggregated — the API server deduplicates events with the same reason/message within a window — so the emission is safe without explicit idempotency tracking. It shows up in `kubectl describe workspace` / `kubectl get events`.

### InferenceSet Support

For InferenceSet, the annotation goes on `spec.template.metadata.annotations`:

```yaml
apiVersion: kaito.sh/v1alpha1
kind: InferenceSet
metadata:
  # Scaling annotations belong on the InferenceSet itself.
  annotations:
    scaledobject.kaito.sh/auto-provision: "true"
    scaledobject.kaito.sh/metricName: "vllm:num_requests_waiting"
    scaledobject.kaito.sh/threshold: "10"
  name: deepseek-r1
  namespace: default
spec:
  replicas: 2
  nodeCountLimit: 5
  labelSelector:
    matchLabels:
      apps: deepseek-r1
  template:
    metadata:
      # ← Per-Workspace annotation goes here. Propagated verbatim to every
      #   child Workspace by NewWorkspaceForInferenceSet.
      annotations:
        kaito.sh/enable-speculative-decoding: "true"
    inference:
      preset:
        accessMode: public
        name: deepseek-r1-0528
    resource:
      instanceType: Standard_ND96isr_H200_v5
```

`kubectl apply -f` and the InferenceSet controller creates `replicas` Workspaces, each with `kaito.sh/enable-speculative-decoding: "true"` in its own annotation map. Each child then goes through the exact same preset-controller injection and admission-webhook validation flow.

#### Which Annotations Go Where

| Annotation location | Purpose | Reaches child Workspace? |
|---|---|---|
| `InferenceSet.metadata.annotations` | Cluster-level policy on the InferenceSet itself (e.g. `scaledobject.kaito.sh/*` autoscaling) | ❌ No - controller-scoped |
| `InferenceSet.spec.template.metadata.annotations` | Per-Workspace behavior (**this is where `kaito.sh/enable-speculative-decoding` goes**) | ✅ Yes - cloned to each child Workspace |

#### Rejection Semantics for InferenceSet

Rejection must happen at `apply` time for **both** served API versions:

- **`v1beta1`**: the InferenceSet webhook already projects the template through `NewWorkspaceForInferenceSet` (which is `v1beta1`-typed: it accepts `*v1beta1.InferenceSet` and returns `v1beta1.Workspace`) and runs `Workspace.ValidateCreate`. `validateSpeculativeDecoding` (Step 4) is invoked as part of that projection, so an unsupported template preset is rejected at `kubectl apply -f <InferenceSet>` - no reconcile-time surprise.
- **`v1alpha1`**: the current webhook does **not** project the templated child. Because `NewWorkspaceForInferenceSet` is `v1beta1`-typed, the `v1alpha1` webhook cannot call it directly. Two acceptable implementations; this proposal recommends option (a):
  - **(a) Shared version-neutral helper.** Factor the annotation + preset + runtime + PP checks out of `Workspace.ValidateCreate` into a helper that takes the annotation map, preset name, runtime name, and resource spec - no version-typed dependency. Both webhooks call it. This avoids introducing a conversion round-trip on the admission hot path.
  - **(b) Explicit v1alpha1 → v1beta1 conversion.** Convert `*v1alpha1.InferenceSet` to `*v1beta1.InferenceSet` (either via the existing conversion webhook or an in-webhook shim), then reuse `NewWorkspaceForInferenceSet` + `ValidateCreate`. Larger blast radius; only pick this if the annotation set grows beyond what a small helper cleanly captures.

Either way, rejection happens at `apply` time on both served versions, and the create/update parity requirement from Step 4 (`ValidateCreate` **and** `ValidateUpdate`) applies here too - an existing InferenceSet must not be able to add the annotation post-hoc and bypass the check.

#### Scaling Implication (Unchanged)

Speculative decoding is a **per-replica** speedup - MTP verifies within a single vLLM engine. Turning it on across an InferenceSet's replicas just means every replica gets the same per-request latency win. It does **not** share draft state across replicas and does **not** replace autoscaling - you still want KEDA / auto-provision to grow replicas under high QPS, because the throughput of speculative decoding degrades toward 1.0× as QPS climbs. The two features are complementary.

### MultiRoleInference Support

For MultiRoleInference, the annotation goes on the top-level `metadata.annotations` (there is no `spec.template` at the MRI layer — the MRI controller synthesises one InferenceSet per role from `spec.roles`, and MRI-level annotations are what propagate onto each generated InferenceSet's `spec.template.metadata.annotations`):

```yaml
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: deepseek-r1-pd
  namespace: default
  # ← Speculative-decoding opt-in goes here. Propagated verbatim onto each
  #   child InferenceSet's Spec.Template.Annotations by the MRI controller,
  #   which the InferenceSet controller then clones onto every Workspace.
  annotations:
    kaito.sh/enable-speculative-decoding: "true"
spec:
  labelSelector:
    matchLabels:
      apps: deepseek-r1-pd
  model:
    name: deepseek-r1-0528
  roles:
  - type: prefill
    replicas: 1
    instanceType: Standard_ND96isr_H200_v5
  - type: decode
    replicas: 1
    instanceType: Standard_ND96isr_H200_v5
```

Both the prefill and the decode role get the same `--speculative-config` injected into their vLLM command line. Because MTP verifies within a single vLLM engine, this remains a per-replica speedup on each side of the disaggregation — it does **not** couple the prefill and decode engines' draft state.

#### Propagation Chain

```
MRI.metadata.annotations
  → InferenceSet.spec.template.metadata.annotations   (MRI controller: pkg/controllers/multiroleinference/controller.go, cloneMRIAnnotationsOntoTemplate)
    → Workspace.metadata.annotations                  (InferenceSet controller: pkg/utils/inferenceset/inferenceset.go, NewWorkspaceForInferenceSet)
      → vLLM `--speculative-config` flag              (preset controller: pkg/workspace/inference/preset_inferences.go, applySpeculativeDecoding)
```

Runtime injection happens **once**, at the Workspace layer. Neither the MRI controller nor the InferenceSet controller inspects the speculative-decoding annotation — they just propagate it verbatim. This keeps the injection code path and its test surface single-owner (Step 3 in this proposal).

#### Which Annotations Go Where

| Annotation location | Purpose | Reaches child Workspace? |
|---|---|---|
| `MultiRoleInference.metadata.annotations` | Fleet-level per-Workspace behavior propagated across **both** prefill and decode roles (**this is where `kaito.sh/enable-speculative-decoding` goes**) | ✅ Yes - cloned onto every generated InferenceSet's `spec.template.metadata.annotations`, then onto every Workspace |

(MRI has no top-level `spec.template` and no separate per-role annotation surface today — a per-role opt-in would require an API addition and is called out as a follow-up below.)

#### Rejection Semantics for MultiRoleInference

MRI is served as `v1alpha1`. The MRI admission webhook mirrors the version-neutral speculative-decoding helper documented in the InferenceSet section (option (a)): it reads `metadata.annotations[kaito.sh/enable-speculative-decoding]` together with `spec.model.name` and `metadata.annotations[kaito.sh/runtime]`, and rejects at `kubectl apply -f <MultiRoleInference>` time if the annotation value is malformed, the model is empty, the preset is not in `SupportedSpeculativeDecodingPresets()`, or the runtime is not vLLM. Both `ValidateCreate` and `ValidateUpdate` invoke the check — an existing MRI cannot add the annotation post-hoc and bypass validation, matching the create/update parity requirement from Step 4.

Defence-in-depth also applies: even if a malformed MRI slipped past its own webhook, the generated InferenceSets and Workspaces each re-run their own speculative-decoding validation on admission (Step 4 for Workspace; the InferenceSet variant documented above). The MRI-level check exists so users see the rejection at the top of the resource chain instead of tracing it through synthesised children.

#### Per-Role Opt-In (Deferred)

A reasonable follow-up is to let the user opt in per role — e.g. decode only, prefill only — to isolate the throughput/latency tradeoff. Today `MultiRoleInferenceRoleSpec` has no annotation map, so this would require an API addition (e.g. `RoleSpec.Annotations` merged with the top-level MRI annotations by the MRI controller). Called out here so the current "all roles or none" semantics are an explicit design choice, not an oversight.

## Model Coverage

Cross-referencing the KAITO preset catalog ([`presets/workspace/models/model_catalog.yaml`](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/model_catalog.yaml)) against vLLM's speculative-decoding docs ([features/speculative_decoding/](https://github.com/vllm-project/vllm/tree/main/docs/features/speculative_decoding)) gives a clear picture of what this proposal ships versus what could be layered on later.

### Committed (Initial Preset Coverage)

The initial shipping list favors presets that (a) are already in `model_catalog.yaml`, (b) do not need a separately sourced draft checkpoint, and (c) have upstream evidence (vLLM benchmark, vLLM recipe, or vendor release notes) that speculative decoding gives a net win on realistic KAITO-shape workloads.

| KAITO preset | HF ID | In KAITO catalog? | Method | `num_speculative_tokens` | Extra memory / download |
|---|---|---|---|---|---|
| `deepseek-r1-0528` | `deepseek-ai/DeepSeek-R1-0528` | ✅ Yes | `mtp` | 1 | none - MTP head is in the checkpoint |
| `deepseek-v3-0324` | `deepseek-ai/DeepSeek-V3-0324` | ✅ Yes | `mtp` | 1 | none - same |

The initial ship list is intentionally scoped to `deepseek-r1-0528` and `deepseek-v3-0324` (as declared in the PR summary): both are already in the catalog, both use the self-contained-head `mtp` path with `MTPConfig` as defined in Step 1, and both have KAITO-shape upstream evidence (vLLM MTP benchmark #12755). Any additional preset in the initial catalog PR would need its own required-catalog-test coverage without expanding proposal scope.

Other in-catalog DeepSeek presets (`deepseek-v3.2` and any subsequent V3 point-releases sharing the same in-checkpoint MTP path) are the immediate follow-up candidates - same code path, same `MTPConfig`, no new type work - and are called out in **Free-to-Onboard Next** below.

DeepSeek-V4-Flash-0731 and V4-Pro-0813 are DSpark candidates. Per the vLLM recipe, V4-Flash-0731 carries a **fused** DSpark module in its served checkpoint - no assistant checkpoint sourcing needed - so it lands in **Ready to Onboard (`dspark`, DeepSeek-V4 Family)** below as a fused `DSparkConfig` (no `Model` field). V4-Pro-0813 is deferred until the upstream V4-Pro recipe pins the exact fused-vs-assistant shape.

**Scope note for the PR description.** The PR summary phrases V4-Flash-0731 as "onboarded as fused DSpark," which — read literally — implies shipping in this PR. To match this proposal's scope, that line should be read as "classified as ready to onboard (fused DSpark)": neither V4 variant is in the initial ship list, and both land in the `dspark` follow-up preset PR described in the **Ready to Onboard** section below. The PR description will be updated to use "ready to onboard" wording so scope statements agree.

⚠️ Notes:
- The distilled presets `DeepSeek-R1-Distill-Llama-8B` and `DeepSeek-R1-Distill-Qwen-14B` are **not** MTP candidates - they are Llama / Qwen architectures with no MTP head in the checkpoint.
- `zai-org/GLM-5.2-FP8` and the `Qwen3.x` family are in the catalog but do not yet have vLLM-documented speculative-decoding recipes on the shape KAITO ships (single-node, TP-only). They are called out under **Free-to-Onboard Next** rather than the initial ship list, and are the first candidates to promote once vLLM guidance stabilizes.

### Free-to-Onboard Next (Same `mtp` Path, No Extra Memory / Download; or awaiting upstream recipe)

| KAITO preset | HF ID | In KAITO catalog? | Notes / vLLM evidence |
|---|---|---|---|
| `deepseek-v3.2` | `deepseek-ai/DeepSeek-V3.2` | ✅ Yes | Same self-contained-head `mtp` path as V3 family; add in the follow-up preset PR once the initial `deepseek-r1-0528` / `deepseek-v3-0324` shipping catalog + required tests are merged |
| `zai-org/GLM-5.2-FP8` | `zai-org/GLM-5.2-FP8` | ✅ Yes | Add once vLLM publishes a speculative-decoding recipe for GLM-5.2 on TP-only single-node layouts |
| Qwen3.x MoE (e.g. `Qwen/Qwen3.6-35B-A3B`) | `Qwen/Qwen3.6-35B-A3B` (and family) | ✅ Yes | Onboard method-by-method as vLLM recipes for Qwen3 speculative decoding land; `ngram` is a plausible first target |

### Ready to Onboard (`mtp`, Assistant Checkpoint Required)

Gemma 4 `-it` presets support `mtp` via vLLM, but per upstream vLLM MTP documentation the assistant checkpoint is served **separately** in `speculative_config.model` - it is not bundled in the base checkpoint. So the onboarding cost is real:

1. Source/mirror the Gemma 4 assistant checkpoints into the KAITO preset image (or fetch at pod startup).
2. Populate `MTPConfig.Model` - which means extending `MTPConfig` with a `Model` field of the same shape as `DSparkConfig.Model` (see Step 1).
3. Re-verify against KAITO's pinned vLLM version.

This is architecturally the same shape as the `dspark` case below.

| KAITO preset | HF ID | Assistant checkpoint required | Notes |
|---|---|---|---|
| `gemma-4-E2B-it` | `google/gemma-4-E2B-it` | ✅ Yes | vLLM MTP doc: Gemma 4 IT uses a separate assistant checkpoint |
| `gemma-4-E4B-it` | `google/gemma-4-E4B-it` | ✅ Yes | same |
| `gemma-4-12B-it` | `google/gemma-4-12B-it` | ✅ Yes | same |
| `gemma-4-26B-A4B-it` | `google/gemma-4-26B-A4B-it` | ✅ Yes | same |
| `gemma-4-31B-it` | `google/gemma-4-31B-it` | ✅ Yes | same |

### Ready to Onboard (`dspark`, DeepSeek-V4 Family)

As of August 2026 the DeepSeek-V4 presets are DSpark candidates. Per the upstream [vLLM DeepSeek-V4-Flash recipe](https://recipes.vllm.ai/deepseek-ai/DeepSeek-V4-Flash), the four V4-Flash variants split cleanly along the fused-vs-assistant-checkpoint axis:

| Variant | HF ID | Draft module | Onboarding shape |
|---|---|---|---|
| **FP8 (0731, official default)** | `deepseek-ai/DeepSeek-V4-Flash-0731` | Fused DSpark (module in checkpoint) | Emit `{"method":"dspark", "num_speculative_tokens":...}` with no assistant `model` |
| **FP8 (Preview)** | `deepseek-ai/DeepSeek-V4-Flash` | MTP (in checkpoint) | Emit `{"method":"mtp", ...}` - same shape as R1-0528 / V3-0324 |
| **NVFP4** | `nvidia/DeepSeek-V4-Flash-NVFP4` | MTP (in checkpoint) | Same as FP8 Preview; Blackwell-only, FP4 indexer cache |
| **DSpark (Preview + fused DSpark module)** | `deepseek-ai/DeepSeek-V4-Flash-DSpark` | Fused DSpark (module attached to preview weights) | Same shape as 0731 - method-only blob |

So 0731 and `-DSpark` are the **fused DSpark** case (no extra checkpoint, no extra draft download beyond the served weights). The FP8 Preview and NVFP4 variants are the **fused MTP** case and behave like R1-0528 / V3-0324. None of these V4-Flash variants map to the earlier assumption of a separately sourced assistant checkpoint - the vLLM recipe does not identify one.

DeepSeek-V4-Pro-0813 is treated separately: it is DSpark-eligible per the vLLM family, but until an upstream V4-Pro recipe pins the exact fused-vs-assistant shape, its onboarding is deferred to a follow-up preset PR that lands after 0731.

Onboarding steps for V4-Flash-0731 (and the parallel FP8 Preview / NVFP4 / -DSpark rows above):

1. `deepseek-ai/DeepSeek-V4-Flash-0731` is **already in `model_catalog.yaml:741`** — no catalog addition needed for this preset. The remaining catalog work is limited to the sibling entries: rename `deepseek-v4-pro` → `-0813` when it lands, and add the FP8 Preview / NVFP4 / -DSpark rows alongside the existing 0731 entry.
2. Populate `DSparkConfig` - fused entries set `Variant: "fused"` (or leave unset) and need only `NumSpeculativeTokens` (and, when applicable, `DraftSampleMethod: "probabilistic"` and any hardware `AttentionBackend` override the recipe calls out). Runtime `vllmFormat` emits a method-only-plus-parameters blob; catalog-generation validation requires `Model == ""` for `Variant: "fused"`. The assistant-checkpoint case (`Variant: "assistant"` + non-empty `Model`) is defined but has no shipping preset in Step 1.
3. Re-verify against KAITO's pinned vLLM version.

The `mtp` variants (FP8 Preview and NVFP4) reuse the exact `MTPConfig` shape already shipped for R1-0528 / V3-0324 - no new type work, no assistant checkpoint. They can land in the initial `dspark` follow-up PR or as a parallel `mtp` follow-up, at maintainer discretion.

A V4-Flash preset that does not want speculative decoding at all can run with `--speculative-config` off; the existing `inference_config.yaml` passthrough remains the escape hatch for maintainers who want to sweep alternative recipes (e.g. lifting `num_speculative_tokens` or switching `draft_sample_method`).

| KAITO preset (base) | HF ID | Draft module (per vLLM recipe) | Method |
|---|---|---|---|
| `deepseek-v4-flash-0731` | `deepseek-ai/DeepSeek-V4-Flash-0731` | Fused DSpark (in checkpoint) | `dspark` (method-only blob) |
| `deepseek-v4-flash` (preview) | `deepseek-ai/DeepSeek-V4-Flash` | Fused MTP (in checkpoint) | `mtp` |
| `deepseek-v4-flash-nvfp4` | `nvidia/DeepSeek-V4-Flash-NVFP4` | Fused MTP (in checkpoint), Blackwell | `mtp` |
| `deepseek-v4-flash-dspark` | `deepseek-ai/DeepSeek-V4-Flash-DSpark` | Fused DSpark (attached to preview) | `dspark` (method-only blob) |
| `deepseek-v4-pro-0813` (rename `deepseek-v4-pro` → `-0813`) | `deepseek-ai/DeepSeek-V4-Pro-0813` | TBD per V4-Pro recipe (defer until published) | `dspark` (once recipe pinned) |

### Deferred - EAGLE / EAGLE-3 (Separate Draft Checkpoint)

Out of scope for this proposal. Each target needs a matching, maintained draft checkpoint plus real extra GPU memory. Candidate draft collections:

- [`RedHatAI/speculator-models`](https://huggingface.co/collections/RedHatAI/speculator-models)
- [`yuhuili/models` (EAGLE)](https://huggingface.co/yuhuili/models?search=eagle)

### Deferred - MLP Speculator (IBM Accelerators)

Also out of scope for the same reason. See vLLM's MLP speculator docs ([mlp.md](https://github.com/vllm-project/vllm/blob/main/docs/features/speculative_decoding/mlp.md)) for IBM's `*-accelerator` checkpoints.

### `ngram` / `suffix` - Universal, Not Part of Initial Commitment

These methods do not need a draft model at all - they lookup against the prompt and generation history. In principle any preset in the catalog could opt in. Not defined per-preset in this proposal; a good candidate for a follow-up if the maintainers decide to expose it.

### Summary Table

| Bucket | Presets | Status |
|---|---|---|
| **Shipping (this proposal)** | `deepseek-r1-0528`, `deepseek-v3-0324` | `mtp`, `numSpeculativeTokens: 1`, wired via `speculativeDecodingByPreset` in `presets/workspace/generator/generator.go` from day one |
| **Free-to-onboard next (same `mtp` path)** | `deepseek-v3.2`, `zai-org/GLM-5.2-FP8`, Qwen3.x MoE | Needs one re-verification + one `speculativeDecodingByPreset` entry each |
| **Assistant-checkpoint MTP (Gemma 4 IT family)** | `gemma-4-{E2B,E4B,12B,26B-A4B,31B}-it` | Requires assistant-checkpoint sourcing + node-estimator budgeting + a non-empty `MTPConfig.Model` in the `speculativeDecodingByPreset` entry - see Ready to Onboard (Gemma 4 IT) |
| **Ready to onboard (`dspark`, fused)** | `deepseek-v4-flash-0731`, `deepseek-v4-flash-dspark` | Base preset lands + a fused `DSparkConfig` (no `Model`) in `speculativeDecodingByPreset`; no separate assistant checkpoint sourcing needed |
| **Ready to onboard (`mtp`, fused V4)** | `deepseek-v4-flash` (preview), `deepseek-v4-flash-nvfp4` | Same `MTPConfig` shape as R1/V3 initial ship |
| **Deferred (V4-Pro, pending recipe)** | `deepseek-v4-pro-0813` | Onboard after upstream vLLM V4-Pro recipe pins fused-vs-assistant shape |
| **Deferred (EAGLE / MLP draft)** | Llama-3.1/3.3, Qwen3.*, Mistral-7B, etc. | Out of scope; needs draft-checkpoint sourcing design |
| **Universal opt-in (`ngram` / `suffix`)** | Any preset | Not part of initial commitment |

## TL;DR

- **User**: adds one annotation. Gets ~1.6×-1.7× interactive-latency win (QPS ≈ 1) on supported presets, degrading to ~1.2×-1.3× at QPS 2-4 and regressing under saturated traffic. Zero risk on unsupported presets (webhook rejects at `apply` time).
- **Preset maintainer**: adds a `SpeculativeDecoding` entry to `speculativeDecodingByPreset` in `presets/workspace/generator/generator.go` (sibling to `catalogOverrides`, not a field on it). `model_catalog.yaml` is not touched by this workflow. A unit test over the map validates method/sub-struct exclusivity and positive fields at build time.
- **The per-preset config is not user-tunable by design.** Users who need that keep using the existing `inference_config.yaml` ConfigMap passthrough (which takes precedence - see Step 5).
