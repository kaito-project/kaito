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

    def bytes_per_token:
      . as $model
      | ($model.headDim
          // (($model.hiddenSize // 0) / ($model.numAttentionHeads // 1))) as $headDim
      | (if (($model.kvLoraRank // -1) != -1)
          then ($model.kvLoraRank + ($model.qkRopeHeadDim // 0))
          else (2 * ($model.numKeyValueHeads // $model.numAttentionHeads // 0) * $headDim)
          end)
        * ($model.numHiddenLayers // 0)
        * 2;

    # Mirrors pkg/workspace/estimator/nodesestimator for the default 2,048-token
    # sizing context so the matrix can choose a node shape before provisioning.
    def estimated_nodes($model; $gpuConfig; $gpusPerNode):
      1073741824 as $gib
      | ($gpuConfig.gpuMemoryGiBPerNode[($gpusPerNode | tostring)] / $gpusPerNode) as $gpuMemoryGiB
      | ($gpuConfig.gpuMemoryUtilization // 0.92) as $memoryUtilization
      | ($gpuConfig.baseOverheadGiB // 2.3) as $baseOverheadGiB
      | (($model | bytes_per_token) * 2048 / $gpusPerNode / $gib) as $kvCacheGiB
      | (($gpuMemoryGiB * $memoryUtilization - $baseOverheadGiB - $kvCacheGiB) / 1.05) as $availableWeightGiB
      | (($model | model_size_gib) * 1.02
          + (($model.mambaStateBytesPerSeq // 0) * 64 / $gib)) as $requiredGiB
      | ((($requiredGiB / $availableWeightGiB) | floor) + 1) as $minimumGPUs
      | (($minimumGPUs / $gpusPerNode) | ceil);

    $config[0] as $config
    | $config.profiles[$profile] as $profileConfig
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
        | [ $policy.gpusPerNodeOptions[] as $gpusPerNode
            | {
                gpusPerNode: $gpusPerNode,
                instanceType: $gpuConfig.instanceTypes[($gpusPerNode | tostring)],
                estimatedNodes: estimated_nodes($model; $gpuConfig; $gpusPerNode)
              }
          ]
        | sort_by(.estimatedNodes, .gpusPerNode)
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