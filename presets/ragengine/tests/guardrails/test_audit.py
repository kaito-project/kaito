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
import json
import threading
import time

import httpx
import pytest
import respx
from ragengine.metrics.prometheus_metrics import (
    rag_output_guardrails_audit_backpressure_total,
    rag_output_guardrails_audit_cancelled_deliveries_total,
    rag_output_guardrails_audit_dropped_deliveries_total,
    rag_output_guardrails_audit_in_flight_background_tasks,
    rag_output_guardrails_audit_shutdown_drain_latency,
    rag_output_guardrails_audit_shutdown_drain_total,
)

from ragengine.guardrails.audit import (
    FileGuardrailAuditSink,
    GuardrailAuditEmitter,
    GuardrailAuditEvent,
    GuardrailAuditReport,
    LogGuardrailAuditSink,
    RemoteGuardrailAuditSink,
)


class FailingAuditSink:
    def emit_event(self, event):
        raise RuntimeError("sink exploded")

    def emit_report(self, report):
        raise RuntimeError("sink exploded")


class BlockingAuditSink:
    is_blocking = True

    def __init__(self):
        self.event_called = threading.Event()
        self.report_called = threading.Event()

    def emit_event(self, event):
        time.sleep(0.2)
        self.event_called.set()

    def emit_report(self, report):
        time.sleep(0.2)
        self.report_called.set()


class AsyncBlockingAuditSink:
    def __init__(self):
        self.event_called = asyncio.Event()
        self.report_called = asyncio.Event()

    async def aemit_event(self, event):
        await asyncio.sleep(0.05)
        self.event_called.set()

    async def aemit_report(self, report):
        await asyncio.sleep(0.05)
        self.report_called.set()


class NeverEndingAsyncAuditSink:
    def __init__(self):
        self.cancelled = asyncio.Event()

    async def aemit_event(self, event):
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            self.cancelled.set()
            raise


def _counter_value(metric, **labels) -> float:
    return metric.labels(**labels)._value.get()


def _counter_total_value(metric) -> float:
    return metric._value.get()


def _gauge_value(metric) -> float:
    return metric._value.get()


def _histogram_count(metric, **labels) -> float:
    sample_values = {
        sample.name: sample.value
        for collected_metric in metric.collect()
        for sample in collected_metric.samples
        if sample.labels == labels
    }
    return (
        sample_values[f"{metric._name}_sum"],
        sample_values[f"{metric._name}_count"],
    )


def _ensure_histogram_series(metric, **labels) -> None:
    metric.labels(**labels).observe(0)


def _build_report() -> GuardrailAuditReport:
    return GuardrailAuditReport(
        stream_id="stream-audit-1",
        last_sequence=4,
        sequence_total=4,
        checksum_algorithm="sha256",
        end_of_stream_checksum="3c5e2f3f6fb2f8f4e0d4eb6fcd8c1fbff0ff9719a7b9d8d7c7e132ac6f4f1d73",
        response_id="chatcmpl-audit",
        model="mock-model",
        response_mode="passthrough",
        elapsed_ms=1.25,
        events=[
            GuardrailAuditEvent(
                stream_id="stream-audit-1",
                sequence=4,
                response_id="chatcmpl-audit",
                response_mode="passthrough",
                model="mock-model",
                choice_index=0,
                triggered=True,
                action="redact",
            )
        ],
    )


def test_file_audit_sink_writes_jsonl_report(tmp_path):
    report = _build_report()
    file_path = tmp_path / "audit" / "guardrails.jsonl"

    sink = FileGuardrailAuditSink(str(file_path))
    sink.emit_report(report)

    lines = file_path.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1
    payload = json.loads(lines[0])
    assert payload["event_type"] == "ragengine.output_guardrails.report"
    assert payload["sequence_total"] == 4
    assert payload["checksum_algorithm"] == "sha256"
    assert payload["end_of_stream_checksum"]
    assert payload["response_id"] == "chatcmpl-audit"
    assert payload["events"][0]["action"] == "redact"


