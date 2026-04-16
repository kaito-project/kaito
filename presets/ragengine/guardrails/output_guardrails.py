import json
import hashlib
import logging
import time
import uuid
from dataclasses import dataclass, field
from typing import Any

from llm_guard import scan_output
from llm_guard.output_scanners import BanSubstrings, Regex

from ragengine import config
from ragengine.guardrails.audit import (
    GuardrailAuditEvent,
    GuardrailAuditReport,
    GuardrailScannerResult,
)
from ragengine.models import ChatCompletionResponse, get_message_content

logger = logging.getLogger(__name__)

DEFAULT_BLOCK_MESSAGE = "The model output was blocked by output guardrails."
STREAM_CHECKSUM_ALGORITHM = "sha256"


@dataclass
class OutputGuardrails:
    enabled: bool
    fail_open: bool
    action_on_hit: str
    regex_patterns: list[str]
    banned_substrings: list[str]
    block_message: str = DEFAULT_BLOCK_MESSAGE
    stream_holdback_chars: int = 32

    @classmethod
    def from_config(cls) -> "OutputGuardrails":
        return cls(
            enabled=config.OUTPUT_GUARDRAILS_ENABLED,
            fail_open=config.OUTPUT_GUARDRAILS_FAIL_OPEN,
            action_on_hit=config.OUTPUT_GUARDRAILS_ACTION_ON_HIT,
            regex_patterns=list(config.OUTPUT_GUARDRAILS_REGEX_PATTERNS),
            banned_substrings=list(config.OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS),
            block_message=config.OUTPUT_GUARDRAILS_BLOCK_MESSAGE,
            stream_holdback_chars=config.OUTPUT_GUARDRAILS_STREAM_HOLDBACK_CHARS,
        )

    def create_stream_session(
        self,
        request: dict[str, Any],
        response_metadata: dict[str, Any],
        request_metadata: dict[str, Any] | None = None,
    ) -> "OutputGuardrailsStreamSession":
        request_context = {
            "request_id": (request_metadata or {}).get("request_id"),
            "trace_id": (request_metadata or {}).get("trace_id"),
            "index_name": request.get("index_name"),
            "model": response_metadata.get("model") or request.get("model"),
            "has_tools": bool(request.get("tools") or request.get("functions")),
            "response_id": response_metadata.get("id"),
            "response_mode": response_metadata.get("response_mode", "passthrough"),
        }
        return OutputGuardrailsStreamSession(
            guardrails=self,
            request=request,
            request_context=request_context,
            scanners=self._build_scanners(),
        )

    def guard_response(
        self,
        response: ChatCompletionResponse,
        request: dict[str, Any],
        request_metadata: dict[str, Any] | None = None,
    ) -> tuple[ChatCompletionResponse, GuardrailAuditReport | None]:
        if not self.enabled:
            return response, None

        scanners = self._build_scanners()
        if not scanners:
            return response, None

        start_time = time.perf_counter()
        request_context = self._build_request_context(
            request, response, request_metadata or {}
        )

        try:
            prompt = self._extract_prompt(request)
            response_data = response.model_dump(mode="python")
            events: list[GuardrailAuditEvent] = []

            for choice_index, choice in enumerate(response_data.get("choices", [])):
                message = choice.get("message") or {}
                content = message.get("content")
                if message.get("role") != "assistant" or not isinstance(content, str):
                    continue

                sanitized_output, results_valid, results_score = scan_output(
                    scanners, prompt, content, fail_fast=False
                )
                triggered_scanners = [
                    scanner_name
                    for scanner_name, is_valid in results_valid.items()
                    if not is_valid
                ]
                scanner_results = [
                    GuardrailScannerResult(
                        scanner=scanner_name,
                        valid=is_valid,
                        score=results_score.get(scanner_name),
                    )
                    for scanner_name, is_valid in results_valid.items()
                ]

                if triggered_scanners:
                    if self.action_on_hit == "block":
                        message["content"] = self.block_message
                    else:
                        message["content"] = sanitized_output

                events.append(
                    GuardrailAuditEvent(
                        request_id=request_context["request_id"],
                        trace_id=request_context["trace_id"],
                        response_id=request_context["response_id"],
                        response_mode=request_context["response_mode"],
                        model=request_context["model"],
                        index_name=request_context["index_name"],
                        has_tools=request_context["has_tools"],
                        choice_index=choice_index,
                        triggered=bool(triggered_scanners),
                        action=self.action_on_hit if triggered_scanners else "allow",
                        scanner_results=scanner_results,
                    )
                )

            report = GuardrailAuditReport(
                request_id=request_context["request_id"],
                trace_id=request_context["trace_id"],
                response_id=request_context["response_id"],
                model=request_context["model"],
                index_name=request_context["index_name"],
                has_tools=request_context["has_tools"],
                response_mode=request_context["response_mode"],
                elapsed_ms=round((time.perf_counter() - start_time) * 1000, 3),
                events=events,
            )

            if events:
                logger.info(
                    "output_guardrails_completed context=%s events=%s",
                    json.dumps(request_context, sort_keys=True),
                    json.dumps(
                        [event.model_dump(mode="python") for event in events],
                        sort_keys=True,
                    ),
                )

            return ChatCompletionResponse(**response_data), report
        except Exception as exc:
            logger.exception("output_guardrails_failed")
            if self.fail_open:
                report = GuardrailAuditReport(
                    request_id=request_context["request_id"],
                    trace_id=request_context["trace_id"],
                    response_id=request_context["response_id"],
                    model=request_context["model"],
                    index_name=request_context["index_name"],
                    has_tools=request_context["has_tools"],
                    response_mode=request_context["response_mode"],
                    elapsed_ms=round((time.perf_counter() - start_time) * 1000, 3),
                    events=[
                        GuardrailAuditEvent(
                            request_id=request_context["request_id"],
                            trace_id=request_context["trace_id"],
                            response_id=request_context["response_id"],
                            response_mode=request_context["response_mode"],
                            model=request_context["model"],
                            index_name=request_context["index_name"],
                            has_tools=request_context["has_tools"],
                            choice_index=0,
                            triggered=False,
                            action="fail_open",
                            fail_open=True,
                            error=str(exc),
                        )
                    ],
                )
                return response, report
            raise RuntimeError(f"Output guardrails failed: {exc}") from exc

    def _build_scanners(self) -> list[Any]:
        scanners: list[Any] = []

        if self.regex_patterns:
            scanners.append(Regex(patterns=self.regex_patterns, redact=True))

        if self.banned_substrings:
            scanners.append(
                BanSubstrings(
                    substrings=self.banned_substrings,
                    redact=self.action_on_hit == "redact",
                )
            )

        return scanners

    def _extract_prompt(self, request: dict[str, Any]) -> str:
        messages = request.get("messages", [])
        if not isinstance(messages, list):
            return ""

        prompt_parts = []
        for message in messages:
            if not isinstance(message, dict):
                continue
            content = get_message_content(message)
            if content:
                prompt_parts.append(content)

        return "\n\n".join(prompt_parts)

    def _build_request_context(
        self,
        request: dict[str, Any],
        response: ChatCompletionResponse,
        request_metadata: dict[str, Any],
    ) -> dict[str, Any]:
        return {
            "request_id": request_metadata.get("request_id"),
            "trace_id": request_metadata.get("trace_id"),
            "index_name": request.get("index_name"),
            "model": request.get("model") or response.model,
            "has_tools": bool(request.get("tools") or request.get("functions")),
            "response_id": response.id,
            "response_mode": "passthrough"
            if response.source_nodes is None
            else "rag",
        }


