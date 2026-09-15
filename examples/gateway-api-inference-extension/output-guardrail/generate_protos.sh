#!/bin/bash
# Generate Envoy ext_proc Python protos with all dependencies

set -e

WORK_DIR="${1:-.}"
PROTO_OUT="$WORK_DIR/envoy"

echo "Generating Envoy protos to $PROTO_OUT..."

# Clone Envoy (shallow)
if [ ! -d "/tmp/envoy-proto-build" ]; then
  git clone --depth 1 https://github.com/envoyproxy/envoy.git /tmp/envoy-proto-build
fi

cd /tmp/envoy-proto-build/api

# Install grpc tools if needed
pip install -q grpcio-tools==1.60.0 googleapis-common-protos

# Generate all protos that ext_proc depends on
# (This is the complete dependency tree for external_processor.proto)
python -m grpc_tools.protoc \
  -I. \
  -I. \
  --python_out="$PROTO_OUT" \
  --grpc_python_out="$PROTO_OUT" \
  envoy/service/ext_proc/v3/external_processor.proto \
  envoy/config/core/v3/base.proto \
  envoy/config/core/v3/address.proto \
  envoy/config/core/v3/socket_option.proto \
  envoy/config/core/v3/extension.proto \
  envoy/config/core/v3/backoff.proto \
  envoy/config/core/v3/http_uri.proto \
  envoy/type/v3/percent.proto \
  envoy/type/v3/http_status.proto \
  envoy/type/v3/semantic_version.proto \
  envoy/extensions/filters/http/ext_proc/v3/processing_mode.proto \
  2>&1 | grep -v "^Deprecation" || true

# Ensure __init__.py exists at each level
find "$PROTO_OUT" -type d -exec touch "{}/__init__.py" \;

echo "✓ Protos generated to $PROTO_OUT"
ls -la "$PROTO_OUT/envoy/service/ext_proc/v3/"
