---
title: RAGEngine Guardrails Current Behavior
authors:
  - "@xiaoqi-7"
reviewers:
  - "@Fei-Guo"
creation-date: 2026-04-16
last-updated: 2026-05-25
status: implemented
see-also:
  - "/docs/proposals/20250715-inference-aware-routing-layer.md"
---

## Summary

This document describes the current user-visible behavior of RAGEngine output
guardrails as implemented today.

The current model is intentionally small at the CRD layer:

```yaml
spec:
  guardrails:
    enabled: true
```

Detailed scanner behavior lives in a mounted `guardrails.yaml` policy file rather
than in scanner-specific CRD fields.

## Current Support

- Output guardrails run on non-streaming `/v1/chat/completions` responses.
- The runtime currently supports two scanner types:
  - `regex`
  - `ban_substrings`
- Each scanner can use `action: redact` or `action: block`.
- Policies are loaded from a YAML file referenced by
  `OUTPUT_GUARDRAILS_POLICY_PATH`.
- Policy hot reload is supported.
- Guardrails expose Prometheus metrics and structured logs.

## Current Non-Support

- Streaming guardrails are not implemented.
- Scanner-specific CRD fields are not implemented.
- Per-scanner fail-open/fail-closed controls are not implemented.
- Audit event storage is not implemented.

## UX and Configuration

### Minimal CRD Entry Point

The user-facing enablement switch remains a minimal `guardrails.enabled` field.

```yaml
apiVersion: kaito.sh/v1beta1
kind: RAGEngine
metadata:
  name: ragengine-with-guardrails
spec:
  guardrails:
    enabled: true
```

The CRD does not currently expose scanner-specific fields such as `action`,
`patterns`, `substrings`, or `blockMessage`.

### ConfigMap-Based YAML Policy

Detailed policy is defined in YAML and delivered through a ConfigMap-mounted file.

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
        action: redact
        patterns:
          - '-----BEGIN (?:[A-Z ]+)?PRIVATE KEY-----'
      - type: ban_substrings
        action: block
        substrings:
          - secret
```

Policy parsing is permissive by design:

- unknown scanner types are skipped
- invalid scanner configs are skipped
- invalid actions fall back to the default action
- one invalid scanner does not prevent other valid scanners from running

### Default ConfigMap Support

If `spec.guardrails.enabled` is `true` and `configMapRef` is not set, the
controller copies the default guardrails policy ConfigMap
(`ragengine-guardrails-policy-template`) into the RAGEngine namespace and mounts
it into the Pod.

- Auto-copied ConfigMaps are namespace-scoped shared resources and do not carry
  an `OwnerReference` to any individual RAGEngine. This avoids deleting a
  shared ConfigMap during cleanup of one RAGEngine while other RAGEngines in the
  same namespace still depend on it.
- User-provided ConfigMaps are not modified or owned by the controller.

The default template provides a conservative deterministic baseline for
credential and lightweight PII leakage:

- a regex scanner for a few high-signal token formats
- a `secrets` scanner backed by `detect-secrets`
- a lightweight `sensitive` scanner for common PII patterns

The regex scanner covers obvious credential leakage, including:

- PEM private key headers
- AWS access key IDs (`AKIA...`)
- Google API keys (`AIza...`)
- GitHub tokens (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`)
- `sk-...` style API keys
- `Bearer ...` authorization tokens

The `sensitive` scanner covers:

- email addresses
- phone numbers
- credit card-like numbers (Luhn-validated)
- IPv4 addresses

This is baseline protection, not a complete content-safety policy. Additional
scanners can still be added via a custom ConfigMap.

## Supported Scanners

For clarity, the single-scanner examples below show only scanner-level
`action` values. Multi-scanner policies can also set an optional top-level
default `action` for scanner entries that omit one.

Common action behavior:

- `redact`: scanner-specific sanitization is applied and scanning continues
- `block`: the full assistant message is replaced with `blockMessage`

### `regex`

Purpose:
Match output content against one or more regular expressions.

YAML shape:

```yaml
scanners:
  - type: regex
    action: redact
    patterns:
      - '(?i)ssn:\\s*\\d{3}-\\d{2}-\\d{4}'
```

Supported fields:

- `patterns`: required non-empty list of regex strings
- `match_type`: optional, defaults to `search`
- `is_blocked`: optional boolean, defaults to `true`

### `ban_substrings`

Purpose:
Match output content against exact substrings using llm-guard substring matching.

Example use case:
Block common internal-only disclosure labels in model output.

YAML shape:

```yaml
scanners:
  - type: ban_substrings
    action: redact
    substrings:
      - For Internal Use Only
      - Do Not Distribute
    match_type: word
```

Supported fields:

- `substrings`: required non-empty list of strings
- `match_type`: optional, defaults to `word`
- `case_sensitive`: optional boolean, defaults to `false`
- `contains_all`: optional boolean, defaults to `false`

### `json`

Purpose:
Validate that output is parseable JSON.

YAML shape:

```yaml
scanners:
  - type: json
    action: redact
    repair: false
```

Supported fields:

