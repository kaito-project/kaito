FROM python:3.11-slim

WORKDIR /app

# Copy and install RAGEngine dependencies
COPY presets/ragengine/requirements.txt /tmp/ragengine-requirements.txt
RUN pip install --no-cache-dir -r /tmp/ragengine-requirements.txt

# Copy and install ext_proc additional dependencies (grpcio, protobuf, etc.)
COPY examples/gateway-api-inference-extension/output-guardrail/requirements.txt /tmp/extproc-requirements.txt
RUN pip install --no-cache-dir -r /tmp/extproc-requirements.txt

# Copy presets (guardrails library)
COPY presets/ ./presets/

# Copy ext_proc server
COPY examples/gateway-api-inference-extension/output-guardrail/ext_proc_server.py .

# Set Python path so 'import ragengine' finds presets/ragengine
ENV PYTHONPATH=/app/presets
ENV OUTPUT_GUARDRAILS_POLICY_PATH=/etc/kaito/guardrails/policy.yaml

# Run as non-root
USER 1000:1000

ENTRYPOINT ["python", "ext_proc_server.py"]
