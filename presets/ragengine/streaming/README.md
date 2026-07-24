# RAG Engine Streaming Guardrails

Incremental output scanning for OpenAI-compatible chat completion SSE streams.

## Scope

- single choice only (`n=1`, choice index `0`)
- `action: block` only
- supported scanners: `ban_substrings`, `invisible_text`, `secrets`, `sensitive`

The API rejects `n > 1`. The pipeline also fails closed on multiple choices, a nonzero choice index, or malformed SSE.

Streaming redaction is not supported because emitted bytes cannot be withdrawn. Text is held and scanned before release so a scanner hit can safely block the response.

## Flow

```text
upstream chunks
  -> SSE framing
  -> OpenAI event parsing
  -> holdback window
  -> output scanners
  -> safe deltas OR block message + content_filter + [DONE]
```

## Files

| File | Responsibility |
| --- | --- |
| `sse.py` | Frame network chunks into complete SSE events |
| `openai.py` | Parse and build OpenAI-compatible SSE events |
| `buffer_window.py` | Retain and scan an un-emitted text tail |
| `guardrails.py` | Validate policy, run scanners, and emit or block |

The default holdback is 256 characters. The window keeps only the pending tail, so it supports local pattern detection but not scanners that need the complete response.

## Scanner Support

| Scanner | Detects |
| --- | --- |
| `ban_substrings` | Configured prohibited strings |
| `invisible_text` | Invisible or non-printable Unicode characters |
| `secrets` | Common credentials and secret formats |
| `sensitive` | Email, phone, credit card, and IPv4 patterns |

Not supported in streaming:

- `json`: requires the complete document
- `reading_time`: requires cumulative output
- `token_limit`: requires cumulative token count
- `regex`: outside the current delivery scope

## Limitations

- Content events are rebuilt with choice index `0`; original content-event metadata is not preserved.
- Patterns longer than the 256-character holdback may cross the release boundary.
- Multi-choice requires per-choice windows and defined partial-block semantics.

## Future Redaction

Implement separately in this order:

1. `invisible_text`
2. `sensitive`
3. `secrets`
4. `ban_substrings`

Sanitized text must replace held text before any matching bytes are emitted.

## Policy Example

```yaml
action: block
blockMessage: "The model output was blocked by policy."
scanners:
  - type: invisible_text
  - type: secrets
  - type: sensitive
    detectors: [email, phone, credit_card, ip_address]
  - type: ban_substrings
    substrings: [prohibited phrase]
    match_type: str
```

## Validation

```bash
.venv/bin/python -m pytest presets/ragengine/tests/streaming -v
.venv/bin/ruff check presets/ragengine/streaming presets/ragengine/tests/streaming
.venv/bin/ruff format --check presets/ragengine/streaming presets/ragengine/tests/streaming
```
