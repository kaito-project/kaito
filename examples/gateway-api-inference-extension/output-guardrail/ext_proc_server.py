#!/usr/bin/env python3
# Copyright (c) KAITO authors.
# Licensed under the Apache License, Version 2.0 (the "License");

import asyncio
import json
import logging
import os

from grpc import aio
from envoy.service.ext_proc.v3 import external_processor_pb2, external_processor_pb2_grpc

# Import KAITO guardrails and models
from ragengine.guardrails import GuardrailsReloader
from ragengine.models import ChatCompletionResponse

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class ExtProcService(external_processor_pb2_grpc.ExternalProcessorServicer):
    """ext_proc service that applies response guardrails."""

    def __init__(self, policy_path: str):
        self.guardrails_reloader = GuardrailsReloader(policy_path=policy_path)
        self.guardrails_reloader.start()
        logger.info("GuardrailsReloader started with policy: %s", policy_path)

    async def Process(self, request_iterator, context):
        """Bidirectional streaming RPC for ext_proc."""
        async for request in request_iterator:
            try:
                # Only process response_body
                if request.HasField("response_body"):
                    response = await self._process_response_body(request.response_body)
                else:
                    # Pass through other fields
                    response = external_processor_pb2.ProcessingResponse()

                yield response
            except Exception as e:
                logger.error("Error processing request: %s", e, exc_info=True)
                # fail-open: return empty response
                yield external_processor_pb2.ProcessingResponse()

    async def _process_response_body(self, response_body_msg):
        """Process response body with guardrails."""
        body_bytes = response_body_msg.body
        logger.info("Response body received, size=%d", len(body_bytes))

        try:
            body_str = body_bytes.decode("utf-8")
            response_obj = json.loads(body_str)
        except (UnicodeDecodeError, json.JSONDecodeError) as e:
            logger.warning("Failed to parse response as JSON (unsupported format): %s", e)
            # fail-open: unsupported response format, pass through unchanged
            return external_processor_pb2.ProcessingResponse()

        # Apply guardrails
        try:
            modified = await self._apply_guardrails(response_obj)
        except Exception as e:
            logger.error("Guardrails scanner failed (fail-closed): %s", e, exc_info=True)
            # fail-closed: OutputGuardrails policy error → block response
            # Return block response while preserving original structure
            try:
                if "choices" in response_obj and response_obj["choices"]:
                    # Use guardial's block message if available
                    guardrails = self.guardrails_reloader.get_current()
                    block_msg = guardrails.block_message if guardrails else "Response blocked"
                    # Update first choice's content to block message
                    response_obj["choices"][0]["message"]["content"] = block_msg
                    modified = response_obj
                else:
                    # Minimal fallback for malformed choices
                    return external_processor_pb2.ProcessingResponse()
            except Exception as fallback_e:
                logger.error("Error constructing block response: %s", fallback_e)
                return external_processor_pb2.ProcessingResponse()

        # Re-serialize
        try:
            modified_bytes = json.dumps(modified, ensure_ascii=False).encode("utf-8")
        except Exception as e:
            logger.error("Failed to serialize response: %s", e)
            return external_processor_pb2.ProcessingResponse()

        # Return mutation
        if modified_bytes != body_bytes:
            logger.info(
                "Response mutated: %d bytes → %d bytes",
                len(body_bytes),
                len(modified_bytes),
            )

        return external_processor_pb2.ProcessingResponse(
            body_mutation=external_processor_pb2.BodyMutation(body=modified_bytes)
        )

    async def _apply_guardrails(self, response_obj: dict) -> dict:
        """Apply guardrails to OpenAI response using existing OutputGuardrails."""
        guardrails = self.guardrails_reloader.get_current()
        if not guardrails or not guardrails.enabled:
            logger.info("Guardrails disabled")
            return response_obj

        try:
            # Convert dict to ChatCompletionResponse (expected by guard_response)
            response = ChatCompletionResponse(**response_obj)

            # Call existing OutputGuardrails: guard_response() handles all scanners internally
            guarded = guardrails.guard_response(
                response,
                request={},  # Response-only mode: no request context
            )

            # Convert back to dict for JSON serialization
            return guarded.model_dump(mode="python")

        except Exception as e:
            logger.error("Guardrails processing error: %s", e, exc_info=True)
            # Let guardrails' own fail-closed behavior apply
            # Don't create synthetic block here; let OutputGuardrails policy decide
            raise


async def serve():
    """Start ext_proc gRPC server."""
    policy_path = os.getenv("OUTPUT_GUARDRAILS_POLICY_PATH", "/etc/kaito/guardrails/policy.yaml")
    logger.info("Loading guardrails from: %s", policy_path)

    server = aio.server()
    external_processor_pb2_grpc.add_ExternalProcessorServicer_to_server(
        ExtProcService(policy_path),
        server,
    )
    server.add_insecure_port("[::]:9000")

    await server.start()
    logger.info("ext_proc server started on :9000")
    await server.wait_for_termination()


if __name__ == "__main__":
    asyncio.run(serve())
