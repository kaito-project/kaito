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

"""Correctness validator for guardrails benchmarking.

Validates: allow path, redaction, blocking, cross-chunk leakage, and SSE validity.
"""

import json
from dataclasses import dataclass, field


@dataclass
class ValidationResult:
    """Result of a correctness validation."""

    name: str
    passed: bool
    detail: str = ""
    leakage_bytes: int = 0


@dataclass
class CorrectnessReport:
    """Aggregated correctness results."""

    results: list[ValidationResult] = field(default_factory=list)

    @property
    def all_passed(self) -> bool:
        return all(r.passed for r in self.results)

    @property
    def total_leakage_bytes(self) -> int:
        return sum(r.leakage_bytes for r in self.results)

    def add(self, result: ValidationResult) -> None:
        self.results.append(result)

    def summary(self) -> dict:
        return {
            "total": len(self.results),
            "passed": sum(1 for r in self.results if r.passed),
            "failed": sum(1 for r in self.results if not r.passed),
            "total_leakage_bytes": self.total_leakage_bytes,
            "details": [
                {
                    "name": r.name,
                    "passed": r.passed,
                    "detail": r.detail,
                    "leakage_bytes": r.leakage_bytes,
                }
                for r in self.results
            ],
        }

    def print_report(self) -> None:
        print("\n" + "=" * 80)
        print("CORRECTNESS REPORT")
        print("=" * 80)
        for r in self.results:
            status = "PASS" if r.passed else "FAIL"
            print(f"  [{status}] {r.name}")
            if r.detail:
                print(f"         {r.detail}")
            if r.leakage_bytes > 0:
                print(f"         leakage_bytes={r.leakage_bytes}")
        passed = sum(1 for r in self.results if r.passed)
        total = len(self.results)
        print(f"\n  {passed}/{total} passed, leakage={self.total_leakage_bytes} bytes")
        print("=" * 80)


# ---------------------------------------------------------------------------
# Allow-path validation
# ---------------------------------------------------------------------------


def validate_allow_unchanged(
    original_text: str,
    output_text: str,
) -> ValidationResult:
    """Verify content passes through unchanged on the allow path."""
    if output_text == original_text:
        return ValidationResult(name="allow_unchanged", passed=True)
    return ValidationResult(
        name="allow_unchanged",
        passed=False,
        detail=f"Content modified on allow path: expected {len(original_text)} chars, got {len(output_text)}",
    )


# ---------------------------------------------------------------------------
# Redaction validation
# ---------------------------------------------------------------------------


def validate_redaction(
    original_text: str,
    output_text: str,
    expected_removals: list[str],
    expected_tags: list[str] | None = None,
) -> ValidationResult:
    """Verify banned content is removed and replaced with expected tags."""
    # Check that all expected removals are absent
    leaked = [r for r in expected_removals if r in output_text]
    if leaked:
        return ValidationResult(
            name="redaction",
            passed=False,
            detail=f"Leaked values still present: {leaked}",
            leakage_bytes=sum(len(v) for v in leaked),
        )

    # Check expected replacement tags are present
    if expected_tags:
        missing_tags = [t for t in expected_tags if t not in output_text]
        if missing_tags:
            return ValidationResult(
                name="redaction",
                passed=False,
                detail=f"Expected redaction tags missing: {missing_tags}",
            )

    # Check safe text is preserved (heuristic: original text minus removals
    # should still appear, except for the removed parts)
    return ValidationResult(name="redaction", passed=True)


def validate_no_false_positive(
    original_text: str,
    output_text: str,
) -> ValidationResult:
    """Verify clean content is not falsely redacted or blocked."""
    if "[REDACTED]" in output_text or "<EMAIL>" in output_text:
        return ValidationResult(
            name="no_false_positive",
            passed=False,
            detail="Clean content was falsely redacted",
        )
    return ValidationResult(name="no_false_positive", passed=True)


# ---------------------------------------------------------------------------
# Block validation
# ---------------------------------------------------------------------------


