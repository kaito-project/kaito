#!/bin/sh
set -ex

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

TMPDIR="$(mktemp -d)"

VOL_DIR="${1}"
[ ! -z "${VOL_DIR}" ] || VOL_DIR='{{ .volDir }}'

IMG_REF="${2}"
[ ! -z "${IMG_REF}" ] || IMG_REF='{{ .imgRef }}'

[ ! -z '{{ "" }}' ] || ANNOTATIONS_DATA='{{ .annotationsData }}'
ANNOTATIONS_DATA="${ANNOTATIONS_DATA:-{}}"

[ ! -z '{{ "" }}' ] || SENTINEL_PATH='{{ .sentinelPath }}'
SENTINEL_PATH="${SENTINEL_PATH:-.}"

#{{`

wait() {
    until [ -e "${SENTINEL_PATH}" ]
    do
        sleep 1
    done
}

mklayer() {
    local DATA_DIR="${TMPDIR}/data"
    mkdir -p "${DATA_DIR}"

    cp -R "${VOL_DIR}/adapter_config.json" "${VOL_DIR}/adapter_model.safetensors" "${DATA_DIR}"

    local TAR_LAYER_PATH="${TMPDIR}/layer.tar"

    tar c -f "${TAR_LAYER_PATH}" -C "$(dirname "${DATA_DIR}")" "$(basename "${DATA_DIR}")"

    TAR_LAYER_DIFF="$(sha256sum "${TAR_LAYER_PATH}" | cut -d ' ' -f '1')"

    gzip -9 "${TAR_LAYER_PATH}"

    TGZ_LAYER_MIME='application/vnd.oci.image.layer.v1.tar+gzip'
    TGZ_LAYER_PATH="${TAR_LAYER_PATH}.gz"
}

mkconfig() {
    CONFIG_MIME='application/vnd.oci.image.config.v1+json'
    CONFIG_PATH="${TMPDIR}/config.json"

    printf '{"rootfs":{"diff_ids":["sha256:%s"]}}' "${TAR_LAYER_DIFF}" > "${CONFIG_PATH}"
}

mkannotations() {
    ANNOTATIONS_PATH="${TMPDIR}/annotations.json"

    printf '%s' "${ANNOTATIONS_DATA}" > "${ANNOTATIONS_PATH}"
}

mklayout() {
    LAYOUT_REF="${TMPDIR}/layout:latest"

    cd "$(dirname "${TGZ_LAYER_PATH}")"
    oras push --disable-path-validation --annotation-file "${ANNOTATIONS_PATH}" --config "${CONFIG_PATH}:${CONFIG_MIME}" --oci-layout "${LAYOUT_REF}" "$(basename "${TGZ_LAYER_PATH}"):${TGZ_LAYER_MIME}"
    cd -
}

push() {
    oras cp --from-oci-layout "${LAYOUT_REF}" "${IMG_REF}"
}

resume() {
    killall -SIGCHLD 'pause' 0</dev/null 1>&0 2>&0 || true
}

#`}}

wait
mklayer
mkconfig
mkannotations
mklayout
push
resume
