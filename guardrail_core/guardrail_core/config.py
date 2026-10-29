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

"""Shared scanner configuration, extracted from RAGEngine's scanner schemas."""

import re
from collections.abc import Mapping
from dataclasses import dataclass, fields
from typing import ClassVar

from guardrail_core._sensitive_scanner import (
    _DEFAULT_SENSITIVE_DETECTORS,
    _SENSITIVE_DETECTORS,
    _PatternPIIScanner,
)
from guardrail_core.native_scanners import BanSubstringsMatchType, RegexMatchType
from guardrail_core.scanning import Scanner

_BAN_SUBSTRINGS_MATCH_TYPES = frozenset(("word", "str"))
_REGEX_MATCH_TYPES = frozenset(("search", "fullmatch", "all"))
_SECRETS_REDACT_MODES = frozenset({"all", "partial", "hash"})


@dataclass
class SecretsConfig:
    supports_redact: ClassVar[bool] = True
    redact_mode: str = "all"

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "SecretsConfig":
        redact_mode = str(raw.get("redact_mode", "all")).lower()
        if redact_mode not in _SECRETS_REDACT_MODES:
            raise ValueError(
                f"secrets 'redact_mode' must be one of "
                f"{sorted(_SECRETS_REDACT_MODES)}, got {redact_mode!r}"
            )
        return cls(redact_mode=redact_mode)

    def build(self, action_on_hit: str) -> Scanner:
        del action_on_hit
        from guardrail_core.secrets import NativeSecretsScanner

        return NativeSecretsScanner(redact_mode=self.redact_mode)


@dataclass
class SensitiveConfig:
    supports_redact: ClassVar[bool] = True
    detectors: list[str]

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "SensitiveConfig":
        detectors_value = raw.get("detectors")
        if detectors_value is None:
            return cls(detectors=list(_DEFAULT_SENSITIVE_DETECTORS))
        detectors = [item.lower() for item in _coerce_string_list(detectors_value)]
        if not detectors:
            raise ValueError(
                "sensitive requires 'detectors' to be a non-empty list of strings"
            )
        invalid_detectors = [
            detector for detector in detectors if detector not in _SENSITIVE_DETECTORS
        ]
        if invalid_detectors:
            raise ValueError(
                f"sensitive 'detectors' must be drawn from "
                f"{sorted(_SENSITIVE_DETECTORS)}, got {invalid_detectors!r}"
            )
        return cls(detectors=_dedupe_strings(detectors))

    def build(self, action_on_hit: str) -> Scanner:
        return _PatternPIIScanner(
            detectors=self.detectors, redact=(action_on_hit == "redact")
        )


@dataclass
class BanSubstringsConfig:
    supports_redact: ClassVar[bool] = True
    substrings: list[str]
    match_type: BanSubstringsMatchType = BanSubstringsMatchType.WORD
    case_sensitive: bool = False
    contains_all: bool = False

    def __post_init__(self) -> None:
        if self.match_type is None or isinstance(self.match_type, str):
            from guardrail_core.native_scanners import BanSubstringsMatchType

            if self.match_type is None:
                self.match_type = BanSubstringsMatchType.WORD
            elif isinstance(self.match_type, str):
                self.match_type = BanSubstringsMatchType(self.match_type)

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "BanSubstringsConfig":
        from guardrail_core.native_scanners import BanSubstringsMatchType

        substrings = _coerce_string_list(raw.get("substrings"))
        if not substrings:
            raise ValueError(
                "ban_substrings requires 'substrings' to be a non-empty list of strings"
            )
        match_type_str = str(raw.get("match_type", "word")).lower()
        if match_type_str not in _BAN_SUBSTRINGS_MATCH_TYPES:
            raise ValueError(
                f"ban_substrings 'match_type' must be one of "
                f"{sorted(_BAN_SUBSTRINGS_MATCH_TYPES)}, got {match_type_str!r}"
            )
        return cls(
            substrings=substrings,
            match_type=BanSubstringsMatchType(match_type_str),
            case_sensitive=_coerce_bool(
                raw.get("case_sensitive"), False, field="case_sensitive"
            ),
            contains_all=_coerce_bool(
                raw.get("contains_all"), False, field="contains_all"
            ),
        )

    def build(self, action_on_hit: str) -> Scanner:
        from guardrail_core.native_scanners import NativeBanSubstringsScanner

        return NativeBanSubstringsScanner(
            substrings=list(self.substrings),
            match_type=self.match_type,
            case_sensitive=self.case_sensitive,
            contains_all=self.contains_all,
            redact=(action_on_hit == "redact"),
        )


