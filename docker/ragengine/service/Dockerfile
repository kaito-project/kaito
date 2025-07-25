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

FROM dependencies AS base

WORKDIR /app

# Copy all files from ragengine/services into the app/services folder
COPY presets/ragengine/ ragengine/

# Set the PYTHONPATH environment variable
ENV PYTHONPATH=/app

# Install dependencies from requirements.txt
RUN pip install --no-cache-dir -r ragengine/requirements.txt

# Set the final working directory
WORKDIR /app/ragengine

# Build and Push Using Docker
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
