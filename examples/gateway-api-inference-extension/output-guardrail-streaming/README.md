# Output-guardrail streaming: Envoy ext_proc SSE guard PoC (issue #2358)

A Docker harness showing that kaito's **streaming** output guardrails can scan
and act on an SSE stream as it flows, using Envoy's `ext_proc` filter in
`FULL_DUPLEX_STREAMED` mode. Issue #2358 is the streaming variant of the
buffered `output-guardrail/` proof (PR #2357). No Istio, no Kubernetes.

```
client ── SSE ──► Envoy ──ext_proc (gRPC, FULL_DUPLEX_STREAMED)──► mock SSE backend
                       │
                       └── stream_guard.py: streaming scan → redact / block / holdback
```

## Quick start

```bash
docker compose up -d --build
python scripts/run_experiments.py
```

The guard re-reads `extproc-config/stream_guard.json` (`scan_enabled`,
`holdback_bytes`) per stream, so the driver flips settings without a restart and
restores the file when it finishes.

## Experiments

| # | Scenario | Holdback | Asserts |
|---|----------|----------|---------|
| 1 | `normal`  | 0, scan off | pass-through is byte-identical to the unguarded mock |
| 2 | `aligned` | 0 | banned substring inside one SSE event is redacted |
| 3 | `split`   | 0 | one event split across many writes is reassembled |
| 4 | `cross`   | 64 | a secret straddling two SSE events is redacted |
| 5a| `slow`    | 64 | TTFT overhead vs direct-to-mock; content preserved |
| 5b| `block`   | 0 | fail-closed: `PROHIBITED` blocked, no content reaches the client |

Every verdict is a streaming-time decision, not a post-hoc scan. The driver
exits non-zero if any experiment fails.

## Ports

| Service | Port | Purpose |
|---------|------|---------|
| Envoy SSE | `18080` | downstream client (guarded path) |
| Mock SSE | `18081` | direct control path, bypasses the guard |
| Envoy admin | `9901` | stats and `/logging` |

## Findings

**1. `FULL_DUPLEX_STREAMED` over HTTP/1.1 intermittently drops the stream.**
A healthy stream sometimes resolves to a local 400 page (an HTML body, not SSE)
or stalls without the terminating chunk. It is Envoy-side and
scenario-independent, and it fires with the guard off too, so it is not a guard
verdict. The driver retries any non-200 or incomplete stream (the `att`
column). No model content escapes when it fires, so the race fails closed.
Direct-to-mock is deterministic. In production, prefer an HTTP/2 downstream or
buffer the body for fail-closed actions.

**2. Policy keys are schema-exact; a casing slip silently disables a scanner.**
Scanner types are matched after lowercasing, so `banSubstrings` becomes
`bansubstrings` and never matches, and the rule is dropped without warning. Use
the documented snake_case keys: `ban_substrings`, `secrets`, `regex`.

**3. `holdback_bytes` is required to catch a cross-event secret, not optional.**
A secret straddling two SSE events is invisible to any per-event scan. That is the #2358 vulnerability. Reassembling frames only helps while both halves sit in the buffer, and with `holdback_bytes: 0` the buffer is flushed on every body chunk. So the secret is caught only when both halves happen to arrive in the same chunk; when they do not, the client reassembles the leak. Measured on this stack: roughly 1 run in 6 leaked the full secret, and the rate moves with upstream timing. A holdback makes it deterministic, because both halves are then always in the window together, and it additionally keeps secret *prefixes* off the wire since a released prefix cannot be retracted. This is the argument for the `holdbackBytes` field on the production CRD.

Size it against the secrets you must catch, not against the window you hope for: the stand-in patterns have no upper length bound (`sk-...{10,}` matches a key of any length), so there is no single "longest pattern". `stream_guard.json` ships `holdback_bytes: 64`, which covers every concrete match the stand-in can produce at its minimum length, but a longer real key would still straddle the window. Production should derive the holdback from the secret formats it actually enforces.

**4. Fail-closed.** Any scan error or missing verdict frame becomes a `block`:
the client gets a `guardrails_block` error frame and zero content.

## Layout

| Path | Role |
|------|------|
| `client/sse_client.py` | SSE client on `http.client`; timestamps each read for TTFT |
| `mock/mock_stream_backend.py` | SSE backend (`normal`, `aligned`, `split`, `cross`, `slow`, `block`) |
| `extproc/ext_proc_stream_server.py` | gRPC ext_proc service |
| `extproc/stream_guard.py` | streaming scan, holdback, redact, block |
| `extproc/test_stream_guard.py` | unit tests, `make guardrail-streaming-test` |
| `envoy/bootstrap.yaml` | ext_proc filter, clusters, admin |
| `extproc-config/` | `policy.yaml` (scan rules), `stream_guard.json` (runtime) |
| `scripts/run_experiments.py` | experiment driver |

## Notes

- Scope is the streaming path. Detection here is deliberately small: the
  `ban_substrings` and `secrets` scanners only. Production should call the
  llm_guard scanner objects on the window text, reusing
  `presets/ragengine/guardrails/scanner_schemas.py` rather than the regex
  stand-in in `stream_guard.py`.
- For the Istio equivalent, translate the `ext_proc` filter in
  `envoy/bootstrap.yaml` into an `EnvoyFilter`; the filter config is identical.

