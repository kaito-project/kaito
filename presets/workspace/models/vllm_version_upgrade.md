# Upgrading the vLLM Version

This is the maintainer runbook for bumping the vLLM engine version baked into the
KAITO preset base image. A vLLM bump touches more than one file: the pinned
dependency, the base image tag, the supported-architecture allowlist, the
parser mappings, and several engine-argument overrides. Work through every step
below — skipping one usually surfaces later as a pod that crash-loops at engine
init or a model that silently gets the wrong parser.

---

## 1. Bump the pinned dependency

- [`presets/workspace/dependencies/requirements.txt`](../dependencies/requirements.txt) — change `vllm==<old>` to `vllm==<new>`. This is the source of truth; the Dockerfile greps `VLLM_VERSION` from it and installs the matching CUDA wheel from `https://wheels.vllm.ai/${VLLM_VERSION}/cu129`.
- Re-pin the dependent stack in the same file when the new vLLM requires it: `torch`, `transformers`, `lmcache`, etc.

## 2. Rebuild the base image and bump its tag

The base image content changes with vLLM, so publish it under a **new tag** (the
node `imagePullPolicy` is `IfNotPresent`, so reusing a tag won't re-pull). See
[`docs/preset-image-building-process.md`](../../../docs/preset-image-building-process.md) for
the build itself; the Dockerfile is [`docker/presets/models/tfs/Dockerfile`](../../../docker/presets/models/tfs/Dockerfile).

Then bump the tag and the recorded engine version in **both** of these files
(they are hand-maintained mirrors and are diffed by `make compare-model-configs`):

- [`presets/workspace/models/supported_models.yaml`](supported_models.yaml) — the `base` entry:
  - `tag:` → the new base image tag,
  - `runtimeVersion.vllm:` → the new vLLM version,
  - `runtimeVersion.transformers:` → the transformers version if it changed,
  - add a line to the `# Tag history` comment block.
- [`charts/kaito/workspace/templates/supported-models-configmap.yaml`](../../../charts/kaito/workspace/templates/supported-models-configmap.yaml) — mirror the `tag:` and `runtimeVersion` changes.

```bash
make compare-model-configs   # must pass (compares the two files, ignoring comments)
```

The controller embeds `supported_models.yaml` via `go:embed`, so the controller
must be rebuilt for the new tag/version to take effect.

## 3. Regenerate the supported-architecture allowlist

[`presets/workspace/models/vllm_model_arch_list.txt`](vllm_model_arch_list.txt)
is the set of architectures KAITO will register (embedded via `go:embed` in
`vllm_model_arch_list.go`). vLLM adds/removes architectures every release, so
regenerate it against the **new** base image:

```bash
./hack/generate_vllm_arch_list.sh
```

The script runs `list_supported_llm_archs.py` inside the kaito-base image whose
tag it reads from `supported_models.yaml` — so run it **after** step 2 (the tag
bump), and after the new base image is published/pullable. Commit the diff.

> Architectures dropped by the new vLLM (moved to `_PREVIOUSLY_SUPPORTED_MODELS`)
> will disappear from the list. If any onboarded catalog model uses such an
> architecture, that model must be removed or its arch pinned to an older image.

## 4. Sync the parser mappings

vLLM's reasoning and tool-call parser registries change across releases (parsers
added, renamed, or removed). Reconcile the four maps in
[`presets/workspace/generator/generator.go`](../generator/generator.go)
against the authoritative registries for the **new** version:

- Reasoning: `vllm/reasoning/__init__.py` → `_REASONING_PARSERS_TO_REGISTER`
- Tool call: `vllm/tool_parsers/__init__.py` → `_TOOL_PARSERS_TO_REGISTER`

Fetch them pinned to the tag, e.g.
`https://raw.githubusercontent.com/vllm-project/vllm/v<new>/vllm/reasoning/__init__.py`.

Maps to reconcile:

