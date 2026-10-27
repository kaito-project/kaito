# RAG Engine Streaming Guardrails

Incremental output scanning for OpenAI-compatible chat completion SSE streams.

## Scope

- single choice only (`n=1`, choice index `0`)
- supported scanners: `ban_substrings`, `invisible_text`, `secrets`, `sensitive`

The API rejects `n > 1`. The pipeline also fails closed on multiple choices, a nonzero choice index, or malformed SSE.

Text is held and scanned before release so a scanner can safely block or redact the response.

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

The default holdback is 256 characters. Policies with longer banned substrings
increase it to the longest configured substring length minus one for `str`
matching, or the full substring length for `word` matching. The window retains
the pending tail and the preceding character's boundary class, so it supports
local pattern detection but not scanners that need the complete response.

## Scanner Support

| Scanner | Detects |
| --- | --- |
| `ban_substrings` | Configured prohibited strings |
| `invisible_text` | Invisible or non-printable Unicode characters |
| `secrets` | Common credentials and secret formats |
| `sensitive` | Email, phone, credit card, and IPv4 patterns |

Streaming supports `block` for all listed scanners and `redact` for
`ban_substrings`, `invisible_text`, and `sensitive`, plus `secrets` with
`redactMode: all`.
Redaction scanners sanitize held text before block scanners validate the final text.
Secrets redaction replaces all occurrences of detected values and fails closed if
the sanitized output cannot be verified.
Streaming `ban_substrings` supports `str` and `word` matching.
`contains_all: true` is not supported because the current windowed implementation
does not track matches across the complete response.

Not supported in streaming:

- `json`: requires the complete document
- `reading_time`: requires cumulative output
- `token_limit`: requires cumulative token count
- `regex`: outside the current delivery scope

## Limitations

- Content events are rebuilt with choice index `0`; original content-event metadata is not preserved.

## Policy Example

```yaml
action: block
blockMessage: "The model output was blocked by policy."
scanners:
  - type: invisible_text
    action: redact
  - type: secrets
    action: redact
    redactMode: all
  - type: sensitive
    detectors: [email, phone, credit_card, ip_address]
  - type: ban_substrings
    action: redact
    substrings: [prohibited phrase]
    match_type: str
```

## Validation

```bash
.venv/bin/python -m pytest presets/ragengine/tests/streaming -v
.venv/bin/ruff check presets/ragengine/streaming presets/ragengine/tests/streaming
.venv/bin/ruff format --check presets/ragengine/streaming presets/ragengine/tests/streaming
```
