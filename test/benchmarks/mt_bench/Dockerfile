FROM python:3.12-slim

LABEL org.opencontainers.image.source="https://github.com/kaito-project/kaito"
LABEL org.opencontainers.image.description="MT-Bench evaluation for Kaito workspaces"

RUN groupadd -r mtbench && useradd -r -g mtbench -d /app mtbench

WORKDIR /app

COPY benchmarks/mt_bench/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY benchmarks/mt_bench/mt_bench_eval.py .

RUN mkdir -p /app/data && chown mtbench:mtbench /app/data
RUN mkdir /results && chown mtbench:mtbench /results

USER mtbench

ENTRYPOINT ["python3", "mt_bench_eval.py"]
