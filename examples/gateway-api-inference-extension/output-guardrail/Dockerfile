FROM python:3.11-slim

WORKDIR /app

# Copy KAITO requirements and presets
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY presets/ ./presets/

# Copy ext_proc server
COPY examples/gateway-api-inference-extension/output-guardrail/ext_proc_server.py .

# Set Python path so 'import ragengine' finds presets/ragengine
ENV PYTHONPATH=/app/presets
ENV OUTPUT_GUARDRAILS_POLICY_PATH=/etc/kaito/guardrails/policy.yaml

# Run as non-root
USER 1000:1000

ENTRYPOINT ["python", "ext_proc_server.py"]