@dataclass
class RegexConfig:
    supports_redact: ClassVar[bool] = True
    patterns: list[str]
    is_blocked: bool = True
    match_type: RegexMatchType = RegexMatchType.SEARCH

    def __post_init__(self) -> None:
        if self.match_type is None or isinstance(self.match_type, str):
            from guardrail_core.native_scanners import RegexMatchType

            if self.match_type is None:
                self.match_type = RegexMatchType.SEARCH
            elif isinstance(self.match_type, str):
                self.match_type = RegexMatchType(self.match_type)

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "RegexConfig":
        from guardrail_core.native_scanners import RegexMatchType

        patterns = _coerce_string_list(raw.get("patterns"))
        if not patterns:
            raise ValueError(
                "regex requires 'patterns' to be a non-empty list of strings"
            )
        for pattern in patterns:
            try:
                re.compile(pattern)
            except re.error as exc:
                raise ValueError(
                    f"regex pattern {pattern!r} is not a valid regular expression: {exc}"
                ) from exc
        match_type_str = str(raw.get("match_type", "search")).lower()
        if match_type_str not in _REGEX_MATCH_TYPES:
            raise ValueError(
                f"regex 'match_type' must be one of "
                f"{sorted(_REGEX_MATCH_TYPES)}, got {match_type_str!r}"
            )
        return cls(
            patterns=patterns,
            is_blocked=_coerce_bool(raw.get("is_blocked"), True, field="is_blocked"),
            match_type=RegexMatchType(match_type_str),
        )

    def build(self, action_on_hit: str) -> Scanner:
        from guardrail_core.native_scanners import NativeRegexScanner

        return NativeRegexScanner(
            patterns=list(self.patterns),
            is_blocked=self.is_blocked,
            match_type=self.match_type,
            redact=(action_on_hit == "redact"),
        )


@dataclass
class InvisibleTextConfig:
    supports_redact: ClassVar[bool] = True

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "InvisibleTextConfig":
        if raw:
            raise ValueError("invisible_text does not accept any configuration fields")
        return cls()

    def build(self, action_on_hit: str) -> Scanner:
        from guardrail_core.native_scanners import NativeInvisibleTextScanner

        return NativeInvisibleTextScanner()


@dataclass
class TokenLimitConfig:
    supports_redact: ClassVar[bool] = True
    limit: int = 4096
    encoding_name: str = "cl100k_base"
    model_name: str | None = None

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "TokenLimitConfig":
        limit = raw.get("limit", 4096)
        if not isinstance(limit, int) or limit <= 0:
            raise ValueError("token_limit 'limit' must be a positive integer")
        encoding_name = raw.get("encoding_name", "cl100k_base")
        if not isinstance(encoding_name, str) or not encoding_name:
            raise ValueError("token_limit 'encoding_name' must be a non-empty string")
        model_name = raw.get("model_name")
        if model_name is not None and (
            not isinstance(model_name, str) or not model_name
        ):
            raise ValueError(
                "token_limit 'model_name' must be a non-empty string when set"
            )
        return cls(limit=limit, encoding_name=encoding_name, model_name=model_name)

    def build(self, action_on_hit: str) -> Scanner:
        from guardrail_core.native_scanners import NativeTokenLimitScanner

        return NativeTokenLimitScanner(
            limit=self.limit,
            encoding_name=self.encoding_name,
            model_name=self.model_name,
        )


@dataclass
class JSONConfig:
    supports_redact: ClassVar[bool] = True
    required_elements: int = 0
    repair: bool = True

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "JSONConfig":
        required_elements = raw.get("required_elements", 0)
        if not isinstance(required_elements, int) or required_elements < 0:
            raise ValueError("json 'required_elements' must be a non-negative integer")
        repair = _coerce_bool(raw.get("repair"), True, field="repair")
        return cls(required_elements=required_elements, repair=repair)

    def build(self, action_on_hit: str) -> Scanner:
        from guardrail_core.native_scanners import NativeJSONScanner

        return NativeJSONScanner(
            required_elements=self.required_elements, repair=self.repair
        )


