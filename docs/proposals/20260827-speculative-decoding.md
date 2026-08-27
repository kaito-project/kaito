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
- Add a typed `SpeculativeDecoding` field to `model.Metadata` in `supported_models.yaml`, propagated through `PresetParam` / `Generator` / `vLLMCompatibleModel` so both the controller and the admission webhook can read it
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

**The config lives in `supported_models.yaml`, but the data flow to get it into the controller and webhook does not exist today — this proposal specifies it.**

Today: `metadata.go` loads `supported_models.yaml` into a private map used only for validation; `vllm_model.go` redirects short preset names to full HuggingFace IDs and constructs the registered runtime model out of `model_catalog.yaml`. `ModelRegister` also has no `Get` API that exposes metadata to callers. So a field added only to `supported_models.yaml` is invisible at reconcile and admission time.

An earlier draft put `SpeculativeDecoding` on `CatalogEntry` (populated via `catalogOverrides`). Three problems with that:

1. **Semantic mismatch.** `CatalogEntry` / `model_catalog.yaml` is generated from HuggingFace `config.json` and holds model-metadata facts (architecture, hidden sizes, token limits, quant config). Speculative decoding is an operator-side runtime choice about a maintainer-validated flag — not a model-metadata fact.
2. **Webhook can't read it.** `model_catalog.yaml` (embedded by `presets/workspace/models/vllm_model.go`) is baked into the *preset image* at build time. The controller and admission webhook binaries do not carry it.
3. **Controller drops it.** Even inside the generator, `CatalogEntry` is parsed into `model.PresetParam`; `vLLMCompatibleModel` retains only the fields needed for run parameters. Extending `CatalogEntry` alone would discard `SpeculativeDecoding` before reconciliation.

The fix is a small, explicit plumbing change (part of this proposal, not "already there"):

1. Add `SpeculativeDecoding` to the entry type parsed out of `supported_models.yaml` in `metadata.go`, and store it in the per-preset metadata map.
2. Extend `vllm_model.go`'s registration path so the `SpeculativeDecoding` field is copied from that metadata map into the runtime `model.Model` returned via `plugin.KaitoModelRegister` — alongside the HF-ID redirection it already does.
3. Add a `GetMetadata(name string) *model.Metadata` (or equivalent) method on `ModelRegister` so the admission webhook can look up the field directly without going through the runtime-model path.

After these three changes, both the controller (via the registered `model.Model`) and the webhook (via `KaitoModelRegister.GetMetadata`) see the same `SpeculativeDecoding` value from a single source of truth. `model_catalog.yaml` is not touched by this proposal.

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

type MTPConfig struct {
    NumSpeculativeTokens int `yaml:"numSpeculativeTokens"`
}

type NGramConfig struct {
    NumSpeculativeTokens int `yaml:"numSpeculativeTokens"`
    PromptLookupMax      int `yaml:"promptLookupMax"`
}

type DSparkConfig struct {
    // Model is the assistant/draft checkpoint reference (e.g.
    // "deepseek-ai/DeepSeek-V4-Flash-0731-DSpark") serialized as
    // speculative_config.model in the vLLM JSON blob. DSpark uses a
    // separate draft checkpoint, so this field is required for the
    // converter to emit valid vLLM configuration.
    Model                string `yaml:"model"`
    NumSpeculativeTokens int    `yaml:"numSpeculativeTokens"`
}

// Added to model.Metadata (or PresetParam).
type Metadata struct {
    // ... existing fields ...
    SpeculativeDecoding *SpeculativeDecodingConfig `yaml:"speculativeDecoding,omitempty"`
}
```

The typed sub-structs constrain the shape, but they do not by themselves prevent an author from pairing `Method: "mtp"` with a nil `MTP` or a populated `NGram`. Validation happens in two places:

- **Catalog generation** (unit test over `supported_models.yaml`): asserts method / sub-struct exclusivity (exactly one non-nil sub-config, matching `method`), required positive fields (`numSpeculativeTokens > 0`, `promptLookupMax > 0` for `ngram`), and supported methods. Fails the build if a maintainer authors a broken entry.
- **Runtime** (`vllmFormat`, Step 3): returns a typed error rather than dereferencing a possibly-nil pointer.

`DSpark` is included even though DeepSeek-V4 onboarding is a follow-up, so the type surface is stable when the next preset lights up.

#### Step 2 — Declare per-preset config in `supported_models.yaml`

`presets/workspace/models/supported_models.yaml`:

```yaml
- name: deepseek-r1-0528
  type: text-generation
  runtime: vllm
  # ... existing fields ...
  speculativeDecoding:
    method: mtp
    mtp:
      numSpeculativeTokens: 1

