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

LLM decoding is fundamentally token-by-token: each step produces one token and the GPU is heavily under-utilized. **Speculative decoding** is a pure **lossless** speedup:

1. **Draft** — cheaply guess the next N tokens (small model / n-gram lookup / MTP head bundled in the checkpoint).
2. **Verify** — run the target model **once**, in parallel, over those N candidates.
3. Accept the matching prefix, resample at the first mismatch.

Net: multiple tokens per GPU forward pass; end-to-end tok/s goes up; the output distribution is identical to normal decoding.

Today, enabling speculative decoding in KAITO requires users to write a ConfigMap with raw vLLM JSON passthrough — they must know the vLLM speculative decoding API, method taxonomy, and parameter tuning. Every misspelled field, wrong method, or wrong parameter causes pod startup failure or silent inference degradation.

### Evidence

- DeepSeek DSpark paper (arXiv:2607.05147): **60–85% faster per-user generation** vs the prior MTP baseline.
- vLLM's MTP benchmark on DeepSeek-R1 (vllm-project/vllm#12755): **~1.6–1.7× speedup at QPS = 1**, decaying toward ~1.0× above QPS ~6–8.
- `deepseek-v3-0324` and `deepseek-r1-0528` (existing KAITO presets) already support `mtp` at zero extra memory / download cost.

### Goals

- Define a one-annotation toggle (`kaito.sh/enable-speculative-decoding: "true"`) for users to enable speculative decoding on supported presets
- Add a typed `SpeculativeDecoding` field to the model catalog entry, populated per preset via `catalogOverrides`
- Preset controller reads the field and injects vLLM `--speculative-config` flag automatically
- Admission webhook rejects unsupported preset + annotation combinations at `kubectl apply` time
- Initial preset coverage: `mtp` for `deepseek-r1-0528` and `deepseek-v3-0324` (zero extra memory/download)
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
| `dspark` | DeepSeek-V4's own semi-autoregressive block drafting; bundled in checkpoint | No | Any workload on DeepSeek-V4 |
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
      speculative-config: '{"method":"mtp","num_speculative_tokens":3}'
---
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-r1
inference:
  preset:
    name: deepseek-r1-distill-llama-8b
  config: my-inference-config
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
    name: deepseek-r1-distill-llama-8b
```

`kubectl apply -f` and KAITO:

1. Looks up the preset's `SpeculativeDecoding` config (validated in advance by preset maintainers).
2. Injects `--speculative-config '{"method":"mtp","num_speculative_tokens":3}'` into the vLLM command.
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

Expected latency win for interactive chat / agent workloads on `deepseek-r1-*` at low-to-medium QPS: roughly **1.5×–1.7×** based on the vLLM MTP benchmark.

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
Error from server (Forbidden): admission webhook "workspace-validation.kaito.sh"
denied the request: preset "llama-3.1-8b-instruct" does not have a validated
speculative decoding configuration; remove kaito.sh/enable-speculative-decoding
annotation or choose a supported preset (e.g. deepseek-r1-*, deepseek-v3-*).
```

### Per-Preset Config Ownership

**The per-preset config is baked in by KAITO maintainers, not a user knob.**

| Who | Controls |
|---|---|
| **KAITO maintainers** (code) | Which method (`mtp` / `dspark` / `ngram` / …), the parameters (`num_speculative_tokens`, `prompt_lookup_max` …), which presets are enabled |
| **User** (annotation) | On / off |

#### Escape Hatch for Power Users

The existing `inference_config.yaml` ConfigMap `vllm:` passthrough is **not going away** — a researcher who wants to sweep `num_speculative_tokens` or try `eagle` can still write raw `--speculative-config` themselves. A typed override field on `InferenceSpec` is called out as **out of scope** for this proposal.

### Where the Per-Preset Config Lives in Code

Two locations. The pattern reuses the existing `catalogOverrides` mechanism in the KAITO code generator.

#### Location 1 — Source of Truth: `catalogOverrides` Map

