---
title: RAGEngine Streaming Guardrails
authors:
  - "@xiaoqi-7"
reviewers:
creation-date: 2026-06-12
last-updated: 2026-06-12
status: provisional
see-also:
  - "/docs/proposals/20260416-ragengine-guardrails-ux-api.md"
---

# RAGEngine Streaming Guardrails

## Summary

This proposal defines how streaming output guardrails should work in RAGEngine.

The current codebase supports non-streaming guardrails. The next step is to make streaming guardrails a well-defined runtime
feature with clear scanner contracts, explicit user-visible behavior, and phased support
for passthrough and RAG streaming.

The guiding principles are:

- start with deterministic scanners whose streaming behavior is predictable
- keep the first implementation conservative and buffering-first
- treat passthrough streaming and RAG streaming as separate workstreams
- define explicit runtime contracts rather than relying on implicit behavior

## Motivation

RAGEngine currently applies output guardrails to complete responses after generation.
This works for non-streaming responses because scanners can evaluate a full answer
after generation has completed. It does not yet define how scanners should behave
when the model returns output incrementally in chunks.

Once streaming is introduced, the runtime must decide:

- when content can be emitted safely
- when content must remain buffered
- what users see for redact and block
- how scanner capabilities affect overall stream behavior

These questions do not exist in the same form for full-response scanning. They are
specific to streaming, where content may already be on its way to the client before
the full response is available for inspection.

Because these choices affect correctness, user-visible behavior, and API
compatibility, they should be defined explicitly before streaming support expands
further.

### Goals

- Define the runtime contract for streaming scanners.
- Support a conservative initial implementation for deterministic scanners.
- Define a path for existing output scanners to participate safely in streaming over time, starting with deterministic scanners in the first phase.
- Clearly separate request-level streaming behavior from service-level guardrails config.
- Establish explicit UX rules for redact, block, and late violations.
- Define a staged implementation plan for passthrough streaming first, then RAG streaming.

### Non-Goals

- Use ConfigMap-backed runtime policy to replace request-level `stream` selection for individual calls.
- Deliver true token-by-token RAG streaming in the first phase.
- Guarantee that all regex patterns behave identically in streaming and full-response modes.

## Current State

The current codebase contains the following pieces:

- a non-streaming output guardrails runtime that scans full `ChatCompletionResponse` objects
- a policy/config pipeline driven by environment variables and ConfigMap-backed YAML

## Proposed Design

### Request-Level Control

Streaming is request-driven.

- request-level configuration decides whether a specific call uses streaming
- scanner-level configuration only affects how scanners participate after a call has entered the streaming path

In the current RAGEngine UX model, `spec.guardrails.enabled` should remain a minimal
capability switch. Detailed streaming participation policy should live with other
runtime policy in ConfigMap-backed configuration rather than being folded into
`guardrails.enabled` itself.

That yields the following control model:

1. request-level `stream` determines whether a call enters the streaming path, and scanner-level policy applies only after that point

### Control Model

Request-level streaming does not require the originating actor to set `stream`
manually on every call. In many deployments, the direct caller of
`/v1/chat/completions` is an SDK, workflow engine, agent orchestrator, or UI
backend rather than a human-operated API client.

The effective `stream` value for a call should be resolved using the following
logic:

```python
if request explicitly provides stream:
  use request.stream
else:
  use false
```

For the first implementation, omission of `stream` should remain conservative and
default to non-streaming.

Representative request shapes are:

Direct API caller:

```json
{
  "model": "example-model",
  "messages": [{"role": "user", "content": "hello"}],
  "stream": true
}
```

Explicit non-streaming caller:

```json
{
  "model": "example-model",
  "messages": [{"role": "user", "content": "hello"}],
  "stream": false
}
```

In the second case, the caller explicitly keeps the request on the non-streaming
path. The runtime does not infer streaming intent from scanner policy.

Recommended placement is:

- request materialization happens in the client, SDK, or orchestrator layer
- scanner streaming policy lives in ConfigMap-backed runtime configuration
- final `stream` resolution happens at the `/v1/chat/completions` entrypoint before transport selection
- scanner-level streaming policy does not determine whether a request is streamed; it only determines how a scanner participates after a request has entered the streaming path
- streaming guardrails logic runs only after the request has already been classified as streaming

In the current RAGEngine structure, that means:

