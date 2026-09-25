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

"""Unit tests for the streaming guard primitives (issue #2358 experiments).

python -m pytest test_stream_guard.py -v
"""

import json
import os
import tempfile
import unittest

from stream_guard import (
    DEFAULT_REDACT_MARKER,
    ScanPolicy,
    StreamGuard,
    SubstringRule,
    collect_matches,
    load_policy,
)

CROSS_SECRET = "sk-1234567890abcdef"


def chunk(content, obj_hook=None):
    """Build the ``data: ...`` payload of one chat.completion.chunk event."""
    obj = {
        "id": "chatcmpl-mock",
        "object": "chat.completion.chunk",
        "created": 0,
        "model": "mock",
        "choices": [{"index": 0, "delta": {"content": content}, "finish_reason": None}],
    }
    if obj_hook:
        obj.update(obj_hook)
    return json.dumps(obj, ensure_ascii=False).encode("utf-8")


def sse(payload_bytes):
    return b"data: " + payload_bytes + b"\n\n"


def done_event():
    return b"data: [DONE]\n\n"


def joined_content(capture):
    """Concatenate ``choices[*].delta.content`` across every SSE frame.

    This is the proxy a real SSE client and therefore the *security* metric:
    the joined stream must not contain the secret.
    """
    parts = []
    for frame in capture.split(b"\n\n"):
        if not frame.strip():
            continue
        line = frame.decode("utf-8", errors="replace")
        if not line.startswith("data:"):
            continue
        payload = line[len("data:") :].strip()
        if not payload or payload == "[DONE]":
            continue
        try:
            obj = json.loads(payload)
        except json.JSONDecodeError:
            continue
        choices = obj.get("choices")
        if not isinstance(choices, list):
            continue
        for choice in choices:
            delta = (choice or {}).get("delta")
            if isinstance(delta, dict) and isinstance(delta.get("content"), str):
                parts.append(delta["content"])
    return "".join(parts)


def policy_with(*, ban=("foo",), secret=True):
    """Build a ScanPolicy mirroring the harness default policy."""
    return ScanPolicy(
        enabled=True,
        substrings=[SubstringRule(value=value, match_type="word") for value in ban],
        secrets=secret,
    )


def run_stream(guard, chunks):
    """Feed chunks and collect all emitted bytes (mirrors client capture)."""
    capture = bytearray()
    for chunk_data, eos in chunks:
        emit = guard.consume(chunk_data, end_of_stream=eos)
        capture.extend(emit.body)
        if eos:
            break
    return bytes(capture)


class TestWithinEventMutation(unittest.TestCase):
    def test_foo_redacted_in_event_content(self):
        guard = StreamGuard(policy_with(), holdback_bytes=64)
        capture = run_stream(guard, [(sse(chunk("contains foo inside")), True)])
        self.assertNotIn("foo", joined_content(capture))
        self.assertIn(DEFAULT_REDACT_MARKER, joined_content(capture))

    def test_clean_event_preserves_content(self):
        guard = StreamGuard(policy_with(), holdback_bytes=64)
        capture = run_stream(guard, [(sse(chunk("clean text")), True)])
        self.assertEqual(joined_content(capture), "clean text")

    def test_done_event_survives(self):
        guard = StreamGuard(policy_with(), holdback_bytes=64)
        capture = run_stream(
            guard,
            [(sse(chunk("hi")), False), (done_event(), True)],
        )
        self.assertEqual(joined_content(capture), "hi")
        self.assertTrue(capture.endswith(done_event()))


class TestSplitEventReconstruction(unittest.TestCase):
    def test_event_reconstructed_across_chunks(self):
        guard = StreamGuard(policy_with(), holdback_bytes=64)
        event = sse(chunk("reassembled across many writes"))
        third = len(event) // 3
        first, rest = event[:third], event[third:]
        second, third_ = rest[:third], rest[third:]
        capture = run_stream(
            guard,
            [(first, False), (second, False), (third_, True)],
        )
        self.assertEqual(joined_content(capture), "reassembled across many writes")
        self.assertFalse(guard.blocked)


