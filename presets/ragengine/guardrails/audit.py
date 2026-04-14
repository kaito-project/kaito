import asyncio
from datetime import UTC, datetime
import hashlib
import json
import logging
from pathlib import Path
import time
from typing import Any, Protocol
import uuid

import httpx
from ragengine import config
from ragengine.metrics.prometheus_metrics import (
    STATUS_SUCCESS,
    rag_output_guardrails_audit_backpressure_total,
    rag_output_guardrails_audit_cancelled_deliveries_total,
    rag_output_guardrails_audit_dropped_deliveries_total,
    rag_output_guardrails_audit_in_flight_background_tasks,
    rag_output_guardrails_audit_shutdown_drain_latency,
    rag_output_guardrails_audit_shutdown_drain_total,
)
from pydantic import BaseModel, Field


logger = logging.getLogger(__name__)


class GuardrailScannerResult(BaseModel):
    scanner: str
    valid: bool
    score: float | None = None


class GuardrailAuditEvent(BaseModel):
    event_id: str = Field(default_factory=lambda: uuid.uuid4().hex)
    stream_id: str | None = None
    sequence: int | None = None
    sequence_total: int | None = None
    checksum_algorithm: str | None = None
    end_of_stream_checksum: str | None = None
    event_type: str = "ragengine.output_guardrails"
    event_version: str = "v1alpha1"
    created_at: str = Field(
        default_factory=lambda: datetime.now(UTC).isoformat().replace("+00:00", "Z")
    )
    request_id: str | None = None
    trace_id: str | None = None
    response_id: str | None = None
    response_mode: str
    model: str | None = None
    index_name: str | None = None
    has_tools: bool = False
    choice_index: int
    stage: str = "final"
    chunk_index: int | None = None
    observed_chars: int | None = None
    emitted_chars: int | None = None
    triggered: bool
    action: str
    fail_open: bool = False
    scanner_results: list[GuardrailScannerResult] = Field(default_factory=list)
    error: str | None = None


class GuardrailAuditReport(BaseModel):
    report_id: str = Field(default_factory=lambda: uuid.uuid4().hex)
    stream_id: str | None = None
    last_sequence: int | None = None
    sequence_total: int | None = None
    checksum_algorithm: str | None = None
    end_of_stream_checksum: str | None = None
    event_type: str = "ragengine.output_guardrails.report"
    event_version: str = "v1alpha1"
    created_at: str = Field(
        default_factory=lambda: datetime.now(UTC).isoformat().replace("+00:00", "Z")
    )
    request_id: str | None = None
    trace_id: str | None = None
    response_id: str | None = None
    model: str | None = None
    index_name: str | None = None
    has_tools: bool = False
    response_mode: str
    elapsed_ms: float
    events: list[GuardrailAuditEvent] = Field(default_factory=list)


class GuardrailAuditSink(Protocol):
    def emit_event(self, event: GuardrailAuditEvent) -> None: ...

    def emit_report(self, report: GuardrailAuditReport) -> None: ...


class LogGuardrailAuditSink:
    is_blocking = False

    def emit_event(self, event: GuardrailAuditEvent) -> None:
        logger.info("output_guardrails_audit_event %s", event.model_dump_json())

    def emit_report(self, report: GuardrailAuditReport) -> None:
        logger.info("output_guardrails_audit_report %s", report.model_dump_json())


class FileGuardrailAuditSink:
    is_blocking = False

    def __init__(self, file_path: str):
        if not file_path:
            raise ValueError(
                "OUTPUT_GUARDRAILS_AUDIT_FILE_PATH must be set for the file audit sink."
            )
        self.file_path = Path(file_path)
        self.file_path.parent.mkdir(parents=True, exist_ok=True)

    def emit_event(self, event: GuardrailAuditEvent) -> None:
        with self.file_path.open("a", encoding="utf-8") as audit_file:
            audit_file.write(event.model_dump_json())
            audit_file.write("\n")

    def emit_report(self, report: GuardrailAuditReport) -> None:
        with self.file_path.open("a", encoding="utf-8") as audit_file:
            audit_file.write(report.model_dump_json())
            audit_file.write("\n")


