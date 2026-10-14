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

"""KAITO-specific secret patterns."""

import re

_TOKEN_END = r"(?=['\"\n\r\s`;]|$)"
_GITHUB_TOKEN_END = r"(?![0-9A-Za-z_])"

GITHUB_TOKEN_PATTERNS = (
    re.compile(rf"(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{{36}}{_GITHUB_TOKEN_END}"),
    re.compile(rf"github_pat_[0-9A-Za-z_]{{82}}{_GITHUB_TOKEN_END}"),
)

GCP_API_KEY_PATTERNS = (
    re.compile(rf"\bAIza[0-9A-Za-z_-]{{35}}{_TOKEN_END}", re.IGNORECASE),
)

OPENAI_API_KEY_PATTERNS = (
    re.compile(
        rf"\bsk-[A-Za-z0-9]{{20}}T3BlbkFJ[A-Za-z0-9]{{20}}{_TOKEN_END}",
        re.IGNORECASE,
    ),
    re.compile(
        rf"\bsk-(?:proj|svcacct)-[A-Za-z0-9_-]{{80,}}{_TOKEN_END}",
        re.IGNORECASE,
    ),
)
