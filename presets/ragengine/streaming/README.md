# RAG Engine Streaming Guardrails

This package implements Server-Sent Events (SSE) handling and incremental output guardrail scanning for OpenAI-compatible chat completion streams.

## Scope

The current implementation intentionally supports a narrow, reviewable contract:

- OpenAI-compatible chat completion SSE responses
- one choice (`n=1`, choice index `0`)
- `action: block` only
- these output scanners:
  - `ban_substrings`
  - `invisible_text`
  - `secrets`
  - `sensitive`

The implementation does not support streaming redaction. Once bytes have been sent to a client, they cannot be withdrawn or replaced safely. Blocking is possible because the implementation retains an un-emitted tail and scans it before release.

## Data Flow

```text
upstream byte chunks
        |
        v
  SSEFramer / iter_sse_events        streaming/sse.py
        |
        v
  parse_openai_chat_sse_event        streaming/openai.py
        |
        v
  StreamingBufferWindow             streaming/buffer_window.py
        |
        v
  llm_guard output scanners         streaming/guardrails.py
        |
        +-- safe: emit OpenAI SSE delta chunks
        |
        +-- blocked: emit refusal + content_filter + [DONE]
```

`apply_streaming_guardrails()` owns the complete pipeline. It closes the upstream async iterator when the stream completes or terminates early.

## Files

### `sse.py`

Frames arbitrary text chunks into complete SSE events. Network chunk boundaries do not need to align with SSE event boundaries. Both `\n\n` and `\r\n\r\n` event separators are accepted.

`SSEEvent` preserves the raw event text and exposes combined `data:` lines for protocol-specific parsing.

### `openai.py`

Parses and validates OpenAI chat completion SSE events. It recognizes normal JSON events and the terminal `data: [DONE]` event, and extracts choice content and finish reasons.

The module also builds normalized OpenAI-compatible SSE chunks for:

- content deltas
- finish reasons
- `[DONE]`

Malformed or unsupported event shapes are rejected rather than passed through without scanning.

### `buffer_window.py`

Provides the protocol-independent sliding window used for incremental scanning.

The window retains the newest `holdback_len` characters. Older text is emitted only after the complete pending buffer has passed every configured scanner. On stream completion, `flush()` scans the final retained tail before emitting it.

The default holdback in `guardrails.py` is 256 characters. This lets scanners detect values that cross upstream chunk boundaries before any part of the value is released.

The window keeps only a sliding tail, not the full response. It is therefore appropriate for local pattern detection, but not for scanners that require the entire completed document.

### `guardrails.py`

Integrates SSE parsing, the sliding window, scanner execution, and OpenAI-compatible response generation.

Before streaming starts, `validate_streaming_guardrails()` rejects:

- any action other than `block`
- any scanner outside the streaming allowlist

Each candidate window is scanned with the configured `llm_guard` scanners. When all scanners accept it, safe content is emitted. When any scanner rejects it, the stream emits:

1. the configured block message
2. `finish_reason: "content_filter"`
3. `data: [DONE]`

A malformed upstream SSE event follows the same fail-closed refusal path.

## Supported Scanners

| Scanner | Detects | Streaming suitability |
| --- | --- | --- |
| `ban_substrings` | Configured prohibited words or strings | Local text matching |
| `invisible_text` | Invisible or non-printable Unicode content | Local character detection |
| `secrets` | Common secret and credential formats | Local pattern detection |
| `sensitive` | Email, phone, credit card, and IPv4 patterns | Local pattern detection |

All four scanners must use `action: block` in streaming mode. Their non-streaming redaction capabilities do not change this streaming restriction.

The following registered output scanners are deliberately not in the streaming allowlist:

- `json`: validity can only be decided from the complete document
- `reading_time`: requires cumulative output length
- `token_limit`: requires a cumulative token count
- `regex`: not part of the current n=1 delivery scope

## Current Limitations

### One choice only

The implementation uses one global `StreamingBufferWindow` and emits rebuilt chunks with choice index `0`. The API rejects requests with `n > 1`. The guardrails pipeline also fails closed if an upstream event contains multiple parsed choices or a choice index other than `0`, preventing independent choices from being mixed in one scanner window.

This is an intentional product scope decision rather than a parser limitation. Multi-choice streaming would require an independent scanner window for each choice and a defined policy for partial failures: if one choice is blocked, the service must decide whether to terminate only that choice or the entire response. It would also multiply inference and scanning cost for a workflow that normally consumes one answer. The OpenAI parser remains capable of recognizing multiple choices so this can be revisited without replacing the protocol layer, but implementation should wait for a concrete `n > 1` use case and an agreed blocking contract.

### Normalized output events

Content events are rebuilt instead of forwarding the raw upstream event. Metadata carried only on the original content event is not preserved. Finish-reason events are forwarded after the final buffered content is scanned.

### Fixed character holdback

The 256-character holdback is a safety boundary for local patterns, not an unlimited guarantee. A configured prohibited value or credential format longer than the holdback could be split across released and retained text. Changes to scanner behavior must be evaluated against this boundary.

### Fail-closed parsing

Malformed OpenAI SSE events produce the configured refusal. This prevents unparsed content from bypassing guardrails, but means protocol extensions must be added to `openai.py` before they can pass through this pipeline.

## Policy Example

```yaml
action: block
blockMessage: "The model output was blocked by policy."
scanners:
  - type: invisible_text
    action: block
  - type: secrets
    action: block
  - type: sensitive
    action: block
    detectors:
      - email
      - phone
      - credit_card
      - ip_address
  - type: ban_substrings
    action: block
    substrings:
      - prohibited phrase
    match_type: str
```

The exact enclosing policy structure is owned by the RAG Engine output guardrails configuration. The important streaming constraints are that every scanner uses `block` and every scanner type appears in the streaming allowlist.

## Tests

Streaming-specific tests are under `presets/ragengine/tests/streaming/`.

Run the focused suite from the repository root:

```bash
.venv/bin/python -m pytest presets/ragengine/tests/streaming -v
```

Run lint and formatting checks for this package and its tests:

```bash
.venv/bin/ruff check --output-format=github \
  presets/ragengine/streaming \
  presets/ragengine/tests/streaming
.venv/bin/ruff format --check \
  presets/ragengine/streaming \
  presets/ragengine/tests/streaming
```

SSE test fixtures must include an integer `index` in every OpenAI choice object. Omitting it exercises the malformed-event refusal path instead of the intended scanner path.
