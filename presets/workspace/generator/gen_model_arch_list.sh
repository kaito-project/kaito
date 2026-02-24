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

header_file="hack/boilerplate.go.txt"
output_file="presets/workspace/models/vllm_model_arch_list.go"

if [[ "$#" -gt 0 ]]; then
  model_arch_file="$1"
else
  echo "Usage: $0 <model-arch-file>"
  exit 1
fi

if [[ ! -f "$model_arch_file" ]]; then
  echo "Error: Model architecture file '$model_arch_file' not found."
  exit 1
fi

line=$(cat "$model_arch_file")

# Remove single quotes and spaces, then replace commas with newlines
models=$(echo "$line" | tr -d "'" | tr ',' '\n' | sed '/^\s*$/d' | sed 's/^[ \t]*//;s/[ \t]*$//')

# sort the models and remove duplicates
models=$(echo "$models" | sort -u)

# Overwrite the Go file with the new map content
cat > "$output_file" <<EOF
$(cat "$header_file")

package models

var vLLMModelArchMap = map[string]bool{
EOF

{
  while IFS= read -r model; do
    printf '\t"%s": true,\n' "$model"
  done <<< "$models"
  echo "}"
} >> "$output_file"

gofmt -s -w "$output_file"

echo "Go file '$output_file' generated successfully."
