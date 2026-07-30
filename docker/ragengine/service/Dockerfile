FROM python:3.12-slim AS dependencies

# Install system dependencies for building Python packages
RUN apt-get update && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends  \
    build-essential \
    gcc \
    g++ \
    make \
    perl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN pip3 install --upgrade pip

# Force-upgrade CVE-flagged packages that the base image ships with:
#   - setuptools 70.3.0 pre-installed by the python:3.12-slim base image.
#     Trivy flags CVE-2025-47273 (HIGH, fixed 78.1.1) and CVE-2026-59890
#     (MEDIUM, fixed 83.0.0).
#   - msgpack 1.1.2 pulled in via the base image / pip's own deps chain.
#     Trivy flags GHSA-6v7p-g79w-8964 (HIGH, fixed 1.2.1).
# We upgrade in this dependencies stage so 'docker build --target dependencies'
# (used by the trivy scan CI job) also picks up the fix.
RUN pip3 install --no-cache-dir --upgrade "setuptools>=83.0.0" "msgpack>=1.2.1" && \
    # 'pip install --upgrade' only touches packages tracked by pip in
    # site-packages. Trivy additionally scans bundled wheels and vendored
    # dist-info METADATA inside pip itself; those do NOT participate in pip's
    # dep resolver and were left behind by the previous fix (removing the
    # vendored msgpack package directory did not remove its dist-info, so
    # trivy still reported msgpack 1.1.2 from pip/_vendor/msgpack-1.1.2.dist-info).
    # Scrub every bundled/vendored copy AND its dist-info.
    rm -rf /usr/local/lib/python3.12/ensurepip/_bundled/*.whl \
           /usr/local/lib/python3.12/site-packages/pip/_vendor/msgpack* \
           /usr/local/lib/python3.12/site-packages/pip/_vendor/setuptools* \
           /usr/local/lib/python3.12/site-packages/pip/_vendor/pkg_resources* \
           /usr/local/lib/python3.12/site-packages/pip/_vendor/vendor.txt \
           /usr/local/lib/python3.12/site-packages/pip/_vendor/bom.cdx.json \
           /root/.cache/pip && \
    # Belt-and-braces: catch any other stale dist-info anywhere on disk.
    find / -type d \( \
        -name 'setuptools-6*.dist-info' -o \
        -name 'setuptools-7*.dist-info' -o \
        -name 'msgpack-1.0*.dist-info' -o \
        -name 'msgpack-1.1*.dist-info' \
    \) -prune -exec rm -rf {} + 2>/dev/null || true

# NOTE: 'pip install --upgrade' only touches packages under
# /usr/local/lib/python3.12/site-packages. Trivy also scans stale bundled
# wheels shipped by CPython's ensurepip module and vendored copies inside pip
# itself, which do not participate in pip's dependency resolver, so we scrub
# any pre-fix leftovers explicitly above.

FROM dependencies AS base

ARG VERSION
ENV VERSION=$VERSION

WORKDIR /app

# Set the PYTHONPATH environment variable
ENV PYTHONPATH=/app

# Copy requirements.txt first to leverage Docker layer caching
COPY presets/ragengine/requirements.txt ragengine/requirements.txt

# Install dependencies with pip cache mount for faster rebuilds
# This allows reusing downloaded packages across builds
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r ragengine/requirements.txt

# Copy the rest of the application code after dependencies are installed
# This ensures dependency installation layer is cached when only code changes
COPY presets/ragengine/ ragengine/

# Set the final working directory
WORKDIR /app/ragengine

# Build and Push Using Docker with BuildKit (recommended for caching)
# Enable BuildKit for better caching: export DOCKER_BUILDKIT=1
# Replace <ACR_PATH> with your Azure Container Registry path, e.g., myregistry.azurecr.io
# docker build -f docker/ragengine/service/Dockerfile -t kaito-rag-service:0.0.1 .
# docker tag kaito-rag-service:0.0.1 <ACR_PATH>/kaito-rag-service:0.0.1
# docker push <ACR_PATH>/kaito-rag-service:0.0.1

# Multi-Platform Build and Push Using Docker Buildx
# Requires QEMU for cross-platform builds (linux/amd64, linux/arm64)
# docker buildx build \
#   --platform linux/amd64,linux/arm64 \
#   -f docker/ragengine/service/Dockerfile \
#   -t <ACR_PATH>/kaito-rag-service:0.0.1 \
#   --push .
