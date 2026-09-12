#!/usr/bin/env bash

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

# Deploys every preset model targeting a single GPU pool, one at a time, on the
# currently selected Kubernetes cluster and records a machine-readable result per
# model. A model passes the same checks as test/e2e/preset_vllm_test.go:
# WorkspaceSucceeded, BenchmarkCompleted with a valid peakTokensPerMinute metric,
# a /v1/models response carrying the model id, and a working /v1/chat/completions.
# Never aborts on a model failure: each model is isolated, torn down, and the run
# continues so the aggregated report covers the whole matrix. The script exits
# non-zero once every model has run if any of them failed.
#
# Driven by .github/workflows/preset-model-regression.yaml.
#
# Required environment:
#   GPU            gpu pool key from the config (a10 | a100 | h100)
# Optional environment:
#   CONFIG_FILE    test matrix json (default .github/preset-regression-models.json)
#   RESULTS_FILE   where the json result array is written
#   ARTIFACT_DIR   directory for per-model diagnostics
#   MODEL_FILTER   comma-separated substrings; only matching models run
#   NAMESPACE      namespace for the Workspaces (default "default")
#   GLOBAL_DEADLINE_EPOCH  unix time after which remaining models are skipped (0 = no budget)
#   POLL_INTERVAL_SECONDS  workspace status poll interval (default 20)
#   ENDPOINT_TIMEOUT_SECONDS  per-endpoint validation budget (default 300)
#   RESOURCE_READY_TIMEOUT_MINUTES  GPU node provisioning budget (default 30)
#   INFERENCE_FAILURE_GRACE_MINUTES  how long a terminal InferenceReady reason must
#                                    persist after ResourceReady before the model is
#                                    failed (default 5)
#   DIAG_LOG_LINES  pod log lines echoed into the job log on failure (default 200)

set -euo pipefail

GPU="${GPU:?GPU must be set (a10 | a100 | h100)}"
CONFIG_FILE="${CONFIG_FILE:-.github/preset-regression-models.json}"
RESULTS_FILE="${RESULTS_FILE:-results-${GPU}.json}"
ARTIFACT_DIR="${ARTIFACT_DIR:-artifacts/${GPU}}"
MODEL_FILTER="${MODEL_FILTER:-}"
NAMESPACE="${NAMESPACE:-default}"
GLOBAL_DEADLINE_EPOCH="${GLOBAL_DEADLINE_EPOCH:-0}"
POLL_INTERVAL_SECONDS="${POLL_INTERVAL_SECONDS:-20}"
ENDPOINT_TIMEOUT_SECONDS="${ENDPOINT_TIMEOUT_SECONDS:-300}"
RESOURCE_READY_TIMEOUT_MINUTES="${RESOURCE_READY_TIMEOUT_MINUTES:-30}"
INFERENCE_FAILURE_GRACE_MINUTES="${INFERENCE_FAILURE_GRACE_MINUTES:-5}"
DIAG_LOG_LINES="${DIAG_LOG_LINES:-200}"

# Exit 137 is a SIGKILL from the startup/liveness probe, which large models hit
# legitimately while weights download, so only the restart count catches those.
MAX_CONTAINER_RESTARTS=3

# Retries for a Workspace apply rejected by an unreachable validating webhook.
APPLY_ATTEMPTS=6

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

mkdir -p "$ARTIFACT_DIR"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }

