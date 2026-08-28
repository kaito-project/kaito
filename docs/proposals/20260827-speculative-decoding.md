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

This proposal introduces a preset-driven speculative decoding toggle for KAITO. Users enable speculative decoding with a single annotation (`kaito.sh/enable-speculative-decoding: "true"`) on a Workspace or InferenceSet. The per-preset configuration (method, parameters) is maintained by KAITO preset maintainers in the model catalog, not exposed as a user knob. The preset controller injects the appropriate vLLM `--speculative-config` flag automatically, and the admission webhook rejects unsupported preset + annotation combinations at `kubectl apply` time.

## Motivation

LLM decoding is fundamentally token-by-token: each step produces one token and the GPU is heavily under-utilized. **Speculative decoding** is a **theoretically lossless** speedup:

1. **Draft** — cheaply guess the next N tokens (small model / n-gram lookup / MTP head bundled in the checkpoint).
2. **Verify** — run the target model **once**, in parallel, over those N candidates.
3. Accept the matching prefix, resample at the first mismatch.

Net: multiple tokens per GPU forward pass; end-to-end tok/s goes up. The output distribution is intended to match normal decoding, but as [vLLM's own docs note](https://docs.vllm.ai/en/latest/features/spec_decode.html) the guarantee is only up to hardware/precision effects — batching and numerical differences can shift log probabilities and, in edge cases, individual token choices. In practice the outputs are indistinguishable for user-facing chat/agent workloads.

Today, enabling speculative decoding in KAITO requires users to write a ConfigMap with raw vLLM JSON passthrough — they must know the vLLM speculative decoding API, method taxonomy, and parameter tuning. Every misspelled field, wrong method, or wrong parameter causes pod startup failure or silent inference degradation.

### Evidence

- DeepSeek DSpark paper (arXiv:2607.05147): **60–85% faster per-user generation** vs the prior MTP baseline.
- vLLM's MTP benchmark on DeepSeek-R1 (vllm-project/vllm#12755): **1.63×–1.69× speedup at QPS = 1**, 1.18×–1.32× at QPS = 2–4, and regressing below 1× at QPS ≥ 6 on TP=8. The benchmark uses MTP depth = 1 (see “Default value” note below).
- `deepseek-v3-0324` and `deepseek-r1-0528` (existing KAITO presets) already support `mtp` at zero extra memory / download cost.

### Goals

- Define a one-annotation toggle (`kaito.sh/enable-speculative-decoding: "true"`) for users to enable speculative decoding on supported presets
- Add a typed `SpeculativeDecoding` field to `CatalogEntry` in `model_catalog.yaml` (populated via `catalogOverrides` in `presets/workspace/generator/generator.go`), propagated through `PresetParam` / `Generator` / `vLLMCompatibleModel` so both the controller and the admission webhook can read it. `supported_models.yaml` is intentionally not extended: it is being deprecated (only the `base` image entry remains actively used); `model_catalog.yaml` + `catalogOverrides` is the going-forward source of truth for per-preset metadata.
- Preset controller reads the field **off the already-resolved `model.Model`** — via `GetInferenceParameters()` on the model object the controller already resolved through `models.GetModelByName` (which calls `GetModelByNameWithToken` internally) — and injects the vLLM `--speculative-config` flag automatically. The controller must not re-look up the preset by its short name at reconcile time: `GetModelByNameWithToken` rewrites short aliases like `deepseek-r1-0528` to `deepseek-ai/deepseek-r1-0528` before registering them (`presets/workspace/models/vllm_model.go`), so a direct `plugin.KaitoModelRegister.MustGet` keyed on the raw annotation'd preset name is not guaranteed to hit.
- Admission webhook rejects unsupported preset + annotation combinations at `kubectl apply` time — using `models.GetModelByName(ws.Inference.Preset.Name, accessSecret)` which performs the same alias rewrite and lazy model generation as the controller; the webhook then reads `GetInferenceParameters().SpeculativeDecoding` from the resolved model.
- Initial preset coverage: `mtp` on the two DeepSeek presets that are catalog-present today and vLLM-verified against KAITO's pinned vLLM at merge time — `deepseek-r1-0528` and `deepseek-v3-0324`. `deepseek-v3.2` is a Free-to-onboard-next candidate (same `mtp` path, no new type work) but is not in the initial ship list. DeepSeek-V4-Flash / V4-Pro land in **Ready to Onboard (`dspark`, fused)** below: the vLLM V4-Flash recipe classifies the dated releases (`DeepSeek-V4-Flash-0731`, `DeepSeek-V4-Flash-DSpark`) as fused served-checkpoint DSpark, not assistant-checkpoint — see the corrected DeepSeek-V4 section for details.
- Support InferenceSet via `spec.template.metadata.annotations` propagation

### Non-Goals/Future Work

- EAGLE / Medusa separate-draft-model methods (need checkpoint sourcing design)
- Typed override field on `InferenceSpec` for power users to tune speculative decoding parameters
- Custom scheduling algorithms for speculative decoding
- Automatically enabling speculative decoding by default

## Proposal

### Background — Speculative Decoding Methods

vLLM 0.10 collapsed the older `--speculative-model` / `--num-speculative-tokens` CLI flags into a single JSON blob passed to `--speculative-config`:

```bash
# MTP — DeepSeek-R1, no extra download, no extra memory
vllm serve deepseek-ai/DeepSeek-R1 \
  --speculative-config '{"method":"mtp","num_speculative_tokens":3}'

# ngram — zero-cost across any preset that opts in
--speculative-config '{"method":"ngram","num_speculative_tokens":5,"prompt_lookup_max":4}'
```

#### The Four Methods (vLLM `speculative_config.method`)

| Method | How it drafts | Extra GPU memory? | Best-fit workload |
|---|---|---|---|
| `mtp` | Multi-Token Prediction head bundled in the checkpoint (DeepSeek-V3 / R1, etc.) | No | Any workload on that model family |
| `dspark` | DeepSeek-V4's own semi-autoregressive block drafting; requires a **separately sourced assistant checkpoint** (e.g. `DeepSeek-V4-Flash-DSpark`) — not bundled in the base weights | Yes (separate assistant checkpoint loaded alongside target; requires node-estimator budgeting) | Any workload on DeepSeek-V4 |
| `eagle` / `eagle3` | Separate draft model trained to mimic the target | Yes (separate checkpoint loaded alongside target) | General-purpose; mainstream across vLLM/SGLang/TensorRT-LLM |
| `ngram` / `suffix` | Pure lookup against prompt + generation history | No | Code completion, RAG, summarization, translation, agent tool-call echo |

#### Why It Isn't Always On

Throughput can *regress* at high QPS (draft is wasted work when the batch is already saturated). It has to stay **opt-in**, never default. Several vLLM compatibility caveats also exist (pipeline parallelism, prefix caching, chunked prefill, logprob stability, LoRA/tool-calling) that need per-preset re-verification against KAITO's pinned vLLM version.

### User Experience

#### Today (Before This Proposal) — Painful

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

#### After — One Annotation

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
| Interactive (QPS ≈ 1) | 1.63×–1.69× |
| Low QPS (QPS 2–4) | 1.18×–1.32× |
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
| **KAITO maintainers** (code) | Which method (`mtp` / `dspark` / `ngram` / …), the parameters (`num_speculative_tokens`, `prompt_lookup_max` …), which presets are enabled |
| **User** (annotation) | On / off |

#### Escape Hatch for Power Users

The existing `inference_config.yaml` ConfigMap `vllm:` passthrough is **not going away** — a researcher who wants to sweep `num_speculative_tokens` or try a different method (e.g. `ngram`) can still write raw `--speculative-config` themselves. A typed override field on `InferenceSpec` is called out as **out of scope** for this proposal.

> **Caveat on the passthrough for methods that need a separate draft model (`eagle` / `medusa` / assistant-checkpoint `mtp` / `dspark`).** The passthrough gets vLLM the `speculative_config` JSON, but the KAITO node estimator (`workspace_controller.go`, resource-planning path) sizes the pod GPU memory from the **target** model only. A separate draft checkpoint of non-trivial size can silently push the pod OOM at load, or evict KV cache under load. So `inference_config.yaml` passthrough is safe for zero-extra-weight methods (`mtp` with self-contained head, `ngram`, `suffix`), but for draft-model methods the estimator has to be updated in the same PR that ships the config. Step 1 covers the DSpark assistant-checkpoint case as first-class typed config (`DSparkConfig.Model`). Assistant-checkpoint `mtp` (Gemma 4 IT family) is **not** first-class in Step 1 — Step 1's `MTPConfig` is intentionally a self-contained-head-only shape (see the type below). Extending `MTPConfig` with a `Model` field and updating the node estimator are called out as **future work** in the Gemma 4 IT onboarding section below, alongside assistant-checkpoint sourcing.

### Where the Per-Preset Config Lives in Code

**The config lives in `model_catalog.yaml` (populated via `catalogOverrides` in `presets/workspace/generator/generator.go`), and this proposal plumbs it end-to-end from `CatalogEntry` → `PresetParam` → registered `model.Model` → admission webhook.**

Status quo:

- `presets/workspace/models/supported_models.yaml` is being deprecated. Only the `base` image entry is still actively consumed; per-preset metadata that used to live there is being migrated to `model_catalog.yaml`. Adding a new per-preset field to `supported_models.yaml` would be adding to a file the project is trying to phase out.
- `presets/workspace/models/model_catalog.yaml` is generated from HuggingFace `config.json` by `presets/workspace/generator/update_model_catalog/main.go`; hand-curated per-preset overrides land in the `catalogOverrides` map in `presets/workspace/generator/generator.go` (already used today for missing HF metadata like Gemma 3 context length and Mistral Large 3 architecture). This is the correct home for a maintainer-controlled runtime knob.
- Today `CatalogEntry` is parsed into `model.PresetParam` by `GeneratePreset`; `vLLMCompatibleModel` (the object registered in `plugin.KaitoModelRegister`) currently retains only the fields it uses at command-build time. `ModelRegister` also has no `Get` API that exposes richer metadata to callers such as the admission webhook.

The plumbing change this proposal adds:

1. Add a `SpeculativeDecoding *SpeculativeDecodingConfig` field to `generator.CatalogEntry` (`presets/workspace/generator/model_catalog.go`) so it round-trips through `LoadCatalog` / `SaveCatalog`. `update_model_catalog/main.go` treats it as a preserved-across-refresh field (same treatment as other override-only fields) so a HF `config.json` refresh does not blow it away. Two coordinated updates are required in `update_model_catalog/main.go`:
   - Extend the `catalogFields` change-detection map (lines ~97-143) to include `SpeculativeDecoding`, so a speculative-only override change is detected on refresh rather than silently reported as "no changes."
   - Extend the explicit field-by-field copy in `FetchCatalogEntry` (`model_catalog.go:200-232`) to copy `ovr.SpeculativeDecoding` into the generated entry. Without this, the field stays nil in the generated catalog even after the override is authored. (Alternative: replace the manual merge with a reflect-based generic merge, but that is a larger refactor and out of scope for this proposal.)
2. Populate it via `catalogOverrides` in `presets/workspace/generator/generator.go` for the presets listed in the Model Coverage table. `catalogOverrides` is the existing hook for maintainer-authored fields whose values are not on HuggingFace.
3. Extend `generator.Generator.Param` (`model.PresetParam`) with a `SpeculativeDecoding` field, and add an explicit assignment in the `Generator.loadFromCatalog` field-copy block (same section that already retains fields like `Architectures`, `HiddenSize`, etc.): `param.SpeculativeDecoding = entry.SpeculativeDecoding`. `loadFromCatalog` assigns each retained field explicitly today — without this concrete copy step every `PresetParam` still sees `SpeculativeDecoding == nil` at reconcile time, even though the field is defined on the type.
4. Extend `vLLMCompatibleModel` in `presets/workspace/models/vllm_model.go` to hold the field, and make `GetInferenceParameters()` return it on the copy it hands back to the controller — this is what makes the value visible to the reconcile-time injection site in Step 3.
5. Use `models.GetModelByName(name, accessSecret)` in the admission webhook to resolve the preset. This function calls `GetModelByNameWithToken` internally, which normalizes aliases (`deepseek-r1-0528` → `deepseek-ai/deepseek-r1-0528`), lazily generates catalog models on first access, and resolves access secrets — unlike `plugin.KaitoModelRegister.MustGet`, which is a direct map lookup that would miss unregistered short names and panic on cache misses. Both the controller and the webhook thus resolve the same object for the same annotation-facing preset name.

After these changes, both the controller and the webhook read the same `SpeculativeDecoding` value from one source of truth in `model_catalog.yaml` + `catalogOverrides` — both go through `models.GetModelByName(ws.Inference.Preset.Name, accessSecret)` → `GetModelByNameWithToken` → `GetInferenceParameters()`, which honors the alias rewrite performed at registration (`presets/workspace/models/vllm_model.go`). `supported_models.yaml` is not touched by this proposal.

### Implementation

#### Step 1 — Extend the model metadata type

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
// a `Model` field of the same shape as `DSparkConfig.Model` — see the
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

// Added to model.Metadata (or PresetParam).
type Metadata struct {
    // ... existing fields ...
    SpeculativeDecoding *SpeculativeDecodingConfig `yaml:"speculativeDecoding,omitempty"`
}
```

The typed sub-structs constrain the shape, but they do not by themselves prevent an author from pairing `Method: "mtp"` with a nil `MTP` or a populated `NGram`. Validation happens in two places:

- **Catalog generation** (unit test over `model_catalog.yaml` after regeneration from `catalogOverrides`): asserts method / sub-struct exclusivity (exactly one non-nil sub-config, matching `method`), required positive fields (`numSpeculativeTokens > 0`, `promptLookupMax > 0` for `ngram`), and supported methods. For `dspark`, a typed `DSparkConfig.Variant` discriminator (`"fused"` | `"assistant"`, defaulting to `"fused"` when unset for backwards compatibility) decides whether `Model` is required: `Variant: "assistant"` requires a non-empty `Model`; `Variant: "fused"` requires `Model` to be empty. Any `MTPConfig` in Step 1 is implicitly self-contained-head; a parallel `MTPConfig.Variant` discriminator is deferred to the Gemma 4 IT assistant-checkpoint follow-up (see the caveat in [Per-Preset Config Ownership](#per-preset-config-ownership)). Fails the build if a maintainer authors a broken `catalogOverrides` entry.
- **Runtime** (`vllmFormat`, Step 3): returns a typed error rather than dereferencing a possibly-nil pointer.

`DSpark` is included even though DeepSeek-V4 onboarding is a follow-up, so the type surface is stable when the next preset lights up.

#### Step 2 — Declare per-preset config via `catalogOverrides` (feeding `model_catalog.yaml`)

Add `SpeculativeDecoding` entries to the `catalogOverrides` map in `presets/workspace/generator/generator.go`. On the next `update_model_catalog` run these are written into `presets/workspace/models/model_catalog.yaml` alongside the existing HF-derived fields, and are preserved on subsequent refreshes.

`presets/workspace/generator/generator.go`:

```go
catalogOverrides = map[string]CatalogEntry{
    // ... existing entries ...
    "deepseek-ai/deepseek-r1-0528": {
        SpeculativeDecoding: &SpeculativeDecodingConfig{
            Method: "mtp",
            MTP:    &MTPConfig{NumSpeculativeTokens: 1},
        },
    },
    "deepseek-ai/deepseek-v3-0324": {
        SpeculativeDecoding: &SpeculativeDecodingConfig{
            Method: "mtp",
            MTP:    &MTPConfig{NumSpeculativeTokens: 1},
        },
    },
    // See the "Model Coverage" section for the full initial-ship list.
}
```

After regeneration, the corresponding `model_catalog.yaml` block looks like:

```yaml
- name: deepseek-ai/DeepSeek-R1-0528
  # ... existing HF-derived fields (architectures, hiddenSize, etc.) ...
  speculativeDecoding:
    method: mtp
    mtp:
      numSpeculativeTokens: 1
```

> **Why `numSpeculativeTokens: 1`?** Matches the evidence: vllm-project/vllm#12755 explicitly benchmarks MTP depth = 1, and current upstream vLLM guidance calls 1 a good starting default. Larger depths materially change acceptance ratios and load behavior, so lifting to 2/3 requires a KAITO-pinned benchmark before it can ship as “pre-validated.” Committed as a follow-up.

#### Step 3 — Preset controller reads the field and injects the vLLM flag

The existing runtime command is assembled as a shell string via `BuildCmdStr` and executed with `/bin/sh -c`. `ModelRunParams` appends `--key=value` **verbatim** to that string — it does **not** shell-quote the value, and today `kv-events-config` works around this by storing the value with literal single quotes already embedded. Handing a raw `json.Marshal` result to `ModelRunParams` would let `/bin/sh` strip the JSON's double quotes and mis-tokenize the braces/commas.

Two acceptable options; the design commits to option (a) because it is smaller and localized:

**(a) Explicit shell-escape at the injection site.** Wrap the JSON in single quotes and escape any embedded single quotes with the standard `'\''` sequence before assigning to `ModelRunParams`. A unit test asserts the final `/bin/sh -c` command line by round-tripping through `sh -c 'echo $1'` and re-parsing as JSON.

**(b) Move `--speculative-config` off the shell-string path.** Emit it directly into `container.Args` (argv), bypassing `BuildCmdStr` for this one flag. Larger change; called out as a follow-up if more flags need JSON payloads.

> **On per-preset parameter defaults.** For presets whose vLLM recipe upstream gives concrete recommended values (e.g. DeepSeek-V4-Flash's [vLLM recipe](https://recipes.vllm.ai/deepseek-ai/DeepSeek-V4-Flash) covers `num_speculative_tokens`, tensor-parallel size, and KV-cache dtype), the `catalogOverrides` entry for that preset carries those values so the injected `--speculative-config` matches the upstream-recommended profile out of the box. When the recipe is silent, `numSpeculativeTokens: 1` is used as a safe default (see “Why 1?” in Step 2).

**Where the injection lives.** `GenerateInferencePodSpec` (`pkg/workspace/inference/preset_inferences.go:522`) is the code that builds the workload command. On line 578 it calls `ctx.Model.GetInferenceParameters().DeepCopy()` to obtain a fresh `PresetParam` for **this** pod, then on line 600 it calls `inferenceParam.GetInferenceCommand(...)` which walks `inferenceParam.VLLM.ModelRunParams` to build the shell string. `GetInferenceParameters` on `vLLMCompatibleModel` allocates a new `PresetParam` on every call, so **mutating any earlier copy is lost**. The injection therefore has to run **after** the line-578 `DeepCopy()` and **before** the line-600 `GetInferenceCommand(...)`, and it has to mutate `inferenceParam.VLLM.ModelRunParams` on that exact copy — no free-floating `runParams` map, no earlier `PresetParam` snapshot:

```go
// pkg/workspace/inference/preset_inferences.go, inside GenerateInferencePodSpec's
// returned func, immediately after:
//     inferenceParam := ctx.Model.GetInferenceParameters().DeepCopy()
// and BEFORE:
//     commands := inferenceParam.GetInferenceCommand(...)
if ws.Annotations["kaito.sh/enable-speculative-decoding"] == "true" {
    // Guard: reject multi-node (pipeline parallelism) at reconcile time.
    // The admission webhook checks this too, but estimation can change
    // between admission and reconcile (e.g. SKU availability), so we
    // re-check status.targetNodeCount here before injecting.
    if ws.Status.TargetNodeCount > 1 {
        // Set a Workspace condition explaining why speculative decoding
        // was not injected, rather than silently skipping.
        setCondition(ws, ConditionSpeculativeDecodingDisabled,
            "multi-node distributed inference (pipeline parallelism) is incompatible "+
            "with speculative decoding; resolved to %d nodes", ws.Status.TargetNodeCount)
        // Do NOT inject --speculative-config.
    } else if inferenceParam.VLLM != nil {
        // Extract SpeculativeDecoding from the already-resolved model.
        // ctx.Model was resolved by the caller via models.GetModelByName
        // (which triggers GetModelByNameWithToken internally and rewrites
        // short aliases like deepseek-r1-0528 -> deepseek-ai/deepseek-r1-0528).
        // Read the field off inferenceParam (the DeepCopy above), not off
        // ctx.Model.GetInferenceParameters() again — the latter would allocate
        // yet another PresetParam and any prior edits to inferenceParam would
        // not appear on it.
        if inferenceParam.SpeculativeDecoding != nil {
            // Skip injection if the user already provided --speculative-config
            // via inference_config.yaml passthrough. ConfigMap wins to preserve
            // the power-user escape hatch. See Step 5 for precedence rules.
            if !userSpecifiedSpeculativeConfig(inferenceConfig) {
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
            }
        }
    }
}

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
func vllmFormat(sd *SpeculativeDecodingConfig) (string, error) {
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
        // Belt-and-suspenders: caught earlier by the catalog-generation
        // validation over `model_catalog.yaml` + `catalogOverrides`
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

#### Step 4 — Admission webhook validates the annotation

Wired into both `v1alpha1` and `v1beta1` webhooks, on **both create and update** — `v1beta1.ValidateCreate` / `validateAnnotations` currently skips the update branch, so a Workspace could otherwise be created without the annotation and then have it added later, bypassing all of these checks. The design requires the check to run on `ValidateCreate` **and** `ValidateUpdate` for both served versions.

```go
var validAnnotationValues = map[string]bool{"true": true, "false": true}

func validateSpeculativeDecoding(ws *Workspace) error {
    val, present := ws.Annotations["kaito.sh/enable-speculative-decoding"]
    if !present || val == "false" {
        return nil
    }
    if !validAnnotationValues[val] {
        // Non-boolean values (typos, "1", "yes", etc.) must not be silently treated as
        // disabled — that would defeat the admission-time feedback promise.
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

    // (b) Preset must have a validated config in the catalog. Use
    //     models.GetModelByName (which calls GetModelByNameWithToken
    //     internally, handling alias rewrites like deepseek-r1-0528 ->
    //     deepseek-ai/deepseek-r1-0528, lazy-generation of catalog
    //     models, and access-secret resolution). A direct
    //     KaitoModelRegister.Get/MustGet would bypass alias
    //     normalization and fail on short preset names.
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
    //       - v1alpha1: "" (empty string — no per-preset secret)
    //     Both variants normalize alias rewrites via GetModelByNameWithToken
    //     internally.
    resolved, err := models.GetModelByName(
        ctx,
        ws.Inference.Preset.Name,
        accessSecret, // explicit input, see helper signature comment above
        ws.Namespace,
        webhookClient,
    )
    if err != nil || resolved == nil {
        return fmt.Errorf(
            "preset %q could not be resolved; "+
            "remove kaito.sh/enable-speculative-decoding annotation or choose a supported preset",
            ws.Inference.Preset.Name,
        )
    }
    params := resolved.GetInferenceParameters()
    if params == nil || params.SpeculativeDecoding == nil {
        return fmt.Errorf(
            "preset %q does not have a validated speculative decoding configuration; "+
            "remove kaito.sh/enable-speculative-decoding annotation or choose a "+
            "supported preset (currently: deepseek-r1-0528, deepseek-v3-0324)",
            ws.Inference.Preset.Name,
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

    // (d) Reject pipeline parallelism.
    //     Rationale: KAITO's cross-node runtime today is pipeline
    //     parallelism (see workspace_controller.go lines 1578-1621, where
    //     multi-node presets are resolved and PP is what gets configured
    //     when the resolved node count > 1). Upstream vLLM's own
    //     speculative-decoding docs likewise gate their per-method
    //     recipes on single-node deployments, and MTP / DSpark PP
    //     support is still being landed / stabilized in vLLM at the time
    //     of this proposal (see vLLM issues tracking
    //     speculative-decoding+PP). Enabling `--speculative-config` on a
    //     PP layout in the current KAITO+vLLM combination would either
    //     hard-fail vLLM startup or silently disable speculation on the
    //     non-first stages, neither of which we want to route through
    //     `enable-speculative-decoding: "true"`.
    //
    //     Note this is a conservative admission-time gate, not an
    //     upstream compatibility statement. When vLLM's PP support for a
    //     given method stabilizes and we’ve verified it on a KAITO PP
    //     layout, the gate is lifted per-method (a `PPCompatible bool`
    //     on `SpeculativeDecodingConfig` is the natural extension).
    //
    //     resource.count is NOT the resolved vLLM node count — the
    //     controller computes status.targetNodeCount from model size and
    //     SKU AFTER admission. A nil or 1 count can still become
    //     multiple nodes and pull in PP. So we enforce single-node
    //     compatibility using the same estimator the controller uses,
    //     and add a reconcile-time guard before injection.
    //
    //     Admission time (this webhook): reject if the resource estimator
    //     already knows the preset requires PP for the resolved SKU.
    //     Reconcile time (Step 3 injection site): re-check
    //     status.targetNodeCount and refuse to inject if it exceeds 1,
    //     emitting a Workspace condition explaining why.
    //
    //     The admission webhook cannot import
    //     `pkg/workspace/estimator/nodesestimator` directly — that package
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
    //     direct import — the webhook site does not change.
    targetNodes, err := estimateTargetNodeCount(ws)
    if err != nil {
        return fmt.Errorf(
            "kaito.sh/enable-speculative-decoding: failed to estimate node count "+
            "for preset %q on SKU %q: %w; cannot validate pipeline-parallelism "+
            "compatibility — resolve the estimation error or remove the annotation",
            ws.Inference.Preset.Name, ws.Resource.InstanceType, err,
        )
    }
    if targetNodes > 1 {
        return fmt.Errorf(
            "kaito.sh/enable-speculative-decoding is incompatible with "+
            "multi-node distributed inference (pipeline parallelism); "+
            "preset %q on SKU %q resolves to %d nodes",
            ws.Inference.Preset.Name, ws.Resource.InstanceType, targetNodes,
        )
    }

    return nil
}
```

#### Step 5 — Precedence when the user also sets `speculative-config` in a ConfigMap

The existing `inference_config.yaml` passthrough (`vllm.speculative-config: '...'`) stays. When both are present:

- **ConfigMap wins.** The preset controller skips its own `--speculative-config` injection if the user's ConfigMap already contains a `speculative-config` key under `vllm:`. This preserves the power-user escape hatch (sweep `num_speculative_tokens`, try `eagle`) without producing two conflicting `--speculative-config` flags on the vLLM command line.
- The admission webhook still enforces (b) — the annotation still requires a supported preset. This keeps the failure mode consistent regardless of ConfigMap contents.
- When both sources are present, the **reconcile-time preset controller** emits a Kubernetes Event (`SpeculativeDecodingConfigMapOverride`) on the Workspace at the same place it decides to skip injection. Events are only emitted from the reconcile path — not the validating webhook — because the webhook is declared with `sideEffects: None` (`charts/kaito/workspace/templates/webhooks.yaml`) and can be invoked for dry-run or retried admission requests. Emitting an Event from admission would violate the webhook contract and could produce duplicate or spurious events; the reconcile-time signal may fire on repeated reconciliations of the same generation (status updates, child-resource watches, retries). Kubernetes Events are inherently aggregated — the API server deduplicates events with the same reason/message within a window — so the emission is safe without explicit idempotency tracking. It shows up in `kubectl describe workspace` / `kubectl get events`.

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
| `InferenceSet.metadata.annotations` | Cluster-level policy on the InferenceSet itself (e.g. `scaledobject.kaito.sh/*` autoscaling) | ❌ No — controller-scoped |
| `InferenceSet.spec.template.metadata.annotations` | Per-Workspace behavior (**this is where `kaito.sh/enable-speculative-decoding` goes**) | ✅ Yes — cloned to each child Workspace |

#### Rejection Semantics for InferenceSet

Rejection must happen at `apply` time for **both** served API versions:

- **`v1beta1`**: the InferenceSet webhook already projects the template through `NewWorkspaceForInferenceSet` (which is `v1beta1`-typed: it accepts `*v1beta1.InferenceSet` and returns `v1beta1.Workspace`) and runs `Workspace.ValidateCreate`. `validateSpeculativeDecoding` (Step 4) is invoked as part of that projection, so an unsupported template preset is rejected at `kubectl apply -f <InferenceSet>` — no reconcile-time surprise.
- **`v1alpha1`**: the current webhook does **not** project the templated child. Because `NewWorkspaceForInferenceSet` is `v1beta1`-typed, the `v1alpha1` webhook cannot call it directly. Two acceptable implementations; this proposal recommends option (a):
  - **(a) Shared version-neutral helper.** Factor the annotation + preset + runtime + PP checks out of `Workspace.ValidateCreate` into a helper that takes the annotation map, preset name, runtime name, and resource spec — no version-typed dependency. Both webhooks call it. This avoids introducing a conversion round-trip on the admission hot path.
  - **(b) Explicit v1alpha1 → v1beta1 conversion.** Convert `*v1alpha1.InferenceSet` to `*v1beta1.InferenceSet` (either via the existing conversion webhook or an in-webhook shim), then reuse `NewWorkspaceForInferenceSet` + `ValidateCreate`. Larger blast radius; only pick this if the annotation set grows beyond what a small helper cleanly captures.

Either way, rejection happens at `apply` time on both served versions, and the create/update parity requirement from Step 4 (`ValidateCreate` **and** `ValidateUpdate`) applies here too — an existing InferenceSet must not be able to add the annotation post-hoc and bypass the check.

#### Scaling Implication (Unchanged)

Speculative decoding is a **per-replica** speedup — MTP verifies within a single vLLM engine. Turning it on across an InferenceSet's replicas just means every replica gets the same per-request latency win. It does **not** share draft state across replicas and does **not** replace autoscaling — you still want KEDA / auto-provision to grow replicas under high QPS, because the throughput of speculative decoding degrades toward 1.0× as QPS climbs. The two features are complementary.

## Model Coverage

Cross-referencing the KAITO preset catalog ([`presets/workspace/models/model_catalog.yaml`](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/model_catalog.yaml)) against vLLM's speculative-decoding docs ([features/speculative_decoding/](https://github.com/vllm-project/vllm/tree/main/docs/features/speculative_decoding)) gives a clear picture of what this proposal ships versus what could be layered on later.

### Committed (Initial Preset Coverage)

The initial shipping list favors presets that (a) are already in `model_catalog.yaml`, (b) do not need a separately sourced draft checkpoint, and (c) have upstream evidence (vLLM benchmark, vLLM recipe, or vendor release notes) that speculative decoding gives a net win on realistic KAITO-shape workloads.

| KAITO preset | HF ID | In KAITO catalog? | Method | `num_speculative_tokens` | Extra memory / download |
|---|---|---|---|---|---|
| `deepseek-r1-0528` | `deepseek-ai/DeepSeek-R1-0528` | ✅ Yes | `mtp` | 1 | none — MTP head is in the checkpoint |
| `deepseek-v3-0324` | `deepseek-ai/DeepSeek-V3-0324` | ✅ Yes | `mtp` | 1 | none — same |

The initial ship list is intentionally scoped to `deepseek-r1-0528` and `deepseek-v3-0324` (as declared in the PR summary): both are already in the catalog, both use the self-contained-head `mtp` path with `MTPConfig` as defined in Step 1, and both have KAITO-shape upstream evidence (vLLM MTP benchmark #12755). Any additional preset in the initial catalog PR would need its own required-catalog-test coverage without expanding proposal scope.

Other in-catalog DeepSeek presets (`deepseek-v3.2` and any subsequent V3 point-releases sharing the same in-checkpoint MTP path) are the immediate follow-up candidates — same code path, same `MTPConfig`, no new type work — and are called out in **Free-to-Onboard Next** below.

DeepSeek-V4-Flash-0731 and V4-Pro-0813 are DSpark candidates. Per the vLLM recipe, V4-Flash-0731 carries a **fused** DSpark module in its served checkpoint — no assistant checkpoint sourcing needed — so it lands in **Ready to Onboard (`dspark`, DeepSeek-V4 Family)** below as a fused `DSparkConfig` (no `Model` field). V4-Pro-0813 is deferred until the upstream V4-Pro recipe pins the exact fused-vs-assistant shape. Neither variant is in the initial ship list; see that section for the corrected onboarding steps.

⚠️ Notes:
- The distilled presets `DeepSeek-R1-Distill-Llama-8B` and `DeepSeek-R1-Distill-Qwen-14B` are **not** MTP candidates — they are Llama / Qwen architectures with no MTP head in the checkpoint.
- `zai-org/GLM-5.2-FP8` and the `Qwen3.x` family are in the catalog but do not yet have vLLM-documented speculative-decoding recipes on the shape KAITO ships (single-node, TP-only). They are called out under **Free-to-Onboard Next** rather than the initial ship list, and are the first candidates to promote once vLLM guidance stabilizes.

### Free-to-Onboard Next (Same `mtp` Path, No Extra Memory / Download; or awaiting upstream recipe)

| KAITO preset | HF ID | In KAITO catalog? | Notes / vLLM evidence |
|---|---|---|---|
| `deepseek-v3.2` | `deepseek-ai/DeepSeek-V3.2` | ✅ Yes | Same self-contained-head `mtp` path as V3 family; add in the follow-up preset PR once the initial `deepseek-r1-0528` / `deepseek-v3-0324` shipping catalog + required tests are merged |
| `zai-org/GLM-5.2-FP8` | `zai-org/GLM-5.2-FP8` | ✅ Yes | Add once vLLM publishes a speculative-decoding recipe for GLM-5.2 on TP-only single-node layouts |
| Qwen3.x MoE (e.g. `Qwen/Qwen3.6-35B-A3B`) | `Qwen/Qwen3.6-35B-A3B` (and family) | ✅ Yes | Onboard method-by-method as vLLM recipes for Qwen3 speculative decoding land; `ngram` is a plausible first target |

### Ready to Onboard (`mtp`, Assistant Checkpoint Required)

Gemma 4 `-it` presets support `mtp` via vLLM, but per upstream vLLM MTP documentation the assistant checkpoint is served **separately** in `speculative_config.model` — it is not bundled in the base checkpoint. So the onboarding cost is real:

1. Source/mirror the Gemma 4 assistant checkpoints into the KAITO preset image (or fetch at pod startup).
2. Populate `MTPConfig.Model` — which means extending `MTPConfig` with a `Model` field of the same shape as `DSparkConfig.Model` (see Step 1).
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
| **FP8 (0731, official default)** | `deepseek-ai/DeepSeek-V4-Flash-0731` | Fused DSpark (module in checkpoint) | Emit `{"method":"dspark", "num_speculative_tokens":…}` with no assistant `model` |
| **FP8 (Preview)** | `deepseek-ai/DeepSeek-V4-Flash` | MTP (in checkpoint) | Emit `{"method":"mtp", …}` — same shape as R1-0528 / V3-0324 |
| **NVFP4** | `nvidia/DeepSeek-V4-Flash-NVFP4` | MTP (in checkpoint) | Same as FP8 Preview; Blackwell-only, FP4 indexer cache |
| **DSpark (Preview + fused DSpark module)** | `deepseek-ai/DeepSeek-V4-Flash-DSpark` | Fused DSpark (module attached to preview weights) | Same shape as 0731 — method-only blob |

So 0731 and `-DSpark` are the **fused DSpark** case (no extra checkpoint, no extra draft download beyond the served weights). The FP8 Preview and NVFP4 variants are the **fused MTP** case and behave like R1-0528 / V3-0324. None of these V4-Flash variants map to the earlier assumption of a separately sourced assistant checkpoint — the vLLM recipe does not identify one.

DeepSeek-V4-Pro-0813 is treated separately: it is DSpark-eligible per the vLLM family, but until an upstream V4-Pro recipe pins the exact fused-vs-assistant shape, its onboarding is deferred to a follow-up preset PR that lands after 0731.

Onboarding steps for V4-Flash-0731 (and the parallel FP8 Preview / NVFP4 / -DSpark rows above):

1. Add the base preset to the catalog (rename `deepseek-v4-pro` → `-0813` when it lands; add `deepseek-v4-flash-0731` alongside).
2. Populate `DSparkConfig` — fused entries set `Variant: "fused"` (or leave unset) and need only `NumSpeculativeTokens` (and, when applicable, `DraftSampleMethod: "probabilistic"` and any hardware `AttentionBackend` override the recipe calls out). Runtime `vllmFormat` emits a method-only-plus-parameters blob; catalog-generation validation requires `Model == ""` for `Variant: "fused"`. The assistant-checkpoint case (`Variant: "assistant"` + non-empty `Model`) is defined but has no shipping preset in Step 1.
3. Re-verify against KAITO's pinned vLLM version.

The `mtp` variants (FP8 Preview and NVFP4) reuse the exact `MTPConfig` shape already shipped for R1-0528 / V3-0324 — no new type work, no assistant checkpoint. They can land in the initial `dspark` follow-up PR or as a parallel `mtp` follow-up, at maintainer discretion.

A V4-Flash preset that does not want speculative decoding at all can run with `--speculative-config` off; the existing `inference_config.yaml` passthrough remains the escape hatch for maintainers who want to sweep alternative recipes (e.g. lifting `num_speculative_tokens` or switching `draft_sample_method`).

| KAITO preset (base) | HF ID | Draft module (per vLLM recipe) | Method |
|---|---|---|---|
| `deepseek-v4-flash-0731` | `deepseek-ai/DeepSeek-V4-Flash-0731` | Fused DSpark (in checkpoint) | `dspark` (method-only blob) |
| `deepseek-v4-flash` (preview) | `deepseek-ai/DeepSeek-V4-Flash` | Fused MTP (in checkpoint) | `mtp` |
| `deepseek-v4-flash-nvfp4` | `nvidia/DeepSeek-V4-Flash-NVFP4` | Fused MTP (in checkpoint), Blackwell | `mtp` |
| `deepseek-v4-flash-dspark` | `deepseek-ai/DeepSeek-V4-Flash-DSpark` | Fused DSpark (attached to preview) | `dspark` (method-only blob) |
| `deepseek-v4-pro-0813` (rename `deepseek-v4-pro` → `-0813`) | `deepseek-ai/DeepSeek-V4-Pro-0813` | TBD per V4-Pro recipe (defer until published) | `dspark` (once recipe pinned) |

### Deferred — EAGLE / EAGLE-3 (Separate Draft Checkpoint)

Out of scope for this proposal. Each target needs a matching, maintained draft checkpoint plus real extra GPU memory. Candidate draft collections:

- [`RedHatAI/speculator-models`](https://huggingface.co/collections/RedHatAI/speculator-models)
- [`yuhuili/models` (EAGLE)](https://huggingface.co/yuhuili/models?search=eagle)

### Deferred — MLP Speculator (IBM Accelerators)

Also out of scope for the same reason. See vLLM's MLP speculator docs ([mlp.md](https://github.com/vllm-project/vllm/blob/main/docs/features/speculative_decoding/mlp.md)) for IBM's `*-accelerator` checkpoints.

### `ngram` / `suffix` — Universal, Not Part of Initial Commitment

These methods do not need a draft model at all — they lookup against the prompt and generation history. In principle any preset in the catalog could opt in. Not defined per-preset in this proposal; a good candidate for a follow-up if the maintainers decide to expose it.

### Summary Table

| Bucket | Presets | Status |
|---|---|---|
| **Shipping (this proposal)** | `deepseek-r1-0528`, `deepseek-v3-0324` | `mtp`, `numSpeculativeTokens: 1`, wired via `catalogOverrides` into `model_catalog.yaml` from day one |
| **Free-to-onboard next (same `mtp` path)** | `deepseek-v3.2`, `zai-org/GLM-5.2-FP8`, Qwen3.x MoE | Needs one re-verification + one `catalogOverrides` entry each |
| **Assistant-checkpoint MTP (Gemma 4 IT family)** | `gemma-4-{E2B,E4B,12B,26B-A4B,31B}-it` | Requires assistant-checkpoint sourcing + node-estimator budgeting + a non-empty `MTPConfig.Model` in `catalogOverrides` — see Ready to Onboard (Gemma 4 IT) |
| **Ready to onboard (`dspark`, fused)** | `deepseek-v4-flash-0731`, `deepseek-v4-flash-dspark` | Base preset lands + a fused `DSparkConfig` (no `Model`) in `catalogOverrides`; no separate assistant checkpoint sourcing needed |
| **Ready to onboard (`mtp`, fused V4)** | `deepseek-v4-flash` (preview), `deepseek-v4-flash-nvfp4` | Same `MTPConfig` shape as R1/V3 initial ship |
| **Deferred (V4-Pro, pending recipe)** | `deepseek-v4-pro-0813` | Onboard after upstream vLLM V4-Pro recipe pins fused-vs-assistant shape |
| **Deferred (EAGLE / MLP draft)** | Llama-3.1/3.3, Qwen3.*, Mistral-7B, etc. | Out of scope; needs draft-checkpoint sourcing design |
| **Universal opt-in (`ngram` / `suffix`)** | Any preset | Not part of initial commitment |

## TL;DR

- **User**: adds one annotation. Gets ~1.6×–1.7× interactive-latency win (QPS ≈ 1) on supported presets, degrading to ~1.2×–1.3× at QPS 2–4 and regressing under saturated traffic. Zero risk on unsupported presets (webhook rejects at `apply` time).
- **Preset maintainer**: adds a `SpeculativeDecoding` entry to `catalogOverrides` in `presets/workspace/generator/generator.go`; `update_model_catalog` writes it into `model_catalog.yaml`, and a catalog-generation unit test validates method/sub-struct exclusivity and positive fields at build time.
- **The per-preset config is not user-tunable by design.** Users who need that keep using the existing `inference_config.yaml` ConfigMap passthrough (which takes precedence — see Step 5).
