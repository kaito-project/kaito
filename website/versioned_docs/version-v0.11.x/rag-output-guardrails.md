---
title: RAG Output Guardrails
---

This page describes the current user-facing behavior of RAGEngine output
guardrails.

RAGEngine output guardrails can scan assistant responses before they are
returned from the non-streaming `/v1/chat/completions` endpoint.

The CRD entry point is intentionally small:

```yaml
spec:
	guardrails:
		enabled: true
```

Detailed scanner behavior is configured through a mounted `guardrails.yaml`
policy file rather than scanner-specific CRD fields.

## Current Support

- assistant text returned by `/v1/chat/completions`
- non-streaming responses
- ConfigMap-backed YAML policy configuration
- policy hot reload
- Prometheus metrics and structured logs
- these scanner types:
	- `regex`
	- `ban_substrings`
	- `json`
	- `reading_time`
	- `secrets`
	- `sensitive`
	- `invisible_text`
	- `token_limit`

## Current Non-Support

- streaming response scanning
- scanner-specific CRD fields
- per-scanner fail-open or fail-closed controls
- audit event storage

## Configuration

### Minimal CRD Entry Point

The user-facing switch remains `spec.guardrails.enabled`:

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

Detailed policy is supplied in a ConfigMap-mounted YAML file:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
	name: ragengine-guardrails-policy
data:
	guardrails.yaml: |
		blockMessage: The model output was blocked by output guardrails.
		scanners:
			- type: regex
				action: redact
				patterns:
					- 'https?://\\S+'
			- type: ban_substrings
				action: block
				substrings:
					- For Internal Use Only
					- Do Not Distribute
```

Policy parsing is permissive by design:

- unknown scanner types are skipped
- invalid scanner configs are skipped
- invalid actions fall back to the default action
- one invalid scanner does not prevent other valid scanners from running

This example uses explicit scanner-level `action` values, so it does not need a
top-level `action`. A top-level `action` is optional and serves only as the
default for scanner entries that omit their own `action`; if both are present,
the scanner-level `action` wins.

### Default ConfigMap Behavior

If `spec.guardrails.enabled` is `true` and `configMapRef` is not set, the
controller copies the default guardrails policy ConfigMap
(`ragengine-guardrails-policy-template`) into the RAGEngine namespace and mounts
it into the Pod.

- auto-copied ConfigMaps are namespace-scoped shared resources and do not carry
	an `OwnerReference` to any individual RAGEngine
- user-provided ConfigMaps are not modified or owned by the controller

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

## Scanner Types

For clarity, the single-scanner examples below show only scanner-level
`action` values. Multi-scanner policies can also set an optional top-level
default `action` for scanner entries that omit one.

Common action behavior:

- `redact`: replace matching content with sanitized output
- `block`: replace the assistant message with a fixed block message

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
Match output content against exact substrings using substring matching.

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
Detect likely secrets in output using the deterministic secrets scanner.

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

## Runtime Behavior

The runtime is currently fail-closed.

Relevant environment variables:

| Env Var | Default | Description |
| --- | --- | --- |
| `OUTPUT_GUARDRAILS_ENABLED` | `false` | Master switch. When `false`, guardrails are bypassed. |
| `OUTPUT_GUARDRAILS_POLICY_PATH` | `""` | Absolute path to the mounted `guardrails.yaml` file. |
| `OUTPUT_GUARDRAILS_HOT_RELOAD_ENABLED` | `true` | Enables background hot reload of the policy file. |

Current behavior:

- if guardrails are disabled, no policy file is loaded
- if the policy path is empty, the runtime skips policy loading and keeps the in-memory defaults
- if policy loading fails, the runtime logs the failure and keeps the current guardrails instance
- if scanner execution raises during response scanning, the runtime raises `OutputGuardrailsError` and `/v1/chat/completions` returns HTTP 500

Sample error response:

```http
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"detail": "Output guardrails failed while scanning the model response."}
```

Policy parsing is best-effort, while scanner execution is fail-closed.

## Reload, Metrics, and Logs

Hot reload is implemented by `GuardrailsReloader`.

Current semantics:

- the policy is loaded once during process startup
- when hot reload is enabled, the runtime watches the policy file parent directory so ConfigMap symlink swaps are detected
- reload events are debounced by 1 second
- a successfully loaded new policy atomically replaces the old one
- if reload fails, the previous policy remains active
- if the newly loaded policy is identical to the current one, the reload is a noop

Guardrails-specific Prometheus metrics currently exposed by the runtime:

| Metric | Labels | Meaning |
| --- | --- | --- |
| `output_guardrails_policy_load_total` | `policy_status` | Policy load attempts by outcome: `success`, `missing`, `invalid`, `load_failed` |
| `output_guardrails_scanner_build_total` | `type`, `status` | Scanner build attempts by scanner type and outcome |
| `output_guardrails_actions_total` | `action` | Applied output actions, currently `redact` or `block` |
| `guardrails_policy_reload_total` | `result` | Reload results: `success`, `failure`, `noop` |
| `guardrails_policy_loaded_timestamp_seconds` | none | Unix timestamp of the most recent active policy update |
| `guardrails_active_policy_info` | `path`, `sha256`, `enabled`, `scanner_count` | Metadata for the active policy |

Request-level metrics such as `rag_chat_requests_total` still capture overall
API success and failure for `/v1/chat/completions`, including failures caused
by guardrails.

The runtime emits structured guardrails logs with stable event names and
fields.

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

- output guardrails only run on assistant messages with string `content`
- responses that contain tool calls but no string content are skipped
- output guardrails currently run only on the non-streaming `/v1/chat/completions` response path
- streaming responses are not scanned before tokens are returned to the client
- there is no per-scanner runtime fail mode; request-time scanner failures are fail-closed
- there is no scanner-specific CRD surface; detailed policy must be provided in YAML
- there is no audit-event persistence model yet

## Related Docs

- See [RAG API](./rag-api.md) for the chat completions endpoint.
- See [Retrieval-Augmented Generation (RAG)](./rag.md) for the broader RAGEngine overview.