| Map | Keyed by | Value must be… |
| --- | --- | --- |
| `reasoningParserModeNamePrefixMap` | lowercased, org-stripped model-name prefix | a registered reasoning parser name |
| `reasoningParserArchMap` | architecture name | a registered reasoning parser name |
| `toolCallParserModeNamePrefixMap` | lowercased, org-stripped model-name prefix | a registered tool parser name |
| `toolCallParserArchMap` | architecture name | a registered tool parser name |

Checklist:

1. **Every VALUE** in all four maps must still be a registered parser name. A
   value that vLLM removed/renamed → the pod fails at engine init with an invalid
   `--reasoning-parser` / `--tool-call-parser`.
2. **Name-prefix takes precedence over the arch map** (name is matched first,
   then arch as a fallback). A too-broad prefix key can shadow an onboarded
   model and give it the wrong parser — prefer specific prefixes.
3. Add mappings for newly-supported architectures you intend to onboard. Use the
   vLLM model registry (`vllm/model_executor/models/registry.py`) for the exact
   architecture names, and the model's vLLM recipe (`https://recipes.vllm.ai/`)
   for the correct `--reasoning-parser` / `--tool-call-parser` values.
4. Map keys in the `…ModeNamePrefixMap`s must be lowercase
   (`TestReasoningParserMap` / `TestToolCallParserMap` enforce this).

## 5. Re-validate the fragile integrations

These have historically broken across vLLM bumps — smoke-test the code path, not
just the import:

- **DeepGEMM** (FP8 block-quant models: DeepSeek-V4/V3.2, GLM MoE DSA): requires a
  runtime CUDA toolkit + `VLLM_USE_DEEP_GEMM=1`. The default JIT backend and the
  set of kernels it needs (nvcc vs NVRTC) can change.
- **FlashInfer** (JIT kernels for sampling, RoPE, allreduce fusion): needs nvcc at
  runtime; KAITO disables the sampler + the allreduce-RMS fusion to avoid it.
- **LMCache** (CPU KV offload): API and the abort-path bug both shift between
  versions; KAITO disables it for hybrid-KV / MIG models.
- **guidellm** (startup benchmark): its `BenchmarkGenerativeTextArgs` API changed
  across releases — verify `benchmark_entrypoint.py` still constructs it.

## 6. Test and validate

```bash
make fmt vet lint unit-test          # Go: includes generator + model tests
make compare-model-configs           # supported_models.yaml vs configmap
ruff check --output-format=github .  # Python
ruff format --check .
make inference-api-e2e               # Python inference wrapper tests
```

Then deploy at least one small preset model on the new base image and confirm it
reaches `Ready` and answers a chat completion.

---

## File reference

| Concern | File |
| --- | --- |
| Pinned vLLM version + deps | [`presets/workspace/dependencies/requirements.txt`](../dependencies/requirements.txt) |
| Base image build | [`docker/presets/models/tfs/Dockerfile`](../../../docker/presets/models/tfs/Dockerfile) |
| Base image tag + `runtimeVersion` | [`presets/workspace/models/supported_models.yaml`](supported_models.yaml) |
| Base image tag mirror | [`charts/kaito/workspace/templates/supported-models-configmap.yaml`](../../../charts/kaito/workspace/templates/supported-models-configmap.yaml) |
| Supported-arch allowlist | [`presets/workspace/models/vllm_model_arch_list.txt`](vllm_model_arch_list.txt) |
| Arch-list generator | [`hack/generate_vllm_arch_list.sh`](../../../hack/generate_vllm_arch_list.sh) |
| Parser maps + engine-arg overrides | [`presets/workspace/generator/generator.go`](../generator/generator.go) |
| Controller-side engine args | [`pkg/model/interface.go`](../../../pkg/model/interface.go) |
| Runtime wrapper / defaults | [`presets/workspace/inference/vllm/inference_api.py`](../inference/vllm/inference_api.py) |
| Image build details | [`docs/preset-image-building-process.md`](../../../docs/preset-image-building-process.md) |
| Model onboarding | [`presets/workspace/models/model_catalog.md`](model_catalog.md) |
