import logging
from dataclasses import dataclass
from typing import Any

from llm_guard import scan_output
from llm_guard.output_scanners import BanSubstrings, Regex

from ragengine import config
from ragengine.models import ChatCompletionResponse, get_message_content

logger = logging.getLogger(__name__)

DEFAULT_BLOCK_MESSAGE = "The model output was blocked by output guardrails."


@dataclass
class OutputGuardrails:
    enabled: bool
    action_on_hit: str
    regex_patterns: list[str]
    banned_substrings: list[str]
    block_message: str = DEFAULT_BLOCK_MESSAGE

    @classmethod
    def from_config(cls) -> "OutputGuardrails":
        return cls(
            enabled=config.OUTPUT_GUARDRAILS_ENABLED,
            action_on_hit=config.OUTPUT_GUARDRAILS_ACTION_ON_HIT,
            regex_patterns=list(config.OUTPUT_GUARDRAILS_REGEX_PATTERNS),
            banned_substrings=list(config.OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS),
            block_message=config.OUTPUT_GUARDRAILS_BLOCK_MESSAGE,
        )

    def guard_response(
        self,
        response: ChatCompletionResponse,
        request: dict[str, Any],
    ) -> ChatCompletionResponse:
        if not self.enabled:
            return response

        scanners = self._build_scanners()
        if not scanners:
            return response

        try:
            prompt = self._extract_prompt(request)
            response_data = response.model_dump(mode="python")

            for choice in response_data.get("choices", []):
                message = choice.get("message") or {}
                content = message.get("content")
                if message.get("role") != "assistant" or not isinstance(content, str):
                    continue

                sanitized_output, results_valid, results_score = scan_output(
                    scanners, prompt, content, fail_fast=False
                )
                triggered_scanners = {
                    scanner_name: results_score.get(scanner_name)
                    for scanner_name, is_valid in results_valid.items()
                    if not is_valid
                }
                if not triggered_scanners:
                    continue

                if self.action_on_hit == "block":
                    message["content"] = self.block_message
                else:
                    message["content"] = sanitized_output

                logger.info(
                    "output_guardrails_triggered action=%s response_id=%s scanners=%s",
                    self.action_on_hit,
                    response.id,
                    triggered_scanners,
                )

            return ChatCompletionResponse(**response_data)
        except Exception:
            logger.exception("output_guardrails_failed")
            return response

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
