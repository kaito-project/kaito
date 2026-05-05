---
title: RAGEngine Guardrails UX and API
authors:
  - "@xiaoqi-7"
reviewers:
  - "@Fei-Guo"
creation-date: 2026-04-16
last-updated: 2026-05-05
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

### Runtime Failure Semantics

Output guardrails wrap an external ML pipeline (`llm_guard`) whose scanners may fail at
runtime (e.g. GPU OOM, model download failure, tokenizer errors, library bugs). The
runtime exposes an operator-level switch to choose between availability and safety when
this happens.

The switch is delivered as a pod-level environment variable rather than a policy-file
field, because it must remain effective even when the policy file itself fails to load.

| Env Var                          | Default | Description                                                                                  |
| -------------------------------- | ------- | -------------------------------------------------------------------------------------------- |
| `OUTPUT_GUARDRAILS_ENABLED`      | `false` | Master switch. When `false`, guardrails are bypassed entirely. |
| `OUTPUT_GUARDRAILS_FAIL_OPEN`    | `true`  | When guardrails themselves break (e.g. GPU OOM, model load error). `true` lets the response through unscanned; `false` returns HTTP 500. Has no effect on normal redact/block behavior. |
| `OUTPUT_GUARDRAILS_POLICY_PATH`  | `""`    | Path to the policy ConfigMap YAML. When unset, the runtime falls back to the default ConfigMap shipped with the system. |

Behavior:

- **Fail-open** (`OUTPUT_GUARDRAILS_FAIL_OPEN=true`, default): If a scanner raises during
  `guard_response`, the runtime logs `output_guardrails_failed` with the response id and
  returns the original LLM response unchanged. Preserves availability; trades safety. This
  matches the prior implicit behavior and is the default for backward compatibility.
- **Fail-closed** (`OUTPUT_GUARDRAILS_FAIL_OPEN=false`): On the same failure, the runtime
  raises `OutputGuardrailsError`, which the `/v1/chat/completions` handler maps to
  `HTTP 500` with a fixed detail message
  (`"Output guardrails failed while scanning the model response."`). The original
  exception is preserved via `__cause__` for logs, but is not exposed in the HTTP body.
  Recommended for regulated workloads (PII, PHI, public-facing endpoints).

Operator guidance: fail-closed should be paired with model pre-warming, dedicated GPU
quota for guardrails, and Prometheus alerts on `output_guardrails_failed` log volume to
avoid converting transient ML failures into request errors.

Example deployment (fail-closed for a regulated workload):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ragengine-guardrails-failclosed
spec:
  containers:
    - name: ragengine
      image: kaito/ragengine:latest
      env:
        - name: OUTPUT_GUARDRAILS_ENABLED
          value: "true"
        - name: OUTPUT_GUARDRAILS_FAIL_OPEN
          value: "false"
        - name: OUTPUT_GUARDRAILS_POLICY_PATH
          value: /etc/ragengine/guardrails.yaml
      volumeMounts:
        - name: guardrails-policy
          mountPath: /etc/ragengine
  volumes:
    - name: guardrails-policy
      configMap:
        name: ragengine-guardrails-policy
```

Sample HTTP response when a scanner fails under fail-closed:

```http
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"detail": "Output guardrails failed while scanning the model response."}
```

Future work may introduce per-scanner fail modes inside the policy YAML; the env-level
switch will remain as the global default and as the fallback when policy parsing itself
fails.

## Deferred Scope

This proposal defines the UX shape only. The following items are deferred to follow-up
implementation PRs:

- YAML policy loading implementation
- default ConfigMap wiring
- scanner registry and additional scanners
- audit event model
- streaming scanning behavior
- per-scanner fail modes inside the policy YAML

## Follow-Up Implementation Plan

This proposal is intended to support the following implementation sequence:

1. Land the initial non-streaming output guardrails hook.
2. Define explicit error-handling semantics. *(implemented: `OUTPUT_GUARDRAILS_FAIL_OPEN`
   env switch and `OutputGuardrailsError → HTTP 500` mapping; see Runtime Failure
   Semantics above.)*
3. Introduce a runtime YAML policy loader.
4. Add default ConfigMap support.
5. Refactor scanner construction into a registry/factory structure.
6. Add more scanners in small batches.
7. Add audit foundations.
8. Add minimal streaming scanning support.
9. Polish graceful UX and operational behavior.

The CRD exposure for `guardrails.enabled` can be added later if we decide the final user
experience should include an explicit RAGEngine spec toggle rather than relying only on
ConfigMap-based policy.