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
RUN pip3 install --no-cache-dir --upgrade "setuptools>=83.0.0" "msgpack>=1.2.1"

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
