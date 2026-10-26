# KAITO Guardrail Core

`guardrail-core` provides KAITO-owned scanners and `guardrail_core.streaming`, a
standard-library-only submodule for SSE framing and OpenAI chat chunk
parsing/building. The streaming submodule does not scan, block, or implement
holdback policy.

## Installation and development

```sh
python -m pip install ./guardrail_core
python -m pip install build==1.2.2.post1 pytest==8.4.2
python -m build --wheel ./guardrail_core
python -m pytest guardrail_core/tests/streaming
```

The streaming tests import the installed package, not a `sys.path`-injected source
tree. CI builds the `guardrail-core` wheel, installs it without dependencies in a
fresh virtual environment, and runs the tests from outside the repository on
Python 3.12 through 3.14.

## Byte-oriented SSE framing

```python
from guardrail_core.streaming.sse import SSEFramer
from guardrail_core.streaming.openai import parse_openai_chat_sse_event

framer = SSEFramer(max_event_bytes=1024 * 1024)
for chunk in [b'data: {"choices":[]}\r', b"\n\r", b"\n"]:
    for event in framer.feed(chunk):
        result = parse_openai_chat_sse_event(event)
        print(result.status, result.payload)
for event in framer.finish():
    print(event.data)
```

Call `feed(bytes)` in stream order and `finish()` exactly once at EOF. Both return
lists of complete `SSEEvent` objects. LF, CRLF, CR, mixed endings, and arbitrary
byte boundaries (including UTF-8 code points) are supported. A trailing CR is
held until the next byte or EOF determines whether it is part of CRLF.
Only complete events are decoded, using strict UTF-8. EOF never invents a blank
line: a single final line ending is not an event terminator.

Every terminating blank line produces an event, including blank-only and
comment-only events. `data` and `data:` both represent an empty data field;
one optional leading ASCII space is removed from field values. Multiple data
fields are joined with LF. Comments and other fields remain available but do not
contribute to `data`. Only CR/LF are line endings (not Unicode line separators).

- `event.raw_bytes`: exact received bytes, **including** line endings and the
  terminating blank line, suitable for lossless forwarding.
- `event.raw`: decoded event body, excluding its final line ending and blank line.
- `event.lines`: body lines, including comments and non-data fields.
- `event.data`: joined data fields, or `None` when no data fields exist.
- `SSEEvent.from_raw(text)`: parse an already decoded event **body**, without
  framing; its `raw_bytes` defaults to the UTF-8 encoding of that body.

### Errors and resource bounds

`SSEFramingError` is a `ValueError` with these subclasses:

| Exception | Meaning |
| --- | --- |
| `SSEDecodeError` | A complete event is not valid UTF-8; the original decode error is chained. |
| `SSEIncompleteEventError` | EOF left an unterminated event, including an incomplete UTF-8 sequence. |
| `SSEEventTooLargeError` | An event exceeded `max_event_bytes` (default 1 MiB). |

The positive integer limit counts wire bytes, including delimiters, independently
for each event. Buffering and processing are bounded by that limit and linear in
input size; a huge delimiter-free chunk is rejected without copying it in full.
Invalid limits and non-byte input raise `ValueError`/`TypeError`.

On a framing error, `error.raw_bytes` contains the failing event's buffered bytes
(at most the limit). `error.completed_events` contains earlier events completed
in the **same** call, so callers can explicitly choose whether to forward them.
The framer becomes unusable after either a framing error or `finish()`; subsequent
calls raise `ValueError`. There is no silent recovery, dropped malformed event,
or automatic failure policy.

## OpenAI parsing contract

`parse_openai_chat_sse_event(event)` returns `OpenAIChatChunkParseResult`, retaining
RAGEngine's status names and convenience fields:

| Input | `OpenAIChatChunkParseStatus` |
| --- | --- |
| No data field, empty data, or whitespace-only data | `NO_DATA` |
| Data equal to `[DONE]` after stripping whitespace | `DONE` |
| Invalid JSON | `MALFORMED_JSON`, with `error` |
| Non-object payload, non-list choices, or non-object choice | `INVALID_PAYLOAD`, with `error` |
| Missing/non-integer choice index (including bool) | `INVALID_PAYLOAD`, with `error` |
| Missing/null delta | `PARSED`, treating delta as an empty object |
| Non-object, non-null delta | `INVALID_PAYLOAD`, with `error` |
| Missing/null content | `PARSED`, no text |
| Non-string, non-null content or finish_reason | `INVALID_PAYLOAD`, with `error` |
| Valid choices, including empty, role-only, and multi-choice chunks | `PARSED` |

Missing `choices` is treated as an empty list, matching RAGEngine (e.g. usage-only
chunks). Every choice is retained in order; integer indexes need not be zero or
contiguous. `parsed_choices` includes role-only and tool-call-only choices.
Each `ParsedOpenAIChoice` exposes `choice_index`, `content`, `finish_reason`, and
the complete original `choice` dictionary. `payload` retains all decoded fields
for object payloads, including invalid objects. `contents` and `finish_reasons`
contain only non-null extracted values; use `parsed_choices` for alignment.
The parser does not mutate input. Bare `[DONE]` without a data field is `NO_DATA`,
not a compatibility shortcut. Stream bytes remain available on the original event.

## SSE builders

The functions in `guardrail_core.streaming.openai` return UTF-8-encodable strings
with LF delimiters, matching RAGEngine:

- `build_openai_chat_delta_sse_chunk(content, *, choice_index=0)`
- `build_openai_chat_finish_reason_sse_chunk(*, finish_reason="content_filter", choice_index=0)`
- `build_sse_done_chunk()`
- `build_sse_data_chunk(payload)` for arbitrary JSON object fields and choices

Text builders create new minimal chunks; use the generic builder with a retained
payload to preserve metadata, or forward `event.raw_bytes` for byte-exact replay.

The framing regression cases are adapted from
[production-stack#228](https://github.com/kaito-project/production-stack/pull/228).
Its single-choice/index-zero restrictions are intentionally not adopted.
