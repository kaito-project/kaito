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

"""Queue-depth rate limit guard for the KAITO vLLM preset.

Installed via vLLM's ``--middleware`` extension point. Returns HTTP 429 on
guarded inference endpoints when the waiting queue exceeds ``max_num_seqs``.
"""

import logging

from prometheus_client import Counter
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse
from vllm.v1.metrics.prometheus import get_prometheus_registry

logger = logging.getLogger(__name__)

_registry = get_prometheus_registry()

kaito_ratelimit_rejected_total = Counter(
    "kaito_ratelimit_rejected_total",
    "Requests rejected with HTTP 429 by the KAITO queue-depth rate limit guard",
    registry=_registry,
)

# Only inference endpoints are guarded — control-plane paths (/health,
# /metrics, /v1/models, …) must never 429.
GUARDED_PREFIXES = (
    "/v1/completions",
    "/v1/chat/completions",
    "/v1/embeddings",
    "/tokenize",
    "/pooling",
    "/score",
    "/rerank",
    "/v1/rerank",
)

# Set by configure() before vLLM installs the middleware. None = no-op.
_max_num_seqs: int | None = None

# In-flight guarded requests, used to hedge the stale Prometheus metric.
# Process-local. Safe for KAITO's multinode mode because Ray colocates the
# single FastAPI server on the head node — all HTTP traffic funnels through
# one process, so one counter observes every admit.
_pending: int = 0


def configure(max_num_seqs: int) -> None:
    """Set the queue-depth threshold used by the middleware."""
    global _max_num_seqs
    _max_num_seqs = int(max_num_seqs)


def _current_num_requests_waiting() -> float:
    """Read vllm:num_requests_waiting, summed across label sets.

    vLLM tags the gauge with model_name; this process serves one model, so
    summing is both correct and safe.
    """
    for family in _registry.collect():
        if family.name != "vllm:num_requests_waiting":
            continue
        return sum(
            s.value for s in family.samples if s.name == "vllm:num_requests_waiting"
        )
    logger.error("vllm:num_requests_waiting metric not found in registry")
    return 0.0


class RateLimitMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        global _pending
        threshold = _max_num_seqs
        if threshold is None:
            return await call_next(request)
        if not any(request.url.path.startswith(p) for p in GUARDED_PREFIXES):
            return await call_next(request)

        # The Prometheus gauge lags admits: under a burst, N concurrent
        # invocations would all read the same stale value and pass. Hedge
        # with _pending — admits beyond batch capacity must be queued
        # waiting, so max(0, _pending - threshold) is a local estimate that
        # leads the metric. Take max so we never underestimate. No await
        # between read and increment — asyncio cannot interleave.
        waiting = _current_num_requests_waiting()
        local_waiting = max(0, _pending - threshold)
        effective = max(waiting, local_waiting)
        if effective > threshold:
            kaito_ratelimit_rejected_total.inc()
            logger.warning(
                "Rate limit: rejecting, waiting=%s local=%d effective=%s > threshold=%d",
                waiting,
                local_waiting,
                effective,
                threshold,
            )
            return JSONResponse(
                status_code=429,
                content={
                    "error": {
                        "message": (
                            f"Too many requests: waiting queue "
                            f"({int(effective)}) exceeds max-num-seqs "
                            f"({threshold})."
                        ),
                        "type": "requests",
                        "param": None,
                        "code": "rate_limit_exceeded",
                    }
                },
            )
        _pending += 1
        try:
            return await call_next(request)
        finally:
            _pending -= 1
