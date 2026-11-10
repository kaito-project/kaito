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

# Wait for every DaemonSet in a namespace to finish rolling out, so that
# crash-looping subchart components (e.g. local-csi-driver) fail CI instead of
# being hidden behind a healthy controller Deployment.
#
# Usage: ./wait-for-daemonsets.sh <namespace> [timeout] [settle-seconds]
#   timeout:        per-DaemonSet rollout timeout (default: 600s)
#   settle-seconds: delay before re-checking readiness, to catch containers
#                   that crash shortly after starting (default: 30)

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <namespace> [timeout] [settle-seconds]" >&2
  exit 1
fi

namespace="$1"
timeout="${2:-600s}"
settle="${3:-30}"

mapfile -t daemonsets < <(kubectl get daemonset -n "${namespace}" -o name)
if [[ ${#daemonsets[@]} -eq 0 ]]; then
  echo "No DaemonSets found in namespace ${namespace}."
  exit 0
fi

report() {
  local ds="$1" labels
  echo "::error::${ds} in ${namespace} is not ready" >&2
  # shellcheck disable=SC2016 # go-template variables, not shell expansions
  labels="$(kubectl get "${ds}" -n "${namespace}" -o go-template='{{range $k, $v := .spec.selector.matchLabels}}{{$k}}={{$v}},{{end}}')"
  kubectl get pods -n "${namespace}" -l "${labels%,}" -o wide || true
  kubectl logs -n "${namespace}" -l "${labels%,}" --all-containers --tail=50 --prefix || true
}

failed=0
pending=()
for ds in "${daemonsets[@]}"; do
  echo "Waiting for ${ds} in ${namespace}..."
  if kubectl rollout status "${ds}" -n "${namespace}" --timeout="${timeout}"; then
    pending+=("${ds}")
  else
    failed=1
    report "${ds}"
  fi
done

# A rollout counts a pod as available as soon as it is running, so a container
# without a readiness probe that exits right after start can still pass.
if [[ ${#pending[@]} -gt 0 && "${settle}" -gt 0 ]]; then
  echo "Re-checking readiness after ${settle}s..."
  sleep "${settle}"
  for ds in "${pending[@]}"; do
    read -r desired ready < <(kubectl get "${ds}" -n "${namespace}" \
      -o jsonpath='{.status.desiredNumberScheduled} {.status.numberReady}{"\n"}')
    if [[ "${ready:-0}" != "${desired}" ]]; then
      echo "${ds}: ${ready:-0}/${desired} pods ready" >&2
      failed=1
      report "${ds}"
    fi
  done
fi

exit "${failed}"
