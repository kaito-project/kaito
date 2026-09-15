FROM python:3.11-slim as builder

WORKDIR /build

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    && rm -rf /var/lib/apt/lists/*

# Install grpc tools
RUN pip install --no-cache-dir grpcio-tools==1.60.0

# Clone Envoy and generate protos
RUN git clone --depth 1 https://github.com/envoyproxy/envoy.git && \
    cd envoy/api && \
    python -m grpc_tools.protoc \
      -I. \
      --python_out=/build \
      --grpc_python_out=/build \
      envoy/service/ext_proc/v3/external_processor.proto

---

FROM python:3.11-slim

WORKDIR /app

# Install runtime dependencies
COPY processor/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy generated proto files from builder
COPY --from=builder /build/envoy ./envoy/

# Copy server code
COPY processor/server.py .

# Run as non-root
USER 1000

CMD ["python", "server.py"]
