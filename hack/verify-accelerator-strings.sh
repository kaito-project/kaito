#!/bin/bash

# Copyright KAITO authors.
#
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

# Fail when an accelerator vendor's resource or label domain (nvidia.com)
# appears in non-test Go code outside the accelerator packages. Use the
# drivers in internal/accelerator or the constants in pkg/accelerator instead.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

matches=$(git grep -n -I --untracked 'nvidia\.com' -- '*.go' \
	':!*_test.go' \
	':!pkg/accelerator/' \
	':!internal/accelerator/' || true)

if [[ -n "${matches}" ]]; then
	echo "nvidia.com must only appear in pkg/accelerator/ and internal/accelerator/:" >&2
	echo "${matches}" >&2
	exit 1
fi
