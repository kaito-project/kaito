#!/usr/bin/env python3
# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");

"""Integration tests for ext_proc guardrails adapter with real code execution."""

import asyncio
import json
import sys
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

# Mock gRPC imports before ext_proc_server import
class MockProcessingResponse:
    def __init__(self, **kwargs):
        self.response = kwargs.get("response")
        self.body_mutation = kwargs.get("body_mutation")

class MockBodyMutation:
    def __init__(self, body):
        self.body = body

class MockExternalProcessor:
    ProcessingResponse = MockProcessingResponse
    BodyMutation = MockBodyMutation

sys.modules["envoy"] = MagicMock()
sys.modules["envoy.service"] = MagicMock()
sys.modules["envoy.service.ext_proc"] = MagicMock()
sys.modules["envoy.service.ext_proc.v3"] = MagicMock()
sys.modules["envoy.service.ext_proc.v3"].external_processor_pb2 = MockExternalProcessor()
sys.modules["envoy.service.ext_proc.v3"].external_processor_pb2_grpc = MagicMock()

# Now import the actual ext_proc_server
from ext_proc_server import ExtProcService
from ragengine.models import ChatCompletionResponse


class TestExtProcRealExecution(unittest.TestCase):
    """Test ext_proc adapter by calling real production code."""

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

    @patch('ext_proc_server.GuardrailsReloader')
    def test_apply_guardrails_disabled_real_call(self, mock_reloader_class):
        """Test real _apply_guardrails() call with disabled guardrails."""
        mock_reloader = MagicMock()
        mock_reloader_class.return_value = mock_reloader

        mock_gr = MagicMock()
        mock_gr.enabled = False
        mock_reloader.get_current.return_value = mock_gr

        service = ExtProcService('/fake/policy.yaml')
        response_obj = self._create_response_obj("test")

        # Call real production method
        result = asyncio.run(service._apply_guardrails(response_obj))

        self.assertEqual(result, response_obj)
        mock_gr.guard_response.assert_not_called()

    @patch('ext_proc_server.GuardrailsReloader')
    def test_apply_guardrails_allow_real_call(self, mock_reloader_class):
        """Test real _apply_guardrails() call with allow action."""
        mock_reloader = MagicMock()
        mock_reloader_class.return_value = mock_reloader

        mock_gr = MagicMock()
        mock_gr.enabled = True

        response_obj = self._create_response_obj("clean")

        # Mock guard_response to return an object with model_dump()
        mock_response = MagicMock()
        mock_response.model_dump.return_value = response_obj
        mock_gr.guard_response.return_value = mock_response

        mock_reloader.get_current.return_value = mock_gr

        service = ExtProcService('/fake/policy.yaml')

        # Call real production method
        result = asyncio.run(service._apply_guardrails(response_obj))

        self.assertEqual(result, response_obj)
        mock_gr.guard_response.assert_called_once()

    @patch('ext_proc_server.GuardrailsReloader')
    def test_process_response_body_fail_closed_real_call(self, mock_reloader_class):
        """Test real _process_response_body() fail-closed (all choices blocked)."""
        mock_reloader = MagicMock()
        mock_reloader_class.return_value = mock_reloader

        mock_gr = MagicMock()
        mock_gr.enabled = True
        mock_gr.block_message = "Response blocked"
        mock_gr.guard_response.side_effect = RuntimeError("Scanner failed")

        mock_reloader.get_current.return_value = mock_gr

        service = ExtProcService('/fake/policy.yaml')

        response_obj = {
            "id": "test",
            "choices": [
                {"message": {"content": "response 1"}, "index": 0},
                {"message": {"content": "response 2"}, "index": 1},
            ],
        }

        response_body = MagicMock()
        response_body.body = json.dumps(response_obj).encode("utf-8")

        # Call real production method (_process_response_body)
        result = asyncio.run(service._process_response_body(response_body))

        # Should return a mutation (fail-closed: block all choices)
        self.assertIsNotNone(result.body_mutation)

        # Verify blocked response has all choices blocked
        mutated_obj = json.loads(result.body_mutation.body.decode("utf-8"))
        for choice in mutated_obj["choices"]:
            self.assertEqual(
                choice["message"]["content"],
                "Response blocked",
                "All choices should be blocked in fail-closed mode"
            )

        # Metadata preserved
        self.assertEqual(mutated_obj["id"], "test")


if __name__ == "__main__":
    unittest.main()
