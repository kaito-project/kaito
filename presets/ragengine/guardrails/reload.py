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

"""Hot-reload support for the output guardrails policy.

The reloader watches the policy file referenced by ``OUTPUT_GUARDRAILS_POLICY_PATH``
and, on change, rebuilds an :class:`OutputGuardrails` instance. The new instance
replaces the old one with a single attribute assignment so in-flight requests
either see the previous policy in full or the new policy in full -- never a
partially-mutated object.

Filesystem events come from ``watchfiles`` (inotify on Linux). To survive
ConfigMap atomic-symlink swaps that can race with single events, the watcher
uses a debounce window that doubles as a safety-net poll interval.
"""

from __future__ import annotations

import asyncio
import logging
import time
from collections.abc import Callable
from pathlib import Path
from typing import Any

from ragengine.guardrails.output_guardrails import OutputGuardrails
from ragengine.metrics.prometheus_metrics import (
    RELOAD_RESULT_FAILURE,
    RELOAD_RESULT_NOOP,
    RELOAD_RESULT_SUCCESS,
    guardrails_policy_loaded_timestamp,
    guardrails_policy_reload_total,
)

logger = logging.getLogger(__name__)


GuardrailsFactory = Callable[[], OutputGuardrails]


class GuardrailsReloader:
    """Watches the guardrails policy file and atomically swaps in updates."""

    def __init__(
        self,
        policy_path: str,
        *,
        debounce_seconds: float = 60.0,
        factory: GuardrailsFactory = OutputGuardrails.from_config,
        watcher: Callable[..., Any] | None = None,
    ) -> None:
        self._policy_path = policy_path
        self._debounce_seconds = max(0.0, debounce_seconds)
        self._factory = factory
        # Injectable for tests; default binds at start() to avoid importing
        # watchfiles when hot reload is disabled.
        self._watcher_factory = watcher
        self._current = factory()
        guardrails_policy_loaded_timestamp.set(time.time())
        self._task: asyncio.Task[None] | None = None
        self._stop_event: asyncio.Event | None = None

    @property
    def current(self) -> OutputGuardrails:
        """Return the most recently loaded :class:`OutputGuardrails` instance."""
        return self._current

    def start(self) -> None:
        """Start the background watcher task. No-op if already running."""
        if self._task is not None and not self._task.done():
            return
        if not self._policy_path:
            logger.info("output_guardrails_hot_reload_disabled reason=no_policy_path")
            return
        self._stop_event = asyncio.Event()
        self._task = asyncio.create_task(self._run(), name="guardrails-reloader")

    async def stop(self) -> None:
        """Signal the watcher to stop and wait for it to exit."""
        if self._task is None:
            return
        if self._stop_event is not None:
            self._stop_event.set()
        try:
            await self._task
        except asyncio.CancelledError:
            pass
        finally:
            self._task = None
            self._stop_event = None

    async def _run(self) -> None:
        try:
            async for _ in self._watch():
                self._reload()
        except asyncio.CancelledError:
            raise
        except Exception:
            # Keep the application up even if the watcher dies; operators see
            # the failure metric and can restart the Pod if the watcher is
            # permanently broken.
            logger.exception("output_guardrails_reloader_terminated")

    def _watch(self) -> Any:
        """Build the async iterator of filesystem-change batches."""
        if self._watcher_factory is not None:
            return self._watcher_factory(
                self._policy_path,
                stop_event=self._stop_event,
                debounce_seconds=self._debounce_seconds,
            )
        # Local import keeps watchfiles optional for unit tests of the
        # guardrails core that disable hot reload.
        from watchfiles import awatch

        # ConfigMap mounts are symlinked; watching the parent directory catches
        # the atomic ``..data`` rename that kubelet performs on update.
        watch_target = (
            str(Path(self._policy_path).parent)
            if Path(self._policy_path).parent != Path(self._policy_path)
            else self._policy_path
        )
        # ``debounce`` is in milliseconds; the same window doubles as a
        # heartbeat that re-checks the file in case an event is missed.
        return awatch(
            watch_target,
            stop_event=self._stop_event,
            debounce=int(self._debounce_seconds * 1000),
        )

    def _reload(self) -> None:
        try:
            new_instance = self._factory()
        except Exception:
            guardrails_policy_reload_total.labels(
                **{"result": RELOAD_RESULT_FAILURE}
            ).inc()
            logger.exception(
                "output_guardrails_policy_reload_failed path=%s", self._policy_path
            )
            return

        if new_instance == self._current:
            guardrails_policy_reload_total.labels(
                **{"result": RELOAD_RESULT_NOOP}
            ).inc()
            return

        self._current = new_instance
        guardrails_policy_reload_total.labels(
            **{"result": RELOAD_RESULT_SUCCESS}
        ).inc()
        guardrails_policy_loaded_timestamp.set(time.time())
        logger.info(
            "output_guardrails_policy_reloaded path=%s enabled=%s scanners=%d",
            self._policy_path,
            new_instance.enabled,
            len(new_instance.scanner_configs),
        )