- `required_elements`: optional non-negative integer, defaults to `0`
- `repair`: optional boolean, defaults to `true`

Action behavior notes:

- with `redact`, valid JSON is returned as-is and invalid output is handled by the runtime action

### `reading_time`

Purpose:
Limit output length by estimated reading time.

YAML shape:

```yaml
scanners:
  - type: reading_time
    action: redact
    max_time: 0.25
    truncate: true
```

Supported fields:

- `max_time`: required positive number, expressed in minutes
- `truncate`: optional boolean, defaults to `false`

Action behavior notes:

- with `redact`, `truncate: true` shortens over-limit output to fit

### `secrets`

Purpose:
Detect likely secrets in output using llm-guard's deterministic secrets scanner.

YAML shape:

```yaml
scanners:
  - type: secrets
    action: redact
    redact_mode: partial
```

Supported fields:

- `redact_mode`: optional string, one of `all`, `partial`, `hash`; defaults to `all`

### `sensitive`

Purpose:
Detect common sensitive data in output such as email addresses, phone numbers,
credit cards, and IPv4 addresses.

YAML shape:

```yaml
scanners:
  - type: sensitive
    action: redact
    detectors:
      - email
```

Supported fields:

- `detectors`: optional list drawn from `email`, `phone`, `credit_card`, `ip_address`

### `invisible_text`

Purpose:
Detect and remove invisible or non-printable Unicode characters from model output.

YAML shape:

```yaml
scanners:
  - type: invisible_text
    action: redact
```

Supported fields:

- no scanner-specific config fields

Action behavior notes:

- with `redact`, invisible characters are removed from the output

### `token_limit`

Purpose:
Limit the token count of model output.

YAML shape:

```yaml
scanners:
  - type: token_limit
    action: redact
    limit: 128
    encoding_name: cl100k_base
```

Supported fields:

- `limit`: optional positive integer, defaults to `4096`
- `encoding_name`: optional non-empty string, defaults to `cl100k_base`
- `model_name`: optional non-empty string

Action behavior notes:

- with `redact`, valid output is returned as-is and over-limit output is handled by the runtime action

## Failure Semantics

### Runtime Behavior

The runtime is currently fail-closed.

Relevant environment variables:

| Env Var | Default | Description |
| --- | --- | --- |
| `OUTPUT_GUARDRAILS_ENABLED` | `false` | Master switch. When `false`, guardrails are bypassed. |
| `OUTPUT_GUARDRAILS_POLICY_PATH` | `""` | Absolute path to the mounted `guardrails.yaml` file. |
| `OUTPUT_GUARDRAILS_HOT_RELOAD_ENABLED` | `true` | Enables background hot reload of the policy file. |

Current behavior:

- If guardrails are disabled, no policy file is loaded.
- If the policy path is empty, the runtime skips policy loading and keeps the in-memory defaults.
- If policy loading fails, the runtime logs the failure and keeps the current guardrails instance.
- If scanner execution raises during response scanning, the runtime raises
  `OutputGuardrailsError` and `/v1/chat/completions` returns HTTP 500.

Sample error response:

```http
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"detail": "Output guardrails failed while scanning the model response."}
```

### Parse-Time vs Runtime Failure Handling

Policy parsing is best-effort, scanner execution is fail-closed.

- parse-time config issues are logged and the invalid scanner is skipped
- runtime scanner failures cause request failure

## Reload Behavior

Hot reload is implemented by `GuardrailsReloader`.

Current semantics:

- the policy is loaded once during process startup
- when hot reload is enabled, the runtime watches the policy file parent directory
  so ConfigMap symlink swaps are detected
- reload events are debounced by 1 second
- a successfully loaded new policy atomically replaces the old one
- if reload fails, the previous policy remains active
- if the newly loaded policy is identical to the current one, the reload is a noop

## Metrics

Guardrails-specific Prometheus metrics currently exposed by the runtime:

| Metric | Labels | Meaning |
| --- | --- | --- |
| `output_guardrails_policy_load_total` | `policy_status` | Policy load attempts by outcome: `success`, `missing`, `invalid`, `load_failed` |
| `output_guardrails_scanner_build_total` | `type`, `status` | Scanner build attempts by scanner type and outcome |
| `output_guardrails_actions_total` | `action` | Applied output actions, currently `redact` or `block` |
| `guardrails_policy_reload_total` | `result` | Reload results: `success`, `failure`, `noop` |
| `guardrails_policy_loaded_timestamp_seconds` | none | Unix timestamp of the most recent active policy update |
| `guardrails_active_policy_info` | `path`, `sha256`, `enabled`, `scanner_count` | Metadata for the active policy |

Request-level metrics such as `rag_chat_requests_total` still capture overall API
success and failure for `/v1/chat/completions`, including failures caused by
guardrails.

## Structured Logs

The runtime emits structured guardrails logs with stable event names and fields.

Common policy-load events:

