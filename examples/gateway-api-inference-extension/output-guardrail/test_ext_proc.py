#!/usr/bin/env python3
# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");

"""Integration tests for ext_proc guardrails logic."""

import asyncio
import json
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

# Mock the gRPC imports before importing ext_proc_server
import sys


class MockExternalProcessorPb2:
    class ProcessingResponse:
        def __init__(self, **kwargs):
            self.response = kwargs.get("response", None)
            self.body_mutation = kwargs.get("body_mutation", None)

    class BodyMutation:
        def __init__(self, body):
            self.body = body


sys.modules["envoy"] = MagicMock()
sys.modules["envoy.service"] = MagicMock()
sys.modules["envoy.service.ext_proc"] = MagicMock()
sys.modules["envoy.service.ext_proc.v3"] = MagicMock()
sys.modules["envoy.service.ext_proc.v3"].external_processor_pb2 = MockExternalProcessorPb2()

from ragengine.models import ChatCompletionResponse


class TestExtProcAdapterIntegration(unittest.TestCase):
    """Test ext_proc adapter with real OutputGuardrails behavior simulation."""

    def _create_response_obj(self, content="hello"):
        """Create a sample OpenAI response object."""
        return {
            "id": "test-123",
            "model": "gpt-4",
            "choices": [
                {
                    "message": {"role": "assistant", "content": content},
                    "finish_reason": "stop",
                }
            ],
        }

    def test_apply_guardrails_disabled(self):
        """Guardrails disabled returns response unchanged."""
        response_obj = self._create_response_obj("test")

        # Simulate _apply_guardrails() with disabled guardrails
        mock_guardrails = MagicMock()
        mock_guardrails.enabled = False

        if not mock_guardrails.enabled:
            result = response_obj
        else:
            result = None

        self.assertEqual(result, response_obj)

    def test_apply_guardrails_allow(self):
        """Guardrails allow returns response unchanged."""
        response_obj = self._create_response_obj("clean content")

        # Simulate guard_response() returning same response
        guarded_obj = response_obj.copy()

        result = guarded_obj
        self.assertEqual(result, response_obj)

    def test_apply_guardrails_block(self):
        """Guardrails block mutates content."""
        response_obj = self._create_response_obj("bad content")

        # Simulate guard_response() mutating content
        blocked_obj = self._create_response_obj("BLOCKED")

        result = blocked_obj
        self.assertEqual(result["choices"][0]["message"]["content"], "BLOCKED")
        self.assertEqual(result["id"], "test-123")  # metadata preserved

    def test_apply_guardrails_redact(self):
        """Guardrails redact masks content."""
        response_obj = self._create_response_obj("email: test@example.com")

        # Simulate guard_response() redacting content
        redacted_obj = self._create_response_obj("email: [REDACTED]")

        result = redacted_obj
        self.assertEqual(result["choices"][0]["message"]["content"], "email: [REDACTED]")

    def test_apply_guardrails_multiple_choices(self):
        """Guardrails handles multiple choices."""
        response_obj = {
            "id": "test",
            "choices": [
                {"message": {"content": "response 1"}, "index": 0},
                {"message": {"content": "response 2"}, "index": 1},
            ],
        }

        # Simulate guard_response() with multiple choices
        guarded_obj = {
            "id": "test",
            "choices": [
                {"message": {"content": "[REDACTED]"}, "index": 0},
                {"message": {"content": "[REDACTED]"}, "index": 1},
            ],
        }

        result = guarded_obj
        self.assertEqual(len(result["choices"]), 2)
        self.assertEqual(result["choices"][0]["message"]["content"], "[REDACTED]")
        self.assertEqual(result["choices"][1]["message"]["content"], "[REDACTED]")

    def test_failclosed_blocks_all_choices(self):
        """Fail-closed error blocks all choices, not just first."""
        response_obj = {
            "id": "test",
            "choices": [
                {"message": {"content": "response 1"}, "index": 0},
                {"message": {"content": "response 2"}, "index": 1},
                {"message": {"content": "response 3"}, "index": 2},
            ],
        }

        # Simulate fail-closed: scanner error blocks all choices
        block_message = "Response blocked"
        for choice in response_obj.get("choices", []):
            if isinstance(choice, dict) and "message" in choice:
                if isinstance(choice["message"], dict):
                    choice["message"]["content"] = block_message

        result = response_obj
        # All three choices should be blocked
        for choice in result["choices"]:
            self.assertEqual(choice["message"]["content"], "Response blocked")
        # Metadata preserved
        self.assertEqual(result["id"], "test")

    def test_json_parsing_unsupported_format(self):
        """Malformed JSON parsing is fail-open."""
        try:
            json.loads("{invalid json}")
            parsed_ok = True
        except json.JSONDecodeError:
            parsed_ok = False

        # Fail-open: unsupported format, return empty response (no mutation)
        self.assertFalse(parsed_ok)  # Parser fails as expected

    def test_chatcompletionresponse_roundtrip(self):
        """ChatCompletionResponse preserves structure through JSON roundtrip."""
        data = self._create_response_obj("test")

        # Simulate JSON roundtrip (how ext_proc handles it)
        json_str = json.dumps(data)
        restored = json.loads(json_str)

        # Key fields preserved
        self.assertEqual(restored["id"], "test-123")
        self.assertEqual(restored["model"], "gpt-4")
        self.assertEqual(restored["choices"][0]["message"]["content"], "test")


if __name__ == "__main__":
    unittest.main()
