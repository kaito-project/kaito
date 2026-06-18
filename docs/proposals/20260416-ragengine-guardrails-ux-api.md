---
title: RAGEngine Streaming Output Guardrails Design
authors:
  - "@xiaoqi-7"
reviewers:
  - "@Fei-Guo"
creation-date: 2026-04-16
last-updated: 2026-06-18
status: provisional
see-also:
  - "/docs/proposals/20260612-ragengine-streaming-guardrails.md"
  - "/docs/proposals/20250715-inference-aware-routing-layer.md"
---

## Summary

This design adds streaming support for RAGEngine chat completions and introduces a
streaming output guardrail path.

When a client sends `stream: true`, RAGEngine will request the upstream LLM backend
with `stream: true`, consume upstream OpenAI-compatible SSE chunks, apply
streaming-compatible output guardrails, and emit safe SSE chunks to the client.

The existing non-streaming guardrail path remains unchanged.

## Goals

- Support OpenAI-compatible streaming for `/v1/chat/completions`.
- Forward `stream: true` from RAGEngine to the upstream LLM backend.
- Add a server-side streaming guardrail processor between upstream LLM chunks and
  downstream client chunks.
- Support low-latency streaming for scanners that can operate on partial output.
- Avoid scanning the full accumulated response on every chunk.
- Keep each implementation PR small, around 200 core lines where possible.

## Non-Goals

- Do not change upstream LLM token generation behavior.
- Do not guarantee full-response-equivalent safety for all scanner types in true
  streaming mode.
- Do not run expensive or full-output scanners on every streaming chunk.
- Do not introduce full semantic or LLM-based streaming scanners in the first
  version.
- Do not refactor the entire RAG pipeline in the first PR.

## Current Behavior

Today, RAGEngine handles `/v1/chat/completions` as a non-streaming request.

The high-level flow is:

```text
client request
  ↓
RAGEngine /v1/chat/completions
  ↓
rag_ops.chat_completion(request)
  ↓
wait for full upstream response
  ↓
guardrails.guard_response(response, request)
  ↓
return full JSON response
```

This works for non-streaming guardrails, but it cannot provide token-level
streaming because RAGEngine currently waits for the complete response before
applying guardrails.

## Proposed Behavior

When the request contains:

```json
{
  "stream": true
}
```

RAGEngine enters the streaming path:

```text
Client
  ↓ POST /v1/chat/completions stream=true
RAGEngine
  ↓ call upstream LLM with stream=true
Upstream LLM
  ↓ returns OpenAI-compatible SSE chunks
RAGEngine streaming guardrail processor
  ↓ buffer / scan / redact / block
RAGEngine
  ↓ emits safe SSE chunks
Client
```

When `stream` is absent or false, RAGEngine continues using the existing
non-streaming path.

## Architecture

### 1. Endpoint Layer

The `/v1/chat/completions` endpoint should branch on `stream`.

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
guardrails = guardrails_reloader.get_current()
response = guardrails.guard_response(response, request)
return response
```

### 2. Upstream Streaming Client

RAGEngine should call the upstream LLM backend with:

```json
{
  "stream": true
}
```

The upstream response is expected to be OpenAI-compatible SSE:

```text
data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]
```

RAGEngine consumes these upstream SSE events asynchronously.

### 3. Streaming Guardrail Processor

The guardrail processor sits between the upstream LLM stream and the downstream
client stream.

```text
upstream SSE event
  ↓
parse event
  ↓
extract choices[].delta.content
  ↓
append to pending buffer
  ↓
scan recent window
  ↓
emit safe prefix
  ↓
keep holdback suffix
```

RAGEngine should not directly forward every upstream chunk. It should only emit
content after it has been checked by streaming-compatible scanners.

## Buffering Strategy

Use an un-emitted `pending_buffer`.

```text
pending_buffer += new_delta
```

The processor should keep a holdback suffix so that patterns split across chunks
can still be detected.

Example:

```text
chunk 1: "u"
chunk 2: "rl"
```

If the policy blocks `"url"`, RAGEngine should not emit `"u"` immediately. It
keeps `"u"` in the holdback buffer. When `"rl"` arrives, the scanner sees
`"url"` before unsafe content is sent to the client.

Recommended defaults:

```text
min_scan_chars   = 128
scan_interval_ms = 50
holdback_chars   = 256
max_window_chars = 2048
max_emit_chars   = 512
```

Parameter meanings:

```text
min_scan_chars:
  Coalesce small chunks before scanning.

scan_interval_ms:
  Avoid waiting too long when model output is slow.

holdback_chars:
  Keep the last N characters un-emitted to detect patterns split across chunks.

max_window_chars:
  Scan only a bounded recent window instead of the full accumulated response.

max_emit_chars:
  Control downstream SSE chunk size.
```

At stream end, the holdback buffer must not be dropped. RAGEngine performs a
final scan and then flushes, redacts, or blocks the remaining content.

## Scanner Capability Model

Streaming guardrails should classify scanners into two groups.

### Streaming-Compatible Scanners

These scanners can make local decisions over partial text and are suitable for
the streaming path:

```text
ban_substrings
regex
secrets
sensitive
invisible_text
token_limit
```

### Finalize-Only Scanners

These scanners require complete output and should not run on every streaming
chunk:

```text
json
reading_time
full-structure checks
semantic scanners
LLM-based scanners
```

## Mixed Scanner Policy

If a policy contains both streaming-compatible scanners and finalize-only
scanners, RAGEngine must use clear product semantics.

Recommended default: **strict mode**.

```text
If all scanners are streaming-compatible:
  use true streaming guardrails

