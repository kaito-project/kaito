FROM python:3.11-slim

WORKDIR /app

# Install dependencies
COPY processor/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy server code
COPY processor/server.py .

# Run as non-root
USER 1000

# Start server
CMD ["python", "server.py"]
