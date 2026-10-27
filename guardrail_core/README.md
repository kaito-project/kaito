# KAITO Guardrail Core

`kaito-guardrail-core` provides KAITO-owned scanners and `guardrail_core.streaming`, a
standard-library-only submodule for SSE framing and OpenAI chat chunk
parsing/building. The streaming submodule does not scan, block, or implement
holdback policy.

`guardrail_core.streaming_window` separately provides a policy-independent text
window. This module is unreleased and is not included in PyPI version `0.1.0`.
RAGEngine continues using its own window and its pinned PyPI dependency until a
new shared-library version is published; consumer adoption is a separate change.

## Installation and development

```sh
python -m pip install -e ./guardrail_core
python -m pip install build==1.2.2.post1 twine==6.1.0 pytest==9.1.1
python -m build ./guardrail_core
python -m twine check --strict guardrail_core/dist/*
python -m pytest guardrail_core/tests guardrail_core/guardrail_core
```

The streaming tests import the installed package, not a `sys.path`-injected source
tree. CI builds the `kaito-guardrail-core` wheel, installs it without dependencies in a
fresh virtual environment, and runs the tests from outside the repository on
Python 3.12 through 3.14.

Window tests live in `tests/streaming/test_window.py`, so the same wheel CI and
release checks exercise them independently of RAGEngine. RAGEngine tests install
the exact published version from its requirements, never an editable source copy.

The distribution name is `kaito-guardrail-core`; Python imports remain `guardrail_core`,
including `guardrail_core.streaming`. The initial release keeps existing scanner
dependencies mandatory, although importing streaming itself uses only the standard
library. Packaging does not change parser, scanner, or holdback behavior.

## Publishing

Only `guardrail_core/` is built, not RAGEngine or the repository as a whole.
`pyproject.toml` is the single source of package metadata and version. Wheels
exclude tests; source distributions include tests and the build configuration.
The package license is a copy of the repository's Apache 2.0 license.

Before the first release, confirm the `kaito-guardrail-core` distribution name and
maintainer access with Brandon. An administrator must configure:

- A project or pending publisher on both TestPyPI and PyPI for `kaito-guardrail-core`.
- A GitHub Trusted Publisher on each index: owner `kaito-project`, repository
  `kaito`, workflow filename `release-guardrail-core.yaml`, and the corresponding
  GitHub environment below. Publishing uses GitHub OIDC, not an API token.
- GitHub environments `guardrail-core-testpypi` and `guardrail-core-pypi`, restricted
  to `main`. Require maintainer approval for `guardrail-core-pypi` before enabling
  production publishing.

Reference: [kaito-rag-api release workflow](https://github.com/kaito-project/kaito-rag-api/blob/main/.github/workflows/create-release.yaml).
This package is published from KAITO; no changes to that repository are needed.

After these changes and the intended package version merge into `main`, run the
[Release KAITO Guardrail Core workflow](../.github/workflows/release-guardrail-core.yaml):

1. Select the workflow on `main`. Supply `release_version`, such as `0.1.0`, and
   `release_commit`, the full lowercase 40-character commit SHA on `main`.
   The workflow checks ancestry, metadata/version agreement, and that the package
   tag does not already exist. It does not update the source version.
2. Build the source distribution and then its wheel. Validate metadata and
   distribution contents, then run all scanner and streaming tests against the
   installed wheel in a fresh environment outside the source checkout.
3. Publish those artifacts to TestPyPI. Download its wheel using only TestPyPI and
   compare it byte-for-byte with the original. Install dependencies separately
   from PyPI, then install the downloaded wheel in a fresh environment and smoke
   test the version, imports, framing, and scanners. No mixed-index resolution is
   used.
4. Approve the `guardrail-core-pypi` environment after verification succeeds.
   Publish the same stored wheel and source distribution to PyPI, without
   rebuilding.
5. Create `kaito-guardrail-core-v<version>` and a GitHub Release pointing to the exact
   source commit, with those distributions attached. Package tags do not trigger
   KAITO's application release workflow, and the package release is not marked as
   the repository's latest release.

For transient publishing or index availability failures, inspect the error and
rerun only failed jobs in the same workflow run to reuse the original artifacts.
Existing versions are not silently skipped or overwritten. If a partial upload
prevents retrying, coordinate recovery with the maintainers; do not rebuild
different content under the same version. After a successful release, bump the
version in a new PR before publishing again.

Only after the version exists on production PyPI should RAGEngine and
production-stack adopt an exact dependency such as `kaito-guardrail-core==0.1.0`.
Production images should install requirements rather than copy the library source.
Local development may continue using editable installation. Consumer changes and
adoption of the shared window are separate follow-ups.

## Streaming window

The `guardrail_core.streaming_window` module uses only the Python standard
library. Importing it does not load scanners, FastAPI, RAGEngine, or Envoy.
Its public types are:

- `StreamingBufferWindow(scanner, *, holdback_len)`
- `WindowScanner`, a structural protocol with
  `scan(text, *, flush=False, preceding_char="") -> WindowScanResult`
- `WindowScanResult(sanitized_text=None, blocked=False)`
- `WindowEmitResult(chunks, blocked=False)`
- `WindowScanError`, a scanner failure with the original exception as `__cause__`

### Units and release rules

`holdback_len` is a non-negative count of Python string characters (Unicode code
points), not UTF-8 bytes, tokens, or grapheme clusters.

`feed(text)` appends to the pending buffer. Once it exceeds the holdback, the
scanner sees the whole pending buffer, including the retained tail, before any
prefix is released. Zero holdback scans and releases each non-empty feed.

The scanner may return:

- `WindowScanResult()` to allow pending text unchanged.
- `sanitized_text` to replace the entire pending buffer. Release length is
  recalculated from the sanitized text; `redacted` becomes true.
- `blocked=True` to discard pending text and permanently stop release. Blocking
  takes precedence over `sanitized_text`.

`preceding_char` is the last character actually released, or `""` before the first
release. `flush()` scans the remaining buffer with `flush=True` before releasing
it, allowing right-boundary checks at EOF. Flushing an empty buffer emits nothing.
After blocking, every `feed()` and `flush()` returns an empty, blocked result
without calling the scanner again. A normal flush does not close the window;
the caller determines the stream lifecycle.

### Errors and caller responsibilities

An operational exception from `scanner.scan()` becomes `WindowScanError` with the
original exception chained. The window discards pending text, becomes blocked,
and raises without emitting. Do not retry a failed window. Cancellation and other
`BaseException` control signals propagate unchanged.

The window emits no refusal messages, logs no model text, and chooses no fail-open
policy. Callers must handle `WindowScanError` explicitly and terminate on
cancellation. Callers also own scanner construction, sufficient holdback sizing,
policy ordering, response formatting, and upstream closure. Insufficient holdback
cannot guarantee detection of cross-feed patterns.

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