If policy contains finalize-only scanners with block/redact action:
  fall back to non-streaming guarded execution

If future policy supports audit/log-only finalize scanners:
  allow best-effort streaming and run finalize scanners at the end
```

Reason:

```text
Once content is emitted to the client, it cannot be recalled.
Therefore, finalize-only blocking scanners cannot provide full blocking guarantees in true streaming mode.
```

This means a mixed policy with blocking JSON validation will not get true
streaming in strict mode. This is intentional to preserve safety semantics.

## Guardrail Actions

### Pass

No scanner hit. RAGEngine emits safe content as OpenAI-compatible SSE chunks.

```text
data: {"choices":[{"delta":{"content":"safe text"}}]}
```

### Redact

Sensitive spans are replaced before content is emitted.

Example:

```text
original: my email is user@example.com
redacted: my email is [REDACTED]
```

### Block

RAGEngine stops forwarding upstream content and emits a guarded block chunk,
followed by `[DONE]`.

```text
data: {"choices":[{"delta":{"content":"Response blocked by output guardrails."},"finish_reason":"content_filter"}]}

data: [DONE]
```

## Performance Strategy

Do not scan every upstream chunk with every scanner.

Instead:

```text
coalesce small chunks
  ↓
run only streaming-compatible fast scanners
  ↓
scan only a bounded recent window
  ↓
emit safe prefixes continuously
  ↓
flush only the holdback buffer at the end
```

This keeps scanning pipelined with LLM generation.

The goal is not necessarily to reduce total CPU compared with non-streaming
scanning. The goal is to reduce time-to-first-safe-output while keeping total
overhead bounded.

## Safety Limitations

Streaming guardrails are not equivalent to full-response non-streaming
guardrails for all scanner types.

Limitations:

```text
Already-emitted content cannot be recalled.
Holdback only protects bounded local patterns.
Global scanners still require full output.
Complex semantic scanners are not suitable for per-chunk execution.
```

Therefore:

```text
Fast local scanners can support true streaming.
Full-output blocking scanners should fall back to non-streaming in strict mode.
```

## Failure Behavior

Recommended behavior:

```text
scanner error:
  fail closed

unsupported streaming scanner in strict mode:
  fall back to non-streaming guarded execution

upstream stream error:
  terminate downstream stream safely

client disconnect:
  stop consuming upstream stream

buffer overflow:
  fail closed or block stream
```

## Metrics

Add streaming guardrail metrics:

```text
stream_guardrails_requests_total
stream_guardrails_fallback_total{reason}
stream_guardrails_scans_total
stream_guardrails_scan_latency_seconds
stream_guardrails_block_total
stream_guardrails_redact_total
stream_guardrails_pending_buffer_chars
stream_guardrails_time_to_first_safe_chunk_seconds
stream_guardrails_finalize_latency_seconds
```

Useful fallback reasons:

```text
finalize_only_scanner
scanner_error
buffer_overflow
unsupported_policy
```

## Testing Plan

Add tests for:

```text
stream=true calls upstream with stream=true
stream=true returns text/event-stream
safe chunks are emitted downstream
blocked substring split across chunks is detected
holdback is not emitted early
holdback is flushed at [DONE]
block emits block message and [DONE]
redact emits redacted content
finalize-only scanner triggers strict fallback
guardrails disabled preserves streaming passthrough
non-stream path remains unchanged
```

## PR Plan

To keep PRs small, split implementation into small reviewable changes.

### PR 1: Streaming Passthrough

Scope:

```text
Add stream=true branch in /v1/chat/completions
Call upstream LLM with stream=true
Consume upstream SSE events
Return StreamingResponse to client
No guardrail processing yet
```

Expected core code size: around 150–220 lines.

### PR 2: SSE Utilities

Scope:

```text
Add SSE parsing helpers
Detect data: [DONE]
Extract delta.content
Build downstream OpenAI-compatible SSE chunks
Build block SSE chunk
```

Expected core code size: around 120–180 lines.

### PR 3: Buffer and Holdback

Scope:

```text
Add pending buffer
Add holdback suffix
Add min_scan_chars / max_window_chars / max_emit_chars
Add final flush behavior
Use no-op scanner initially
```

Expected core code size: around 150–220 lines.

### PR 4: Fast Scanner Block Path

Scope:

```text
Run streaming-compatible scanners on scan window
Start with ban_substrings and/or regex
Support block action
Emit block message and [DONE]
Add split-chunk block tests
```

Expected core code size: around 180–250 lines.

### PR 5: Redact, Fallback, and Metrics

Scope:

```text
Support redact action
Add scanner capability classification
Add strict fallback for finalize-only scanners
Add streaming guardrail metrics
Add mixed-policy tests
```

Expected core code size: around 180–250 lines.

## Recommendation

Implement strict mode by default.

This gives clear semantics:

```text
Policies with only streaming-compatible scanners:
  true streaming guardrails

Policies with finalize-only blocking scanners:
  fallback to non-streaming guarded execution
```

This avoids claiming that streaming guardrails provide the same guarantees as
full-response non-stream scanning for all scanner types.