class TestCrossEventSecret(unittest.TestCase):
    def _secret_events(self):
        prefix = CROSS_SECRET[:3]
        suffix = CROSS_SECRET[3:]
        return [
            (sse(chunk(f"The key is {prefix}")), False),
            (sse(chunk(f"{suffix} and that is all")), True),
        ]

    def test_naive_no_holdback_leaks_secret(self):
        guard = StreamGuard(policy_with(ban=(), secret=True), holdback_bytes=0)
        capture = run_stream(guard, self._secret_events())
        self.assertIn(CROSS_SECRET, joined_content(capture), "naive path should leak")

    def test_holdback_redacts_secret_before_release(self):
        guard = StreamGuard(policy_with(ban=(), secret=True), holdback_bytes=64)
        capture = run_stream(guard, self._secret_events())
        joined = joined_content(capture)
        self.assertNotIn(CROSS_SECRET, joined, "secret must not leak")
        self.assertIn(DEFAULT_REDACT_MARKER, joined)
        self.assertFalse(guard.blocked)

    def test_holdback_smaller_than_pattern_leaks_prefix(self):
        guard = StreamGuard(policy_with(ban=(), secret=True), holdback_bytes=2)
        capture = run_stream(guard, self._secret_events())
        self.assertIn(CROSS_SECRET, joined_content(capture))


class TestWindowBoundary(unittest.TestCase):
    def test_secret_near_event_end_is_safe(self):
        guard = StreamGuard(policy_with(ban=(), secret=True), holdback_bytes=64)
        first = sse(chunk("The key is " + CROSS_SECRET[:3]))
        second = sse(chunk(CROSS_SECRET[3:] + " and that is all"))
        capture = run_stream(guard, [(first, False), (second, True)])
        joined = joined_content(capture)
        self.assertNotIn(CROSS_SECRET, joined)
        self.assertIn(DEFAULT_REDACT_MARKER, joined)

    def test_large_frame_prefix_leak_is_documented_limit(self):
        guard = StreamGuard(policy_with(ban=(), secret=True), holdback_bytes=64)
        first = sse(chunk("A" * 400 + CROSS_SECRET[:3]))
        second = sse(chunk(CROSS_SECRET[3:] + " tail"))
        capture = run_stream(guard, [(first, False), (second, True)])
        self.assertIn(CROSS_SECRET, joined_content(capture))


class TestBlockFailClosed(unittest.TestCase):
    def test_block_cuts_remaining_stream(self):
        policy = ScanPolicy(
            enabled=True,
            substrings=[SubstringRule(value="PROHIBITED", action="block")],
        )
        guard = StreamGuard(policy, holdback_bytes=0)
        capture = run_stream(
            guard,
            [
                (sse(chunk("starts fine ")), False),
                (sse(chunk("PROHIBITED here")), True),
            ],
        )
        self.assertIn("starts fine", joined_content(capture))
        self.assertNotIn("PROHIBITED", joined_content(capture))
        self.assertTrue(guard.blocked)
        self.assertIn(b"guardrails_block", capture)


class TestPassThrough(unittest.TestCase):
    def test_scan_disabled_byte_exact_echo(self):
        guard = StreamGuard(policy_with(), holdback_bytes=64, scan_enabled=True)
        guard.scan_enabled = False
        data = sse(chunk("anything including foo"))
        capture = run_stream(guard, [(data, True)])
        self.assertEqual(capture, data)


class TestPolicyLoading(unittest.TestCase):
    def test_invalid_policy_yields_disabled(self):
        with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as handle:
            handle.write("not: [valid\n")
            path = handle.name
        try:
            policy = load_policy(path)
        finally:
            os.unlink(path)
        self.assertFalse(policy.enabled)

    def test_policy_missing_yields_disabled(self):
        policy = load_policy("/nonexistent/policy.yaml")
        self.assertFalse(policy.enabled)

    def test_ban_substrings_word_match(self):
        policy = ScanPolicy(enabled=True, substrings=[SubstringRule(value="foo")])
        matches = collect_matches("the foo word", policy)
        self.assertEqual([m.start for m in matches], [4])
        self.assertEqual(matches[0].replacement, DEFAULT_REDACT_MARKER)


if __name__ == "__main__":
    unittest.main(verbosity=2)
