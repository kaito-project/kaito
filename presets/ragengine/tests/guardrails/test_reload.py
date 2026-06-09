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

import asyncio
import os
import sys
import threading
from dataclasses import replace
from hashlib import sha256

import pytest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "../../..")))

from ragengine.guardrails.output_guardrails import OutputGuardrails
from ragengine.guardrails.reload import GuardrailsReloader
from ragengine.metrics.prometheus_metrics import guardrails_active_policy


def _factory(instances):
    """Return a factory that yields instances in order, repeating the last one."""
    iterator = iter(instances)
    last: list = [None]

    def factory():
        try:
            value = next(iterator)
        except StopIteration:
            return last[0]
        last[0] = value
        return value

    return factory


def _disabled() -> OutputGuardrails:
    return OutputGuardrails(enabled=False)


def _enabled(block_message: str = "blocked") -> OutputGuardrails:
    return OutputGuardrails(enabled=True, block_message=block_message)


def _with_policy_meta(
    guardrails: OutputGuardrails, *, policy_path: str, content: str, scanners: int = 0
) -> OutputGuardrails:
    return replace(
        guardrails,
        policy_path=policy_path,
        policy_hash=sha256(content.encode("utf-8")).hexdigest(),
        scanner_configs=tuple(object() for _ in range(scanners)),
    )


def _info_labels(metric, sample_name: str) -> dict[str, str]:
    for collected in metric.collect():
        for sample in collected.samples:
            if sample.name == sample_name:
                return sample.labels
    raise AssertionError(f"sample {sample_name!r} not found")


def test_initial_load_uses_factory_once():
    initial = _with_policy_meta(
        _disabled(),
        policy_path="/tmp/does-not-matter",
        content="enabled: false\n",
    )
    reloader = GuardrailsReloader(
        policy_path="/tmp/does-not-matter",
        debounce_seconds=0,
        factory=_factory([initial]),
    )
    assert reloader.get_current() is initial
    assert reloader.current is initial
    labels = _info_labels(guardrails_active_policy, "guardrails_active_policy_info")
    assert labels["path"] == "/tmp/does-not-matter"
    assert labels["sha256"] == initial.policy_hash


def test_start_is_noop_when_policy_path_is_empty():
    reloader = GuardrailsReloader(
        policy_path="",
        debounce_seconds=0,
        factory=_factory([_disabled()]),
    )

    async def run():
        reloader.start()
        await reloader.stop()

    asyncio.run(run())
    assert reloader._task is None


def test_default_debounce_is_short_for_runtime_reload():
    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        factory=_factory([_disabled()]),
    )

    assert reloader._debounce_seconds == 1.0


def test_reload_swaps_in_new_instance_on_change():
    first = _with_policy_meta(
        _enabled("first"),
        policy_path="/tmp/policy.yaml",
        content="blockMessage: first\n",
        scanners=1,
    )
    second = _with_policy_meta(
        _enabled("second"),
        policy_path="/tmp/policy.yaml",
        content="blockMessage: second\n",
        scanners=2,
    )
    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        debounce_seconds=0,
        factory=_factory([first, second]),
    )
    assert reloader.get_current() is first

    reloader._reload()

    assert reloader.get_current() is second
    labels = _info_labels(guardrails_active_policy, "guardrails_active_policy_info")
    assert labels["path"] == "/tmp/policy.yaml"
    assert labels["sha256"] == second.policy_hash
    assert labels["scanner_count"] == "2"


def test_reload_keeps_current_when_factory_raises(caplog):
    first = _with_policy_meta(
        _enabled("first"),
        policy_path="/tmp/policy.yaml",
        content="blockMessage: first\n",
    )
    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        debounce_seconds=0,
        factory=_factory([first]),
    )

    def boom():
        raise RuntimeError("policy load broke")

    reloader._factory = boom

    with caplog.at_level("ERROR"):
        reloader._reload()

    assert reloader.get_current() is first
    assert "fallback_action=keep_current" in caplog.text
    labels = _info_labels(guardrails_active_policy, "guardrails_active_policy_info")
    assert labels["sha256"] == first.policy_hash


def test_reload_noop_when_policy_unchanged(caplog):
    first = _with_policy_meta(
        _enabled("same"),
        policy_path="/tmp/policy.yaml",
        content="blockMessage: same\n",
    )
    duplicate = _with_policy_meta(
        _enabled("same"),
        policy_path="/tmp/policy.yaml",
        content="blockMessage: same\n",
    )
    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        debounce_seconds=0,
        factory=_factory([first, duplicate]),
    )

    with caplog.at_level("INFO"):
        reloader._reload()

    assert reloader.get_current() is first
    assert "output_guardrails_reload_noop" in caplog.text
    assert first.policy_hash in caplog.text


def test_get_current_returns_snapshot_while_reload_builds_new_instance():
    first = _enabled("v1")
    second = _enabled("v2")
    reload_started = threading.Event()
    allow_reload_to_finish = threading.Event()
    state = {"calls": 0}

    def factory():
        state["calls"] += 1
        if state["calls"] == 1:
            return first

        reload_started.set()
        assert allow_reload_to_finish.wait(timeout=1)
        return second

    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        debounce_seconds=0,
        factory=factory,
    )

    reload_thread = threading.Thread(target=reloader._reload)
    reload_thread.start()

    assert reload_started.wait(timeout=1)
    snapshot = reloader.get_current()
    assert snapshot is first

    allow_reload_to_finish.set()
    reload_thread.join(timeout=1)

    assert reloader.get_current() is second
    assert snapshot is first


def test_watcher_drives_reload_on_event():
    first = _enabled("v1")
    second = _enabled("v2")
    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        debounce_seconds=0,
        factory=_factory([first, second]),
    )

    async def fake_watch(*_args, **_kwargs):
        yield {("created", "/tmp/policy.yaml")}

    reloader._watcher_factory = fake_watch

    async def run():
        reloader.start()
        for _ in range(20):
            if reloader.get_current() is second:
                break
            await asyncio.sleep(0.01)
        await reloader.stop()

    asyncio.run(run())

    assert reloader.get_current() is second


def test_watcher_failure_is_swallowed():
    first = _enabled("v1")
    reloader = GuardrailsReloader(
        policy_path="/tmp/policy.yaml",
        debounce_seconds=0,
        factory=_factory([first]),
    )

    async def boom_watch(*_args, **_kwargs):
        raise RuntimeError("inotify exploded")
        yield  # pragma: no cover - generator marker

    reloader._watcher_factory = boom_watch

    async def run():
        reloader.start()
        await asyncio.sleep(0.05)
        await reloader.stop()

    asyncio.run(run())
    assert reloader.get_current() is first


@pytest.mark.parametrize("debounce_seconds", [-1.0, 0.0, 30.0])
def test_debounce_seconds_is_clamped_non_negative(debounce_seconds):
    reloader = GuardrailsReloader(
        policy_path="/tmp/x",
        debounce_seconds=debounce_seconds,
        factory=_factory([_disabled()]),
    )
    assert reloader._debounce_seconds >= 0
