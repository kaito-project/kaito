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

- Watch policy file changes.
- Atomically swap the whole OutputGuardrails instance.
- Watch the parent directory and debounce for ConfigMap symlink updates.
"""

from __future__ import annotations

import asyncio
import logging
import threading
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
        debounce_seconds: float = 1.0,
        factory: GuardrailsFactory = OutputGuardrails.from_config,
        watcher: Callable[..., Any] | None = None,
    ) -> None:
        self._policy_path = policy_path
        self._debounce_seconds = max(0.0, debounce_seconds)
        self._factory = factory
        # Tests can inject a watcher; otherwise _watch() falls back to watchfiles.
        self._watcher_factory = watcher
        self._current_lock = threading.Lock()
        self._current = factory()
        guardrails_policy_loaded_timestamp.set(time.time())
        self._task: asyncio.Task[None] | None = None
        self._stop_event: asyncio.Event | None = None

    def get_current(self) -> OutputGuardrails:
        """Return the most recently loaded :class:`OutputGuardrails` instance."""
        with self._current_lock:
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
            # Log watcher failures and keep the app running.
            logger.exception("output_guardrails_reloader_terminated")

    def _watch(self) -> Any:
        if self._watcher_factory is not None:
            return self._watcher_factory(
                self._policy_path,
                stop_event=self._stop_event,
                debounce_seconds=self._debounce_seconds,
            )
        from watchfiles import awatch

        # Watch the parent directory because ConfigMap updates swap symlinks.
        watch_target = (
            str(Path(self._policy_path).parent)
            if Path(self._policy_path).parent != Path(self._policy_path)
            else self._policy_path
        )
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

        with self._current_lock:
            if new_instance == self._current:
                guardrails_policy_reload_total.labels(
                    **{"result": RELOAD_RESULT_NOOP}
                ).inc()
                return

            self._current = new_instance

        guardrails_policy_reload_total.labels(**{"result": RELOAD_RESULT_SUCCESS}).inc()
        guardrails_policy_loaded_timestamp.set(time.time())
        logger.info(
            "output_guardrails_policy_reloaded path=%s enabled=%s scanners=%d",
            self._policy_path,
            new_instance.enabled,
            len(new_instance.scanner_configs),
        )
