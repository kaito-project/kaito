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

# Copy all files from autoindexer/services into the app/services folder
COPY presets/autoindexer/ autoindexer/

# Set the PYTHONPATH environment variable
ENV PYTHONPATH=/app

# Install dependencies from requirements.txt
RUN pip install --no-cache-dir -r autoindexer/requirements.txt

# Set the final working directory
WORKDIR /app/autoindexer