class RemoteGuardrailAuditSink:
    is_blocking = True

    def __init__(
        self,
        remote_url: str,
        timeout_seconds: float = 3.0,
        auth_header: str = "Authorization",
        auth_token: str = "",
        auth_token_prefix: str = "Bearer ",
        max_retries: int = 2,
        retry_backoff_seconds: float = 0.2,
        event_url: str = "",
        report_url: str = "",
        event_auth_header: str = "",
        event_auth_token: str = "",
        event_auth_token_prefix: str = "",
        report_auth_header: str = "",
        report_auth_token: str = "",
        report_auth_token_prefix: str = "",
    ):
        if not remote_url and not event_url and not report_url:
            raise ValueError(
                "OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL or endpoint-specific remote URLs must be set for the remote audit sink."
            )
        self.remote_url = remote_url
        self.event_url = event_url or remote_url
        self.report_url = report_url or remote_url
        self.timeout_seconds = timeout_seconds
        self.auth_header = auth_header
        self.auth_token = auth_token
        self.auth_token_prefix = auth_token_prefix
        self.event_auth_header = event_auth_header or auth_header
        self.event_auth_token = event_auth_token or auth_token
        self.event_auth_token_prefix = event_auth_token_prefix or auth_token_prefix
        self.report_auth_header = report_auth_header or auth_header
        self.report_auth_token = report_auth_token or auth_token
        self.report_auth_token_prefix = report_auth_token_prefix or auth_token_prefix
        self.max_retries = max_retries
        self.retry_backoff_seconds = retry_backoff_seconds

    def emit_event(self, event: GuardrailAuditEvent) -> None:
        self._post_payload(
            self.event_url,
            event.model_dump(mode="json"),
            auth_header=self.event_auth_header,
            auth_token=self.event_auth_token,
            auth_token_prefix=self.event_auth_token_prefix,
        )

    def emit_report(self, report: GuardrailAuditReport) -> None:
        self._post_payload(
            self.report_url,
            report.model_dump(mode="json"),
            auth_header=self.report_auth_header,
            auth_token=self.report_auth_token,
            auth_token_prefix=self.report_auth_token_prefix,
        )

    async def aemit_event(self, event: GuardrailAuditEvent) -> None:
        await self._post_payload_async(
            self.event_url,
            event.model_dump(mode="json"),
            auth_header=self.event_auth_header,
            auth_token=self.event_auth_token,
            auth_token_prefix=self.event_auth_token_prefix,
        )

    async def aemit_report(self, report: GuardrailAuditReport) -> None:
        await self._post_payload_async(
            self.report_url,
            report.model_dump(mode="json"),
            auth_header=self.report_auth_header,
            auth_token=self.report_auth_token,
            auth_token_prefix=self.report_auth_token_prefix,
        )

    def _post_payload(
        self,
        url: str,
        payload: dict,
        *,
        auth_header: str,
        auth_token: str,
        auth_token_prefix: str,
    ) -> None:
        headers = {"Content-Type": "application/json"}
        if auth_token:
            headers[auth_header] = f"{auth_token_prefix}{auth_token}"

        with httpx.Client(timeout=self.timeout_seconds) as client:
            attempts = self.max_retries + 1
            last_error: Exception | None = None
            for attempt in range(1, attempts + 1):
                try:
                    response = client.post(
                        url,
                        json=payload,
                        headers=headers,
                    )
                    if response.status_code >= 500:
                        response.raise_for_status()
                    if response.status_code >= 400:
                        response.raise_for_status()
                    return
                except (httpx.HTTPStatusError, httpx.RequestError) as exc:
                    last_error = exc
                    should_retry = attempt < attempts and self._is_retryable(exc)
                    if not should_retry:
                        raise
                    time.sleep(self.retry_backoff_seconds * attempt)

            if last_error is not None:
                raise last_error

    async def _post_payload_async(
        self,
        url: str,
        payload: dict,
        *,
        auth_header: str,
        auth_token: str,
        auth_token_prefix: str,
    ) -> None:
        headers = {"Content-Type": "application/json"}
        if auth_token:
            headers[auth_header] = f"{auth_token_prefix}{auth_token}"

        async with httpx.AsyncClient(timeout=self.timeout_seconds) as client:
            attempts = self.max_retries + 1
            last_error: Exception | None = None
            for attempt in range(1, attempts + 1):
                try:
                    response = await client.post(
                        url,
                        json=payload,
                        headers=headers,
                    )
                    if response.status_code >= 500:
                        response.raise_for_status()
                    if response.status_code >= 400:
                        response.raise_for_status()
                    return
                except (httpx.HTTPStatusError, httpx.RequestError) as exc:
                    last_error = exc
                    should_retry = attempt < attempts and self._is_retryable(exc)
                    if not should_retry:
                        raise
                    await asyncio.sleep(self.retry_backoff_seconds * attempt)

            if last_error is not None:
                raise last_error

    def _is_retryable(self, exc: Exception) -> bool:
        if isinstance(exc, httpx.RequestError):
            return True
        if isinstance(exc, httpx.HTTPStatusError):
            return exc.response.status_code >= 500
        return False


