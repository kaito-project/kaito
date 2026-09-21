#!/bin/bash

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

# Script to compare the content of base_images.yaml with the BaseImages
# key in the Helm ConfigMap template, ignoring comment lines.
#
# Usage: ./compare_base_image_configs.sh [OPTIONS]
#
# Options:
#   -v, --verbose     Show verbose output including normalized files
#   -d, --debug       Keep temporary files for debugging
#   -h, --help        Show this help message

set -o errexit
set -o nounset
set -o pipefail

# Parse command line arguments
VERBOSE=false
DEBUG=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -d|--debug)
            DEBUG=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo
            echo "Compare the content of base_images.yaml with the BaseImages"
            echo "key in the Helm ConfigMap template, ignoring comment lines."
            echo
            echo "Options:"
            echo "  -v, --verbose     Show verbose output including normalized files"
            echo "  -d, --debug       Keep temporary files for debugging"
            echo "  -h, --help        Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use -h or --help for usage information"
            exit 1
            ;;
    esac
done

BASE_IMAGES_FILE="presets/workspace/models/base_images.yaml"
CONFIGMAP_FILE="charts/kaito/workspace/templates/base-images-configmap.yaml"

# Check if files exist
if [[ ! -f "$BASE_IMAGES_FILE" ]]; then
    echo "Error: base_images.yaml not found at $BASE_IMAGES_FILE"
    exit 1
fi

if [[ ! -f "$CONFIGMAP_FILE" ]]; then
    echo "Error: base-images-configmap.yaml not found at $CONFIGMAP_FILE"
    exit 1
fi

# Function to strip comments and normalize YAML
normalize_yaml() {
    local file="$1"
    # Remove comment lines (lines starting with # after optional whitespace)
    # Remove inline comments (# and everything after it on a line)
    # Remove empty lines
    # Trim leading/trailing whitespace
    sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$file" | sed 's/^[[:space:]]*//' | sed 's/[[:space:]]*$//'
}

# Function to extract the BaseImages value from the ConfigMap
extract_configmap_base_images() {
    local file="$1"
    # Extract the value under BaseImages (everything after "BaseImages: |").
    awk '
    /BaseImages: \|/ {
        found=1; 
        next 
    }
    found {
        print 
    }' "$file"
}

echo "Comparing base image configurations..."
echo "Source file: $BASE_IMAGES_FILE"
echo "ConfigMap file: $CONFIGMAP_FILE"
echo

# Create temporary files for comparison
TEMP_DIR=$(mktemp -d)
BASE_IMAGES_NORMALIZED="$TEMP_DIR/base_images_normalized.yaml"
CONFIGMAP_NORMALIZED="$TEMP_DIR/configmap_normalized.yaml"

# Normalize the original base images file
normalize_yaml "$BASE_IMAGES_FILE" > "$BASE_IMAGES_NORMALIZED"

# Extract and normalize the ConfigMap base images section
extract_configmap_base_images "$CONFIGMAP_FILE" | normalize_yaml /dev/stdin > "$CONFIGMAP_NORMALIZED"

# Show verbose output if requested
if [[ "$VERBOSE" == "true" ]]; then
    echo "Normalized base images file content:"
    echo "================================"
    cat "$BASE_IMAGES_NORMALIZED"
    echo
    echo "Normalized ConfigMap content:"
    echo "============================="
    cat "$CONFIGMAP_NORMALIZED"
    echo
fi

# Compare the normalized files
if diff -u "$BASE_IMAGES_NORMALIZED" "$CONFIGMAP_NORMALIZED"; then
    echo "✅ SUCCESS: Base image configurations are identical (ignoring comments)"
    exit_code=0
else
    echo "❌ FAILURE: Model configurations differ"
    echo
    echo "Differences found between:"
    echo "  Left:  $BASE_IMAGES_FILE (normalized)"
    echo "  Right: ConfigMap BaseImages value (normalized)"
    echo
    echo "To see the normalized files for debugging:"
    echo "  Base images file (normalized): $BASE_IMAGES_NORMALIZED"
    echo "  ConfigMap section (normalized): $CONFIGMAP_NORMALIZED"
    exit_code=1
fi

# Clean up temporary files unless debugging or there were differences
if [[ $exit_code -eq 0 && "$DEBUG" != "true" ]]; then
    rm -rf "$TEMP_DIR"
elif [[ $exit_code -ne 0 || "$DEBUG" == "true" ]]; then
    echo
    echo "Temporary files preserved for debugging in: $TEMP_DIR"
    echo "  Base images file (normalized):  $BASE_IMAGES_NORMALIZED"
    echo "  ConfigMap section (normalized): $CONFIGMAP_NORMALIZED"
fi

exit $exit_code
