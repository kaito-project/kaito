FROM python:3.11-slim as builder

WORKDIR /build

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    && rm -rf /var/lib/apt/lists/*

# Install proto compilation tools
RUN pip install --no-cache-dir grpcio-tools==1.60.0 googleapis-common-protos

# Clone Envoy and generate complete proto dependency tree
RUN git clone --depth 1 https://github.com/envoyproxy/envoy.git && \
    cd envoy/api && \
    python -m grpc_tools.protoc \
      -I. \
      --python_out=/build \
      --grpc_python_out=/build \
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
      envoy/extensions/filters/http/ext_proc/v3/processing_mode.proto && \
    # Ensure all __init__.py exist
    find /build/envoy -type d -exec touch "{}/__init__.py" \;

FROM python:3.11-slim

WORKDIR /app

# Install runtime dependencies
COPY processor/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy generated proto files from builder
COPY --from=builder /build/envoy ./envoy/

# Copy server code
COPY processor/server.py .

# Smoke test: verify all required proto types are available
RUN python << 'PYEOF'
from envoy.service.ext_proc.v3 import external_processor_pb2 as p
from envoy.service.ext_proc.v3 import external_processor_pb2_grpc as g

assert hasattr(p, 'ProcessingResponse'), "Missing ProcessingResponse"
assert hasattr(p, 'BodyResponse'), "Missing BodyResponse"
assert hasattr(p, 'CommonResponse'), "Missing CommonResponse"
assert hasattr(p, 'BodyMutation'), "Missing BodyMutation"
assert hasattr(p, 'HttpBody'), "Missing HttpBody"
assert hasattr(g, 'ExternalProcessorServicer'), "Missing ExternalProcessorServicer"

print("✓ All required proto types verified")
PYEOF

# Run as non-root
USER 1000

CMD ["python", "server.py"]
