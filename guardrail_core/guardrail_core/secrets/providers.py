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

"""Provider-specific secret detection."""

from guardrail_core.secrets.patterns import (
    GCP_API_KEY_PATTERNS,
    GITHUB_TOKEN_PATTERNS,
    OPENAI_API_KEY_PATTERNS,
)

KAITO_PROVIDER_PATTERNS = (
    *GITHUB_TOKEN_PATTERNS,
    *GCP_API_KEY_PATTERNS,
    *OPENAI_API_KEY_PATTERNS,
)

BUILTIN_SECRET_TYPES_OVERRIDDEN_BY_KAITO = frozenset({"GitHub Token"})


def detect_kaito_provider_secrets(text: str) -> set[str]:
    """Return secrets matched by KAITO-specific provider patterns."""
    return {
        match.group(0)
        for pattern in KAITO_PROVIDER_PATTERNS
        for match in pattern.finditer(text)
    }
