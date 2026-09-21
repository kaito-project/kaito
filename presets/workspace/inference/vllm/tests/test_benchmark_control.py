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

"""Unit tests for the startup benchmark control middleware."""

import asyncio
import sys
import types
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock


class _StubBaseHTTPMiddleware:
    def __init__(self, app=None):
        self.app = app


class _StubJSONResponse:
    def __init__(self, status_code, content):
        self.status_code = status_code
        self.content = content


_starlette_base = types.ModuleType("starlette.middleware.base")
_starlette_base.BaseHTTPMiddleware = _StubBaseHTTPMiddleware
_starlette_responses = types.ModuleType("starlette.responses")
_starlette_responses.JSONResponse = _StubJSONResponse
_starlette_middleware = types.ModuleType("starlette.middleware")
_starlette = types.ModuleType("starlette")

for _name, _module in {
    "starlette": _starlette,
    "starlette.middleware": _starlette_middleware,
    "starlette.middleware.base": _starlette_base,
    "starlette.responses": _starlette_responses,
}.items():
    sys.modules.setdefault(_name, _module)

_PARENT = str(Path(__file__).resolve().parent.parent)
if _PARENT not in sys.path:
    sys.path.insert(0, _PARENT)

import benchmark_control  # noqa: E402


def _run(coro):
    return asyncio.run(coro)


def _make_request(path="/abort_requests", method="POST", host="127.0.0.1"):
    engine = SimpleNamespace(
        pause_generation=AsyncMock(),
        resume_generation=AsyncMock(),
    )
    request = SimpleNamespace(
        url=SimpleNamespace(path=path),
        method=method,
        client=SimpleNamespace(host=host),
        app=SimpleNamespace(state=SimpleNamespace(engine_client=engine)),
    )
    return request, engine


def test_non_control_path_passes_through():
    request, engine = _make_request(path="/health")
    call_next = AsyncMock(return_value="OK")
    middleware = benchmark_control.BenchmarkControlMiddleware(app=MagicMock())

    assert _run(middleware.dispatch(request, call_next)) == "OK"
    call_next.assert_awaited_once_with(request)
    engine.pause_generation.assert_not_awaited()


def test_abort_requests_rejects_non_loopback_client():
    request, engine = _make_request(host="10.0.0.1")
    middleware = benchmark_control.BenchmarkControlMiddleware(app=MagicMock())

    response = _run(middleware.dispatch(request, AsyncMock()))

    assert response.status_code == 403
    engine.pause_generation.assert_not_awaited()


def test_abort_requests_pauses_with_abort_then_resumes():
    request, engine = _make_request()
    middleware = benchmark_control.BenchmarkControlMiddleware(app=MagicMock())

    response = _run(middleware.dispatch(request, AsyncMock()))

    assert response.status_code == 200
    engine.pause_generation.assert_awaited_once_with(
        mode="abort",
        wait_for_inflight_requests=False,
        clear_cache=True,
    )
    engine.resume_generation.assert_awaited_once_with()


def test_abort_requests_resumes_after_pause_failure():
    request, engine = _make_request()
    engine.pause_generation.side_effect = RuntimeError("pause failed")
    middleware = benchmark_control.BenchmarkControlMiddleware(app=MagicMock())

    response = _run(middleware.dispatch(request, AsyncMock()))

    assert response.status_code == 500
    engine.resume_generation.assert_awaited_once_with()