- `guardrails.enabled` remains the minimal CRD switch for enabling guardrails capability
- scanner-level streaming participation policy should live in ConfigMap-backed runtime policy rather than in `guardrails.enabled`
- the API entrypoint chooses between streaming and non-streaming paths
- the streaming processor and scanners operate only after that path has been selected

### Endpoint Branching

At the `/v1/chat/completions` entrypoint, the first phase should branch directly on
the effective request-level `stream` value.

```python
if request.get("stream") is True:
  return StreamingResponse(
    guarded_stream_generator,
    media_type="text/event-stream",
    headers={
      "Cache-Control": "no-cache",
      "X-Accel-Buffering": "no",
    },
  )

response = await rag_ops.chat_completion(request)
response = guardrails.guard_response(response, request)
return response
```

This keeps the existing non-streaming guardrails path unchanged while making the
streaming path explicit at the transport boundary.

### Streaming Runtime Model

The streaming lifecycle is conceptually:

1. receive upstream SSE (Server-Sent Events) chunks
2. accumulate assistant text
3. invoke scanner `on_chunk(...)`
4. invoke scanner `finalize(...)`
5. emit a downstream SSE response

This proposal keeps that lifecycle, but makes the contract explicit:

- `on_chunk(...)` is the incremental observation point
- `finalize(...)` is the end-of-stream decision point
- the processor is responsible for SSE parsing and re-emission
- scanner results must be conservative by default

The first implementation should explicitly forward `stream: true` to the upstream
LLM backend and consume OpenAI-compatible SSE events such as:

```text
data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]
```

The downstream stream should also remain OpenAI-compatible SSE so existing SDKs
and clients continue to work without transport-specific adaptation.

### Buffering and Holdback

The first phase should use a buffering-first strategy rather than forwarding every
upstream chunk immediately.

Conceptually:

```text
pending_buffer += new_delta
scan bounded recent window
emit only known-safe prefix
retain holdback suffix
```

This allows deterministic scanners to detect patterns that span chunk boundaries.
For example, if one chunk ends with `u` and the next chunk begins with `rl`, a
`ban_substrings` scanner must be able to see `url` before any unsafe suffix is
emitted.

Recommended initial defaults:

```text
min_scan_chars   = 128
scan_interval_ms = 50
holdback_chars   = 256
max_window_chars = 2048
max_emit_chars   = 512
```

These values are not API surface. They are runtime defaults intended to keep
latency bounded while preserving a conservative safe window for early-block
behavior.

### Scanner Contract

The initial scanner contract is intentionally small:

```python
class StreamingScanner:
    def on_chunk(self, text: str) -> StreamingDecision:
        ...

    def finalize(self, prompt: str, content: str) -> StreamingDecision:
        ...
```

This is sufficient for the current buffer/finalize implementation, but follow-up work
will likely extend it with richer decision payloads and scanner capability metadata.

Likely expansion areas include:

- richer `StreamingDecision` fields for partial emission and terminal control
- scanner capability metadata such as early-block support and safe-window requirements
- richer chunk/finalize context instead of plain text-only inputs

### Scanner Support Strategy

The initial streaming feature should focus on deterministic scanners only.

Recommended first scanners:

- `ban_substrings`
- `regex`

Deferred scanners:

- `secrets`
- `sensitive`
- `json`
- `reading_time`
- `token_limit`
- `invisible_text`

These scanners differ in whether they can support early decisions, whether they require
full-response context, and how safe their streaming behavior would be. Streaming
capability should therefore be modeled per scanner type, while final stream behavior
should be resolved using the most conservative aggregate policy.

### Strict Fallback Policy

The default product behavior should be strict.

That means:

```text
if all configured scanners are streaming-compatible:
  use true streaming guardrails

if any configured scanner requires full-output blocking or redact semantics:
  fall back to non-streaming guarded execution
```

This preserves safety semantics. Once content has been emitted to the client, it
cannot be recalled, so finalize-only blocking scanners cannot provide equivalent
guarantees in true streaming mode.

### User Experience and Output Contract

The first streaming UX contract should be conservative.

#### Redact

- redact remains finalize-time only
- the user sees sanitized output after finalize
- the system does not attempt incremental redaction in the first phase

#### Block

- block returns a safe replacement message
- the stream ends with `content_filter` semantics rather than silently disappearing

