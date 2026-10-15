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

"""Minimal /health endpoint middleware for KAITO's vLLM wrapper.

Older vLLM CPU OpenAI server layouts used by KAITO's CPU image do not expose
/health, which makes Kubernetes probes fail with HTTP 404 even after the model
is ready. This middleware keeps probe compatibility by returning HTTP 200 for
/health once the FastAPI app is serving traffic.
"""

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.responses import JSONResponse


class HealthMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        if request.url.path == "/health":
            return JSONResponse(status_code=200, content={"status": "ok"})
        return await call_next(request)
