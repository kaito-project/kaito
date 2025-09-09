#!/bin/bash

# -------------------------------------------------------------------------
# This script forks the local-csi-driver chart into the Kaito workspace.
# It:
# 1. Clones the Azure/local-csi-driver repo at a specific revision
# 2. Copies the chart into Kaito's workspace as a local subchart
# -------------------------------------------------------------------------

SCRIPT_DIR="$(realpath "$(dirname "$0")")"
KAITO_WORKSPACE_DIR="$SCRIPT_DIR/../../charts/kaito/workspace"
TARGET_CHART_DIR="$KAITO_WORKSPACE_DIR/charts/local-csi-driver"

REPO=https://github.com/Azure/local-csi-driver.git
REVISION="v0.2.5"  # Use the latest stable version
SOURCE_CHART_PATH="charts/latest"

TEMP_DIR=$(mktemp -d)
REPO_DIR="$TEMP_DIR/local-csi-driver"

set -euo pipefail

cleanup() {
  echo "Cleaning up temporary directory: $TEMP_DIR"
  rm -rf "$TEMP_DIR"
}

# Set trap to ensure cleanup on script exit or interruption
trap cleanup EXIT INT TERM

echo "=== Forking local-csi-driver chart into Kaito workspace ==="
echo "Created temporary directory: $TEMP_DIR"

# Step 1: Clone the repository
echo "Cloning $REPO repository..."
git clone "$REPO" "$REPO_DIR"

echo "Checking out revision: $REVISION"
(cd "$REPO_DIR" && git checkout "$REVISION") || { 
  echo "Failed to checkout revision $REVISION"; 
  exit 1; 
}

# Step 2: Verify source chart exists
SOURCE_CHART_FULL_PATH="$REPO_DIR/$SOURCE_CHART_PATH"
if [ ! -d "$SOURCE_CHART_FULL_PATH" ]; then
  echo "Error: Source chart not found at $SOURCE_CHART_FULL_PATH"
  exit 1
fi

echo "Found source chart at: $SOURCE_CHART_FULL_PATH"

# Step 3: Remove existing target directory if it exists
if [ -d "$TARGET_CHART_DIR" ]; then
  echo "Removing existing chart directory: $TARGET_CHART_DIR"
  rm -rf "$TARGET_CHART_DIR"
fi

# Step 4: Copy the chart
echo "Copying chart from $SOURCE_CHART_FULL_PATH to $TARGET_CHART_DIR"
mkdir -p "$(dirname "$TARGET_CHART_DIR")"
cp -r "$SOURCE_CHART_FULL_PATH" "$TARGET_CHART_DIR"