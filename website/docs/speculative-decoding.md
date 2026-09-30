---
title: Speculative Decoding
---

KAITO supports an **annotation-driven speculative decoding opt-in** for vLLM-based preset inference.

When enabled, KAITO injects a vLLM `--speculative-config` automatically for the workload:

- for a small set of presets with KAITO-tuned speculative decoding, KAITO injects a preset-specific **`mtp`** config
- for every other vLLM preset, KAITO falls back to a universal **`ngram`** config

This gives you a lightweight way to try speculative decoding without manually building `speculative-config` JSON for each deployment.

## Prerequisites

Speculative decoding in KAITO currently requires all of the following:

- a **preset-based** inference workload
- the **vLLM** runtime
- explicit opt-in via the `kaito.sh/enable-speculative-decoding: "true"` annotation

KAITO validates these requirements at admission time.

## Supported resources

You can enable speculative decoding on:

- [`Workspace`](./workspace.md)
- [`InferenceSet`](./inference.md)
- [`MultiRoleInference`](./prefill-decode-disaggregation.md)

Where to place the annotation:

- **Workspace**: top-level `metadata.annotations`
- **InferenceSet**: `spec.template.metadata.annotations`
- **MultiRoleInference**: top-level `metadata.annotations`

## How KAITO chooses the speculative decoding config

When the annotation is set to `"true"`, KAITO evaluates the preset and injects one of the following:

| Case | Injected method | Current injected config |
| --- | --- | --- |
| Preset has KAITO-tuned speculative decoding config | `mtp` | `num_speculative_tokens=1` |
| Any other vLLM preset | `ngram` | `num_speculative_tokens=5`, `prompt_lookup_max=4` |
| User already set `vllm.speculative-config` in `inference_config.yaml` | none | KAITO leaves the user config unchanged |

This means speculative decoding is available for **any vLLM preset that KAITO accepts**. Tuned presets get model-specific `mtp`; everything else gets the generic `ngram` fallback.

## Current preset-tuned `mtp` models

As of this implementation, KAITO injects a tuned `mtp` config for these presets:

- `deepseek-r1-0528`
- `deepseek-v3-0324`
- `deepseek-ai/DeepSeek-V3.2`
- `zai-org/GLM-5.2-FP8`
- `nvidia/DeepSeek-V4-Flash-NVFP4`
- `XiaomiMiMo/MiMo-7B-Base`
- `Qwen/Qwen3.5-2B`
- `Qwen/Qwen3.5-4B`
- `Qwen/Qwen3.5-9B`
- `Qwen/Qwen3.5-122B-A10B-GPTQ-Int4`
- `Qwen/Qwen3.5-122B-A10B`
- `Qwen/Qwen3.5-397B-A17B-GPTQ-Int4`
- `Qwen/Qwen3.6-35B-A3B-FP8`
- `Qwen/Qwen3.6-35B-A3B`
- `Qwen/Qwen3.6-27B`

All other vLLM presets fall back to the universal `ngram` configuration.

## Quickstart

### Workspace

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-deepseek-r1
  annotations:
    kaito.sh/enable-speculative-decoding: "true"
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: workspace-deepseek-r1
inference:
  preset:
    name: "deepseek-r1-0528"
```

### InferenceSet

```yaml
apiVersion: kaito.sh/v1beta1
kind: InferenceSet
metadata:
  name: phi-4-mini
spec:
  replicas: 2
  labelSelector:
    matchLabels:
      apps: phi-4-mini
  template:
    metadata:
      annotations:
        kaito.sh/enable-speculative-decoding: "true"
    resource:
      instanceType: "Standard_NC24ads_A100_v4"
    inference:
      preset:
        name: "microsoft/Phi-4-mini-instruct"
```

In this example, `microsoft/Phi-4-mini-instruct` does not have a KAITO-tuned `mtp` entry, so KAITO injects the universal `ngram` fallback.

### MultiRoleInference

```yaml
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: qwen35-4b
  annotations:
    kaito.sh/enable-speculative-decoding: "true"
spec:
  labelSelector:
    matchLabels:
      apps: qwen35-4b
  model:
    name: Qwen/Qwen3.5-4B
  roles:
    - type: prefill
      replicas: 1
      instanceType: Standard_NC24ads_A100_v4
    - type: decode
      replicas: 1
      instanceType: Standard_NC24ads_A100_v4
```

## User override behavior

If you already manage speculative decoding explicitly in your inference ConfigMap, KAITO does **not** overwrite it.

For the override to take effect, the workload must actually reference that ConfigMap:

- **Workspace**: `inference.config`
- **InferenceSet**: `spec.template.inference.config`
- **MultiRoleInference**: `spec.roles[*].runtimeConfig`

For example, if `inference_config.yaml` already contains `vllm.speculative-config`, KAITO skips auto-injection for a referenced Workspace config:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-inference-config
data:
  inference_config.yaml: |
    vllm:
      speculative-config: '{"method":"ngram","num_speculative_tokens":8,"prompt_lookup_max":6}'
---
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-phi-4-mini
  annotations:
    kaito.sh/enable-speculative-decoding: "true"
resource:
  instanceType: "Standard_NC24ads_A100_v4"
  labelSelector:
    matchLabels:
      apps: workspace-phi-4-mini
inference:
  preset:
    name: "microsoft/Phi-4-mini-instruct"
  config: my-inference-config
```

This lets you use KAITO's annotation-based default when you want the simple path, and switch to a fully explicit vLLM configuration when you need more control.

## Rollout behavior

Changing `kaito.sh/enable-speculative-decoding` triggers reconciliation of the serving workload:

- direct `Workspace` deployments pick up the change through the workspace rollout path
- `InferenceSet` propagates the annotation to its child Workspaces
- `MultiRoleInference` propagates the annotation to its child `InferenceSet` resources and then to their Workspaces

This means both enabling and disabling speculative decoding are handled through normal KAITO reconciliation rather than requiring manual pod edits.

## Validation rules

KAITO rejects invalid configurations such as:

- annotation value other than `"true"` or `"false"`
- speculative decoding enabled without a preset inference target
- speculative decoding enabled for a non-vLLM runtime

## Monitoring

Once speculative decoding is enabled, vLLM exposes speculative decoding metrics that can be scraped alongside the rest of the inference metrics. See [Monitoring](./monitoring.md) for the current metric list.

## Notes and limitations

- This feature is currently implemented for **vLLM** inference only.
- KAITO's built-in fallback is intentionally conservative: `ngram` with `num_speculative_tokens=5` and `prompt_lookup_max=4`.
- Tuned `mtp` entries are model-specific and may expand over time as KAITO adds validated presets.
- Real performance gains are workload-dependent. Repetition-heavy workloads usually benefit more than highly open-ended generation.
