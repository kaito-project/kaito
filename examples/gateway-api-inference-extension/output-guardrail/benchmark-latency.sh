#!/bin/bash
# Performance benchmark: baseline vs with guardrails
# Measures ext_proc overhead by automatically disabling/enabling the filter

set -e

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
NUM_REQUESTS="${NUM_REQUESTS:-100}"
CONCURRENCY="${CONCURRENCY:-5}"
NAMESPACE="${NAMESPACE:-default}"
ENVOYFILTER_NAME="${ENVOYFILTER_NAME:-gateway-output-guardrail}"

echo "=== Gateway Output Guardrail Performance Benchmark ==="
echo "Gateway URL: $GATEWAY_URL"
echo "Requests: $NUM_REQUESTS"
echo "Concurrency: $CONCURRENCY"
echo ""

benchmark() {
  local label=$1
  local url=$2

  echo "Testing: $label"

  # Use Apache Bench (ab) if available, otherwise curl loop
  if command -v ab &> /dev/null; then
    ab -n $NUM_REQUESTS -c $CONCURRENCY -q \
      -H "content-type: application/json" \
      -p <(echo '{"model":"test","stream":false,"messages":[{"role":"user","content":"hello"}]}') \
      "$url/v1/chat/completions" 2>&1 | grep -E "Requests/sec|Time per request|Failed"
    echo ""
  else
    echo "Apache Bench not found. Using curl loop..."
    total_time=0
    min_time=999999
    max_time=0
    failed=0

    for i in $(seq 1 $NUM_REQUESTS); do
      start=$(date +%s%N)
      if curl -s -m 5 -X POST "$url/v1/chat/completions" \
        -H "content-type: application/json" \
        -d '{"model":"test","stream":false,"messages":[{"role":"user","content":"hello"}]}' \
        > /dev/null 2>&1; then
        end=$(date +%s%N)
        elapsed=$(( (end - start) / 1000000 ))  # Convert to ms
        total_time=$((total_time + elapsed))

        if [ $elapsed -lt $min_time ]; then min_time=$elapsed; fi
        if [ $elapsed -gt $max_time ]; then max_time=$elapsed; fi
      else
        failed=$((failed + 1))
      fi

      if [ $((i % 20)) -eq 0 ]; then
        echo "  Completed $i/$NUM_REQUESTS requests"
      fi
    done

    successful=$((NUM_REQUESTS - failed))
    if [ $successful -gt 0 ]; then
      avg_time=$((total_time / successful))
      echo "Results ($successful successful, $failed failed):"
      echo "  Min: ${min_time}ms"
      echo "  Max: ${max_time}ms"
      echo "  Avg: ${avg_time}ms"
    else
      echo "All requests failed"
    fi
    echo ""
  fi
}

# Step 1: Disable ext_proc filter
echo "=== Phase 1: Baseline (ext_proc disabled) ==="
echo "Disabling ext_proc filter..."
kubectl patch envoyfilter $ENVOYFILTER_NAME -n $NAMESPACE --type merge \
  -p '{"spec":{"workloadSelector":{"labels":{"disabled":"true"}}}}' >/dev/null 2>&1 || \
  kubectl annotate envoyfilter $ENVOYFILTER_NAME -n $NAMESPACE \
  gauge-benchmark-disabled="true" --overwrite >/dev/null 2>&1
sleep 3

benchmark "Baseline (no guardrails)" "$GATEWAY_URL"

# Step 2: Re-enable ext_proc filter
echo "=== Phase 2: With Guardrails (ext_proc enabled) ==="
echo "Enabling ext_proc filter..."
kubectl patch envoyfilter $ENVOYFILTER_NAME -n $NAMESPACE --type merge \
  -p '{"spec":{"workloadSelector":{"labels":{"disabled":null}}}}' >/dev/null 2>&1 || \
  kubectl annotate envoyfilter $ENVOYFILTER_NAME -n $NAMESPACE \
  gauge-benchmark-disabled- --overwrite >/dev/null 2>&1
sleep 3

benchmark "With guardrails" "$GATEWAY_URL"

echo "=== Benchmark Complete ==="
echo ""
echo "Overhead Measurement:"
echo "- If baseline avg is X ms and with-guardrails avg is Y ms"
echo "- Overhead = Y - X ms"
echo "- Overhead % = (Y - X) / X * 100%"
echo ""
echo "Notes:"
echo "- This measures the full request path including gateway routing and scanning"
echo "- Latency includes: HTTP processing + mock backend + ext_proc scanning"
echo "- For streaming, overhead may be lower due to chunked processing"
echo "- For large responses (>100KB), overhead is amortized across chunks"
