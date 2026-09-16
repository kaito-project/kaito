#!/bin/bash
# Latency benchmark for gateway output guardrails
# Measures overhead introduced by ext_proc scanning

set -e

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
NUM_REQUESTS="${NUM_REQUESTS:-100}"
CONCURRENCY="${CONCURRENCY:-5}"

echo "=== Gateway Output Guardrail Latency Benchmark ==="
echo "Gateway URL: $GATEWAY_URL"
echo "Requests: $NUM_REQUESTS"
echo "Concurrency: $CONCURRENCY"
echo ""

# Benchmark without guardrails (theoretical: disable ext_proc filter)
# In practice, this would require scaling down ext_proc or using a different gateway config
benchmark() {
  local label=$1
  local url=$2

  echo "Testing: $label"

  # Use Apache Bench (ab) if available, otherwise curl loop
  if command -v ab &> /dev/null; then
    ab -n $NUM_REQUESTS -c $CONCURRENCY -q \
      -H "content-type: application/json" \
      -p <(echo '{"model":"phi","stream":false,"messages":[{"role":"user","content":"test"}]}') \
      "$url/v1/chat/completions"
    echo ""
  else
    echo "Apache Bench not found. Using curl loop..."
    total_time=0
    min_time=999999
    max_time=0

    for i in $(seq 1 $NUM_REQUESTS); do
      start=$(date +%s%N)
      curl -s -X POST "$url/v1/chat/completions" \
        -H "content-type: application/json" \
        -d '{"model":"phi","stream":false,"messages":[{"role":"user","content":"test"}]}' \
        > /dev/null
      end=$(date +%s%N)

      elapsed=$(( (end - start) / 1000000 ))  # Convert to ms
      total_time=$((total_time + elapsed))

      if [ $elapsed -lt $min_time ]; then min_time=$elapsed; fi
      if [ $elapsed -gt $max_time ]; then max_time=$elapsed; fi

      if [ $((i % 10)) -eq 0 ]; then
        echo "  Completed $i/$NUM_REQUESTS requests"
      fi
    done

    avg_time=$((total_time / NUM_REQUESTS))
    echo "Results:"
    echo "  Min: ${min_time}ms"
    echo "  Max: ${max_time}ms"
    echo "  Avg: ${avg_time}ms"
    echo ""
  fi
}

# Run benchmark
benchmark "With guardrails" "$GATEWAY_URL"

echo "=== Benchmark Complete ==="
echo ""
echo "Notes:"
echo "- This measures the full request path including gateway routing and scanning"
echo "- For accurate overhead measurement, compare with a baseline (ext_proc disabled)"
echo "- Latency includes: LLM inference time (mock backend) + ext_proc scanning"
echo ""
echo "To measure ext_proc overhead specifically:"
echo "1. Disable the ext_proc filter in EnvoyFilter"
echo "2. Run baseline benchmark"
echo "3. Re-enable filter and run this benchmark"
echo "4. Overhead = guardrail_latency - baseline_latency"
