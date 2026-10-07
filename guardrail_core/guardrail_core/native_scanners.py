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

"""KAITO-owned deterministic scanners for output guardrails."""

import json


class NativeInvisibleTextScanner:
    """KAITO-owned scanner for detecting invisible/non-printable Unicode characters."""

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        del prompt
        if output.strip() == "":
            return output, True, -1.0

        # Detect invisible characters (common Unicode categories for hidden text)
        invisible_chars = []
        for i, char in enumerate(output):
            # Detect zero-width chars, directional marks, variation selectors
            if char in (
                "​",
                "‌",
                "‍",
                "‎",
                "‏",
                "﻿",
                "‪",
                "‫",
                "‬",
                "‭",
                "‮",
                "؜",
                "᠎",
                "⁤",
                "⁦",
                "⁧",
                "⁨",
                "⁩",
            ):
                invisible_chars.append(i)

        if invisible_chars:
            # Remove invisible characters
            sanitized = "".join(
                c for i, c in enumerate(output) if i not in invisible_chars
            )
            return sanitized, False, 1.0

        return output, True, -1.0


class NativeJSONScanner:
    """KAITO-owned JSON validator and optional repairer."""

    def __init__(self, required_elements: int = 0, repair: bool = True) -> None:
        self.required_elements = required_elements
        self.repair = repair

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        del prompt
        if output.strip() == "":
            return output, True, -1.0

        try:
            obj = json.loads(output)
            # Check required elements if applicable
            if (
                self.required_elements > 0
                and isinstance(obj, dict)
                and len(obj) < self.required_elements
            ):
                return output, False, 0.5
            return output, True, -1.0
        except json.JSONDecodeError:
            if self.repair:
                # Attempt simple repair: try wrapping in object or array
                try:
                    json.loads(f"[{output}]")
                    return f"[{output}]", False, 0.5
                except json.JSONDecodeError:
                    pass
                try:
                    json.loads("{" + output + "}")
                    return "{" + output + "}", False, 0.5
                except json.JSONDecodeError:
                    pass
                # If repair attempts fail, keep the original output with is_valid=True
                return output, True, -1.0
            return output, False, 1.0


class NativeReadingTimeScanner:
    """KAITO-owned reading time limiter based on word count."""

    def __init__(self, max_time: float = 5.0, truncate: bool = False) -> None:
        self.max_time = max_time  # in minutes
        self.truncate = truncate

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        del prompt
        if output.strip() == "":
            return output, True, -1.0

        # Average reading speed: 200 words per minute
        words = len(output.split())
        reading_time_minutes = words / 200.0

        if reading_time_minutes > self.max_time:
            if self.truncate:
                # Truncate to fit within max_time
                max_words = int(self.max_time * 200)
                truncated = " ".join(output.split()[:max_words])
                return truncated, False, 1.0
            else:
                return output, False, reading_time_minutes / self.max_time

        return output, True, -1.0


class NativeTokenLimitScanner:
    """KAITO-owned token counter using tiktoken."""

    def __init__(
        self,
        limit: int = 4096,
        encoding_name: str = "cl100k_base",
        model_name: str | None = None,
    ) -> None:
        self.limit = limit
        self.encoding_name = encoding_name
        self.model_name = model_name
        try:
            import tiktoken

            self.encoding = tiktoken.get_encoding(encoding_name)
        except Exception:
            # Fallback: rough estimate (1 token ≈ 4 chars)
            self.encoding = None

    def scan(self, prompt: str, output: str) -> tuple[str, bool, float]:
        del prompt
        if output.strip() == "":
            return output, True, -1.0

        if self.encoding:
            tokens = self.encoding.encode(output)
            token_count = len(tokens)
        else:
            # Fallback estimation
            token_count = len(output) // 4
            tokens = None

        if token_count > self.limit:
            # Truncate to fit within limit
            if self.encoding and tokens:
                truncated_tokens = tokens[: self.limit]
                truncated_output = self.encoding.decode(truncated_tokens)
            else:
                # Fallback: estimate based on character count
                truncated_output = output[: self.limit * 4]
            return truncated_output, False, 1.0

        return output, True, -1.0
