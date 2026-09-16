FROM python:3.11-slim

WORKDIR /app

# Copy KAITO requirements
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy presets (guardrails library)
COPY presets/ ./presets/

# Copy ext_proc server
COPY examples/gateway-api-inference-extension/output-guardrail/ext_proc_server.py .

# Default policy path (override with env var or volume mount)
ENV OUTPUT_GUARDRAILS_POLICY_PATH=/etc/kaito/guardrails/policy.yaml

# Run as non-root
USER 1000:1000

ENTRYPOINT ["python", "ext_proc_server.py"]
