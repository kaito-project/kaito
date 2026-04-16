# RAGEngine Output Guardrails Notes

This note captures the current output-guardrails PoC in RAGEngine and explains why `request_id` and `trace_id` are included in guardrail audit reports.

## Current Scope

- Output guardrails only
- RAGEngine only
- `/v1/chat/completions` path, including streaming output scanning
- Structured audit report with pluggable sinks
- Staged streaming audit events for stream start, policy decision, fail-open, and completion

The current insertion point is the final response path in `main.py`, after the chat completion result is produced and before the HTTP response is returned.

For streaming responses, the audit model now emits both staged `GuardrailAuditEvent` records during the lifetime of the stream and one final `GuardrailAuditReport` after the stream completes. This keeps long-running streams observable before the final report is available.

Each streaming audit event now carries a stable `stream_id` plus a monotonically increasing `sequence` value. Remote audit systems should treat `stream_id` as the stream-level correlation key and `sequence` as the in-stream ordering key.

The terminal `stream_completed` staged event now also carries `sequence_total`, `checksum_algorithm`, and `end_of_stream_checksum`. The final report carries the same integrity metadata plus `last_sequence`. This lets downstream systems verify both that they received the full ordered event stream and that the reconstructed final output matches the server-side end-of-stream checksum.

For streaming deltas, the handling strategy is intentionally split by payload type:

- text `content` deltas are scanned by output guardrails before they are emitted downstream
- `tool_calls` and `function_call` deltas are treated as structured protocol payloads, not free-form assistant text, so they are passed through unchanged and produce explicit staged audit events instead of text redaction/blocking

For remote delivery, staged events and final reports can now be sent to separate endpoints. This avoids mixing high-volume in-stream events with end-of-stream summary reports when integrating with external audit pipelines.

These two remote flows can also use separate authentication settings. That allows operators to issue distinct credentials for high-volume event ingestion and lower-volume final-report ingestion instead of forcing both streams to share one token.

Because remote audit delivery now runs in background tasks, RAGEngine drains pending deliveries during FastAPI shutdown before process exit. The wait is bounded by `OUTPUT_GUARDRAILS_AUDIT_SHUTDOWN_DRAIN_TIMEOUT_SECONDS`, which defaults to five seconds.

To avoid unbounded buildup while the service is still running, background audit delivery is also bounded by `OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS`. When the limit is reached, `OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY=drop` discards the new delivery and records backpressure metrics, while `block` awaits the delivery inline instead of scheduling another background task.

The current default remains `drop`. That is an intentional serving-first choice: once the system is already overloaded on audit delivery, preserving the main chat response path is usually safer than allowing audit sink slowness to amplify user-facing latency. Teams that prioritize audit completeness over latency can switch to `block`, but they should expect request-path slowdown when the audit sink is saturated.

The current implementation also exports six Prometheus metrics for this path: `rag_output_guardrails_audit_shutdown_drain_total{status="success|timeout"}`, `rag_output_guardrails_audit_shutdown_drain_latency_seconds{status="success|timeout"}`, `rag_output_guardrails_audit_cancelled_deliveries_total`, `rag_output_guardrails_audit_in_flight_background_tasks`, `rag_output_guardrails_audit_backpressure_total{policy="drop|block",delivery_type="event|report"}`, and `rag_output_guardrails_audit_dropped_deliveries_total{delivery_type="event|report"}`.

Basic monitoring examples for these signals live under `examples/RAG/monitoring/`, including a `PrometheusRule` for timeout, backpressure, and dropped-delivery alerts plus a Grafana dashboard source file for shutdown, backlog, and delivery-loss views.

## Checksum Revalidation Rule For Remote Audit Consumers

Remote audit consumers should treat `end_of_stream_checksum` as the server-side integrity summary for the completed stream. The checksum is currently calculated with `sha256` over a canonical JSON object using UTF-8 encoding and sorted keys.

The canonical payload is:

```json
{
	"finish_reason": "<final finish reason>",
	"final_output": "<final emitted assistant text>",
	"sequence_total": <final staged event count>,
	"stream_id": "<stream id>"
}
```

Revalidation procedure:

1. Collect all staged events for one `stream_id`.
2. Sort them by `sequence` and verify the sequence is contiguous from `1` through `sequence_total`.
3. Read the terminal `stream_completed` event and capture its `sequence_total`, `checksum_algorithm`, and `end_of_stream_checksum`.
4. Reconstruct `final_output` from the serving result, not from intermediate staged events:
	 for normal allow/redact flows, use the final emitted assistant text;
	 for `block`, use the configured block message that was emitted to the client;
	 for `fail_open`, use the original raw output that was passed through.
5. Build the canonical JSON object with exactly these four keys: `finish_reason`, `final_output`, `sequence_total`, and `stream_id`.
6. Serialize it with sorted keys and UTF-8 encoding.
7. Hash the serialized bytes with the declared `checksum_algorithm` and compare the hex digest with `end_of_stream_checksum`.

In Python, the equivalent checksum logic is:

```python
import hashlib
import json

payload = {
		"finish_reason": finish_reason,
		"final_output": final_output,
		"sequence_total": sequence_total,
		"stream_id": stream_id,
}
checksum = hashlib.sha256(
		json.dumps(payload, sort_keys=True).encode("utf-8")
).hexdigest()
```

