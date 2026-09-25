# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Streaming output guardrail primitives for Envoy ext_proc (issue #2358).

No gRPC or Envoy imports here, so this module unit-tests on its own.

* :class:`SSEAssembler` -- Envoy body chunks do not align with SSE events, so incomplete frames are buffered until a terminating ``\\n\\n`` arrives.
* :class:`StreamGuard` -- a holdback window that only releases a text prefix once ``holdback_bytes`` of lookahead exists behind it. That lookahead is what makes a secret spanning two events detectable before it reaches the client. Released bytes cannot be unsent.
* :func:`load_policy` -- reads the ``ban_substrings`` and ``secrets`` scanners from a guardrails policy file.

``ragengine.guardrails.guard_response()`` is deliberately not used: it wants a complete response, not a partial body. Detection in production should call the llm_guard scanner objects directly on the window text.
"""

from __future__ import annotations

import json
import logging
import re
import time
from dataclasses import dataclass, field
from typing import Any

import yaml

logger = logging.getLogger(__name__)

DEFAULT_BLOCK_MESSAGE = "The model output was blocked by output guardrails."
DEFAULT_REDACT_MARKER = "[REDACTED]"

# Stand-in for the llm_guard secrets scanner; this image carries a smaller tree.
_SECRET_PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    ("sk", re.compile(r"\bsk-[A-Za-z0-9\-_]{10,}\b")),
    ("aws", re.compile(r"\bAKIA[0-9A-Z]{16}\b")),
    ("github", re.compile(r"\bghp_[A-Za-z0-9]{36}\b")),
    ("slack", re.compile(r"\bxox[baprs]-[A-Za-z0-9\-]{10,}\b")),
    ("private_key", re.compile(r"-----BEGIN (?:RSA|EC|OPENSSH|DSA) PRIVATE KEY-----")),
)


@dataclass(frozen=True)
class SubstringRule:
    value: str
    action: str = "redact"
    match_type: str = "word"
    case_sensitive: bool = False


@dataclass(frozen=True)
class RegexRule:
    pattern: str
    action: str = "redact"


@dataclass
class ScanPolicy:
    enabled: bool = True
    block_message: str = DEFAULT_BLOCK_MESSAGE
    default_action: str = "redact"
    substrings: list[SubstringRule] = field(default_factory=list)
    regexes: list[RegexRule] = field(default_factory=list)
    secrets: bool = True

    def is_pass_through(self) -> bool:
        return not self.enabled or (
            not self.substrings and not self.regexes and not self.secrets
        )


def _coerce_bool(value: Any, fallback: bool) -> bool:
    if value is None:
        return fallback
    if isinstance(value, bool):
        return value
    raise ValueError(f"invalid boolean option {value!r}; use YAML native true/false")


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]


def _normalize_action(value: Any, fallback: str) -> str:
    if not isinstance(value, str) or not value:
        return fallback
    action = value.lower()
    if action in {"block", "redact"}:
        return action
    logger.warning("invalid action %r, using %r", value, fallback)
    return fallback


def load_policy(policy_path: str) -> ScanPolicy:
    """Read the ``ban_substrings`` and ``secrets`` scanners from a policy file.

    Follows ``ragengine.guardrails.output_guardrails`` in skipping bad entries rather than failing: one malformed scanner should not disable the rest.
    """
    policy: dict[str, Any] = {}
    try:
        with open(policy_path, "rb") as policy_file:
            raw = yaml.safe_load(policy_file.read().decode("utf-8"))
        if isinstance(raw, dict):
            policy = raw
    except (OSError, UnicodeDecodeError, yaml.YAMLError) as exc:
        logger.warning("policy load failure path=%s err=%s", policy_path, exc)
        return ScanPolicy(enabled=False)

    enabled = _coerce_bool(policy.get("enabled"), True)
    default_action = _normalize_action(policy.get("action"), "redact")

    scan_policy = ScanPolicy(
        enabled=enabled,
        default_action=default_action,
    )

    scanners = policy.get("scanners")
    if not isinstance(scanners, list):
        return scan_policy

    for raw_scanner in scanners:
        if not isinstance(raw_scanner, dict):
            continue
        scanner_type = (
            str(raw_scanner.get("type", "")).replace("-", "_").strip().lower()
        )
        action = _normalize_action(raw_scanner.get("action"), default_action)

        if scanner_type == "ban_substrings":
            for value in _coerce_string_list(raw_scanner.get("substrings")):
                scan_policy.substrings.append(
                    SubstringRule(
                        value=value,
                        action=action,
                        match_type=str(raw_scanner.get("match_type", "word")).lower(),
                        case_sensitive=_coerce_bool(
                            raw_scanner.get("case_sensitive"), False
                        ),
                    )
                )
        elif scanner_type == "secrets":
            scan_policy.secrets = True
            if action == "block":
                scan_policy.regexes.extend(
                    RegexRule(pattern=p.pattern, action="block")
                    for p in _SECRET_PATTERNS
                )
        elif scanner_type == "regex":
            for pattern in _coerce_string_list(raw_scanner.get("patterns")):
                scan_policy.regexes.append(RegexRule(pattern=pattern, action=action))
        else:
            logger.warning(
                "windowed scanning handles ban_substrings/secrets/regex only, "
                "ignoring type=%r",
                scanner_type,
            )

    return scan_policy


@dataclass(frozen=True)
class ScanMatch:
    """One policy match, as a span over the window text."""

    start: int
    end: int
    action: str
    replacement: str


def collect_matches(text: str, policy: ScanPolicy) -> list[ScanMatch]:
    """Return every policy match in ``text`` as a span.

    ``StreamGuard`` needs spans rather than a rewritten string so redactions map back onto the delta-content stream: a secret split across two events is only contiguous there, never in the raw framed bytes.
    """
    if not policy.enabled:
        return []

    matches: list[ScanMatch] = []

    def add(start: int, end: int, action: str, replacement: str) -> None:
        matches.append(ScanMatch(start, end, action, replacement))

    for rule in policy.substrings:
        flags = 0 if rule.case_sensitive else re.IGNORECASE
        if rule.match_type == "word":
            pattern = re.compile(r"(?<!\w)" + re.escape(rule.value) + r"(?!\w)", flags)
        else:
            pattern = re.compile(re.escape(rule.value), flags)
        for match in pattern.finditer(text):
            add(match.start(), match.end(), rule.action, DEFAULT_REDACT_MARKER)

    for rule in policy.regexes:
        try:
            pattern = re.compile(rule.pattern)
        except re.error as exc:
            logger.warning("invalid regex %r skipped: %s", rule.pattern, exc)
            continue
        for match in pattern.finditer(text):
            add(match.start(), match.end(), rule.action, DEFAULT_REDACT_MARKER)

    if policy.secrets and not any(m.action == "block" for m in matches):
        for _kind, pattern in _SECRET_PATTERNS:
            for match in pattern.finditer(text):
                add(match.start(), match.end(), "redact", DEFAULT_REDACT_MARKER)

    return _dedupe_scan_matches(matches)


def _dedupe_scan_matches(matches: list[ScanMatch]) -> list[ScanMatch]:
    accepted: list[ScanMatch] = []
    for match in sorted(matches, key=lambda m: (m.start, -(m.end - m.start))):
        if any(
            other.start < match.end and match.start < other.end for other in accepted
        ):
            continue
        accepted.append(match)
    return accepted


def _apply_redactions(text: str, matches: list[ScanMatch]) -> str:
    out = text
    for match in sorted(matches, key=lambda m: m.start, reverse=True):
        out = out[: match.start] + match.replacement + out[match.end :]
    return out


def _extract_delta_content(obj: dict[str, Any]) -> str | None:
    """Join ``choices[*].delta.content`` from one OpenAI chunk into a string.

    Returns ``None`` when the frame carries no model text (e.g. a
    ``[DONE]``-style payload, a role-only frame, or an error object itself).
    """
    parts: list[str] = []
    choices = obj.get("choices")
    if not isinstance(choices, list):
        return None
    for choice in choices:
        if not isinstance(choice, dict):
            continue
        delta = choice.get("delta")
        if not isinstance(delta, dict):
            continue
        content = delta.get("content")
        if isinstance(content, str) and content:
            parts.append(content)
    return "".join(parts) if parts else None


def _encode_content_event(content: str, finish_reason: str | None = None) -> bytes:
    """Re-encode a window slice of model text as a chat.completion.chunk SSE frame."""
    payload = {
        "id": "chatcmpl-guard",
        "object": "chat.completion.chunk",
        "created": 0,
        "model": "guard",
        "choices": [
            {
                "index": 0,
                "delta": {"content": content},
                "finish_reason": finish_reason,
            }
        ],
    }
    return (
        b"data: "
        + json.dumps(payload, ensure_ascii=False).encode("utf-8")
        + SSE_TERMINATOR
    )


SSE_TERMINATOR = b"\n\n"


class SSEAssembler:
    """Reassembles complete SSE frames from an arbitrary byte stream.

    A frame is complete when it ends with ``\\n\\n``.  Non-data lines and
    ``data: [DONE]`` frames are returned untouched.
    """

    def __init__(self) -> None:
        self._buffer = bytearray()

    def push(self, data: bytes) -> list[bytes]:
        self._buffer.extend(data)
        frames: list[bytes] = []
        while True:
            end = self._buffer.find(SSE_TERMINATOR)
            if end == -1:
                break
            term_end = end + len(SSE_TERMINATOR)
            frames.append(bytes(self._buffer[:term_end]))
            del self._buffer[:term_end]
        return frames

    def remainder(self) -> bytes:
        return bytes(self._buffer)


def _extract_data_json(frame: bytes) -> dict[str, Any] | None:
    """Return the JSON payload of a single ``data: {...}\\n\\n`` frame."""
    try:
        lines = frame.decode("utf-8").splitlines()
    except UnicodeDecodeError:
        return None
    json_lines = []
    for line in lines:
        if line.startswith("data:"):
            json_lines.append(line[len("data:") :].strip())
    if not json_lines:
        return None
    payload = "\n".join(json_lines)
    if payload in ("[DONE]", ""):
        return None
    try:
        return json.loads(payload)
    except json.JSONDecodeError:
        return None


class Emit:
    """A chunk of body bytes to stream back downstream."""

    __slots__ = ("body", "end_stream", "blocked")

    def __init__(
        self, body: bytes, end_stream: bool = False, blocked: bool = False
    ) -> None:
        self.body = body
        self.end_stream = end_stream
        self.blocked = blocked


class StreamGuard:
    """Scans the reconstructed model-text stream through a holdback window.

    A secret split across two SSE events is only contiguous in the delta-content channel, so the guard reassembles frames, concatenates the text, scans that, and re-encodes the result as fresh ``chat.completion.chunk`` events.

    Only the trailing ``holdback_bytes`` characters can still be rescanned, so a secret that *starts* beyond the window (deep inside a very large frame) is unrecoverable by the time its tail arrives. Keep ``holdback_bytes`` at or above the longest pattern that must never leak. Released bytes are gone.

    A ``block`` action fails closed: one ``guardrails_block`` error frame goes out and nothing after it. A complete frame that is not a JSON delta (a ``[DONE]`` or a comment) passes through unscanned; a trailing frame that never got its blank-line terminator is scanned as raw text.
    """

    def __init__(
        self,
        policy: ScanPolicy,
        holdback_bytes: int = 0,
        scan_enabled: bool = True,
    ) -> None:
        self.policy = policy
        self.holdback_bytes = max(0, holdback_bytes)
        self.scan_enabled = scan_enabled and not policy.is_pass_through()

        self._assembler = SSEAssembler()
        self._content_buf = bytearray()  # model text awaiting scan
        self._passthrough: list[bytes] = []  # [DONE]/comments, emitted at finish
        self._blocked = False
        self._eos = False
        self.scan_calls = 0
        self.scan_ms_total = 0.0
        self.max_scan_ms = 0.0
        self.bytes_emitted = 0

    @property
    def blocked(self) -> bool:
        return self._blocked

    @property
    def finished(self) -> bool:
        """True once the upstream signalled end_of_stream."""
        return self._eos

    @property
    def held_chars(self) -> int:
        return len(self._content_buf)

    def consume(self, chunk: bytes, end_of_stream: bool = False) -> Emit:
        """Feed one upstream body chunk and return the bytes to emit now."""
        if not self.scan_enabled:
            self._eos = end_of_stream
            return Emit(chunk, end_stream=end_of_stream)

        for frame in self._assembler.push(chunk):
            self._stage(frame)

        if self._blocked:
            return Emit(b"", blocked=True)
        if end_of_stream:
            self._eos = True
            return self._finish()
        return self._release_window()

    def _stage(self, frame: bytes) -> None:
        """Extract model text from one complete SSE frame into the window."""
        payload = _extract_data_json(frame)
        if payload is None:
            self._passthrough.append(frame)
            return
        content = _extract_delta_content(payload)
        if content:
            self._content_buf.extend(content.encode("utf-8"))

    def _run_scan(self, text: str) -> tuple[list[ScanMatch], float]:
        began = time.perf_counter()
        matches = collect_matches(text, self.policy)
        scan_ms = (time.perf_counter() - began) * 1000
        self.scan_calls += 1
        self.scan_ms_total += scan_ms
        self.max_scan_ms = max(self.max_scan_ms, scan_ms)
        return matches, scan_ms

    def _release_window(self) -> Emit:
        """Scan the window and release the prefix that has enough lookahead."""
        if not self._content_buf:
            return Emit(b"")
        text = self._content_buf.decode("utf-8", errors="replace")
        matches, _scan_ms = self._run_scan(text)

        if any(match.action == "block" for match in matches):
            self._blocked = True
            logger.warning("streaming block triggered matches=%s", matches)
            return Emit(_block_frame(self.policy.block_message), blocked=True)

        redacted = _apply_redactions(text, matches)
        if self.holdback_bytes > 0:
            release_chars = max(0, len(redacted) - self.holdback_bytes)
        else:
            release_chars = len(redacted)
        if release_chars <= 0:
            self._content_buf = bytearray(redacted.encode("utf-8"))
            return Emit(b"")

        released = redacted[:release_chars]
        self._content_buf = bytearray(redacted[release_chars:].encode("utf-8"))
        self.bytes_emitted += len(released.encode("utf-8"))
        return Emit(_encode_content_event(released))

    def flush(self) -> Emit:
        """Release everything still held (used at trailers / stream close)."""
        return self._finish()

    def _finish(self) -> Emit:
        """Stream is over: scan and release the remainder one last time."""
        remainder = self._assembler.remainder()
        if remainder:
            self._content_buf.extend(remainder)
            self._assembler = SSEAssembler()

        if self._blocked:
            return Emit(b"", end_stream=True, blocked=True)

        if self._content_buf:
            text = self._content_buf.decode("utf-8", errors="replace")
            matches, _scan_ms = self._run_scan(text)
            if any(match.action == "block" for match in matches):
                self._blocked = True
                logger.warning("streaming block triggered at eos matches=%s", matches)
                return Emit(
                    _block_frame(self.policy.block_message),
                    end_stream=True,
                    blocked=True,
                )
            redacted = _apply_redactions(text, matches)
            body = _encode_content_event(redacted, finish_reason="stop")
            self._content_buf = bytearray()
            self.bytes_emitted += len(redacted.encode("utf-8"))
            out = [body]
        else:
            out = []

        out.extend(self._passthrough)
        return Emit(b"".join(out), end_stream=True)


def _block_frame(block_message: str) -> bytes:
    payload = {
        "error": {
            "message": block_message,
            "type": "guardrails_block",
        }
    }
    return b"data: " + json.dumps(payload).encode("utf-8") + SSE_TERMINATOR