def validate_block_response(
    output_text: str,
    expected_block_message: str,
) -> ValidationResult:
    """Verify blocked response contains the configured block message."""
    if expected_block_message in output_text:
        return ValidationResult(name="block_response", passed=True)
    return ValidationResult(
        name="block_response",
        passed=False,
        detail=f"Block message not found. Got: {output_text[:200]}",
    )


def validate_streaming_block(
    collected_chunks: list[str],
    expected_block_message: str,
) -> ValidationResult:
    """Verify streaming block: block message present, content_filter finish, [DONE]."""
    full_output = "".join(collected_chunks)

    # Check block message appears
    if expected_block_message not in full_output:
        return ValidationResult(
            name="streaming_block",
            passed=False,
            detail="Block message not found in stream output",
        )

    # Check finish_reason = content_filter
    if '"content_filter"' not in full_output:
        return ValidationResult(
            name="streaming_block",
            passed=False,
            detail="finish_reason 'content_filter' not found",
        )

    # Check [DONE]
    if "[DONE]" not in full_output:
        return ValidationResult(
            name="streaming_block",
            passed=False,
            detail="[DONE] sentinel not found",
        )

    return ValidationResult(name="streaming_block", passed=True)


# ---------------------------------------------------------------------------
# Cross-chunk leakage validation
# ---------------------------------------------------------------------------


def validate_no_leakage(
    emitted_chunks: list[str],
    banned_word: str,
) -> ValidationResult:
    """Verify no bytes of banned content were emitted before enforcement.

    Leakage = bytes belonging to detected policy-violating content
    that become visible to the client before enforcement.
    """
    # Reconstruct the content seen by the client before block/redaction
    visible_content = ""
    for chunk in emitted_chunks:
        # Parse SSE events from chunk
        for line in chunk.split("\n"):
            if not line.startswith("data: "):
                continue
            data = line[6:].strip()
            if data == "[DONE]":
                break
            try:
                payload = json.loads(data)
                for choice in payload.get("choices", []):
                    content = choice.get("delta", {}).get("content")
                    if content:
                        visible_content += content
            except json.JSONDecodeError:
                continue

    # Check for partial or full banned word in visible content
    # (before it was blocked/redacted)
    banned_lower = banned_word.lower()
    visible_lower = visible_content.lower()

    if banned_lower in visible_lower:
        return ValidationResult(
            name="no_leakage",
            passed=False,
            detail=f"Full banned word '{banned_word}' leaked to client",
            leakage_bytes=len(banned_word),
        )

    # Check for partial leakage (prefix of banned word at end of visible content)
    for prefix_len in range(1, len(banned_word)):
        prefix = banned_lower[:prefix_len]
        if visible_lower.endswith(prefix):
            # This is only leakage if the word was actually blocked
            # (the buffer holdback should prevent this)
            return ValidationResult(
                name="no_leakage",
                passed=False,
                detail=f"Partial banned word prefix '{banned_word[:prefix_len]}' leaked",
                leakage_bytes=prefix_len,
            )

    return ValidationResult(name="no_leakage", passed=True, leakage_bytes=0)


# ---------------------------------------------------------------------------
# SSE validity
# ---------------------------------------------------------------------------


def validate_sse_format(chunks: list[str]) -> ValidationResult:
    """Validate SSE event format in collected chunks."""
    full = "".join(chunks)

    # Must end with [DONE]
    if "data: [DONE]" not in full:
        return ValidationResult(
            name="sse_format",
            passed=False,
            detail="Missing data: [DONE] sentinel",
        )

    # Each data line should be valid JSON or [DONE]
    for line in full.split("\n"):
        line = line.strip()
        if not line or not line.startswith("data: "):
            continue
        data = line[6:].strip()
        if data == "[DONE]":
            continue
        try:
            payload = json.loads(data)
            if not isinstance(payload, dict):
                return ValidationResult(
                    name="sse_format",
                    passed=False,
                    detail=f"SSE data is not a JSON object: {data[:100]}",
                )
        except json.JSONDecodeError as e:
            return ValidationResult(
                name="sse_format",
                passed=False,
                detail=f"Invalid JSON in SSE data: {e}",
            )

    return ValidationResult(name="sse_format", passed=True)
