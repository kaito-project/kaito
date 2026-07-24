#!/usr/bin/env bash
# cuda-provision.sh — install the CUDA toolkit into a directory at runtime.
#
# Invoked by the cuda-toolkit-provisioner init container for models that require
# DeepGEMM (e.g. FP8 models like DeepSeek-V4), which JIT-compile CUDA kernels with
# nvcc that the slim base image does not ship. This installs the JIT-only subset of
# CUDA 12.9 (nvcc + runtime headers/libs + NVRTC + CCCL) from NVIDIA's apt repo into
# the target directory (a node hostPath the main container uses as CUDA_HOME).
#
# The target dir is a node hostPath, so the install survives pod recreation and is
# shared by all pods on the node — only cold nodes pay the install. It is idempotent
# (skips when nvcc is already present). The host C++ compiler (g++) required by nvcc
# is baked into the base image.
#
# Usage: cuda-provision.sh [TARGET_DIR]   (default: /opt/cuda)
set -euo pipefail

TARGET="${1:-/opt/cuda}"
CUDA_PKG_VERSION="12-9"
CUDA_HOME_SRC="/usr/local/cuda-12.9"

# Idempotent: skip if a working nvcc is already staged (e.g. a previous init run
# on the same pod's shared volume).
if [ -x "${TARGET}/bin/nvcc" ]; then
    echo "cuda-provision: nvcc already present at ${TARGET}/bin/nvcc; skipping install"
    "${TARGET}/bin/nvcc" --version || true
    exit 0
fi

echo "cuda-provision: installing JIT-only CUDA ${CUDA_PKG_VERSION} packages (nvcc + cudart-dev + nvrtc-dev + cccl + cublas-dev + curand-dev) ..."
export DEBIAN_FRONTEND=noninteractive

ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64) REPO_ARCH="x86_64" ;;
    aarch64) REPO_ARCH="sbsa" ;;
    *) echo "cuda-provision: unsupported architecture ${ARCH}" >&2; exit 1 ;;
esac

apt-get update -y
apt-get install --no-install-recommends -y curl ca-certificates

# Pin to NVIDIA's Debian 12 (bookworm) CUDA repo; the base image is bookworm.
KEYRING_URL="https://developer.download.nvidia.com/compute/cuda/repos/debian12/${REPO_ARCH}/cuda-keyring_1.1-1_all.deb"
curl -fsSL "${KEYRING_URL}" -o /tmp/cuda-keyring.deb
dpkg -i /tmp/cuda-keyring.deb
rm -f /tmp/cuda-keyring.deb

apt-get update -y
apt-get install --no-install-recommends -y \
    "cuda-nvcc-${CUDA_PKG_VERSION}" \
    "cuda-cudart-dev-${CUDA_PKG_VERSION}" \
    "cuda-nvrtc-dev-${CUDA_PKG_VERSION}" \
    "cuda-cccl-${CUDA_PKG_VERSION}" \
    "libcublas-dev-${CUDA_PKG_VERSION}" \
    "libcurand-dev-${CUDA_PKG_VERSION}"

if [ ! -d "${CUDA_HOME_SRC}" ]; then
    echo "cuda-provision: expected ${CUDA_HOME_SRC} after install but it is missing" >&2
    exit 1
fi

echo "cuda-provision: staging toolkit into ${TARGET} ..."
mkdir -p "${TARGET}"
cp -a "${CUDA_HOME_SRC}/." "${TARGET}/"

"${TARGET}/bin/nvcc" --version
echo "cuda-provision: done."
