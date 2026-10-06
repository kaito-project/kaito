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

# Bump a Helm chart's subchart dependencies to their latest stable versions,
# then refresh Chart.lock and the vendored tarballs under charts/.
#
# Usage: ./bump-chart-deps.sh <chart-dir> [dependency-name...]
#
# With no dependency names, every dependency in <chart-dir>/Chart.yaml is
# checked. Dependencies are never downgraded and prerelease versions are
# ignored. When running in GitHub Actions, "old_version" and "new_version"
# are written to $GITHUB_OUTPUT if exactly one dependency was bumped.
#
# Requires: helm (v3.8+), yq (v4).

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <chart-dir> [dependency-name...]" >&2
  exit 1
fi

chart_dir="$1"
shift
chart_yaml="${chart_dir}/Chart.yaml"

if [[ ! -f "${chart_yaml}" ]]; then
  echo "Chart.yaml not found: ${chart_yaml}" >&2
  exit 1
fi

if [[ $# -gt 0 ]]; then
  deps=("$@")
else
  mapfile -t deps < <(yq '.dependencies[].name' "${chart_yaml}")
fi

semver_re='^[0-9]+\.[0-9]+\.[0-9]+$'
bumped=0
old_version=""
new_version=""

latest_version() {
  local name="$1" repo="$2"
  if [[ "${repo}" == oci://* ]]; then
    helm show chart "${repo%/}/${name}"
  else
    helm show chart "${name}" --repo "${repo}"
  fi 2>/dev/null | yq '.version'
}

for dep in "${deps[@]}"; do
  export DEP="${dep}"
  if [[ "$(yq '[.dependencies[] | select(.name == strenv(DEP))] | length' "${chart_yaml}")" != "1" ]]; then
    echo "Dependency ${dep} not found in ${chart_yaml}" >&2
    exit 1
  fi
  repo="$(yq '.dependencies[] | select(.name == strenv(DEP)) | .repository' "${chart_yaml}")"
  current="$(yq '.dependencies[] | select(.name == strenv(DEP)) | .version' "${chart_yaml}")"

  if ! latest="$(latest_version "${dep}" "${repo}")" || [[ -z "${latest}" ]]; then
    echo "Failed to query latest version of ${dep} from ${repo}" >&2
    exit 1
  fi

  if [[ ! "${current}" =~ ${semver_re} ]]; then
    echo "::warning::${dep}: current version '${current}' is not a pinned X.Y.Z version; skipping"
    continue
  fi
  if [[ ! "${latest}" =~ ${semver_re} ]]; then
    echo "::warning::${dep}: latest version '${latest}' is not a stable X.Y.Z version; skipping"
    continue
  fi
  if [[ "${latest}" == "${current}" ]] ||
    [[ "$(printf '%s\n%s\n' "${current}" "${latest}" | sort -V | tail -n1)" == "${current}" ]]; then
    echo "${dep}: ${current} is up to date (latest ${latest})"
    continue
  fi

  echo "${dep}: bumping ${current} -> ${latest}"
  # Edit only the version line; yq -i would reformat the whole file.
  line="$(yq '.dependencies[] | select(.name == strenv(DEP)) | .version | line' "${chart_yaml}")"
  sed -i "${line}s/${current//./\\.}/${latest}/" "${chart_yaml}"
  if [[ "$(yq '.dependencies[] | select(.name == strenv(DEP)) | .version' "${chart_yaml}")" != "${latest}" ]]; then
    echo "Failed to update ${dep} version in ${chart_yaml}" >&2
    exit 1
  fi
  rm -f "${chart_dir}/charts/${dep}-${current}.tgz"
  bumped=$((bumped + 1))
  old_version="${current}"
  new_version="${latest}"
done

if [[ "${bumped}" -eq 0 ]]; then
  echo "No dependency updates."
  exit 0
fi

helm dependency update "${chart_dir}"

if [[ -n "${GITHUB_OUTPUT:-}" && "${bumped}" -eq 1 ]]; then
  {
    echo "old_version=${old_version}"
    echo "new_version=${new_version}"
  } >> "${GITHUB_OUTPUT}"
fi
