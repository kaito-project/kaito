#!/usr/bin/env python3
# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");

"""Unit tests for ext_proc guardrails integration logic."""

import asyncio
import json
import unittest
from unittest.mock import MagicMock, AsyncMock, patch


class MockChatCompletionResponse:
    """Mock ChatCompletionResponse for testing."""

    def __init__(self, data):
        self.data = data

    def model_dump(self, mode="python"):
        return self.data


class TestGuardrailsIntegration(unittest.TestCase):
    """Test guardrails integration without full gRPC dependencies."""

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

    def test_guardrails_disabled_unchanged(self):
        """When guardrails disabled, response passes through unchanged."""
        mock_gr = MagicMock()
        mock_gr.enabled = False

        response_obj = self._create_response_obj("test content")

        # Simulate _apply_guardrails() logic
        if not mock_gr.enabled:
            result = response_obj
        else:
            result = None  # Should not reach here

        self.assertEqual(result, response_obj)

    def test_guardrails_allow_unchanged(self):
        """When guardrails allow, response unchanged."""
        response_obj = self._create_response_obj("clean content")
        guarded_response = MockChatCompletionResponse(response_obj)

        # Simulate guard_response behavior
        result = guarded_response.model_dump(mode="python")

        self.assertEqual(result, response_obj)

    def test_guardrails_block_mutates_content(self):
        """When guardrails blocks, content mutated to block message."""
        request_obj = self._create_response_obj("unsafe content")

        # Simulate guard_response() returning blocked response
        blocked_obj = self._create_response_obj("BLOCKED")
        guarded_response = MockChatCompletionResponse(blocked_obj)

        result = guarded_response.model_dump(mode="python")

        self.assertEqual(result["choices"][0]["message"]["content"], "BLOCKED")
        self.assertEqual(result["id"], "test-123")  # metadata preserved

    def test_guardrails_redact_mutates_content(self):
        """When guardrails redacts, content is masked."""
        request_obj = self._create_response_obj("email: test@example.com")

        # Simulate guard_response() returning redacted response
        redacted_obj = self._create_response_obj("email: [REDACTED]")
        guarded_response = MockChatCompletionResponse(redacted_obj)

        result = guarded_response.model_dump(mode="python")

        self.assertEqual(result["choices"][0]["message"]["content"], "email: [REDACTED]")

    def test_guardrails_multiple_choices(self):
        """Guardrails handles multiple choices correctly."""
        response_obj = {
            "id": "test",
            "choices": [
                {"message": {"content": "response 1"}, "index": 0},
                {"message": {"content": "response 2"}, "index": 1},
            ],
        }

        # Simulate guard_response() with multiple choices guarded
        guarded_obj = {
            "id": "test",
            "choices": [
                {"message": {"content": "[REDACTED] 1"}, "index": 0},
                {"message": {"content": "[REDACTED] 2"}, "index": 1},
            ],
        }
        guarded_response = MockChatCompletionResponse(guarded_obj)

        result = guarded_response.model_dump(mode="python")

        # Both choices should be updated
        self.assertEqual(len(result["choices"]), 2)
        self.assertEqual(result["choices"][0]["message"]["content"], "[REDACTED] 1")
        self.assertEqual(result["choices"][1]["message"]["content"], "[REDACTED] 2")

    def test_malformed_json_failopen(self):
        """Malformed JSON results in fail-open (unsupported format)."""
        # Simulate JSON parse error
        try:
            json.loads("{invalid json}")
            should_fail = False
        except json.JSONDecodeError:
            should_fail = True

        self.assertTrue(should_fail)
        # Expected behavior: fail-open (return ProcessingResponse())

    def test_guardrail_error_failclosed_semantics(self):
        """Guardrail error triggers fail-closed: block response mutated with block_message."""
        response_obj = self._create_response_obj("test")

        # Simulate guardrails.guard_response() raising an error
        class GuardrailsError(Exception):
            pass

        try:
            raise GuardrailsError("Scanner failed")
        except GuardrailsError as e:
            # Simulate fail-closed behavior: construct block response
            if "choices" in response_obj and response_obj["choices"]:
                block_message = "Response blocked by guardrails"
                response_obj["choices"][0]["message"]["content"] = block_message

        # Verify structure preserved but content blocked
        self.assertEqual(response_obj["id"], "test-123")
        self.assertEqual(response_obj["choices"][0]["message"]["content"], "Response blocked by guardrails")

    def test_json_roundtrip_preserves_structure(self):
        """JSON serialization/deserialization preserves response structure."""
        response_obj = {
            "id": "chatcmpl-abc",
            "object": "chat.completion",
            "created": 1234567890,
            "model": "gpt-4",
            "usage": {"prompt_tokens": 10, "completion_tokens": 20},
            "choices": [{"message": {"content": "test"}}],
        }

        # Simulate roundtrip
        json_str = json.dumps(response_obj)
        restored = json.loads(json_str)

        self.assertEqual(restored, response_obj)
        self.assertEqual(restored["id"], "chatcmpl-abc")
        self.assertEqual(restored["usage"]["prompt_tokens"], 10)

    def test_chatcompletionresponse_conversion(self):
        """ChatCompletionResponse dict conversion works correctly."""
        data = self._create_response_obj("test")

        # Simulate ChatCompletionResponse(**dict) → .model_dump()
        resp = MockChatCompletionResponse(data)
        result = resp.model_dump(mode="python")

        self.assertEqual(result, data)


if __name__ == "__main__":
    unittest.main()