#### Partial Emission

- partial emission should not be the default
- it is only allowed when all participating scanners support safe early block behavior
- safe-window buffering should be used when partial emission is enabled

If content has already been partially emitted and a later chunk triggers block, the first
phase should use a conservative safe-window strategy: emit only a known-safe prefix,
stop normal output on block, and return the block message.

Representative block output should remain OpenAI-compatible SSE and end with a
terminal marker:

```text
data: {"choices":[{"delta":{"content":"Response blocked by output guardrails."},"finish_reason":"content_filter"}]}

data: [DONE]
```

### Passthrough Streaming vs RAG Streaming

These should be developed separately.

#### Passthrough Streaming

This is the correct first target because the upstream model already emits SSE and the
runtime only needs to parse, buffer, decide, and re-emit.

#### RAG Streaming

This is significantly harder because the current RAG path is full-response based.
The recommended progression is:

1. pseudo-streaming after full generation
2. true streaming using the underlying runtime's streaming chat API

These should remain separate milestones.

## Phased Implementation Plan

Recommended order for follow-up work:

1. Harden the streaming runtime contract.
2. Add `ban_substrings` streaming support.
3. Add bounded `regex` streaming support.
4. Wire scanner construction into the output guardrails runtime.
5. Stabilize passthrough SSE handling under guardrails.
6. Expand streaming metrics and structured logs.
7. Formalize UX / output contract behavior.
8. Add RAG pseudo-streaming.
9. Add true RAG streaming.
10. Document supported behavior and limitations.

### PR Slicing

To keep implementation reviewable, the first phase should be split into small,
behavior-scoped PRs.

1. Streaming passthrough: add `stream=true` endpoint branching, forward `stream=true` upstream, consume SSE, and return `StreamingResponse` without guardrail mutation.
2. SSE utilities: add helpers for parsing `data:` frames, detecting `[DONE]`, extracting `delta.content`, and emitting downstream OpenAI-compatible events.
3. Buffer and holdback: add `pending_buffer`, bounded scan windows, holdback retention, and finalize flush behavior with a no-op scanner.
4. Fast block path: add deterministic scanner execution for `ban_substrings` and bounded `regex`, including cross-chunk block tests.
5. Redact, fallback, and observability: add strict fallback policy, redact behavior, metrics, and mixed-policy coverage.

## Metrics and Observability

The streaming path should introduce dedicated metrics rather than overloading the
existing non-streaming counters.

Recommended metrics:

- `stream_guardrails_requests_total`
- `stream_guardrails_fallback_total{reason}`
- `stream_guardrails_scans_total`
- `stream_guardrails_scan_latency_seconds`
- `stream_guardrails_block_total`
- `stream_guardrails_redact_total`
- `stream_guardrails_pending_buffer_chars`
- `stream_guardrails_time_to_first_safe_chunk_seconds`
- `stream_guardrails_finalize_latency_seconds`

Recommended fallback reasons:

- `finalize_only_scanner`
- `scanner_error`
- `buffer_overflow`
- `unsupported_policy`

## Risks and Mitigations

### Risk: Content Leaks Before Scanners Finish

Streaming can send content to the client before scanners have enough context to make a
safe decision.

Mitigation:

- default to buffering
- only allow partial emission when every participating scanner explicitly supports it
- use safe-window buffering for early-block behavior

### Risk: Different Scanners Need Different Rules

Not every scanner can use the same streaming strategy.

Mitigation:

- treat scanner capability as explicit runtime metadata
- do not assume one streaming policy fits every scanner

### Risk: Transport Logic and UX Get Mixed Together

It is easy to mix up SSE transport handling with scanner behavior and user-visible
output rules.

Mitigation:

- keep SSE handling in dedicated runtime code
- keep scanner contracts explicit
- define UX behavior separately from transport mechanics

## Alternatives and Validation

Rejected alternatives for the first phase:

- make all streaming guardrails finalize-only
- make streaming a service-level default
- require every scanner to implement the same streaming strategy

Validation should cover:

- chunk accumulation correctness
- cross-chunk deterministic scanner hits
- early block vs finalize-only behavior
- fail-open vs fail-closed behavior
- safe-window buffering rules
- SSE passthrough parsing and re-emission
- structured observability outputs

## Implementation History

- [ ] 06/12/2026: Open proposal PR