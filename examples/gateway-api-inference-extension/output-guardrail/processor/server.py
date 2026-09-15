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

    Intercepts response_body and appends test marker to prove mutation works.
    """

    async def Process(
        self,
        request_iterator: AsyncIterator[processor_pb2.ProcessingRequest],
        context: grpc.aio.ServicerContext,
    ) -> AsyncIterator[processor_pb2.ProcessingResponse]:
        """
        Handle bidirectional stream of ProcessingRequest/ProcessingResponse.

        For each ProcessingRequest with response_body, mutate and return.
        Only response_body is expected (headers/trailers are SKIPped in config).

        Args:
            request_iterator: Stream of ProcessingRequest from Envoy
            context: gRPC context

        Yields:
            ProcessingResponse with BodyResponse containing mutation
        """
        async for request in request_iterator:
            if not request.HasField("response_body"):
                continue

            body_bytes = request.response_body.body
            modified_bytes = body_bytes + b" [GATEWAY_TEST]"

            # Envoy ext_proc protocol requires:
            # ProcessingResponse.response_body.response.body_mutation
            yield processor_pb2.ProcessingResponse(
                response_body=processor_pb2.BodyResponse(
                    response=processor_pb2.CommonResponse(
                        body_mutation=processor_pb2.BodyMutation(
                            body=modified_bytes
                        )
                    )
                )
            )


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
