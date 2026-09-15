FROM python:3.11-slim

WORKDIR /app

# Install runtime dependencies
COPY processor/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy server code
# Note: Envoy proto files must be pre-generated and available
# (See README.md for local testing setup)
COPY processor/server.py .
COPY envoy/ ./envoy/

# Run as non-root
USER 1000

CMD ["python", "server.py"]
