#!/usr/bin/env python3
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

"""Envoy ext_proc server for streaming output guardrails (issue #2358).

With ``response_body_mode: FULL_DUPLEX_STREAMED``, ``response_body`` requests arrive as partial chunks and replies carry ``streamed_response``. :func:`load_policy` supplies the rules; :class:`StreamGuard` reassembles the SSE frames and mutates complete events.

A runtime file (``KAI_GUARD_RUNTIME``) toggles ``scan_enabled`` and ``holdback_bytes`` without a restart, which is how the experiments are driven.
"""

import asyncio
import json
import logging
import os
import time

from envoy.service.ext_proc.v3 import (
    external_processor_pb2,
    external_processor_pb2_grpc,
)
from grpc import aio
from stream_guard import Emit, StreamGuard, load_policy

logging.basicConfig(
    level=os.getenv("KAI_GUARD_LOG", "INFO"),
    format="%(levelname)s %(asctime)s %(name)s: %(message)s",
)
logger = logging.getLogger("ext_proc_stream")

DEFAULT_POLICY_PATH = "/etc/kaito/guardrails/policy.yaml"
DEFAULT_RUNTIME_PATH = "/config/stream_guard.json"


class StreamExtProcService(external_processor_pb2_grpc.ExternalProcessorServicer):
    """gRPC servicer guarding streamed response bodies."""

    def __init__(self, policy_path: str, runtime_path: str) -> None:
        self.policy_path = policy_path
        self.runtime_path = runtime_path
        self._runtime = {}
        self._runtime_mtime = -1.0

    def _runtime_config(self) -> dict:
        """Reload the per-experiment runtime file when it changes."""
        try:
            mtime = os.path.getmtime(self.runtime_path)
        except OSError:
            return self._runtime
        if mtime != self._runtime_mtime:
            try:
                with open(self.runtime_path, encoding="utf-8") as handle:
                    value = json.load(handle)
                if isinstance(value, dict):
                    self._runtime = value
                    self._runtime_mtime = mtime
                    logger.info("runtime config: %s", value)
            except (OSError, json.JSONDecodeError) as exc:
                logger.warning("runtime config not loadable: %s", exc)
        return self._runtime

    def _streamed_body_response(
        self, emit: Emit
    ) -> "external_processor_pb2.ProcessingResponse":
        response = external_processor_pb2.ProcessingResponse()
        mutation = response.response_body.response.body_mutation
        mutation.streamed_response.body = emit.body
        mutation.streamed_response.end_of_stream = emit.end_stream
        return response

    async def Process(self, request_iterator, context):
        policy = load_policy(self.policy_path)
        runtime = self._runtime_config()
        guard = StreamGuard(
            policy=policy,
            holdback_bytes=int(runtime.get("holdback_bytes", 0) or 0),
            scan_enabled=bool(runtime.get("scan_enabled", True)),
        )

        stream_calls = 0
        bytes_in = 0
        bytes_out = 0
        started = time.perf_counter()

        async for request in request_iterator:
            try:
                if request.HasField("request_headers") or request.HasField(
                    "response_headers"
                ):
                    yield external_processor_pb2.ProcessingResponse()

                elif request.HasField("response_body"):
                    body = request.response_body.body
                    end_of_stream = request.response_body.end_of_stream
                    bytes_in += len(body)
                    stream_calls += 1

                    emit = guard.consume(body, end_of_stream)
                    bytes_out += len(emit.body)
                    logger.debug(
                        "streamed chunk call=%d in=%d eos=%s emit=%d held=%d",
                        stream_calls,
                        len(body),
                        end_of_stream,
                        len(emit.body),
                        guard.held_chars,
                    )
                    yield self._streamed_body_response(emit)

                elif request.HasField("response_trailers"):
                    # Trailers, not end_of_stream, close a full-duplex stream.
                    emit = guard.flush()
                    bytes_out += len(emit.body)
                    yield self._streamed_body_response(emit)
                    yield external_processor_pb2.ProcessingResponse(
                        response_trailers=external_processor_pb2.TrailersResponse()
                    )

                else:
                    yield external_processor_pb2.ProcessingResponse()
            except Exception as exc:  # noqa: BLE001 - at the RPC boundary
                if guard.blocked:
                    logger.warning("stream already blocked, ignoring error: %s", exc)
                    yield self._streamed_body_response(
                        Emit(b"", end_stream=True, blocked=True)
                    )
                    return
                if not guard.bytes_emitted:
                    logger.error(
                        "guard error, restarting stream guard: %s", exc, exc_info=True
                    )
                    guard = StreamGuard(policy, holdback_bytes=0, scan_enabled=False)
                    yield external_processor_pb2.ProcessingResponse()
                else:
                    logger.error("mid-stream guard error: %s", exc, exc_info=True)
                    yield self._streamed_body_response(
                        Emit(b"", end_stream=True, blocked=True)
                    )
                    return

        elapsed = time.perf_counter() - started
        if not guard.finished:
            emit = guard.flush()
            bytes_out += len(emit.body)
            logger.info(
                "stream closed pre-eos bytes_in=%d bytes_out=%d calls=%d scans=%d "
                "scan_ms_total=%.2f scan_ms_avg=%.2f scan_ms_max=%.2f blocked=%s elapsed=%.3fs",
                bytes_in,
                bytes_out,
                stream_calls,
                guard.scan_calls,
                guard.scan_ms_total,
                guard.scan_ms_total / max(1, guard.scan_calls),
                guard.max_scan_ms,
                guard.blocked,
                elapsed,
            )


async def serve() -> None:
    policy_path = os.getenv("KAI_GUARD_POLICY", DEFAULT_POLICY_PATH)
    runtime_path = os.getenv("KAI_GUARD_RUNTIME", DEFAULT_RUNTIME_PATH)
    port = os.getenv("KAI_GUARD_PORT", "9000")
    logger.info(
        "streaming ext_proc server: policy=%s runtime=%s port=%s",
        policy_path,
        runtime_path,
        port,
    )

    server = aio.server()
    external_processor_pb2_grpc.add_ExternalProcessorServicer_to_server(
        StreamExtProcService(policy_path, runtime_path),
        server,
    )
    server.add_insecure_port(f"[::]:{port}")
    await server.start()
    logger.info("ext_proc server listening on :%s", port)
    await server.wait_for_termination()


if __name__ == "__main__":
    asyncio.run(serve())