If the recomputed checksum differs from the reported value, the consumer should treat the stream as non-verifiable. Typical causes are missing staged events, wrong event ordering, or reconstructing `final_output` from the wrong text representation.

## Why `request_id` And `trace_id` Are In The Audit Report

The audit report is not only meant to describe what scanner fired or whether a response was redacted or blocked. It also needs to identify which HTTP request produced that audit event and how that request maps to the broader serving path.

`request_id` is used to correlate one audit report with one concrete request received by RAGEngine. This is the most practical identifier for operator workflows such as support investigation, log lookup, and user-facing incident handling. RAGEngine also returns this value in the `X-Request-Id` response header so the same identifier can be handed back by clients.

`trace_id` is used to correlate the same request across multiple services when distributed tracing is available. In the longer-term architecture, a single user request may traverse ingress, RAGEngine, inference backends, and external audit systems. `trace_id` allows the guardrail decision to be tied to that end-to-end request path instead of being isolated to one process.

Together, these fields make audit data usable for:

- correlating a blocked or redacted response with the exact API request that produced it
- joining guardrail audit data with application logs, ingress logs, and external observability systems
- investigating false positives and operational incidents without relying on approximate time-based matching
- preserving request lineage when audit reports are exported to file, remote sinks, or SIEM systems

Without `request_id` and `trace_id`, the audit report still describes the guardrail outcome, but it is much weaker as an operational artifact because it cannot be reliably connected back to the request lifecycle.

## Current Request Association Model

The current implementation associates audit reports with FastAPI requests in three steps:

1. Middleware extracts or generates `request_id` from `X-Request-Id` or `X-MS-Client-Request-Id`.
2. Middleware extracts `trace_id` from `traceparent` or `X-Trace-Id` when present.
3. The chat completions handler passes those values into the output-guardrails layer, which writes them into both `GuardrailAuditReport` and `GuardrailAuditEvent`.

This keeps the correlation logic at the HTTP boundary and avoids making the guardrails layer responsible for inferring request context indirectly.

## Design Intent

The design goal is to treat guardrail auditing as a first-class operational signal rather than a best-effort log line.

That means the audit record should answer four questions directly:

- what response was evaluated
- what guardrail action was taken
- which request produced that response
- where that request sits in the broader distributed call chain

`response_id` answers the first question. `request_id` and `trace_id` answer the last two. Together they make the audit record suitable for formal auditing, debugging, and downstream export.

## Current Limitation

Streaming output is now scanned with a small holdback buffer before chunks are emitted. This is sufficient for the current PoC, but it is still a best-effort design rather than a fully optimized streaming inspection pipeline. The next hardening step would be per-chunk metrics and more explicit staged audit events for long-running streams.

Another current limitation is configuration ownership. Today, the controller-owned deployment template only knows about CRD fields already modeled in `RAGEngine.spec`, while output-guardrail and remote-audit options are applied later as ad hoc runtime env overrides. That means a future reconcile can legitimately rewrite the deployment template without understanding those manual overrides.

## Recommended Controller And CRD Direction

The recommended direction is to move these runtime env knobs into declarative `RAGEngine.spec` fields rather than keep relying on post-create `kubectl set env` changes.

Why this is the right direction:

- reconciliation becomes deterministic because the controller owns the full desired deployment env
- operators stop depending on an imperative post-deploy step that is easy to forget
- rollout drift becomes visible in the CR instead of being hidden in a patched deployment
- validation can move into the CRD layer, for example allowed actions, retry values, and mutually exclusive auth settings

A pragmatic shape would be a dedicated spec subtree, for example:

```yaml
spec:
	guardrails:
		output:
			enabled: true
			failOpen: true
			actionOnHit: redact
			regexPatterns:
				- https?://\S+
			blockedMessage: The model output was blocked by output guardrails.
			streamHoldbackChars: 32
			audit:
				sinks:
					- log
					- remote
				filePath: /var/log/ragengine/guardrails.jsonl
				remote:
					eventURL: https://audit.example.com/events
					reportURL: https://audit.example.com/reports
					timeoutSeconds: 3
					maxRetries: 2
					retryBackoffSeconds: 0.2
					auth:
						shared:
							header: Authorization
							tokenSecretRef:
								name: ragengine-audit-shared
								key: token
							tokenPrefix: Bearer 
						event:
							header: X-Event-Auth
							tokenSecretRef:
								name: ragengine-audit-event
								key: token
							tokenPrefix: Token 
						report:
							header: X-Report-Auth
							tokenSecretRef:
								name: ragengine-audit-report
								key: token
							tokenPrefix: Token 
```

Recommended rollout plan:

1. Add the new spec fields and controller support while preserving the current env-based behavior as fallback.
2. Make the controller render the env vars from `RAGEngine.spec.guardrails.output` when present.
3. Treat manual env overrides as legacy compatibility behavior rather than the preferred path.
4. After consumers migrate, deprecate direct runtime env patching in docs.

If this is pursued, credentials should be modeled with `Secret` references in the CRD rather than raw token strings so the controller can keep sensitive values out of manifests and logs.