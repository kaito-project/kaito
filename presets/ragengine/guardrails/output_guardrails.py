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

import logging
import re
from dataclasses import dataclass, field
from typing import Any

import yaml
from llm_guard import scan_output

from ragengine import config
from ragengine.guardrails.scanner_schemas import (
    SCANNER_REGISTRY,
    ParsedScannerConfig,
)
from ragengine.models import ChatCompletionResponse, get_message_content

logger = logging.getLogger(__name__)

DEFAULT_BLOCK_MESSAGE = "The model output was blocked by output guardrails."
DEFAULT_ACTION_ON_HIT = "redact"


class OutputGuardrailsError(RuntimeError):
    pass


@dataclass
class OutputGuardrails:
    enabled: bool
    fail_open: bool = True
    action_on_hit: str = DEFAULT_ACTION_ON_HIT
    block_message: str = DEFAULT_BLOCK_MESSAGE
    scanner_configs: list[ParsedScannerConfig] = field(default_factory=list)

    @classmethod
    def from_config(cls) -> "OutputGuardrails":
        # Guardrails currently hard-code fail-closed behavior; there is no
        # runtime API or env override for fail-open.
        fail_open = False
        # Skip policy I/O when disabled so a malformed policy stays silent.
        if not config.OUTPUT_GUARDRAILS_ENABLED:
            return cls(enabled=False, fail_open=fail_open)

        guardrails = cls(enabled=True, fail_open=fail_open)
        return guardrails._apply_policy_file(config.OUTPUT_GUARDRAILS_POLICY_PATH)

    def _apply_policy_file(self, policy_path: str) -> "OutputGuardrails":
        if not policy_path:
            # Guardrails-enabled deployments should provide a policy path.
            # An empty path currently falls back to fail-open.
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

        default_action_on_hit = _normalize_action(
            policy.get("action"), self.action_on_hit
        )
        scanner_configs = list(self.scanner_configs)
        if "scanners" in policy:
            scanner_configs = _parse_policy_scanner_configs(
                policy.get("scanners"),
                policy_path,
                default_action_on_hit,
            )

        return OutputGuardrails(
            enabled=self.enabled,
            fail_open=self.fail_open,
            action_on_hit=default_action_on_hit,
            block_message=_coerce_string(
                policy.get("blockMessage"), self.block_message
            ),
            scanner_configs=scanner_configs,
        )

    def guard_response(
        self,
        response: ChatCompletionResponse,
        request: dict[str, Any],
    ) -> ChatCompletionResponse:
        if not self.enabled:
            return response

        try:
            built_scanners = self._build_scanners_with_configs()
            if not built_scanners:
                return response

            prompt = self._extract_prompt(request)
            response_data = response.model_dump(mode="python")

            for choice in response_data.get("choices", []):
                message = choice.get("message") or {}
                content = message.get("content")
                if message.get("role") != "assistant" or not isinstance(content, str):
                    continue

                sanitized_output = content
                final_action = None
                triggered_scanners: list[dict[str, Any]] = []
                for parsed, scanner in built_scanners:
                    scanner_action_on_hit = parsed.action_on_hit or self.action_on_hit
                    sanitized_output, results_valid, results_score = scan_output(
                        [scanner], prompt, sanitized_output, fail_fast=False
                    )
                    if all(results_valid.values()):
                        continue

                    triggered_scanners.append(
                        {
                            "type": parsed.type,
                            "action": scanner_action_on_hit,
                            "scores": results_score,
                        }
                    )
                    if scanner_action_on_hit == "block":
                        final_action = "block"
                        break

                    final_action = "redact"

                if not triggered_scanners:
                    continue

                if final_action == "block":
                    message["content"] = self.block_message
                else:
                    message["content"] = sanitized_output

                logger.info(
                    "output_guardrails_triggered action=%s response_id=%s scanners=%s",
                    final_action,
                    response.id,
                    triggered_scanners,
                )

            return ChatCompletionResponse(**response_data)
        except Exception as exc:
            logger.exception(
                "output_guardrails_failed fail_open=%s response_id=%s",
                self.fail_open,
                response.id,
            )
            if self.fail_open:
                return response
            raise OutputGuardrailsError(
                "Output guardrails failed while scanning the model response."
            ) from exc

    def _build_scanners(self) -> list[Any]:
        return [scanner for _, scanner in self._build_scanners_with_configs()]

    def _build_scanners_with_configs(self) -> list[tuple[ParsedScannerConfig, Any]]:
        scanners: list[Any] = []
        for parsed in self.scanner_configs:
            try:
                scanner_action_on_hit = parsed.action_on_hit or self.action_on_hit
                scanners.append((parsed, parsed.config.build(scanner_action_on_hit)))
            except Exception:
                logger.exception(
                    "output_guardrails_policy_scanner_build_failed type=%s",
                    parsed.type,
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


def _parse_policy_scanner_configs(
    value: Any, policy_path: str, default_action_on_hit: str = DEFAULT_ACTION_ON_HIT
) -> list[ParsedScannerConfig]:
    if value is None:
        return []
    if not isinstance(value, list):
        logger.warning("output_guardrails_policy_invalid_scanners path=%s", policy_path)
        return []

    parsed_configs: list[ParsedScannerConfig] = []
    for raw in value:
        if not isinstance(raw, dict):
            continue

        scanner_type = _normalize_scanner_key(str(raw.get("type", "")).strip())
        if not scanner_type:
            continue

        schema_cls = SCANNER_REGISTRY.get(scanner_type)
        if schema_cls is None:
            logger.warning(
                "output_guardrails_policy_unknown_scanner type=%s", scanner_type
            )
            continue

        scanner_action_on_hit = _normalize_action(
            raw.get("action"), default_action_on_hit
        )

        normalized_raw = {
            _normalize_scanner_key(str(key)): item
            for key, item in raw.items()
            if key not in {"type", "action"}
        }
        try:
            cfg = schema_cls.from_dict(normalized_raw)
        except (TypeError, ValueError) as e:
            logger.warning(
                "output_guardrails_policy_invalid_scanner_config type=%s error=%s",
                scanner_type,
                e,
            )
            continue

        parsed_configs.append(
            ParsedScannerConfig(
                type=scanner_type,
                action_on_hit=scanner_action_on_hit,
                config=cfg,
            )
        )

    return parsed_configs


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
