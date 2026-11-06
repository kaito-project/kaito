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
from types import SimpleNamespace

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import inference_api  # noqa: E402


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