# ---------------------------------------------------------------------------
# Build the ordered list of targets for this GPU pool.
# ---------------------------------------------------------------------------
targets_file="${WORKDIR}/targets.json"
jq -c --arg gpu "$GPU" '
  .gpuPools[$gpu] as $pool
  | (.defaults.timeoutMinutes // 90) as $defaultTimeout
  | [ .models[] as $m
      | $m.targets[]
      | select(.gpu == $gpu)
      | {
          model: $m.name,
          gpu: $gpu,
          nodes: .nodes,
          gpusPerNode: .gpusPerNode,
          instanceType: ($pool.instanceTypes[(.gpusPerNode | tostring)] // ""),
          timeoutMinutes: (.timeoutMinutes // $m.timeoutMinutes // $defaultTimeout),
          skipReason: (if ($m.skip // false) then ($m.skipReason // "skipped in the test matrix") else "" end)
        } ]
' "$CONFIG_FILE" >"$targets_file"

if [[ -n "$MODEL_FILTER" ]]; then
  jq -c --arg filter "$MODEL_FILTER" '
    ($filter | split(",") | map(ascii_downcase | gsub("^\\s+|\\s+$"; "")) | map(select(length > 0))) as $needles
    | map(select(.model | ascii_downcase | . as $n | any($needles[]; inside($n))))
  ' "$targets_file" >"${targets_file}.filtered"
  mv "${targets_file}.filtered" "$targets_file"
fi

TARGET_COUNT="$(jq 'length' "$targets_file")"
log "GPU pool '${GPU}': ${TARGET_COUNT} target(s) to process."

results_file="${WORKDIR}/results.ndjson"
: >"$results_file"

record() {
  # record <target-json> <status> <reason> <duration> <actual-nodes> <peak-tpm> <artifacts>
  jq -c -n \
    --argjson target "$1" \
    --arg status "$2" \
    --arg reason "$3" \
    --argjson durationSeconds "$4" \
    --arg actualNodes "$5" \
    --arg peakTPM "$6" \
    --arg artifacts "$7" \
    '$target + {
       status: $status,
       reason: $reason,
       durationSeconds: $durationSeconds,
       actualNodes: $actualNodes,
       peakTPM: $peakTPM,
       artifacts: $artifacts
     }' >>"$results_file"
}

slugify() {
  local name="${1##*/}"
  name="$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]' | sed -e 's/[^a-z0-9]/-/g' -e 's/--*/-/g')"
  name="${name:0:40}"
  printf '%s' "${name%-}"
}

condition_of() {
  # condition_of <workspace-json-file> <condition-type> <field>
  jq -r --arg t "$2" --arg f "$3" \
    '(.status.conditions // []) | map(select(.type == $t)) | (.[0][$f] // "")' "$1"
}

# Mirrors validateWorkspaceBenchmarkCompleted in test/e2e/preset_vllm_test.go.
benchmark_metrics_valid() {
  jq -e '
    (.status.performance.metrics.peakTokensPerMinute // {}) as $m
    | (($m.value // "") | tonumber? // 0) > 0
      and (((["durationSec", "inputTokens", "outputTokens", "maxConcurrency"])
             - (($m.config // {}) | keys)) | length) == 0
  ' "$1" >/dev/null 2>&1
}

# Prints a non-empty reason when the inference pod is failing for good, so the
# model can be failed without waiting out its timeout.
pod_crash_reason() {
  kubectl get pods -n "$NAMESPACE" -l "kaito.sh/workspace=${1}" -o json 2>/dev/null |
    jq -r --argjson maxRestarts "$MAX_CONTAINER_RESTARTS" '
      [ .items[] | .metadata.name as $pod | ((.status.containerStatuses // []) + (.status.initContainerStatuses // []))[]
        | { pod: $pod,
            name: .name,
            restarts: .restartCount,
            waiting: (.state.waiting.reason // ""),
            exit: (.lastState.terminated.exitCode // .state.terminated.exitCode) } ]
      | map(
          if (.waiting | test("ImagePullBackOff|ErrImagePull|CreateContainerError|CreateContainerConfigError|InvalidImageName")) then
            "\(.pod)/\(.name) cannot start (\(.waiting))"
          elif (.exit != null and .exit != 0 and .exit != 137) then
            "\(.pod)/\(.name) exited with code \(.exit) after \(.restarts) restart(s)"
          elif (.restarts >= $maxRestarts) then
            "\(.pod)/\(.name) crash-looping (\(.restarts) restarts)"
          else empty end)
      | .[0] // ""'
}

# Reasons the workspace controller sets on InferenceReady when the workload cannot
# make progress on its own (workspace_controller.go). Waiting out the per-model
# timeout on these only burns GPU hours.
is_terminal_inference_reason() {
  case "$1" in
    Unschedulable | ImagePullError | ContainerCrashLoopBackOff | ContainerOOMKilled | \
      ContainerStartError | PodEvicted | NodeDiskPressure | NodeMemoryPressure | \
      SASTokenFetchFailed)
      return 0
      ;;
    *) return 1 ;;
  esac
}

exec_until_ok() {  # exec_until_ok <workspace-name> <remote-sh-command>
  local ws="$1" cmd="$2" end
  end=$(($(date +%s) + ENDPOINT_TIMEOUT_SECONDS))
  while [[ "$(date +%s)" -lt "$end" ]]; do
    if kubectl exec -n "$NAMESPACE" "${ws}-0" -c "$ws" -- bash -c "$cmd" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$POLL_INTERVAL_SECONDS"
  done
  return 1
}

# Mirrors validateModelsEndpoint + validateChatCompletionsEndpoint in
# test/e2e/preset_vllm_test.go: the served model id is the lowercased repo name.
validate_endpoints() {
  local ws="$1" model="$2" model_id base install_curl payload
  model_id="$(printf '%s' "${model##*/}" | tr '[:upper:]' '[:lower:]')"
  base="http://${ws}.${NAMESPACE}.svc.cluster.local:80"
  install_curl='command -v curl >/dev/null 2>&1 || { apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq curl >/dev/null 2>&1; }'
  ENDPOINT_FAILURE=""

  log "  validating /v1/models (expecting model id '${model_id}')..."
  if ! exec_until_ok "$ws" "${install_curl}; curl -s --max-time 30 -X GET ${base}/v1/models | grep -q '\"id\":\"${model_id}\"'"; then
    ENDPOINT_FAILURE="/v1/models never reported model id '${model_id}'"
    return 1
  fi

  payload='{"model":"MODEL_ID","messages":[{"role":"user","content":"What is Kubernetes?"}],"max_tokens":7,"temperature":0}'
  payload="${payload//MODEL_ID/$model_id}"

  log "  validating /v1/chat/completions..."
  if ! exec_until_ok "$ws" "${install_curl}; curl -s --max-time 30 -X POST -H 'Content-Type: application/json' -d '${payload}' ${base}/v1/chat/completions | grep -q '\"object\":\"chat.completion'"; then
    ENDPOINT_FAILURE="/v1/chat/completions never returned a chat.completion object"
    return 1
  fi
  return 0
}

# Echoes the diagnostics into the job log so a failure can be triaged from the run
# output. Collapsed via ::group:: on GitHub Actions.
print_diagnostics() {
  # print_diagnostics <workspace-name>
  local ws="$1"
  echo "::group::${ws} — workspace status"
  kubectl get workspace "$ws" -n "$NAMESPACE" -o json 2>/dev/null | jq -r '
    "targetNodeCount: \(.status.targetNodeCount // "n/a")",
    "workerNodes: \(.status.workerNodes // [] | join(", "))",
    ((.status.conditions // [])[]
      | "  \(.type)=\(.status) reason=\(.reason // "-") message=\(.message // "-")")
  ' || true
  echo "::endgroup::"

  # NodeClaim conditions and events carry the real provisioning error (capacity,
  # quota, SKU availability); the workspace condition only says "not ready".
  echo "::group::${ws} — node provisioning (NodePool / NodeClaim)"
  kubectl get nodepools.karpenter.sh -o wide 2>&1 || true
  kubectl get nodeclaims.karpenter.sh -o wide 2>&1 || true
  echo "--- NodeClaim conditions ---"
  kubectl get nodeclaims.karpenter.sh -o json 2>/dev/null | jq -r '
    .items[]
    | "NodeClaim \(.metadata.name) node=\(.status.nodeName // "<none>")",
      ((.status.conditions // [])[]
        | "    \(.type)=\(.status) reason=\(.reason // "-") \(.message // "")")
  ' || true
  echo "--- NodeClaim events ---"
  kubectl get events -A --field-selector involvedObject.kind=NodeClaim \
    --sort-by=.lastTimestamp 2>&1 | tail -n 20 || true
  # gpu-provisioner creates NodeClaims directly and has no NodePools, so the query
  # above comes back empty for it; only the controller to read logs from differs.
  local prov_ns prov_label
  if [[ "${TEST_SUITE:-gpuprovisioner}" == "azkarpenter" ]]; then
    prov_ns="${KARPENTER_NAMESPACE:-karpenter}"
    prov_label="app.kubernetes.io/name=karpenter"
  else
    prov_ns="${GPU_PROVISIONER_NAMESPACE:-gpu-provisioner}"
    prov_label="app.kubernetes.io/name=gpu-provisioner"
  fi
  echo "--- ${prov_label#*=} controller log (warnings and errors) ---"
  # The controller floods INFO reconcile chatter that buries the real failure.
  kubectl logs -n "$prov_ns" -l "$prov_label" \
    --all-containers --tail=2000 2>/dev/null |
    grep -vE '"message":"(no dynamic nodepools found|Starting (EventSource|Controller|workers))"' |
    grep -iE '"level":"(WARN|ERROR)"|launch|capacity|quota|unavailable' |
    tail -n 40 || true
  echo "::endgroup::"

  echo "::group::${ws} — pods"
  kubectl get pods -n "$NAMESPACE" -l "kaito.sh/workspace=${ws}" -o wide 2>&1 || true
  kubectl describe pods -n "$NAMESPACE" -l "kaito.sh/workspace=${ws}" 2>&1 | tail -n 60 || true
  echo "::endgroup::"

  # An unschedulable pod is usually a storage or GPU capacity problem, and neither
  # is visible from the pod alone.
  echo "::group::${ws} — storage and node capacity"
  kubectl get pvc -n "$NAMESPACE" -l "kaito.sh/workspace=${ws}" -o wide 2>&1 || true
  echo "--- CSIStorageCapacity (local NVMe) ---"
  kubectl get csistoragecapacity -A \
    -o custom-columns='NAME:.metadata.name,CLASS:.storageClassName,NODES:.nodeTopology.matchLabels,CAPACITY:.capacity' 2>&1 || true
  echo "--- node allocatable ---"
  kubectl get nodes -o custom-columns='NAME:.metadata.name,GPU:.status.allocatable.nvidia\.com/gpu,EPHEMERAL:.status.allocatable.ephemeral-storage,INSTANCE:.metadata.labels.node\.kubernetes\.io/instance-type' 2>&1 || true
  # A zero CSIStorageCapacity means the driver found no usable local disk. Only the
  # node plugin (DaemonSet app=csi-local-node) does that discovery; the manager and
  # webhook pods say nothing about it.
  echo "--- local disk CSI driver ---"
  kubectl get pods -A -o wide 2>/dev/null |
    grep -iE 'csi-local|local-csi|localdisk|acstor' || echo "no local disk CSI driver pods found"
  local csi_ns csi_pod worker_node
  csi_ns="${KAITO_NAMESPACE:-kaito-workspace}"
  worker_node="$(kubectl get workspace "$ws" -n "$NAMESPACE" \
    -o jsonpath='{.status.workerNodes[0]}' 2>/dev/null || true)"
  if [[ -n "$worker_node" ]]; then
    csi_pod="$(kubectl get pods -n "$csi_ns" -l app=csi-local-node \
      --field-selector "spec.nodeName=${worker_node}" -o name 2>/dev/null | head -1)"
  fi
  if [[ -n "${csi_pod:-}" ]]; then
    echo "--- ${csi_pod} on ${worker_node} ---"
    kubectl logs -n "$csi_ns" "$csi_pod" --all-containers --tail=80 2>&1 || true
  else
    echo "no csi-local-node pod found on worker node '${worker_node:-<unknown>}'"
  fi
  echo "::endgroup::"

  echo "::group::${ws} — inference pod logs (last ${DIAG_LOG_LINES} lines)"
  kubectl logs -n "$NAMESPACE" -l "kaito.sh/workspace=${ws}" \
    --all-containers --tail="$DIAG_LOG_LINES" 2>&1 || true
  echo "::endgroup::"

  echo "::group::${ws} — previous inference pod logs (last ${DIAG_LOG_LINES} lines)"
  kubectl logs -n "$NAMESPACE" -l "kaito.sh/workspace=${ws}" \
    --all-containers --tail="$DIAG_LOG_LINES" --previous 2>&1 || true
  echo "::endgroup::"
}

teardown() {
  # teardown <workspace-name>
  local ws="$1"
  log "Tearing down workspace ${ws}..."
  kubectl delete workspace "$ws" -n "$NAMESPACE" --ignore-not-found --timeout=10m >/dev/null 2>&1 || true

  # GPU quota is the scarce resource: do not start the next model until the
  # provisioner has released every node claim from this one.
  local drain_deadline=$(($(date +%s) + 900))
  while [[ "$(date +%s)" -lt "$drain_deadline" ]]; do
    local remaining
    remaining="$(kubectl get nodeclaims --no-headers 2>/dev/null | grep -c . || true)"
    if [[ "${remaining:-0}" -eq 0 ]]; then
      log "All node claims released."
      return 0
    fi
    sleep 15
  done
  log "WARNING: node claims still present after teardown; continuing anyway."
  kubectl get nodeclaims -o wide || true
}

# ---------------------------------------------------------------------------
# Run one target end to end. Always returns 0; the outcome is in the result file.
# ---------------------------------------------------------------------------
run_target() {
  local target="$1"
  local model nodes gpus_per_node instance_type timeout_minutes skip_reason
  model="$(jq -r '.model' <<<"$target")"
  nodes="$(jq -r '.nodes' <<<"$target")"
  gpus_per_node="$(jq -r '.gpusPerNode' <<<"$target")"
  instance_type="$(jq -r '.instanceType' <<<"$target")"
  timeout_minutes="$(jq -r '.timeoutMinutes' <<<"$target")"
  skip_reason="$(jq -r '.skipReason' <<<"$target")"

  log "=============================================================="
  log "${model} -> ${nodes} x ${gpus_per_node}x${GPU} (${instance_type:-<unmapped>})"

  if [[ -n "$skip_reason" ]]; then
    log "SKIPPED: ${skip_reason}"
    record "$target" skipped "$skip_reason" 0 "" "" ""
    return 0
  fi

  if [[ -z "$instance_type" ]]; then
    local reason="no instance type mapped for ${gpus_per_node} ${GPU} GPU(s) per node"
    log "SKIPPED: ${reason}"
    record "$target" skipped "$reason" 0 "" "" ""
    return 0
  fi

  if [[ "$GLOBAL_DEADLINE_EPOCH" -gt 0 && "$(date +%s)" -ge "$GLOBAL_DEADLINE_EPOCH" ]]; then
    log "SKIPPED: run budget exhausted"
    record "$target" skipped "run budget exhausted before this model started" 0 "" "" ""
    return 0
  fi

  local ws ws_yaml="${WORKDIR}/workspace.yaml"
  ws="$(slugify "$model")"

  cat >"$ws_yaml" <<'EOF'
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: placeholder
resource:
  instanceType: placeholder
  labelSelector:
    matchLabels:
      apps: placeholder
inference:
  preset:
    name: placeholder
EOF

  # strenv keeps every value a literal scalar, so config data can never inject YAML.
  WS_NAME="$ws" INSTANCE_TYPE="$instance_type" PRESET_NAME="$model" \
    yq -i '
      .metadata.name = strenv(WS_NAME) |
      .resource.instanceType = strenv(INSTANCE_TYPE) |
      .resource.labelSelector.matchLabels.apps = strenv(WS_NAME) |
      .inference.preset.name = strenv(PRESET_NAME)
    ' "$ws_yaml"

  local artifact_dir="${ARTIFACT_DIR}/${ws}"
  mkdir -p "$artifact_dir"
  cp "$ws_yaml" "${artifact_dir}/workspace-applied.yaml"

  local start_epoch
  start_epoch="$(date +%s)"

  # The validating webhook can be momentarily unreachable (controller rollout or
  # restart), which is transient and unrelated to the model under test.
  local applied=false attempt
  for attempt in $(seq 1 "$APPLY_ATTEMPTS"); do
    if kubectl apply -n "$NAMESPACE" -f "$ws_yaml" >"${artifact_dir}/apply.txt" 2>&1; then
      applied=true
      break
    fi
    if ! grep -qiE 'failed calling webhook|no endpoints available|connection refused|i/o timeout|EOF' "${artifact_dir}/apply.txt"; then
      break
    fi
    log "  webhook unreachable (attempt ${attempt}/${APPLY_ATTEMPTS}); retrying in 15s"
    sleep 15
  done

  if [[ "$applied" != true ]]; then
    local reason
    reason="$(tr '\n' ' ' <"${artifact_dir}/apply.txt" | cut -c1-500)"
    log "FAILED to create workspace: ${reason}"
    record "$target" failed "workspace admission rejected: ${reason}" \
      "$(($(date +%s) - start_epoch))" "" "" "$artifact_dir"
    teardown "$ws"
    return 0
  fi

  local deadline=$((start_epoch + timeout_minutes * 60))
  if [[ "$GLOBAL_DEADLINE_EPOCH" -gt 0 && "$deadline" -gt "$GLOBAL_DEADLINE_EPOCH" ]]; then
    deadline="$GLOBAL_DEADLINE_EPOCH"
  fi
  local resource_deadline=$((start_epoch + RESOURCE_READY_TIMEOUT_MINUTES * 60))

  local status="failed" actual_nodes="" peak_tpm=""
  local reason="timed out waiting for WorkspaceSucceeded and BenchmarkCompleted"
  local ws_json="${WORKDIR}/ws.json"
  local stuck_reason="" stuck_since=0

  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    if ! kubectl get workspace "$ws" -n "$NAMESPACE" -o json >"$ws_json" 2>/dev/null; then
      sleep "$POLL_INTERVAL_SECONDS"
      continue
    fi

    actual_nodes="$(jq -r '.status.targetNodeCount // "" | tostring' "$ws_json")"
    peak_tpm="$(jq -r '.status.performance.metrics.peakTokensPerMinute.value // ""' "$ws_json")"

    local succeeded_reason
    succeeded_reason="$(condition_of "$ws_json" WorkspaceSucceeded reason)"
    if [[ "$succeeded_reason" == "BenchmarkFailed" || "$succeeded_reason" == "workspaceFailed" ]]; then
      status="failed"
      reason="${succeeded_reason}: $(condition_of "$ws_json" WorkspaceSucceeded message)"
      break
    fi

    if [[ "$(condition_of "$ws_json" WorkspaceSucceeded status)" == "True" &&
          "$(condition_of "$ws_json" BenchmarkCompleted status)" == "True" ]]; then
      if benchmark_metrics_valid "$ws_json"; then
        status="passed"
        reason=""
      else
        status="failed"
        reason="BenchmarkCompleted=True but status.performance.metrics.peakTokensPerMinute is missing or not positive"
      fi
      break
    fi

    local resource_status
    resource_status="$(condition_of "$ws_json" ResourceReady status)"
    if [[ "$resource_status" != "True" && "$(date +%s)" -ge "$resource_deadline" ]]; then
      status="failed"
      reason="GPU nodes not ready within ${RESOURCE_READY_TIMEOUT_MINUTES}m (ResourceReady=${resource_status:-<none>}: $(condition_of "$ws_json" ResourceReady message))"
      break
    fi

    local crash
    crash="$(pod_crash_reason "$ws")"
    if [[ -n "$crash" ]]; then
      status="failed"
      reason="inference pod crashing: ${crash}"
      break
    fi

    # Every terminal reason below needs a node to be meaningful: the pod is legitimately
    # Unschedulable for the minutes Karpenter spends provisioning one.
    local inference_status inference_reason
    inference_status="$(condition_of "$ws_json" InferenceReady status)"
    inference_reason="$(condition_of "$ws_json" InferenceReady reason)"
    if [[ "$resource_status" == "True" && "$inference_status" != "True" ]] &&
      is_terminal_inference_reason "$inference_reason"; then
      if [[ "$inference_reason" != "$stuck_reason" ]]; then
        stuck_reason="$inference_reason"
        stuck_since="$(date +%s)"
        log "  InferenceReady=${inference_reason}; failing in ${INFERENCE_FAILURE_GRACE_MINUTES}m unless it clears"
      elif [[ $(($(date +%s) - stuck_since)) -ge $((INFERENCE_FAILURE_GRACE_MINUTES * 60)) ]]; then
        status="failed"
        reason="${inference_reason}: $(condition_of "$ws_json" InferenceReady message)"
        break
      fi
    else
      stuck_reason=""
      stuck_since=0
    fi

    log "  waiting... resource=${resource_status} inference=${inference_status} benchmark=$(condition_of "$ws_json" BenchmarkCompleted status) nodes=${actual_nodes:-?}/${nodes} ($(( (deadline - $(date +%s)) / 60 ))m left)"
    sleep "$POLL_INTERVAL_SECONDS"
  done

  if [[ "$status" == "passed" ]] && ! validate_endpoints "$ws" "$model"; then
    status="failed"
    reason="$ENDPOINT_FAILURE"
  fi

  if [[ "$status" == "passed" ]]; then
    log "PASSED in $((($(date +%s) - start_epoch) / 60))m (peak TPM ${peak_tpm:-n/a})"
    rm -rf "$artifact_dir"
    artifact_dir=""
  else
    # Re-read the latest conditions so the report carries the real blocking message.
    if kubectl get workspace "$ws" -n "$NAMESPACE" -o json >"$ws_json" 2>/dev/null; then
      local resource_msg
      resource_msg="$(condition_of "$ws_json" ResourceReady message)"
      if [[ "$reason" == timed\ out* && -n "$resource_msg" ]]; then
        reason="${reason} (ResourceReady: ${resource_msg})"
      fi
    fi
    log "FAILED: ${reason}"
    print_diagnostics "$ws"
  fi

  record "$target" "$status" "$reason" "$(($(date +%s) - start_epoch))" \
    "$actual_nodes" "$peak_tpm" "$artifact_dir"
  teardown "$ws"
  return 0
}

for i in $(seq 0 "$((TARGET_COUNT - 1))"); do
  run_target "$(jq -c ".[${i}]" "$targets_file")"
done

jq -s '.' "$results_file" >"$RESULTS_FILE"
log "Wrote $(jq 'length' "$RESULTS_FILE") result(s) to ${RESULTS_FILE}"
jq -r '.[] | "  \(.status | ascii_upcase)\t\(.model)\t\(.nodes)x\(.gpusPerNode)x\(.gpu)"' "$RESULTS_FILE"

# Exit non-zero only after every model has run, so the pool job turns red on its
# own instead of relying on the aggregate report job (which a cancelled or
# declined run never reaches).
FAILED_COUNT="$(jq '[.[] | select(.status == "failed")] | length' "$RESULTS_FILE")"
if [[ "$FAILED_COUNT" -gt 0 ]]; then
  log "${FAILED_COUNT} of ${TARGET_COUNT} target(s) failed in the ${GPU} pool:"
  jq -r '.[] | select(.status == "failed")
    | "  - \(.model) on \(.nodes)x \(.gpusPerNode)x\(.gpu) (\(.instanceType)): \(.reason)"' "$RESULTS_FILE"
  exit 1
fi
