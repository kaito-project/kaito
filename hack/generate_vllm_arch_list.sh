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

# Script to regenerate presets/workspace/models/vllm_model_arch_list.txt by
# running list_supported_llm_archs.py inside the kaito-base Docker image.
# The image tag is read from the 'base' entry in supported_models.yaml.
# The resulting text file (one architecture per line) is embedded in Go via
# //go:embed in vllm_model_arch_list.go.
#
# Usage: ./hack/generate_vllm_arch_list.sh

set -o errexit
set -o nounset
set -o pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SUPPORTED_MODELS_YAML="${ROOT_DIR}/presets/workspace/models/supported_models.yaml"
OUTPUT_FILE="${ROOT_DIR}/presets/workspace/models/vllm_model_arch_list.txt"
BASE_REGISTRY="mcr.microsoft.com/aks/kaito/kaito-base"

# ---------------------------------------------------------------------------
# 1. Read the base image tag from supported_models.yaml
# ---------------------------------------------------------------------------
if ! command -v yq &>/dev/null; then
    echo "ERROR: 'yq' is required but not found in PATH" >&2
    exit 1
fi

if ! command -v docker &>/dev/null; then
    echo "ERROR: 'docker' is required but not found in PATH" >&2
    exit 1
fi

BASE_TAG=$(yq '.models[] | select(.name == "base") | .tag' "${SUPPORTED_MODELS_YAML}")
if [[ -z "${BASE_TAG}" ]]; then
    echo "ERROR: could not find 'base' model tag in ${SUPPORTED_MODELS_YAML}" >&2
    exit 1
fi

IMAGE="${BASE_REGISTRY}:${BASE_TAG}"
echo "Using base image: ${IMAGE}"

# ---------------------------------------------------------------------------
# 2. Run the architecture list script inside the container
# ---------------------------------------------------------------------------
echo "Pulling and running ${IMAGE} ..."
docker run --rm \
    --entrypoint python3 \
    "${IMAGE}" \
    /workspace/vllm/list_supported_llm_archs.py > "${OUTPUT_FILE}"

if [[ ! -s "${OUTPUT_FILE}" ]]; then
    echo "ERROR: Docker command produced no output" >&2
    exit 1
fi

echo "Generated ${OUTPUT_FILE}"
echo "Done."
