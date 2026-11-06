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

"""Tests for chaining pre-serve hooks onto vLLM's build_and_serve."""

import asyncio
import sys
from pathlib import Path
from types import ModuleType, SimpleNamespace
from unittest.mock import MagicMock, patch

# Real third-party deps of inference_api are imported up front so restoring
# sys.modules below does not unload them for later test modules.
import prometheus_client  # noqa: F401
import pytest
import yaml  # noqa: F401


class _StubBaseHTTPMiddleware:
    def __init__(self, app=None):
        self.app = app


class _StubJSONResponse:
    def __init__(self, status_code, content):
        self.status_code = status_code
        self.content = content


def _import_inference_api():
    """Import inference_api against stubbed heavy dependencies.

    sys.modules is restored afterwards so neither the stubs nor this copy of
    inference_api leak into other test modules, whatever the collection order.
    """
    starlette_base = ModuleType("starlette.middleware.base")
    starlette_base.BaseHTTPMiddleware = _StubBaseHTTPMiddleware
    starlette_responses = ModuleType("starlette.responses")
    starlette_responses.JSONResponse = _StubJSONResponse
    stubs = {
        name: MagicMock()
        for name in (
            "vllm",
            "vllm.entrypoints",
            "vllm.entrypoints.launchers",
            "vllm.entrypoints.launchers.api_server",
            "vllm.entrypoints.launchers.api_server.entry",
            "vllm.entrypoints.launchers.cli_args",
            "vllm.entrypoints.openai",
            "vllm.entrypoints.openai.models",
            "vllm.entrypoints.openai.models.protocol",
            "vllm.utils",
            "vllm.utils.argparse_utils",
            "vllm.v1",
            "vllm.v1.metrics",
            "vllm.v1.metrics.prometheus",
            "torch",
            "uvloop",
            "psutil",
            "huggingface_hub",
        )
    }
    stubs.update(
        {
            "starlette": ModuleType("starlette"),
            "starlette.middleware": ModuleType("starlette.middleware"),
            "starlette.middleware.base": starlette_base,
            "starlette.responses": starlette_responses,
        }
    )
    parent = str(Path(__file__).resolve().parent.parent)
    with patch.dict(sys.modules, stubs), patch.object(sys, "path", [parent, *sys.path]):
        sys.modules.pop("inference_api", None)
        import inference_api

        return inference_api


inference_api = _import_inference_api()


@pytest.fixture
def entry(monkeypatch):
    """A stand-in for vllm.entrypoints.launchers.api_server.entry whose
    build_and_serve has the vLLM 0.32 signature and records its calls."""
    events = []

    async def build_and_serve(
        engine_client,
        listen_address,
        sock,
        args,
        peer_loads=None,
        **uvicorn_kwargs,
    ):
        events.append(
            (
                "serve",
                engine_client,
                listen_address,
                sock,
                args,
                peer_loads,
                uvicorn_kwargs,
            )
        )
        return "shutdown-task"

    fake = SimpleNamespace(build_and_serve=build_and_serve, events=events)
    monkeypatch.setattr(inference_api, "api_server_entry", fake)
    return fake


def test_forwards_positional_peer_loads_and_kwargs(entry):
    hooked = []
    inference_api._wrap_build_and_serve(hooked.append)

    peer_loads = ([0, 1], 1)
    result = asyncio.run(
        entry.build_and_serve(
            "engine", "0.0.0.0:5000", "sock", "args", peer_loads, ssl_keyfile=None
        )
    )

    assert result == "shutdown-task"
    assert hooked == ["engine"]
    assert entry.events == [
        (
            "serve",
            "engine",
            "0.0.0.0:5000",
            "sock",
            "args",
            peer_loads,
            {"ssl_keyfile": None},
        )
    ]


def test_hook_runs_before_serving(entry):
    order = []
    original = entry.build_and_serve

    async def recording_serve(*args, **kwargs):
        order.append("serve")
        return await original(*args, **kwargs)

    entry.build_and_serve = recording_serve
    inference_api._wrap_build_and_serve(lambda client: order.append(("hook", client)))

    asyncio.run(entry.build_and_serve("engine", "addr", "sock", "args", None))

    assert order == [("hook", "engine"), "serve"]


def test_latest_registered_hook_runs_first_and_serves_once(entry):
    order = []
    inference_api._wrap_build_and_serve(lambda _: order.append("first"))
    inference_api._wrap_build_and_serve(lambda _: order.append("second"))

    asyncio.run(entry.build_and_serve("engine", "addr", "sock", "args", None))

    assert order == ["second", "first"]
    assert len(entry.events) == 1


def test_supports_call_without_peer_loads(entry):
    hooked = []
    inference_api._wrap_build_and_serve(hooked.append)

    asyncio.run(entry.build_and_serve("engine", "addr", "sock", "args", backlog=8))

    assert hooked == ["engine"]
    assert entry.events == [
        ("serve", "engine", "addr", "sock", "args", None, {"backlog": 8})
    ]