def test_file_audit_sink_writes_jsonl_event(tmp_path):
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=1,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_started",
        triggered=False,
        action="allow",
    )
    file_path = tmp_path / "audit" / "guardrails.jsonl"

    sink = FileGuardrailAuditSink(str(file_path))
    sink.emit_event(event)

    payload = json.loads(file_path.read_text(encoding="utf-8").strip())
    assert payload["event_type"] == "ragengine.output_guardrails"
    assert payload["stage"] == "stream_started"
    assert payload["stream_id"] == "stream-audit-1"
    assert payload["sequence"] == 1


def test_audit_emitter_continues_when_one_sink_fails(tmp_path, caplog):
    report = _build_report()
    file_path = tmp_path / "guardrails.jsonl"
    emitter = GuardrailAuditEmitter(
        sinks=[FailingAuditSink(), FileGuardrailAuditSink(str(file_path))]
    )

    caplog.set_level("ERROR")
    emitter.emit_report(report)

    assert "output_guardrails_audit_sink_failed" in caplog.text
    payload = json.loads(file_path.read_text(encoding="utf-8").strip())
    assert payload["report_id"] == report.report_id


def test_audit_emitter_uses_log_sink_by_default(caplog):
    report = _build_report()
    emitter = GuardrailAuditEmitter(sinks=[LogGuardrailAuditSink()])

    caplog.set_level("INFO")
    emitter.emit_report(report)

    assert "output_guardrails_audit_report" in caplog.text
    assert '"response_id":"chatcmpl-audit"' in caplog.text


def test_audit_emitter_logs_staged_event(caplog):
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=1,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_started",
        triggered=False,
        action="allow",
    )
    emitter = GuardrailAuditEmitter(sinks=[LogGuardrailAuditSink()])

    caplog.set_level("INFO")
    emitter.emit_event(event)

    assert "output_guardrails_audit_event" in caplog.text
    assert '"stage":"stream_started"' in caplog.text


@pytest.mark.asyncio
async def test_audit_emitter_offloads_blocking_event_sink():
    sink = BlockingAuditSink()
    emitter = GuardrailAuditEmitter(sinks=[sink])
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=1,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_started",
        triggered=False,
        action="allow",
    )

    start_time = time.perf_counter()
    await emitter.emit_event_async(event)
    elapsed = time.perf_counter() - start_time

    assert elapsed < 0.1
    assert not sink.event_called.is_set()

    for _ in range(20):
        if sink.event_called.wait(0.05):
            break
        await asyncio.sleep(0)

    assert sink.event_called.is_set()


@pytest.mark.asyncio
async def test_audit_emitter_offloads_blocking_report_sink():
    sink = BlockingAuditSink()
    emitter = GuardrailAuditEmitter(sinks=[sink])

    start_time = time.perf_counter()
    await emitter.emit_report_async(_build_report())
    elapsed = time.perf_counter() - start_time

    assert elapsed < 0.1
    assert not sink.report_called.is_set()

    for _ in range(20):
        if sink.report_called.wait(0.05):
            break
        await asyncio.sleep(0)

    assert sink.report_called.is_set()


@pytest.mark.asyncio
async def test_audit_emitter_drain_waits_for_async_sink_completion():
    sink = AsyncBlockingAuditSink()
    emitter = GuardrailAuditEmitter(sinks=[sink])
    _ensure_histogram_series(
        rag_output_guardrails_audit_shutdown_drain_latency, status="success"
    )
    before_success_total = _counter_value(
        rag_output_guardrails_audit_shutdown_drain_total, status="success"
    )
    before_success_latency_sum, before_success_latency_count = _histogram_count(
        rag_output_guardrails_audit_shutdown_drain_latency, status="success"
    )

    await emitter.emit_event_async(
        GuardrailAuditEvent(
            stream_id="stream-audit-1",
            sequence=1,
            response_id="chatcmpl-audit",
            response_mode="passthrough",
            model="mock-model",
            choice_index=0,
            stage="stream_started",
            triggered=False,
            action="allow",
        )
    )
    await emitter.emit_report_async(_build_report())

    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 2

    await emitter.drain_background_tasks(timeout_seconds=1.0)

    assert sink.event_called.is_set()
    assert sink.report_called.is_set()
    assert len(emitter._background_tasks) == 0
    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 0
    assert (
        _counter_value(
            rag_output_guardrails_audit_shutdown_drain_total, status="success"
        )
        == before_success_total + 1
    )
    after_success_latency_sum, after_success_latency_count = _histogram_count(
        rag_output_guardrails_audit_shutdown_drain_latency, status="success"
    )
    assert after_success_latency_count == before_success_latency_count + 1
    assert after_success_latency_sum >= before_success_latency_sum


