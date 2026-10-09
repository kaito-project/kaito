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

# Generates regression targets from model_catalog.yaml model metadata and the
# sizing policies in preset-regression-config.json.

set -euo pipefail

MODEL_CATALOG_FILE="${MODEL_CATALOG_FILE:-presets/workspace/models/model_catalog.yaml}"
REGRESSION_CONFIG_FILE="${REGRESSION_CONFIG_FILE:-.github/preset-regression-config.json}"
REGRESSION_PROFILE="${REGRESSION_PROFILE:-standard}"
GPU="${GPU:-}"
MODEL_FILTER="${MODEL_FILTER:-}"

yq -o=json '.' "$MODEL_CATALOG_FILE" |
  jq -c --slurpfile config "$REGRESSION_CONFIG_FILE" \
    --arg profile "$REGRESSION_PROFILE" --arg gpuFilter "$GPU" --arg filter "$MODEL_FILTER" '
    def model_size_gib:
      .modelFileSize | capture("^(?<size>[0-9]+(?:\\.[0-9]+)?)Gi$").size | tonumber;

    def required_gpus($modelSize; $memoryPerGPU; $minimumGPUs):
      [($modelSize / $memoryPerGPU) | ceil, $minimumGPUs] | max;

    $config[0] as $config
    | $config.profiles[$profile] as $profileConfig
    | ($config.modelSizeSafetyFactor // 1) as $modelSizeSafetyFactor
    | ($filter | ascii_downcase | split(",")
        | map(gsub("^\\s+|\\s+$"; "")) | map(select(length > 0))) as $needles
    | [ .models[] as $model
        | ($model | model_size_gib) as $modelSize
        | $profileConfig.pools | to_entries[]
        | .key as $gpu
        | .value as $policy
        | select($gpuFilter == "" or $gpu == $gpuFilter)
        | select(
            $modelSize > ($policy.minModelSizeGiBExclusive // -1)
            or (($policy.minModelSizeExemptions // [])
                | map(ascii_downcase)
                | index($model.name | ascii_downcase)) != null
          )
        | select($modelSize <= ($policy.maxModelSizeGiB // 1e18))
        | select((($policy.blocklist // []) | map(ascii_downcase) | index($model.name | ascii_downcase)) == null)
        | select(($needles | length) == 0
            or any($needles[]; inside($model.name | ascii_downcase)))
        | $config.gpuPools[$gpu] as $gpuConfig
        | ($config.minimumGPUsOverrides[$model.name][$gpu] // 0) as $minimumGPUs
        | [ $policy.gpusPerNodeOptions[] as $gpusPerNode
            | ($gpuConfig.gpuMemoryGiBPerGPU[($gpusPerNode | tostring)]) as $memoryPerGPU
            | (required_gpus(
                $modelSize * $modelSizeSafetyFactor;
                $memoryPerGPU;
                $minimumGPUs
              )) as $requiredGPUs
            | (($requiredGPUs / $gpusPerNode) | ceil) as $estimatedNodes
            | {
                gpusPerNode: $gpusPerNode,
                instanceType: $gpuConfig.instanceTypes[($gpusPerNode | tostring)],
                estimatedNodes: $estimatedNodes,
                allocatedGPUs: ($estimatedNodes * $gpusPerNode)
              }
          ]
        | sort_by(.estimatedNodes, .allocatedGPUs, .gpusPerNode)
        | .[0] as $shape
        | {
            model: $model.name,
            profile: $profile,
            gpu: $gpu,
            gpusPerNode: $shape.gpusPerNode,
            instanceType: $shape.instanceType,
            timeoutMinutes: (
              if ($profileConfig.largeModelThresholdGiB // 1e18) <= $modelSize
              then ($profileConfig.largeModelTimeoutMinutes // $profileConfig.timeoutMinutes)
              else $profileConfig.timeoutMinutes
              end
            ),
            skipReason: ""
          }
      ]
    | if any(.[]; .instanceType == null or .instanceType == "")
      then error("generated target has no instance type mapping")
      else .
      end
  '