File: [`presets/workspace/generator/generator.go`](https://github.com/kaito-project/kaito/blob/main/presets/workspace/generator/generator.go)

This map already exists to override fields that HuggingFace `config.json` gets wrong or omits (e.g. gemma-3's `ModelTokenLimit`, mistral-large-3's `Architectures`). Adding speculative decoding just extends that pattern.

#### Location 2 — Generated Artifact: `model_catalog.yaml`

File: [`presets/workspace/models/model_catalog.yaml`](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/model_catalog.yaml)

Produced by running `go run ./presets/workspace/generator/update_model_catalog`. This is what the KAITO controller and admission webhook actually read at runtime.

### Implementation

#### Step 1 — Extend the Type

`presets/workspace/generator/model_catalog.go`:

```go
type SpeculativeDecodingConfig struct {
    Method string        `yaml:"method"`         // "mtp" / "dspark" / "ngram" / ...
    MTP    *MTPConfig    `yaml:"mtp,omitempty"`
    NGram  *NGramConfig  `yaml:"ngram,omitempty"`
    // future: EAGLE *EAGLEConfig
}

type MTPConfig struct {
    NumSpeculativeTokens int `yaml:"numSpeculativeTokens"`
}

type NGramConfig struct {
    NumSpeculativeTokens int `yaml:"numSpeculativeTokens"`
    PromptLookupMax      int `yaml:"promptLookupMax"`
}

type CatalogEntry struct {
    // ... existing fields ...
    SpeculativeDecoding *SpeculativeDecodingConfig `yaml:"speculativeDecoding,omitempty"`
}
```

The typed sub-structs make invalid method/parameter combinations impossible to author.

#### Step 2 — Declare Per-Preset Config in `catalogOverrides`

`presets/workspace/generator/generator.go`:

```go
catalogOverrides = map[string]CatalogEntry{
    // ... existing gemma / mistral overrides ...

    "deepseek-ai/deepseek-r1-0528": {
        SpeculativeDecoding: &SpeculativeDecodingConfig{
            Method: "mtp",
            MTP: &MTPConfig{
                NumSpeculativeTokens: 3,
            },
        },
    },
    "deepseek-ai/deepseek-v3-0324": {
        SpeculativeDecoding: &SpeculativeDecodingConfig{
            Method: "mtp",
            MTP: &MTPConfig{
                NumSpeculativeTokens: 3,
            },
        },
    },
}
```

#### Step 3 — Regenerate the Catalog

```
go run ./presets/workspace/generator/update_model_catalog
```

Produces (fragment):

```yaml
models:
  - name: deepseek-r1-0528
    architectures: [DeepseekV3ForCausalLM]
    modelTokenLimit: 163840
    # ...
    speculativeDecoding:
      method: mtp
      mtp:
        numSpeculativeTokens: 3
```

#### Step 4 — Preset Controller Reads the Field and Injects the vLLM Flag

```go
if ws.Annotations["kaito.sh/enable-speculative-decoding"] == "true" {
    if entry.SpeculativeDecoding != nil {
        blob, _ := json.Marshal(vllmFormat(entry.SpeculativeDecoding))
        vllmArgs = append(vllmArgs, "--speculative-config", string(blob))
    }
}
```

Where `vllmFormat` converts the typed Go struct into the JSON shape vLLM expects:

```go
func vllmFormat(sd *SpeculativeDecodingConfig) map[string]any {
    m := map[string]any{"method": sd.Method}
    switch sd.Method {
    case "mtp":
        m["num_speculative_tokens"] = sd.MTP.NumSpeculativeTokens
    case "ngram":
        m["num_speculative_tokens"] = sd.NGram.NumSpeculativeTokens
        m["prompt_lookup_max"]      = sd.NGram.PromptLookupMax
    }
    return m
}
```

#### Step 5 — Admission Webhook Validates the Annotation

```go
func validateSpeculativeDecoding(ws *Workspace) error {
    if ws.Annotations["kaito.sh/enable-speculative-decoding"] != "true" {
        return nil
    }
    entry := lookupCatalogEntry(ws.Inference.Preset.Name)
    if entry == nil || entry.SpeculativeDecoding == nil {
        return fmt.Errorf(
            "preset %q does not have a validated speculative decoding configuration; "+
            "remove kaito.sh/enable-speculative-decoding annotation or choose a "+
            "supported preset (e.g. deepseek-r1-*, deepseek-v3-*)",
            ws.Inference.Preset.Name,
        )
    }
    return nil
}
```

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
      instanceType: Standard_ND96isr_H100_v5
```

`kubectl apply -f` and the InferenceSet controller creates `replicas` Workspaces, each with `kaito.sh/enable-speculative-decoding: "true"` in its own annotation map. Each child then goes through the exact same preset-controller injection and admission-webhook validation flow.

#### Which Annotations Go Where

| Annotation location | Purpose | Reaches child Workspace? |
|---|---|---|
| `InferenceSet.metadata.annotations` | Cluster-level policy on the InferenceSet itself (e.g. `scaledobject.kaito.sh/*` autoscaling) | ❌ No — controller-scoped |
| `InferenceSet.spec.template.metadata.annotations` | Per-Workspace behavior (**this is where `kaito.sh/enable-speculative-decoding` goes**) | ✅ Yes — cloned to each child Workspace |

#### Rejection Semantics for InferenceSet

If the template's preset has no `SpeculativeDecoding` entry in the catalog:

- On `kubectl apply -f <InferenceSet>`, the InferenceSet **itself** may be accepted (it validates its own schema), but each child Workspace that the controller tries to create is rejected by the Workspace admission webhook.
- The rejection surfaces on the InferenceSet's status (create-workspace event / condition), so the user still sees a fast, clear failure — just at reconciliation time rather than at `apply` time.
- (Optional hardening left as follow-up: teach the InferenceSet admission webhook to also validate the annotation against the templated preset, so rejection happens at `apply` time too. Not required for correctness.)

#### Scaling Implication (Unchanged)

Speculative decoding is a **per-replica** speedup — MTP verifies within a single vLLM engine. Turning it on across an InferenceSet's replicas just means every replica gets the same per-request latency win. It does **not** share draft state across replicas and does **not** replace autoscaling — you still want KEDA / auto-provision to grow replicas under high QPS, because the throughput of speculative decoding degrades toward 1.0× as QPS climbs. The two features are complementary.

## Model Coverage

Cross-referencing the KAITO preset catalog ([`presets/workspace/models/model_catalog.yaml`](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/model_catalog.yaml)) against vLLM's speculative-decoding docs ([features/speculative_decoding/](https://github.com/vllm-project/vllm/tree/main/docs/features/speculative_decoding)) gives a clear picture of what this proposal ships versus what could be layered on later.

### Committed (Initial Preset Coverage)

| KAITO preset | HF ID | In KAITO catalog? | Method | `num_speculative_tokens` | Extra memory / download |
|---|---|---|---|---|---|
| `deepseek-r1-0528` | `deepseek-ai/DeepSeek-R1-0528` | ✅ Yes | `mtp` | 3 | none — MTP head is in the checkpoint |
| `deepseek-v3-0324` | `deepseek-ai/DeepSeek-V3-0324` | ✅ Yes | `mtp` | 3 | none — same |

⚠️ Note: the distilled presets `DeepSeek-R1-Distill-Llama-8B` and `DeepSeek-R1-Distill-Qwen-14B` are **not** MTP candidates — they are Llama / Qwen architectures with no MTP head in the checkpoint.

### Free-to-Onboard Next (Same `mtp` Path, No Extra Memory / Download)

These presets already exist in the KAITO catalog and the vLLM upstream MTP docs ([mtp.md](https://github.com/vllm-project/vllm/blob/main/docs/features/speculative_decoding/mtp.md)) confirm the checkpoint ships an MTP path. The maintainer cost is one re-verification against KAITO's pinned vLLM version, then one entry in `catalogOverrides`.

| KAITO preset | HF ID | In KAITO catalog? | Notes / vLLM evidence |
|---|---|---|---|
| `deepseek-v3.2` | `deepseek-ai/DeepSeek-V3.2` | ✅ Yes | DeepSeek-V3 family continuation; same MTP path |
| `gemma-4-E2B-it` | `google/gemma-4-E2B-it` | ✅ Yes | vLLM MTP doc confirms Gemma 4 IT assistant checkpoints are supported |
| `gemma-4-E4B-it` | `google/gemma-4-E4B-it` | ✅ Yes | same |
| `gemma-4-12B-it` | `google/gemma-4-12B-it` | ✅ Yes | same |
| `gemma-4-26B-A4B-it` | `google/gemma-4-26B-A4B-it` | ✅ Yes | same |
| `gemma-4-31B-it` | `google/gemma-4-31B-it` | ✅ Yes | same |

### Ready to Onboard (`dspark`, DeepSeek-V4 Family)

As of August 2026, both DeepSeek-V4 presets are now in the KAITO model catalog (architecture `DeepseekV4ForCausalLM`), so `dspark` onboarding is no longer blocked on preset availability.

| KAITO preset | HF ID | In KAITO catalog? | Method |
|---|---|---|---|
| `deepseek-v4-flash-0731` | `deepseek-ai/DeepSeek-V4-Flash-0731` | ✅ Yes | `dspark` |
| `deepseek-v4-pro` | `deepseek-ai/DeepSeek-V4-Pro` | ✅ Yes | `dspark` |

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
| **Shipping (this proposal)** | `deepseek-r1-0528`, `deepseek-v3-0324` | `mtp`, `num_speculative_tokens: 3`, in `catalogOverrides` from day one |
| **Free-to-onboard next (same `mtp` path)** | `deepseek-v3.2`, `gemma-4-{E2B,E4B,12B,26B-A4B,31B}-it` | Needs one re-verification + one `catalogOverrides` entry each |
| **Ready to onboard (`dspark`)** | `deepseek-v4-flash-0731`, `deepseek-v4-pro` | Presets now in KAITO catalog; needs re-verification + `catalogOverrides` entry |
| **Deferred (EAGLE / MLP draft)** | Llama-3.1/3.3, Qwen3.*, Mistral-7B, etc. | Out of scope; needs draft-checkpoint sourcing design |
| **Universal opt-in (`ngram` / `suffix`)** | Any preset | Not part of initial commitment |

## TL;DR

- **User**: adds one annotation. Gets ~1.5×–1.7× interactive-latency win on supported presets, zero risk on unsupported presets (webhook rejects).
- **Preset maintainer**: adds a few lines to `catalogOverrides` and reruns the catalog generator; verification and tuning happen once, in Go review.
- **The per-preset config is not user-tunable by design.** Users who need that keep using the existing `inference_config.yaml` ConfigMap passthrough.