@pytest.mark.asyncio
async def test_audit_emitter_drain_cancels_timed_out_async_tasks(caplog):
    sink = NeverEndingAsyncAuditSink()
    emitter = GuardrailAuditEmitter(sinks=[sink])
    _ensure_histogram_series(
        rag_output_guardrails_audit_shutdown_drain_latency, status="timeout"
    )
    before_timeout_total = _counter_value(
        rag_output_guardrails_audit_shutdown_drain_total, status="timeout"
    )
    before_cancelled_total = _counter_total_value(
        rag_output_guardrails_audit_cancelled_deliveries_total
    )
    before_timeout_latency_sum, before_timeout_latency_count = _histogram_count(
        rag_output_guardrails_audit_shutdown_drain_latency, status="timeout"
    )
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=1,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_started",
        triggered=False,
        action="allow",
    )

    caplog.set_level("WARNING")
    await emitter.emit_event_async(event)
    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 1
    await emitter.drain_background_tasks(timeout_seconds=0.01)

    assert sink.cancelled.is_set()
    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 0
    assert "output_guardrails_audit_shutdown_drain_timed_out" in caplog.text
    assert (
        _counter_value(
            rag_output_guardrails_audit_shutdown_drain_total, status="timeout"
        )
        == before_timeout_total + 1
    )
    assert (
        _counter_total_value(rag_output_guardrails_audit_cancelled_deliveries_total)
        == before_cancelled_total + 1
    )
    after_timeout_latency_sum, after_timeout_latency_count = _histogram_count(
        rag_output_guardrails_audit_shutdown_drain_latency, status="timeout"
    )
    assert after_timeout_latency_count == before_timeout_latency_count + 1
    assert after_timeout_latency_sum >= before_timeout_latency_sum


@pytest.mark.asyncio
async def test_audit_emitter_drops_delivery_when_backpressure_policy_is_drop(caplog):
    sink = NeverEndingAsyncAuditSink()
    emitter = GuardrailAuditEmitter(
        sinks=[sink],
        max_in_flight_background_tasks=1,
        backpressure_policy="drop",
    )
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=1,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_started",
        triggered=False,
        action="allow",
    )
    before_backpressure_total = _counter_value(
        rag_output_guardrails_audit_backpressure_total,
        policy="drop",
        delivery_type="event",
    )
    before_dropped_total = _counter_value(
        rag_output_guardrails_audit_dropped_deliveries_total,
        delivery_type="event",
    )

    caplog.set_level("WARNING")
    await emitter.emit_event_async(event)
    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 1

    await emitter.emit_event_async(event)

    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 1
    assert (
        _counter_value(
            rag_output_guardrails_audit_backpressure_total,
            policy="drop",
            delivery_type="event",
        )
        == before_backpressure_total + 1
    )
    assert (
        _counter_value(
            rag_output_guardrails_audit_dropped_deliveries_total,
            delivery_type="event",
        )
        == before_dropped_total + 1
    )
    assert "output_guardrails_audit_backpressure_applied" in caplog.text

    await emitter.drain_background_tasks(timeout_seconds=0.01)


