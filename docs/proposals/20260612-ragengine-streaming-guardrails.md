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

- start with deterministic scanners only
- keep the first implementation conservative
- separate passthrough streaming from RAG streaming
- prefer explicit runtime contracts over implicit behavior

## Motivation

RAGEngine currently applies output guardrails to complete responses after generation.
This works for non-streaming responses, but it does not yet define how scanners should
participate in a chunked streaming lifecycle.

Streaming needs explicit decisions for:

- when content can be emitted safely
- when content must remain buffered
- what users see for redact and block
- how scanner capabilities affect overall stream behavior

These choices affect correctness, UX, and API compatibility, so they should be agreed on
before streaming support expands further.

### Goals

- Define the runtime contract for streaming scanners.
- Support a conservative initial implementation for deterministic scanners.
- Clearly separate request-level streaming behavior from service-level guardrails config.
- Establish explicit UX rules for redact, block, and late violations.
- Define a staged implementation plan for passthrough streaming first, then RAG streaming.

### Non-Goals

- Make every existing output scanner streaming-safe in the first phase.
- Treat service-level config as a replacement for request-level `stream` behavior.
- Deliver true token-by-token RAG streaming in the first phase.
- Guarantee fully general regex streaming semantics for arbitrarily complex patterns.

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

1. explicit request value determines whether the call is streaming
2. scanner-level streaming policy applies only after the call has entered the streaming path

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

## Risks and Mitigations

### Risk: Unsafe Partial Emission

If content is emitted too early, later scanners may detect a violation after unsafe
content has already reached the client.

Mitigation:

- default to buffering
- allow partial emission only when every participating scanner explicitly supports it
- use safe-window buffering for early block capable scanners

### Risk: Scanner Semantics Drift

Different scanner types may require different streaming guarantees.

Mitigation:

- treat scanner capability as first-class runtime metadata
- avoid assuming a single policy works for every scanner

### Risk: Conflating Product UX with Transport Mechanics

Streaming transport, scanner lifecycle, and user-visible output can easily become mixed
together in implementation.

Mitigation:

- keep SSE transport concerns in dedicated runtime code
- keep scanner contract decisions explicit
- document UX behavior separately from internal transport details

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