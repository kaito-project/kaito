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

"""KAITO-owned secrets scanner."""

import hashlib
import os
import tempfile
import threading

from detect_secrets.core.secrets_collection import SecretsCollection
from detect_secrets.settings import default_settings

from guardrail_core.scanning import ScanResult
from guardrail_core.secrets.providers import (
    BUILTIN_SECRET_TYPES_OVERRIDDEN_BY_KAITO,
    detect_kaito_provider_secrets,
)

# detect-secrets settings are process-global, so scans must be serialized.
_DETECT_SECRETS_SETTINGS_LOCK = threading.Lock()


class NativeSecretsScanner:
    """Detect and redact secrets using detect-secrets and KAITO patterns."""

    def __init__(self, redact_mode: str = "all") -> None:
        self._redact_mode = redact_mode

    def scan(self, prompt: str, output: str) -> ScanResult:
        del prompt
        if output.strip() == "":
            return ScanResult(output, True, -1.0)

        secret_values = self._detect_secret_values(output)
        if not secret_values:
            return ScanResult(output, True, -1.0)

        sanitized_output = output
        for secret_value in sorted(
            secret_values,
            key=lambda value: (-len(value), value),
        ):
            replacement = self._redact_value(secret_value, self._redact_mode)
            sanitized_output = sanitized_output.replace(secret_value, replacement)

        return ScanResult(sanitized_output, False, 1.0)

    @staticmethod
    def _redact_value(value: str, redact_mode: str) -> str:
        if redact_mode == "all":
            return "******"
        if redact_mode == "partial":
            return f"{value[:2]}..{value[-2:]}"
        if redact_mode == "hash":
            return hashlib.md5(value.encode()).hexdigest()
        raise ValueError(f"redact mode wasn't recognized {redact_mode}")

    def _detect_secret_values(self, text: str) -> set[str]:
        secrets = SecretsCollection()

        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            delete=False,
        ) as temp_file:
            temp_file.write(text)
            temp_path = temp_file.name

        try:
            with _DETECT_SECRETS_SETTINGS_LOCK, default_settings():
                secrets.scan_file(temp_path)
        finally:
            os.remove(temp_path)

        secret_values = {
            found_secret.secret_value
            for file_path in secrets.files
            for found_secret in secrets[file_path]
            if (
                found_secret.secret_value
                and found_secret.type not in BUILTIN_SECRET_TYPES_OVERRIDDEN_BY_KAITO
            )
        }
        secret_values.update(detect_kaito_provider_secrets(text))
        return secret_values
