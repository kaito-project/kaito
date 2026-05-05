---
title: RAGEngine Guardrails UX and API
authors:
  - "@xiaoqi-7"
reviewers:
  - "@Fei-Guo"
creation-date: 2026-04-16
last-updated: 2026-05-19
status: provisional
see-also:
  - "/docs/proposals/20250715-inference-aware-routing-layer.md"
---

## Summary

This proposal defines the intended user-facing model for RAGEngine output guardrails.
The goal is to keep the CRD surface minimal while allowing guardrail policy to evolve as
we add more scanners and runtime capabilities.

The proposed user model is:

```yaml
spec:
  guardrails:
    enabled: true
```

Detailed guardrail behavior is stored in a ConfigMap as YAML rather than modeled as
scanner-specific fields in the `RAGEngine` CRD.

## Goals

- Define a small, stable UX entry point for enabling RAGEngine guardrails.
- Keep detailed guardrail policy outside the CRD in a ConfigMap-backed YAML document.
- Allow scanner additions and policy evolution without repeated CRD changes.

## Non-Goals

- Implement the full runtime behavior in this PR.
- Expose scanner-specific configuration in the CRD.
- Finalize streaming, auditing, or error-handling semantics in this document.

## Proposed UX and API Shape

### Minimal CRD Entry Point

The intended user-facing switch is a minimal `guardrails.enabled` field in the
`RAGEngine` spec.

```yaml
apiVersion: kaito.sh/v1beta1
kind: RAGEngine
metadata:
  name: ragengine-with-guardrails
spec:
  guardrails:
    enabled: true
```

At this stage, the proposal does not add scanner-specific CRD fields such as `action`,
`scanners`, `patterns`, or `blockMessage`.

### ConfigMap-Based YAML Policy

Detailed policy is defined in YAML and delivered through a ConfigMap.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ragengine-guardrails-policy
data:
  guardrails.yaml: |
    action: redact
    blockMessage: The model output was blocked by output guardrails.
    scanners:
      - type: regex
        patterns:
          - 'https?://\\S+'
      - type: ban_substrings
        substrings:
          - secret
```

The exact YAML schema can evolve, but the design principle is fixed: detailed policy lives
in ConfigMap YAML, not in the CRD.

### Default ConfigMap Support

Follow-up implementation may provide a default ConfigMap and default mount path so that
guardrail policy can be enabled without introducing a broad CRD surface in the same step.

## Deferred Scope

This proposal defines the UX shape only. The following items are deferred to follow-up
implementation PRs:

- runtime error-handling semantics
- YAML policy loading implementation
- default ConfigMap wiring
- scanner registry and additional scanners
- audit event model
- streaming scanning behavior

## Follow-Up Implementation Plan

This proposal is intended to support the following implementation sequence:

1. Land the initial non-streaming output guardrails hook. (done)
2. Define explicit error-handling semantics. (done — fail-open with metrics)
3. Introduce a runtime YAML policy loader. (done)
4. Add hot-reload of the guardrails policy ConfigMap. (done)
5. Add default ConfigMap support.
6. Refactor scanner construction into a registry/factory structure.
7. Add more scanners in small batches.
8. Add audit foundations.
9. Add minimal streaming scanning support.
10. Polish graceful UX and operational behavior.

### Hot-reload runtime behavior (implemented)

The RAG runtime watches the guardrails policy file (`OUTPUT_GUARDRAILS_POLICY_PATH`)
for changes using `watchfiles` (inotify on Linux) and atomically swaps the active
`OutputGuardrails` instance when the file content changes. ConfigMap volume updates
are picked up because the runtime watches the parent directory, which catches the
atomic `..data` symlink swap performed by kubelet.

Reload semantics:

- Fail-safe: if the new policy fails to load, the previous policy stays in effect
  and a failure metric is incremented.
- Debounced: changes are coalesced over a configurable window
  (`OUTPUT_GUARDRAILS_HOT_RELOAD_DEBOUNCE_SECONDS`, default `60`) to avoid reload
  storms during rolling ConfigMap updates.
- Optional: hot-reload can be disabled via
  `OUTPUT_GUARDRAILS_HOT_RELOAD_ENABLED=false`, in which case the policy is loaded
  once at startup.

Observability:

- `guardrails_policy_reload_total{result="success|failure|noop"}` counter.
- `guardrails_policy_loaded_timestamp_seconds` gauge.

The CRD exposure for `guardrails.enabled` can be added later if we decide the final user
experience should include an explicit RAGEngine spec toggle rather than relying only on
ConfigMap-based policy.