FROM python:3.11-slim

WORKDIR /app

# Install gRPC tools for proto compilation
RUN pip install --no-cache-dir grpcio==1.60.0 grpcio-tools==1.60.0

# Download and compile Envoy ext_proc proto
RUN git clone --depth 1 https://github.com/envoyproxy/envoy.git /tmp/envoy && \
    python -m grpc_tools.protoc \
      -I/tmp/envoy \
      --python_out=. \
      --grpc_python_out=. \
      /tmp/envoy/envoy/service/ext_proc/v3/external_processor.proto && \
    rm -rf /tmp/envoy

# Install runtime dependencies
COPY processor/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Remove build tools (optional optimization)
RUN pip uninstall -y grpcio-tools

# Copy server code
COPY processor/server.py .

# Run as non-root
USER 1000

CMD ["python", "server.py"]
