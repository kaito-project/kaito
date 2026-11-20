# syntax=ghcr.io/azure/dalec/frontend:0.18

args:
  VERSION: ${VERSION}
  REVISION: ${REVISION}

name: skopeo
version: ${VERSION}
revision: ${REVISION}
description: Work with remote images registries - retrieving information, images, signing content
vendor: Microsoft Corporation
packager: Azure Kubernetes Service
license: Apache-2.0
website: https://github.com/containers/skopeo

targets:
  azlinux3:
    dependencies:
      build:
        skopeo:
          version:
            - ${VERSION}-${REVISION}.azl3
      runtime:
        bash: {}
        findutils: {}
        tar: {}

artifacts:
  binaries:
    /usr/bin/skopeo: {}
  configFiles:
    /etc/containers: {}
  licenses:
    /usr/share/licenses/skopeo/LICENSE: {}

image:
  entrypoint: /usr/bin/skopeo
  env:
    - REGISTRY_AUTH_FILE=/tmp/auth.json

tests:
  - name: Check Permissions
    files:
      /etc/containers/policy.json:
        permissions: 0644
      /usr/bin/skopeo:
        permissions: 0755
      /usr/share/licenses/skopeo/LICENSE:
        permissions: 0644

changelog:
  - date: 2025-10-22
    author: Pedro Tôrres <pedrotorres@microsoft.com>
    changes:
      - initial manifest
