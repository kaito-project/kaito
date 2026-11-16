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

IMG_REF="${1}"
[ ! -z "${IMG_REF}" ] || IMG_REF='{{ .imgRef }}'

VOL_DIR="${2}"
[ ! -z "${VOL_DIR}" ] || VOL_DIR='{{ .volDir }}'

#{{`

pull() {
    LAYOUT_DIR="${TMPDIR}/layout"

    local DOCKER_CONFIG_D="${HOME}/.docker/config.d"
    [ ! -e "${DOCKER_CONFIG_D}" ] || local REGISTRY_AUTH_FILES="$(find -L "${DOCKER_CONFIG_D}" -type 'f' -printf '%p ')"

    for REGISTRY_AUTH_FILE in '' ${REGISTRY_AUTH_FILES}
    do
        local SKOPEO_ARGS='--dest-decompress'
        [ -z "${REGISTRY_AUTH_FILE}" ] || SKOPEO_ARGS="${SKOPEO_ARGS} --authfile=${REGISTRY_AUTH_FILE}"

        skopeo copy ${SKOPEO_ARGS} "docker://${IMG_REF}" "dir:${LAYOUT_DIR}" || continue

        local OK='1'
        break
    done

    [ "${OK}" = '1' ]
}

expand() {
    ROOTFS_DIR="${TMPDIR}/rootfs"
    mkdir -p "${ROOTFS_DIR}"

    local LAYER_DIFFS="$(skopeo inspect --format '{{ range .Layers }}{{ slice . 7 }} {{ end }}' "dir:${LAYOUT_DIR}")"
    for LAYER_DIFF in ${LAYER_DIFFS}
    do
        local LAYER_PATH="${LAYOUT_DIR}/${LAYER_DIFF}"
        tar x -f "${LAYER_PATH}" -C "${ROOTFS_DIR}"
    done
}

relocate() {
    local DATA_PATH="${ROOTFS_DIR}/data"
    mv -f "${DATA_PATH}" "${VOL_DIR}"
}

#`}}

pull
expand
relocate
