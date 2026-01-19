#!/bin/bash

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

set -euo pipefail

VLLM_VERSION="v0.12.0"
url="https://raw.githubusercontent.com/vllm-project/vllm/refs/tags/$VLLM_VERSION/docs/models/supported_models.md"
header_file="hack/boilerplate.go.txt"
output_file="presets/workspace/models/vllm_model_arch_list.go"

models=$(curl -s "$url" | awk -F'|' 'NF > 4 {
  col = $2
  gsub(/`| /, "", col)
  if (col ~ /^[a-zA-Z0-9]+$/) {
    print col
  }
}'| sort | uniq | grep -v rchitecture)

cat > "$output_file" <<EOF
$(cat "$header_file")

package models

var vLLMModelArchMap = map[string]bool{
EOF

while IFS= read -r model; do
  echo "    \"$model\": true," >> "$output_file"
done <<< "$models"

cat >> "$output_file" <<EOF
}
EOF

gofmt -s -w "$output_file"

echo "Go file '$output_file' generated successfully."
