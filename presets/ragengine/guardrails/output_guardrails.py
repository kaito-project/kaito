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

import inspect
import logging
import re
from dataclasses import dataclass, field
from typing import Any

import llm_guard.output_scanners as llm_guard_output_scanners
import yaml
from llm_guard import scan_output
from ragengine import config
from ragengine.models import ChatCompletionResponse, get_message_content

logger = logging.getLogger(__name__)

DEFAULT_BLOCK_MESSAGE = "The model output was blocked by output guardrails."

SUPPORTED_POLICY_SCANNERS = {
    "ban_code": "BanCode",
    "ban_competitors": "BanCompetitors",
    "ban_substrings": "BanSubstrings",
    "ban_topics": "BanTopics",
    "bias": "Bias",
    "code": "Code",
    "factual_consistency": "FactualConsistency",
    "gibberish": "Gibberish",
    "json": "JSON",
    "language": "Language",
    "language_same": "LanguageSame",
    "malicious_urls": "MaliciousURLs",
    "no_refusal": "NoRefusal",
    "no_refusal_light": "NoRefusalLight",
    "reading_time": "ReadingTime",
    "regex": "Regex",
    "relevance": "Relevance",
    "secret_detection": "Sensitive",
    "sensitive": "Sensitive",
    "sentiment": "Sentiment",
    "toxicity": "Toxicity",
    "url_reachability": "URLReachability",
}


@dataclass
class OutputGuardrails:
    enabled: bool
    action_on_hit: str
    block_message: str = DEFAULT_BLOCK_MESSAGE
    scanner_configs: list[dict[str, Any]] = field(default_factory=list)

    @classmethod
    def from_config(cls) -> "OutputGuardrails":
        guardrails = cls(
            enabled=config.OUTPUT_GUARDRAILS_ENABLED,
            action_on_hit=config.OUTPUT_GUARDRAILS_ACTION_ON_HIT,
            block_message=config.OUTPUT_GUARDRAILS_BLOCK_MESSAGE,
        )
        return guardrails._apply_policy_file(config.OUTPUT_GUARDRAILS_POLICY_PATH)

    def _apply_policy_file(self, policy_path: str) -> "OutputGuardrails":
        if not policy_path:
            return self

        try:
            with open(policy_path, encoding="utf-8") as policy_file:
                policy = yaml.safe_load(policy_file) or {}
        except FileNotFoundError:
            logger.warning("output_guardrails_policy_missing path=%s", policy_path)
            return self
        except Exception:
            logger.exception(
                "output_guardrails_policy_load_failed path=%s", policy_path
            )
            return self

        if not isinstance(policy, dict):
            logger.warning("output_guardrails_policy_invalid path=%s", policy_path)
            return self

        scanner_configs = list(self.scanner_configs)
        if "scanners" in policy:
            scanner_configs = _coerce_policy_scanner_configs(
                policy.get("scanners"),
                policy_path,
            )

        return OutputGuardrails(
            enabled=self.enabled,
            action_on_hit=_normalize_action(policy.get("action"), self.action_on_hit),
            block_message=_coerce_string(policy.get("blockMessage"), self.block_message),
            scanner_configs=scanner_configs,
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
        for scanner_config in self.scanner_configs:
            scanner = _build_scanner(scanner_config, self.action_on_hit)
            if scanner is not None:
                scanners.append(scanner)

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


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]


def _coerce_policy_scanner_configs(value: Any, policy_path: str) -> list[dict[str, Any]]:
    if value is None:
        return []
    if not isinstance(value, list):
        logger.warning("output_guardrails_policy_invalid_scanners path=%s", policy_path)
        return []

    scanner_configs: list[dict[str, Any]] = []
    for scanner in value:
        if not isinstance(scanner, dict):
            continue
        scanner_type = _normalize_scanner_key(str(scanner.get("type", "")).strip())
        if not scanner_type:
            continue

        normalized_config = {"type": scanner_type}
        for key, item in scanner.items():
            if key == "type":
                continue
            normalized_config[_normalize_scanner_key(str(key))] = item
        scanner_configs.append(normalized_config)

    return scanner_configs


def _build_scanner(scanner_config: dict[str, Any], action_on_hit: str) -> Any | None:
    scanner_type = str(scanner_config.get("type", "")).lower()
    class_name = SUPPORTED_POLICY_SCANNERS.get(scanner_type)
    if not class_name:
        logger.warning("output_guardrails_policy_unknown_scanner type=%s", scanner_type)
        return None

    scanner_class = getattr(llm_guard_output_scanners, class_name, None)
    if scanner_class is None:
        logger.warning("output_guardrails_policy_unavailable_scanner type=%s", scanner_type)
        return None

    kwargs = _build_scanner_kwargs(scanner_class, scanner_config, action_on_hit)
    if kwargs is None:
        return None

    try:
        return scanner_class(**kwargs)
    except Exception:
        logger.exception(
            "output_guardrails_policy_scanner_build_failed type=%s",
            scanner_type,
        )
        return None


def _build_scanner_kwargs(
    scanner_class: type[Any],
    scanner_config: dict[str, Any],
    action_on_hit: str,
) -> dict[str, Any] | None:
    signature = inspect.signature(scanner_class)
    kwargs: dict[str, Any] = {}
    config_values = {key: value for key, value in scanner_config.items() if key != "type"}

    for name, parameter in signature.parameters.items():
        if parameter.kind not in (
            inspect.Parameter.POSITIONAL_OR_KEYWORD,
            inspect.Parameter.KEYWORD_ONLY,
        ):
            continue

        if name in config_values:
            kwargs[name] = config_values[name]
            continue

        if name == "redact":
            kwargs[name] = action_on_hit == "redact"
            continue

        if name == "patterns" and scanner_config.get("type") == "regex":
            kwargs[name] = _coerce_string_list(scanner_config.get("patterns"))
            continue

        if name == "substrings" and scanner_config.get("type") == "ban_substrings":
            kwargs[name] = _coerce_string_list(scanner_config.get("substrings"))
            continue

        if parameter.default is inspect.Signature.empty:
            logger.warning(
                "output_guardrails_policy_invalid_scanner_config type=%s missing=%s",
                scanner_config.get("type"),
                name,
            )
            return None

    return kwargs


def _normalize_scanner_key(value: str) -> str:
    value = value.replace("-", "_")
    return re.sub(r"(?<!^)(?=[A-Z])", "_", value).lower()


def _coerce_string(value: Any, fallback: str) -> str:
    if isinstance(value, str) and value:
        return value
    return fallback


def _normalize_action(value: Any, fallback: str) -> str:
    if not isinstance(value, str) or not value:
        return fallback

    action = value.lower()
    if action in {"block", "redact"}:
        return action

    logger.warning("output_guardrails_policy_invalid_action action=%s", value)
    return fallback
