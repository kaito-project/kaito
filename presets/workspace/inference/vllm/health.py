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

"""Compatibility /health middleware for KAITO's vLLM wrapper.

Older vLLM CPU OpenAI server layouts used by KAITO's CPU image do not expose
/health, which makes Kubernetes probes fail with HTTP 404 even after the model
is ready. Preserve native /health responses when they exist, and only provide a
CPU-only fallback when the route is missing *and* the engine health check passes.
"""

import inspect
import os

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse


async def _check_engine_health(request):
    engine = getattr(getattr(request.app, "state", None), "engine_client", None)
    if engine is None:
        return None

    check_health = getattr(engine, "check_health", None)
    if check_health is None:
        return None

    result = check_health()
    if inspect.isawaitable(result):
        await result
    return True


class HealthMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        response = await call_next(request)
        if request.url.path != "/health":
            return response

        # Preserve native vLLM health responses (including unhealthy states).
        if response.status_code != 404:
            return response

        # Only older CPU OpenAI layouts need a synthetic /health route.
        if os.environ.get("VLLM_TARGET_DEVICE", "").strip().lower() != "cpu":
            return response

        try:
            healthy = await _check_engine_health(request)
        except Exception as exc:
            return JSONResponse(status_code=503, content={"error": str(exc)})

        if healthy:
            return JSONResponse(status_code=200, content={"status": "ok"})
        return response
