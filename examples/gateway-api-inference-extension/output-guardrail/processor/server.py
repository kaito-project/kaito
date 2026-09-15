"""
Minimal ext_proc gRPC server for Istio Gateway LLM Guard PoC.

This server receives response_body from Envoy and performs simple string mutation.
First version: just appends [GATEWAY_TEST] to prove the mutation chain works.
"""

import asyncio
import logging
from typing import AsyncIterator

import grpc
from google.protobuf import empty_pb2

# Envoy ext_proc protobuf definitions
from envoy.service.ext_proc.v3 import external_processor_pb2 as processor_pb2
from envoy.service.ext_proc.v3 import external_processor_pb2_grpc as processor_grpc

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class GatewayOutputProcessor(processor_grpc.ExternalProcessorServicer):
    """
    Minimal Envoy ext_proc service for output guardrails PoC.

    Implements the ExternalProcessor service which Envoy calls via bidirectional gRPC stream.
    For each ProcessingRequest, returns a ProcessingResponse with optional mutations.
    """

    async def Process(
        self,
        request_iterator: AsyncIterator[processor_pb2.ProcessingRequest],
        context: grpc.aio.ServicerContext,
    ) -> AsyncIterator[processor_pb2.ProcessingResponse]:
        """
        Handle bidirectional stream of ProcessingRequest/ProcessingResponse.

        Args:
            request_iterator: Stream of ProcessingRequest from Envoy
            context: gRPC context

        Yields:
            ProcessingResponse to return to Envoy
        """
        try:
            async for request in request_iterator:
                # Only process response_body messages
                if request.HasField("response_body"):
                    response = self._process_response_body(request.response_body)
                else:
                    # For other message types (headers, trailers, etc), just pass through
                    response = processor_pb2.ProcessingResponse()

                yield response

        except Exception as e:
            logger.error(f"Error in Process: {e}", exc_info=True)
            # Return empty response on error (Envoy will use original response)
            yield processor_pb2.ProcessingResponse()

    def _process_response_body(
        self, response_body: processor_pb2.ResponseBody
    ) -> processor_pb2.ProcessingResponse:
        """
        Process response body by appending test string.

        This is the minimal proof-of-concept:
        - Take the raw response body bytes
        - Append " [GATEWAY_TEST]"
        - Return BodyMutation to Envoy

        Args:
            response_body: The response body from upstream

        Returns:
            ProcessingResponse with optional body mutation
        """
        try:
            body_bytes = response_body.body
            # Simple transformation: append marker
            modified_bytes = body_bytes + b" [GATEWAY_TEST]"

            # Return mutation response
            return processor_pb2.ProcessingResponse(
                body_mutation=processor_pb2.BodyMutation(body=modified_bytes)
            )

        except Exception as e:
            logger.error(f"Error processing response body: {e}", exc_info=True)
            # Return empty response (fail-open)
            return processor_pb2.ProcessingResponse()


async def serve():
    """Start the gRPC server on port 9000."""
    # Create server
    server = grpc.aio.server()

    # Register service
    processor_grpc.add_ExternalProcessorServicer_to_server(
        GatewayOutputProcessor(), server
    )

    # Listen on all interfaces, port 9000
    server.add_insecure_port("[::]:9000")

    logger.info("Starting Gateway Output Processor on port 9000")
    await server.start()

    # Keep server running
    try:
        await server.wait_for_termination()
    except KeyboardInterrupt:
        logger.info("Shutting down...")
        await server.stop(0)


if __name__ == "__main__":
    asyncio.run(serve())