@dataclass
class ReadingTimeConfig:
    supports_redact: ClassVar[bool] = True
    max_time: float
    truncate: bool = False

    @classmethod
    def from_dict(cls, raw: Mapping[str, object]) -> "ReadingTimeConfig":
        max_time = raw.get("max_time")
        if not (isinstance(max_time, (int, float)) and max_time > 0):
            raise ValueError(
                "reading_time 'max_time' must be a positive number (minutes)"
            )
        truncate = _coerce_bool(raw.get("truncate"), False, field="truncate")
        return cls(max_time=float(max_time), truncate=truncate)

    def build(self, action_on_hit: str) -> Scanner:
        from guardrail_core.native_scanners import NativeReadingTimeScanner

        return NativeReadingTimeScanner(max_time=self.max_time, truncate=self.truncate)


type ScannerConfig = (
    BanSubstringsConfig
    | InvisibleTextConfig
    | RegexConfig
    | JSONConfig
    | ReadingTimeConfig
    | TokenLimitConfig
    | SecretsConfig
    | SensitiveConfig
)

SCANNER_REGISTRY: dict[str, type[ScannerConfig]] = {
    "ban_substrings": BanSubstringsConfig,
    "invisible_text": InvisibleTextConfig,
    "regex": RegexConfig,
    "json": JSONConfig,
    "reading_time": ReadingTimeConfig,
    "token_limit": TokenLimitConfig,
    "secrets": SecretsConfig,
    "sensitive": SensitiveConfig,
}


@dataclass
class ParsedScannerConfig:
    """A scanner config that has already passed schema validation."""

    type: str
    config: ScannerConfig
    action_on_hit: str | None = None


def parse_scanner_config(
    raw: Mapping[str, object], *, default_action: str = "block"
) -> ParsedScannerConfig:
    """Validate one flattened scanner entry; never skip invalid configuration."""
    _validate_action(default_action)
    if not isinstance(raw, Mapping):
        raise ValueError("scanner configuration must be a mapping")
    scanner_type = raw.get("type")
    if not isinstance(scanner_type, str) or not scanner_type.strip():
        raise ValueError("scanner 'type' must be a non-empty string")
    scanner_type = _normalize_key(scanner_type.strip())
    schema = SCANNER_REGISTRY.get(scanner_type)
    if schema is None:
        raise ValueError(f"unknown scanner type: {scanner_type!r}")
    action = _validate_action(raw.get("action", default_action))
    if action == "redact" and not schema.supports_redact:
        raise ValueError(f"scanner {scanner_type!r} does not support redaction")
    config: dict[str, object] = {}
    allowed_fields = {field.name for field in fields(schema)}
    for key, value in raw.items():
        if key in {"type", "action"}:
            continue
        if not isinstance(key, str):
            raise ValueError("scanner configuration keys must be strings")
        normalized = _normalize_key(key)
        if normalized not in allowed_fields:
            raise ValueError(f"unknown field for scanner {scanner_type!r}: {key!r}")
        if normalized in config:
            raise ValueError(f"duplicate scanner configuration field: {normalized!r}")
        if value is None and normalized != "model_name":
            raise ValueError(f"scanner field {key!r} must not be null")
        if isinstance(value, list) and (
            not value or any(not isinstance(item, str) or not item for item in value)
        ):
            raise ValueError(f"scanner field {key!r} must contain non-empty strings")
        if isinstance(value, bool) and normalized in {
            "limit",
            "required_elements",
            "max_time",
        }:
            raise ValueError(f"scanner field {key!r} must be a number, not a boolean")
        config[normalized] = value
    return ParsedScannerConfig(
        type=scanner_type, config=schema.from_dict(config), action_on_hit=action
    )


def build_scanner(
    config: ParsedScannerConfig, *, default_action: str = "block"
) -> Scanner:
    """Build a shared scanner; construction errors propagate to the caller."""
    _validate_action(default_action)
    action = _validate_action(
        default_action if config.action_on_hit is None else config.action_on_hit
    )
    if action == "redact" and not config.config.supports_redact:
        raise ValueError(f"scanner {config.type!r} does not support redaction")
    return config.config.build(action)


def _validate_action(value: object) -> str:
    if not isinstance(value, str) or value not in {"block", "redact"}:
        raise ValueError("scanner action must be 'block' or 'redact'")
    return value


def _normalize_key(value: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", value.replace("-", "_")).lower()


def _coerce_string_list(value: object) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]


def _coerce_bool(value: object, fallback: bool, *, field: str) -> bool:
    if value is None:
        return fallback
    if isinstance(value, bool):
        return value
    raise ValueError(f"{field!r} must be a boolean (true/false), got {value!r}")


def _dedupe_strings(values: list[str]) -> list[str]:
    return list(dict.fromkeys(values))
