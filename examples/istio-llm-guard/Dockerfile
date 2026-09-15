FROM python:3.12-slim-bookworm

WORKDIR /app

COPY examples/istio-llm-guard/requirements.txt .
RUN pip install --no-cache-dir --requirement requirements.txt

COPY examples/istio-llm-guard/scanner_service.py .

USER 65532:65532

ENTRYPOINT ["uvicorn", "scanner_service:app", "--host", "0.0.0.0", "--port", "8000"]