@dataclass
class OutputGuardrailsStreamSession:
    guardrails: OutputGuardrails
    request: dict[str, Any]
    request_context: dict[str, Any]
    scanners: list[Any]
    start_time: float = field(default_factory=time.perf_counter)
    raw_output: str = ""
    sanitized_output: str = ""
    emitted_chars: int = 0
    triggered: bool = False
    blocked: bool = False
    decision_emitted: bool = False
    fail_open_error: str | None = None
    scanner_results: list[GuardrailScannerResult] = field(default_factory=list)
    prompt: str = field(init=False)
    chunk_index: int = 0
    stream_id: str = field(default_factory=lambda: uuid.uuid4().hex)
    sequence: int = 0

    def __post_init__(self) -> None:
        self.prompt = self.guardrails._extract_prompt(self.request)

    def append_text(self, delta: str) -> tuple[list[str], list[GuardrailAuditEvent]]:
        if not delta:
            return [], []

        self.chunk_index += 1

        self.raw_output += delta

        if not self.guardrails.enabled or not self.scanners:
            self.sanitized_output += delta
            self.emitted_chars += len(delta)
            return [delta], []

        if self.fail_open_error is not None:
            return [delta], []

        try:
            sanitized_output, results_valid, results_score = scan_output(
                self.scanners, self.prompt, self.raw_output, fail_fast=False
            )
        except Exception as exc:
            logger.exception("output_guardrails_stream_failed")
            if self.guardrails.fail_open:
                self.fail_open_error = str(exc)
                return [delta], [self._build_stage_event(stage="stream_fail_open")]
            raise RuntimeError(f"Output guardrails stream failed: {exc}") from exc

        self.triggered = any(not is_valid for is_valid in results_valid.values())
        self.scanner_results = [
            GuardrailScannerResult(
                scanner=scanner_name,
                valid=is_valid,
                score=results_score.get(scanner_name),
            )
            for scanner_name, is_valid in results_valid.items()
        ]

        staged_events: list[GuardrailAuditEvent] = []
        if self.triggered and not self.decision_emitted:
            self.decision_emitted = True
            staged_events.append(self._build_stage_event(stage="stream_decision"))

        if self.triggered and self.guardrails.action_on_hit == "block":
            if not self.blocked:
                self.blocked = True
                return [self.guardrails.block_message], staged_events
            return [], staged_events

        self.sanitized_output = sanitized_output
        stable_chars = max(
            0,
            len(self.sanitized_output) - self.guardrails.stream_holdback_chars,
        )
        if stable_chars <= self.emitted_chars:
            return [], staged_events

        emit_text = self.sanitized_output[self.emitted_chars:stable_chars]
        self.emitted_chars = stable_chars
        return ([emit_text] if emit_text else []), staged_events

    def observe_structured_delta(
        self, delta: dict[str, Any]
    ) -> list[GuardrailAuditEvent]:
        if not delta:
            return []

        self.chunk_index += 1

        if "tool_calls" in delta:
            return [self._build_stage_event(stage="stream_tool_call_passthrough")]

        if "function_call" in delta:
            return [self._build_stage_event(stage="stream_function_call_passthrough")]

        return []

    def start_event(self) -> GuardrailAuditEvent:
        return self._build_stage_event(stage="stream_started")

    def should_emit_audit_events(self) -> bool:
        return self.guardrails.enabled and bool(self.scanners)

    def finalize(
        self,
    ) -> tuple[list[str], GuardrailAuditReport | None, str, list[GuardrailAuditEvent]]:
        finish_reason = "content_filter" if self.blocked else "stop"
        if not self.guardrails.enabled or not self.scanners:
            return [], None, finish_reason, []

        final_output = self._final_output_text()
        completed_event, sequence_total, end_of_stream_checksum = (
            self._build_completed_event(
                finish_reason=finish_reason, final_output=final_output
            )
        )
        staged_events: list[GuardrailAuditEvent] = [completed_event]

        if self.fail_open_error is not None:
            report = GuardrailAuditReport(
                stream_id=self.stream_id,
                last_sequence=self.sequence,
                sequence_total=sequence_total,
                checksum_algorithm=STREAM_CHECKSUM_ALGORITHM,
                end_of_stream_checksum=end_of_stream_checksum,
                request_id=self.request_context["request_id"],
                trace_id=self.request_context["trace_id"],
                response_id=self.request_context["response_id"],
                model=self.request_context["model"],
                index_name=self.request_context["index_name"],
                has_tools=self.request_context["has_tools"],
                response_mode=self.request_context["response_mode"],
                elapsed_ms=round((time.perf_counter() - self.start_time) * 1000, 3),
                events=[
                    GuardrailAuditEvent(
                        request_id=self.request_context["request_id"],
                        trace_id=self.request_context["trace_id"],
                        response_id=self.request_context["response_id"],
                        response_mode=self.request_context["response_mode"],
                        model=self.request_context["model"],
                        index_name=self.request_context["index_name"],
                        has_tools=self.request_context["has_tools"],
                        choice_index=0,
                        stage="final",
                        chunk_index=self.chunk_index,
                        observed_chars=len(self.raw_output),
                        emitted_chars=self.emitted_chars,
                        triggered=False,
                        action="fail_open",
                        fail_open=True,
                        error=self.fail_open_error,
                    )
                ],
            )
            return [], report, "stop", staged_events

        remaining = []
        if not self.blocked:
            tail = self.sanitized_output[self.emitted_chars :]
            if tail:
                remaining.append(tail)
                self.emitted_chars += len(tail)

        report = GuardrailAuditReport(
            stream_id=self.stream_id,
            last_sequence=self.sequence,
            sequence_total=sequence_total,
            checksum_algorithm=STREAM_CHECKSUM_ALGORITHM,
            end_of_stream_checksum=end_of_stream_checksum,
            request_id=self.request_context["request_id"],
            trace_id=self.request_context["trace_id"],
            response_id=self.request_context["response_id"],
            model=self.request_context["model"],
            index_name=self.request_context["index_name"],
            has_tools=self.request_context["has_tools"],
            response_mode=self.request_context["response_mode"],
            elapsed_ms=round((time.perf_counter() - self.start_time) * 1000, 3),
            events=[
                GuardrailAuditEvent(
                    request_id=self.request_context["request_id"],
                    trace_id=self.request_context["trace_id"],
                    response_id=self.request_context["response_id"],
                    response_mode=self.request_context["response_mode"],
                    model=self.request_context["model"],
                    index_name=self.request_context["index_name"],
                    has_tools=self.request_context["has_tools"],
                    choice_index=0,
                    stage="final",
                    chunk_index=self.chunk_index,
                    observed_chars=len(self.raw_output),
                    emitted_chars=self.emitted_chars,
                    triggered=self.triggered,
                    action=(
                        self.guardrails.action_on_hit if self.triggered else "allow"
                    ),
                    scanner_results=self.scanner_results,
                )
            ],
        )
        return remaining, report, finish_reason, staged_events

    def _build_stage_event(self, stage: str) -> GuardrailAuditEvent:
        self.sequence += 1
        return GuardrailAuditEvent(
            stream_id=self.stream_id,
            sequence=self.sequence,
            request_id=self.request_context["request_id"],
            trace_id=self.request_context["trace_id"],
            response_id=self.request_context["response_id"],
            response_mode=self.request_context["response_mode"],
            model=self.request_context["model"],
            index_name=self.request_context["index_name"],
            has_tools=self.request_context["has_tools"],
            choice_index=0,
            stage=stage,
            chunk_index=self.chunk_index,
            observed_chars=len(self.raw_output),
            emitted_chars=self.emitted_chars,
            triggered=self.triggered,
            action=(
                "fail_open"
                if self.fail_open_error is not None
                else self.guardrails.action_on_hit if self.triggered else "allow"
            ),
            fail_open=self.fail_open_error is not None,
            scanner_results=self.scanner_results,
            error=self.fail_open_error,
        )

    def _build_completed_event(
        self, *, finish_reason: str, final_output: str
    ) -> tuple[GuardrailAuditEvent, int, str]:
        event = self._build_stage_event(stage="stream_completed")
        sequence_total = event.sequence or self.sequence
        end_of_stream_checksum = self._compute_end_of_stream_checksum(
            finish_reason=finish_reason,
            final_output=final_output,
            sequence_total=sequence_total,
        )
        event.sequence_total = sequence_total
        event.checksum_algorithm = STREAM_CHECKSUM_ALGORITHM
        event.end_of_stream_checksum = end_of_stream_checksum
        return event, sequence_total, end_of_stream_checksum

    def _compute_end_of_stream_checksum(
        self, *, finish_reason: str, final_output: str, sequence_total: int
    ) -> str:
        payload = {
            "finish_reason": finish_reason,
            "final_output": final_output,
            "sequence_total": sequence_total,
            "stream_id": self.stream_id,
        }
        return hashlib.sha256(
            json.dumps(payload, sort_keys=True).encode("utf-8")
        ).hexdigest()

    def _final_output_text(self) -> str:
        if self.blocked:
            return self.guardrails.block_message
        if self.fail_open_error is not None:
            return self.raw_output
        if not self.guardrails.enabled or not self.scanners:
            return self.raw_output
        return self.sanitized_output