- name: deepseek-v3-0324
  type: text-generation
  runtime: vllm
  # ... existing fields ...
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

```go
if ws.Annotations["kaito.sh/enable-speculative-decoding"] == "true" {
    meta := plugin.KaitoModelRegister.GetMetadata(ws.Inference.Preset.Name)
    if meta != nil && meta.SpeculativeDecoding != nil {
        // Skip injection if the user already provided --speculative-config
        // via inference_config.yaml passthrough. ConfigMap wins to preserve
        // the power-user escape hatch. See Step 5 for precedence rules.
        if !userSpecifiedSpeculativeConfig(inferenceConfig) {
            blob, err := vllmFormat(meta.SpeculativeDecoding)
            if err != nil {
                return fmt.Errorf("speculative decoding: %w", err)
            }
            // ModelRunParams does NOT shell-quote. Wrap in single quotes
            // and escape embedded single quotes so /bin/sh sees exactly
            // one argv token.
            runParams["speculative-config"] = shellSingleQuote(blob)
        }
    }
}

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
        // dspark ships as a separate assistant checkpoint referenced via
        // speculative_config.model. Until DSparkConfig.Model is populated
        // (see "Ready to Onboard" model coverage), the converter must
        // reject dspark rather than emit a method-only blob vLLM cannot
        // resolve.
        if sd.DSpark == nil || sd.DSpark.Model == "" {
            return "", fmt.Errorf("method=dspark requires dspark.model (assistant checkpoint reference)")
        }
        m["model"] = sd.DSpark.Model
        m["num_speculative_tokens"] = sd.DSpark.NumSpeculativeTokens
    default:
        // Belt-and-suspenders: caught earlier by unit tests over
        // supported_models.yaml, but never silently emit a method-only blob.
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

    // (b) Preset must have a validated config in supported_models.yaml.
    meta := plugin.KaitoModelRegister.GetMetadata(ws.Inference.Preset.Name)
    if meta == nil || meta.SpeculativeDecoding == nil {
        return fmt.Errorf(
            "preset %q does not have a validated speculative decoding configuration; "+
            "remove kaito.sh/enable-speculative-decoding annotation or choose a "+
            "supported preset (e.g. deepseek-r1-*, deepseek-v3-*)",
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

    // (d) Reject pipeline parallelism. resource.count is NOT the resolved
    //     vLLM node count — the controller computes status.targetNodeCount
    //     from model size and SKU AFTER admission (workspace_controller.go
    //     lines 1578-1621). A nil or 1 count can still become multiple
    //     nodes and pull in PP. So we enforce single-node compatibility
    //     using the same estimator the controller uses, and add a
    //     reconcile-time guard before injection.
    //
    //     Admission time (this webhook): reject if the resource estimator
    //     already knows the preset requires PP for the resolved SKU.
    //     Reconcile time (Step 3 injection site): re-check
    //     status.targetNodeCount and refuse to inject if it exceeds 1,
    //     emitting a Workspace condition explaining why.
    if targetNodes, err := estimateTargetNodeCount(ws); err == nil && targetNodes > 1 {
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
- When the webhook can see the referenced ConfigMap and detects both sources present, it emits a **Kubernetes Event** (`SpeculativeDecodingConfigMapOverride`) on the Workspace at admission and reconcile time — not an admission-response warning, because the current Knative resource-semantics validation path returns only `*apis.FieldError` and there is no user-visible admission-warning mechanism in the repo today. Events show up in `kubectl describe workspace` / `kubectl get events` and are the existing user-visible signal path.

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

| KAITO preset | HF ID | In KAITO catalog? | Method | `num_speculative_tokens` | Extra memory / download |
|---|---|---|---|---|---|
| `deepseek-r1-0528` | `deepseek-ai/DeepSeek-R1-0528` | ✅ Yes | `mtp` | 1 | none — MTP head is in the checkpoint |
| `deepseek-v3-0324` | `deepseek-ai/DeepSeek-V3-0324` | ✅ Yes | `mtp` | 1 | none — same |

⚠️ Note: the distilled presets `DeepSeek-R1-Distill-Llama-8B` and `DeepSeek-R1-Distill-Qwen-14B` are **not** MTP candidates — they are Llama / Qwen architectures with no MTP head in the checkpoint.

### Free-to-Onboard Next (Same `mtp` Path, No Extra Memory / Download)

Only the DeepSeek continuation ships as a zero-download addition — its MTP head is bundled in the same checkpoint as the base weights, matching the shipping presets.

| KAITO preset | HF ID | In KAITO catalog? | Notes / vLLM evidence |
|---|---|---|---|
| `deepseek-v3.2` | `deepseek-ai/DeepSeek-V3.2` | ✅ Yes | DeepSeek-V3 family continuation; same in-checkpoint MTP path |

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

As of August 2026, the base DeepSeek-V4 presets (`deepseek-v4-flash-0731`, `deepseek-v4-pro`) are in the KAITO model catalog, but the base checkpoints alone are **not** ready for `dspark`. Upstream vLLM `dspark` usage targets the `DeepSeek-V4-*-DSpark` variants and supplies that checkpoint via `speculative_config.model` — i.e., the draft path is a separate model reference, not a self-contained head in the base checkpoint.

As a result, `dspark` onboarding requires:

1. Sourcing / mirroring the `DeepSeek-V4-*-DSpark` checkpoint into the KAITO preset image (or making it fetchable at pod startup).
2. Extending `SpeculativeDecodingConfig` (specifically `DSparkConfig`) with a `Model` / `Checkpoint` field so the typed config can carry the draft-model reference; `catalogOverrides`-style method-only entries are not sufficient.
3. Re-verifying against KAITO's pinned vLLM version.

The type surface (`DSparkConfig`) is defined up front in Step 1 so this future work is a field addition, not a re-shape.

| KAITO preset (base) | HF ID | DSpark checkpoint needed | Method |
|---|---|---|---|
| `deepseek-v4-flash-0731` | `deepseek-ai/DeepSeek-V4-Flash-0731` | `DeepSeek-V4-Flash-0731-DSpark` | `dspark` |
| `deepseek-v4-pro` | `deepseek-ai/DeepSeek-V4-Pro` | `DeepSeek-V4-Pro-DSpark` | `dspark` |

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
| **Shipping (this proposal)** | `deepseek-r1-0528`, `deepseek-v3-0324` | `mtp`, `numSpeculativeTokens: 1`, in `supported_models.yaml` from day one |
| **Free-to-onboard next (same `mtp` path)** | `deepseek-v3.2`, `gemma-4-{E2B,E4B,12B,26B-A4B,31B}-it` | Needs one re-verification + one `supported_models.yaml` entry each |
| **Ready to onboard (`dspark`)** | `deepseek-v4-flash-0731`, `deepseek-v4-pro` | Base presets in catalog, but needs `DeepSeek-V4-*-DSpark` draft checkpoints sourced + `DSparkConfig.Model` field added |
| **Deferred (EAGLE / MLP draft)** | Llama-3.1/3.3, Qwen3.*, Mistral-7B, etc. | Out of scope; needs draft-checkpoint sourcing design |
| **Universal opt-in (`ngram` / `suffix`)** | Any preset | Not part of initial commitment |

## TL;DR

- **User**: adds one annotation. Gets ~1.6×–1.7× interactive-latency win (QPS ≈ 1) on supported presets, degrading to ~1.2×–1.3× at QPS 2–4 and regressing under saturated traffic. Zero risk on unsupported presets (webhook rejects at `apply` time).
- **Preset maintainer**: adds a few lines to `supported_models.yaml`; validation runs at build time via a catalog-generation unit test.
- **The per-preset config is not user-tunable by design.** Users who need that keep using the existing `inference_config.yaml` ConfigMap passthrough (which takes precedence — see Step 5).
