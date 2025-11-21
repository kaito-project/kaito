#!/bin/bash
set -e

# Usage: ./build_oci_artifact.sh <model_version_url> <image_name> <registry> <image_tag> [hf_token]

MODEL_VERSION_URL="$1"
IMAGE_NAME="$2"
REGISTRY="$3"
IMAGE_TAG="$4"
HF_TOKEN="$5"

if [ -z "$MODEL_VERSION_URL" ] || [ -z "$IMAGE_NAME" ] || [ -z "$REGISTRY" ] || [ -z "$IMAGE_TAG" ]; then
  echo "Usage: $0 <model_version_url> <image_name> <registry> <image_tag> [hf_token]"
  exit 1
fi

# Parse HuggingFace URL
PATH_PART="${MODEL_VERSION_URL#*huggingface.co/}"
IFS='/' read -ra ADDR <<< "$PATH_PART"
ORG="${ADDR[0]}"
REPO="${ADDR[1]}"
REPO_ID="$ORG/$REPO"
REVISION="main"

for i in "${!ADDR[@]}"; do
  if [[ "${ADDR[$i]}" == "tree" || "${ADDR[$i]}" == "commit" || "${ADDR[$i]}" == "blob" || "${ADDR[$i]}" == "tag" ]]; then
    if [[ $((i+1)) -lt ${#ADDR[@]} ]]; then
      REVISION="${ADDR[$i+1]}"
    fi
    break
  fi
done

SOURCE="huggingface://$REPO_ID@$REVISION"
FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"

# Create temporary directory for output
TEMP_DIR=$(mktemp -d)
echo "Created temporary directory: $TEMP_DIR"

# Set up trap to clean up temporary directory on exit
cleanup() {
    if [ -d "$TEMP_DIR" ]; then
        echo "Cleaning up temporary directory: $TEMP_DIR"
        rm -rf "$TEMP_DIR"
    fi
}
trap cleanup EXIT

echo "Building OCI artifact for $IMAGE_NAME from $SOURCE..."

# Export HF_TOKEN for docker buildx secret if provided
if [ -n "$HF_TOKEN" ]; then
  export HF_TOKEN
fi

docker buildx build \
  --secret id=hf-token,env=HF_TOKEN \
  --build-arg BUILDKIT_SYNTAX=ghcr.io/kaito-project/aikit/aikit:latest \
  --target packager/modelpack \
  --build-arg source="$SOURCE" \
  --build-arg name="$IMAGE_NAME" \
  --build-arg exclude="'original/**'" \
  --output="$TEMP_DIR" \
  -<<<""

echo "Pushing to $FULL_IMAGE_NAME..."
oras cp --from-oci-layout "$TEMP_DIR/layout:$IMAGE_NAME" "$FULL_IMAGE_NAME"