@pytest.mark.asyncio
async def test_audit_emitter_blocks_inline_when_backpressure_policy_is_block():
    sink = AsyncBlockingAuditSink()
    emitter = GuardrailAuditEmitter(
        sinks=[sink],
        max_in_flight_background_tasks=1,
        backpressure_policy="block",
    )
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=1,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_started",
        triggered=False,
        action="allow",
    )
    before_backpressure_total = _counter_value(
        rag_output_guardrails_audit_backpressure_total,
        policy="block",
        delivery_type="event",
    )
    before_dropped_total = _counter_value(
        rag_output_guardrails_audit_dropped_deliveries_total,
        delivery_type="event",
    )

    await emitter.emit_event_async(event)
    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 1

    start_time = time.perf_counter()
    await emitter.emit_event_async(event)
    elapsed = time.perf_counter() - start_time

    assert elapsed >= 0.04
    assert _gauge_value(rag_output_guardrails_audit_in_flight_background_tasks) == 1
    assert (
        _counter_value(
            rag_output_guardrails_audit_backpressure_total,
            policy="block",
            delivery_type="event",
        )
        == before_backpressure_total + 1
    )
    assert (
        _counter_value(
            rag_output_guardrails_audit_dropped_deliveries_total,
            delivery_type="event",
        )
        == before_dropped_total
    )

    await emitter.drain_background_tasks(timeout_seconds=1.0)


def test_audit_emitter_rejects_invalid_backpressure_policy():
    with pytest.raises(ValueError, match="OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY"):
        GuardrailAuditEmitter(backpressure_policy="invalid")


def test_audit_emitter_rejects_invalid_max_in_flight_background_tasks():
    with pytest.raises(
        ValueError,
        match="OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS",
    ):
        GuardrailAuditEmitter(max_in_flight_background_tasks=0)


def test_file_audit_sink_requires_file_path():
    with pytest.raises(ValueError, match="OUTPUT_GUARDRAILS_AUDIT_FILE_PATH"):
        FileGuardrailAuditSink("")


@respx.mock
def test_remote_audit_sink_posts_report():
    report = _build_report()
    route = respx.post("http://audit-sink.local/reports").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )

    sink = RemoteGuardrailAuditSink("http://audit-sink.local/reports", 1.5)
    sink.emit_report(report)

    assert route.called
    request_json = route.calls[0].request.content.decode("utf-8")
    payload = json.loads(request_json)
    assert payload["event_type"] == "ragengine.output_guardrails.report"
    assert payload["stream_id"] == "stream-audit-1"
    assert payload["last_sequence"] == 4
    assert payload["sequence_total"] == 4
    assert payload["checksum_algorithm"] == "sha256"
    assert payload["end_of_stream_checksum"]
    assert payload["response_id"] == "chatcmpl-audit"
    assert payload["events"][0]["action"] == "redact"


@respx.mock
def test_remote_audit_sink_sends_auth_header():
    report = _build_report()
    route = respx.post("http://audit-sink.local/reports").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )

    sink = RemoteGuardrailAuditSink(
        "http://audit-sink.local/reports",
        timeout_seconds=1.5,
        auth_header="Authorization",
        auth_token="secret-token",
        auth_token_prefix="Bearer ",
    )
    sink.emit_report(report)

    assert route.called
    assert route.calls[0].request.headers["Authorization"] == "Bearer secret-token"


@respx.mock
def test_remote_audit_sink_retries_transient_failures():
    report = _build_report()
    route = respx.post("http://audit-sink.local/reports").mock(
        side_effect=[
            httpx.Response(503, json={"status": "down"}),
            httpx.Response(202, json={"status": "accepted"}),
        ]
    )

    sink = RemoteGuardrailAuditSink(
        "http://audit-sink.local/reports",
        timeout_seconds=1.5,
        max_retries=1,
        retry_backoff_seconds=0,
    )
    sink.emit_report(report)

    assert route.call_count == 2