class GuardrailAuditEmitter:
    def __init__(
        self,
        sinks: list[GuardrailAuditSink] | None = None,
        max_in_flight_background_tasks: int = 128,
        backpressure_policy: str = "drop",
    ):
        if max_in_flight_background_tasks < 1:
            raise ValueError(
                "OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS must be at least 1."
            )
        if backpressure_policy not in {"drop", "block"}:
            raise ValueError(
                "OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY must be one of: drop, block."
            )
        self.sinks = sinks or [LogGuardrailAuditSink()]
        self.max_in_flight_background_tasks = max_in_flight_background_tasks
        self.backpressure_policy = backpressure_policy
        self._background_tasks: set[asyncio.Task] = set()
        self._sync_in_flight_background_tasks_metric()

    @classmethod
    def from_config(cls) -> "GuardrailAuditEmitter":
        sinks: list[GuardrailAuditSink] = []
        for sink_name in config.OUTPUT_GUARDRAILS_AUDIT_SINKS:
            normalized = sink_name.lower()
            if normalized == "log":
                sinks.append(LogGuardrailAuditSink())
            elif normalized == "file":
                sinks.append(
                    FileGuardrailAuditSink(config.OUTPUT_GUARDRAILS_AUDIT_FILE_PATH)
                )
            elif normalized == "remote":
                sinks.append(
                    RemoteGuardrailAuditSink(
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_TIMEOUT_SECONDS,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_HEADER,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_AUTH_TOKEN_PREFIX,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_MAX_RETRIES,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_RETRY_BACKOFF_SECONDS,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_HEADER,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN_PREFIX,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_HEADER,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN,
                        config.OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN_PREFIX,
                    )
                )
            else:
                raise ValueError(
                    f"Unsupported output guardrails audit sink: '{sink_name}'."
                )

        return cls(
            sinks=sinks,
            max_in_flight_background_tasks=config.OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS,
            backpressure_policy=config.OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY,
        )

    def emit_report(self, report: GuardrailAuditReport) -> None:
        for sink in self.sinks:
            self._emit_report_to_sink(sink, report)

    def emit_event(self, event: GuardrailAuditEvent) -> None:
        for sink in self.sinks:
            self._emit_event_to_sink(sink, event)

    async def emit_report_async(self, report: GuardrailAuditReport) -> None:
        for sink in self.sinks:
            if hasattr(sink, "aemit_report"):
                await self._dispatch_background_delivery(
                    delivery_type="report",
                    sink=sink,
                    build_background_coro=lambda sink=sink, report=report: self._emit_report_async_to_sink(
                        sink, report
                    ),
                    build_inline_coro=lambda sink=sink, report=report: self._emit_report_async_to_sink(
                        sink, report
                    ),
                )
                continue
            if getattr(sink, "is_blocking", False):
                await self._dispatch_background_delivery(
                    delivery_type="report",
                    sink=sink,
                    build_background_coro=lambda sink=sink, report=report: self._emit_report_in_thread(
                        sink, report
                    ),
                    build_inline_coro=lambda sink=sink, report=report: self._emit_report_in_thread(
                        sink, report
                    ),
                )
                continue
            self._emit_report_to_sink(sink, report)

    async def emit_event_async(self, event: GuardrailAuditEvent) -> None:
        for sink in self.sinks:
            if hasattr(sink, "aemit_event"):
                await self._dispatch_background_delivery(
                    delivery_type="event",
                    sink=sink,
                    build_background_coro=lambda sink=sink, event=event: self._emit_event_async_to_sink(
                        sink, event
                    ),
                    build_inline_coro=lambda sink=sink, event=event: self._emit_event_async_to_sink(
                        sink, event
                    ),
                )
                continue
            if getattr(sink, "is_blocking", False):
                await self._dispatch_background_delivery(
                    delivery_type="event",
                    sink=sink,
                    build_background_coro=lambda sink=sink, event=event: self._emit_event_in_thread(
                        sink, event
                    ),
                    build_inline_coro=lambda sink=sink, event=event: self._emit_event_in_thread(
                        sink, event
                    ),
                )
                continue
            self._emit_event_to_sink(sink, event)

    async def _dispatch_background_delivery(
        self,
        *,
        delivery_type: str,
        sink: GuardrailAuditSink,
        build_background_coro,
        build_inline_coro,
    ) -> None:
        if len(self._background_tasks) < self.max_in_flight_background_tasks:
            self._schedule_background_task(build_background_coro())
            return

        rag_output_guardrails_audit_backpressure_total.labels(
            policy=self.backpressure_policy,
            delivery_type=delivery_type,
        ).inc()
        logger.warning(
            "output_guardrails_audit_backpressure_applied policy=%s delivery_type=%s sink=%s in_flight=%s max_in_flight=%s",
            self.backpressure_policy,
            delivery_type,
            type(sink).__name__,
            len(self._background_tasks),
            self.max_in_flight_background_tasks,
        )
        if self.backpressure_policy == "drop":
            rag_output_guardrails_audit_dropped_deliveries_total.labels(
                delivery_type=delivery_type,
            ).inc()
            return

        await build_inline_coro()

    async def drain_background_tasks(self, timeout_seconds: float = 5.0) -> None:
        drain_start_time = time.perf_counter()
        pending_tasks = tuple(self._background_tasks)
        drain_status = STATUS_SUCCESS
        try:
            if not pending_tasks:
                return
            await asyncio.wait_for(
                asyncio.gather(*pending_tasks, return_exceptions=True),
                timeout=timeout_seconds,
            )
            rag_output_guardrails_audit_shutdown_drain_total.labels(
                status=STATUS_SUCCESS
            ).inc()
        except asyncio.TimeoutError:
            drain_status = "timeout"
            cancelled_task_count = len(
                [task for task in pending_tasks if not task.done() or task.cancelled()]
            )
            logger.warning(
                "output_guardrails_audit_shutdown_drain_timed_out pending_tasks=%s timeout_seconds=%s",
                cancelled_task_count,
                timeout_seconds,
            )
            rag_output_guardrails_audit_cancelled_deliveries_total.inc(
                cancelled_task_count
            )
            rag_output_guardrails_audit_shutdown_drain_total.labels(
                status="timeout"
            ).inc()
            for task in pending_tasks:
                if not task.done():
                    task.cancel()
            await asyncio.gather(*pending_tasks, return_exceptions=True)
        finally:
            if pending_tasks:
                rag_output_guardrails_audit_shutdown_drain_latency.labels(
                    status=drain_status
                ).observe(time.perf_counter() - drain_start_time)

    async def _emit_report_in_thread(
        self, sink: GuardrailAuditSink, report: GuardrailAuditReport
    ) -> None:
        await asyncio.to_thread(self._emit_report_to_sink, sink, report)

    async def _emit_event_in_thread(
        self, sink: GuardrailAuditSink, event: GuardrailAuditEvent
    ) -> None:
        await asyncio.to_thread(self._emit_event_to_sink, sink, event)

    async def _emit_report_async_to_sink(
        self, sink: GuardrailAuditSink, report: GuardrailAuditReport
    ) -> None:
        try:
            await sink.aemit_report(report)
        except Exception:
            logger.exception(
                "output_guardrails_audit_sink_failed sink=%s report=%s",
                type(sink).__name__,
                json.dumps(
                    {
                        "report_id": report.report_id,
                        "response_id": report.response_id,
                        "event_type": report.event_type,
                    },
                    sort_keys=True,
                ),
            )

    async def _emit_event_async_to_sink(
        self, sink: GuardrailAuditSink, event: GuardrailAuditEvent
    ) -> None:
        try:
            await sink.aemit_event(event)
        except Exception:
            logger.exception(
                "output_guardrails_audit_sink_failed sink=%s event=%s",
                type(sink).__name__,
                json.dumps(
                    {
                        "event_id": event.event_id,
                        "response_id": event.response_id,
                        "event_type": event.event_type,
                        "stage": event.stage,
                    },
                    sort_keys=True,
                ),
            )

    def _schedule_background_task(self, coro: Any) -> None:
        task = asyncio.create_task(coro)
        self._background_tasks.add(task)
        self._sync_in_flight_background_tasks_metric()
        task.add_done_callback(self._finalize_background_task)

    def _finalize_background_task(self, task: asyncio.Task) -> None:
        self._background_tasks.discard(task)
        self._sync_in_flight_background_tasks_metric()

    def _sync_in_flight_background_tasks_metric(self) -> None:
        rag_output_guardrails_audit_in_flight_background_tasks.set(
            len(self._background_tasks)
        )

    def _emit_report_to_sink(
        self, sink: GuardrailAuditSink, report: GuardrailAuditReport
    ) -> None:
        try:
            sink.emit_report(report)
        except Exception:
            logger.exception(
                "output_guardrails_audit_sink_failed sink=%s report=%s",
                type(sink).__name__,
                json.dumps(
                    {
                        "report_id": report.report_id,
                        "response_id": report.response_id,
                        "event_type": report.event_type,
                    },
                    sort_keys=True,
                ),
            )

    def _emit_event_to_sink(
        self, sink: GuardrailAuditSink, event: GuardrailAuditEvent
    ) -> None:
        try:
            sink.emit_event(event)
        except Exception:
            logger.exception(
                "output_guardrails_audit_sink_failed sink=%s event=%s",
                type(sink).__name__,
                json.dumps(
                    {
                        "event_id": event.event_id,
                        "response_id": event.response_id,
                        "event_type": event.event_type,
                        "stage": event.stage,
                    },
                    sort_keys=True,
                ),
            )
