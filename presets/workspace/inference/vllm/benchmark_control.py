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

"""Loopback-only control endpoint used by KAITO's startup benchmark."""

import logging

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse

logger = logging.getLogger(__name__)

ABORT_REQUESTS_PATH = "/abort_requests"
_LOOPBACK_HOSTS = frozenset(("127.0.0.1", "::1"))


class BenchmarkControlMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        if request.url.path != ABORT_REQUESTS_PATH:
            return await call_next(request)
        if request.method != "POST":
            return JSONResponse(
                status_code=405, content={"error": "method not allowed"}
            )

        client_host = request.client.host if request.client is not None else ""
        if client_host not in _LOOPBACK_HOSTS:
            return JSONResponse(status_code=403, content={"error": "forbidden"})

        engine = request.app.state.engine_client
        error = None
        try:
            await engine.pause_generation(
                mode="abort",
                wait_for_inflight_requests=False,
                clear_cache=True,
            )
        except Exception as exc:
            logger.exception("Failed to abort benchmark requests")
            error = exc
        finally:
            try:
                await engine.resume_generation()
            except Exception as exc:
                logger.exception("Failed to resume generation after benchmark abort")
                error = error or exc

        if error is not None:
            return JSONResponse(status_code=500, content={"error": str(error)})
        return JSONResponse(status_code=200, content={"status": "aborted"})