@respx.mock
def test_remote_audit_sink_posts_event():
    event = GuardrailAuditEvent(
        stream_id="stream-audit-1",
        sequence=2,
        response_id="chatcmpl-audit",
        response_mode="passthrough",
        model="mock-model",
        choice_index=0,
        stage="stream_completed",
        triggered=False,
        action="allow",
    )
    route = respx.post("http://audit-sink.local/reports").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )

    sink = RemoteGuardrailAuditSink("http://audit-sink.local/reports", 1.5)
    sink.emit_event(event)

    payload = json.loads(route.calls[0].request.content.decode("utf-8"))
    assert payload["event_type"] == "ragengine.output_guardrails"
    assert payload["stream_id"] == "stream-audit-1"
    assert payload["sequence"] == 2
    assert payload["stage"] == "stream_completed"


@respx.mock
def test_remote_audit_sink_uses_separate_event_and_report_endpoints():
    report_route = respx.post("http://audit-sink.local/reports").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )
    event_route = respx.post("http://audit-sink.local/events").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )

    sink = RemoteGuardrailAuditSink(
        remote_url="",
        event_url="http://audit-sink.local/events",
        report_url="http://audit-sink.local/reports",
        timeout_seconds=1.5,
    )

    sink.emit_event(
        GuardrailAuditEvent(
            stream_id="stream-audit-1",
            sequence=1,
            response_id="chatcmpl-audit",
            response_mode="passthrough",
            model="mock-model",
            choice_index=0,
            stage="stream_started",
            triggered=False,
            action="allow",
        )
    )
    sink.emit_report(_build_report())

    assert event_route.called
    assert report_route.called


@respx.mock
def test_remote_audit_sink_uses_separate_auth_for_event_and_report():
    report_route = respx.post("http://audit-sink.local/reports").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )
    event_route = respx.post("http://audit-sink.local/events").mock(
        return_value=httpx.Response(202, json={"status": "accepted"})
    )

    sink = RemoteGuardrailAuditSink(
        remote_url="http://audit-sink.local/fallback",
        event_url="http://audit-sink.local/events",
        report_url="http://audit-sink.local/reports",
        auth_header="Authorization",
        auth_token="shared-token",
        auth_token_prefix="Bearer ",
        event_auth_header="X-Event-Auth",
        event_auth_token="event-token",
        event_auth_token_prefix="Token ",
        report_auth_header="X-Report-Auth",
        report_auth_token="report-token",
        report_auth_token_prefix="Token ",
        timeout_seconds=1.5,
    )

    sink.emit_event(
        GuardrailAuditEvent(
            stream_id="stream-audit-1",
            sequence=1,
            response_id="chatcmpl-audit",
            response_mode="passthrough",
            model="mock-model",
            choice_index=0,
            stage="stream_started",
            triggered=False,
            action="allow",
        )
    )
    sink.emit_report(_build_report())

    assert event_route.calls[0].request.headers["X-Event-Auth"] == "Token event-token"
    assert "Authorization" not in event_route.calls[0].request.headers
    assert report_route.calls[0].request.headers["X-Report-Auth"] == "Token report-token"
    assert "Authorization" not in report_route.calls[0].request.headers


def test_remote_audit_sink_requires_some_remote_url():
    with pytest.raises(ValueError, match="OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL"):
        RemoteGuardrailAuditSink(remote_url="", event_url="", report_url="")


def test_remote_audit_sink_requires_remote_url():
    with pytest.raises(ValueError, match="OUTPUT_GUARDRAILS_AUDIT_REMOTE_URL"):
        RemoteGuardrailAuditSink("")


@respx.mock
def test_audit_emitter_continues_when_remote_sink_fails(tmp_path, caplog):
    report = _build_report()
    file_path = tmp_path / "guardrails.jsonl"
    respx.post("http://audit-sink.local/reports").mock(
        return_value=httpx.Response(503, json={"status": "down"})
    )
    emitter = GuardrailAuditEmitter(
        sinks=[
            RemoteGuardrailAuditSink("http://audit-sink.local/reports", 1.0),
            FileGuardrailAuditSink(str(file_path)),
        ]
    )

    caplog.set_level("ERROR")
    emitter.emit_report(report)

    assert "output_guardrails_audit_sink_failed" in caplog.text
    payload = json.loads(file_path.read_text(encoding="utf-8").strip())
    assert payload["report_id"] == report.report_id