| Event | Key fields |
| --- | --- |
| `output_guardrails_policy_missing` | `path` |
| `output_guardrails_policy_load_failed` | `path` |
| `output_guardrails_policy_invalid` | `path` |
| `output_guardrails_policy_invalid_scanners` | `path` |
| `output_guardrails_policy_unknown_scanner` | `type` |
| `output_guardrails_policy_invalid_scanner_config` | `type`, `error` |
| `output_guardrails_policy_incompatible_scanner_action` | `type`, `action` |
| `output_guardrails_policy_invalid_action` | `action` |

Runtime and reload events:

| Event | Key fields |
| --- | --- |
| `output_guardrails_triggered` | `action`, `response_id`, `scanners`, `policy_hash` |
| `output_guardrails_failed` | `fail_open`, `response_id` |
| `output_guardrails_policy_scanner_build_failed` | `type`, `policy_hash`, `path` |
| `output_guardrails_hot_reload_disabled` | `reason` |
| `output_guardrails_reloader_terminated` | stack trace in logger exception output |
| `output_guardrails_reload_failed` | `path`, `current_policy_hash`, `fallback_action` |
| `output_guardrails_reload_noop` | `path`, `policy_hash` |
| `output_guardrails_reload_succeeded` | `path`, `old_policy_hash`, `new_policy_hash`, `enabled`, `scanners` |

`output_guardrails_triggered.scanners` is a list of per-scanner summaries that
includes at least:

- `type`
- `action`
- `scores`

## Known Limitations

- Output guardrails only run on assistant messages with string `content`.
- Responses that contain tool calls but no string content are skipped.
- Output guardrails currently run only on the non-streaming `/v1/chat/completions`
  response path.
- Streaming responses are not scanned before tokens are returned to the client.
- There is no per-scanner runtime fail mode; request-time scanner failures are fail-closed.
- There is no scanner-specific CRD surface; detailed policy must be provided in YAML.
- The default policy template enables deterministic baseline leakage protection, not a full content-safety policy.
- There is no audit-event persistence model yet.

## Streaming Status

Streaming support is not implemented yet.

Today, output guardrails are applied after `rag_ops.chat_completion(request)`
returns a full `ChatCompletionResponse`, and before the final non-streaming HTTP
response is returned. There is no corresponding streaming interception point in
the current FastAPI `/v1/chat/completions` handler.

### Runtime Failure Semantics

Output guardrails wrap an external ML pipeline (`llm_guard`) whose scanners may fail at
runtime (e.g. GPU OOM, model download failure, tokenizer errors, library bugs). The
runtime is currently hard-coded to fail closed when this happens.

| Env Var                          | Default | Description                                                                                  |
| -------------------------------- | ------- | -------------------------------------------------------------------------------------------- |
| `OUTPUT_GUARDRAILS_ENABLED`      | `false` | Master switch. When `false`, guardrails are bypassed entirely. |
| `OUTPUT_GUARDRAILS_POLICY_PATH`  | `""`    | Path to the policy ConfigMap YAML. When unset, the runtime falls back to the default ConfigMap shipped with the system. |

Behavior:

- **Fail-closed** (current behavior): If a scanner raises during `guard_response`, the
  runtime raises `OutputGuardrailsError`, which the `/v1/chat/completions` handler maps
  to `HTTP 500` with a fixed detail message
  (`"Output guardrails failed while scanning the model response."`). The original
  exception is preserved via `__cause__` for logs, but is not exposed in the HTTP body.
  Users who encounter a problematic scanner can work around it by disabling that scanner
  in the guardrails ConfigMap.

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
switch or CRD/API control can be added later if we need operator-configurable failure
handling.

## Deferred Scope

This proposal defines the UX shape only. The following items are deferred to follow-up
implementation PRs:

- additional scanners beyond the current baseline set
- audit event model
- streaming scanning behavior
- per-scanner fail modes inside the policy YAML
- configurable failure handling beyond the current fail-closed behavior

## Follow-Up Implementation Plan

This proposal is intended to support the following implementation sequence:

1. Land the initial non-streaming output guardrails hook. (done)
2. Define explicit error-handling semantics. (done)
3. Introduce a runtime YAML policy loader. (done)
4. Add default ConfigMap support. (done)
5. Add hot-reload of the guardrails policy ConfigMap. (done)
6. Refactor scanner construction into a registry/factory structure. (done)
7. Add more scanners in small batches. (partial)
8. Add audit foundations. (not done)
9. Add minimal streaming scanning support. (not done)
10. Polish graceful UX and operational behavior. (partial)

### Hot-reload runtime behavior (implemented)

The runtime watches the guardrails policy file and swaps the active
`OutputGuardrails` instance when the policy changes. For ConfigMap-mounted files,
it watches the parent directory so Kubernetes symlink updates are detected.

Reload semantics:

- If a new policy fails to load, the previous policy stays active.
- Reloads use a 1-second debounce window.
- Hot reload can be disabled with `OUTPUT_GUARDRAILS_HOT_RELOAD_ENABLED=false`,
  in which case the policy is loaded once at startup.

Observability:

- `guardrails_policy_reload_total{result="success|failure|noop"}`
- `guardrails_policy_loaded_timestamp_seconds`