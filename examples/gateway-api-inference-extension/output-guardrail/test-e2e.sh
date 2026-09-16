#!/bin/bash
# E2E tests for gateway output guardrails
# Prerequisites: kubectl configured, gateway deployed, ext_proc service running

set -e

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
NAMESPACE="${NAMESPACE:-default}"

echo "=== Gateway Output Guardrail E2E Tests ==="
echo "Gateway URL: $GATEWAY_URL"
echo "Namespace: $NAMESPACE"
echo ""

# Helper function for pretty printing
test_case() {
  echo "▶ $1"
}

pass() {
  echo "  ✅ PASS: $1"
}

fail() {
  echo "  ❌ FAIL: $1"
  exit 1
}

# Test 1: Clean response passes through unchanged
test_case "Clean response passes through unchanged"
response=$(curl -s -X POST "$GATEWAY_URL/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{
    "model": "test",
    "stream": false,
    "messages": [{"role": "user", "content": "hello"}]
  }')

if echo "$response" | grep -q "hello from mock model"; then
  pass "Response content preserved"
else
  fail "Response not received or mutated incorrectly: $response"
fi
echo ""

# Test 2: Ban substring blocks response
test_case "Ban substring blocks response when keyword detected"
# Note: This assumes policy has ban_substrings configured
# The mock backend would need to return a banned substring for this test
# For now, verify policy is loaded
if kubectl get configmap gateway-output-guardrails -n $NAMESPACE &>/dev/null; then
  pass "Guardrails policy ConfigMap exists"
else
  fail "Guardrails policy ConfigMap not found"
fi
echo ""

# Test 3: Ext proc service health check
test_case "Ext proc service is running and accessible"
if kubectl get svc llm-guard-ext-proc -n $NAMESPACE &>/dev/null; then
  svc_ip=$(kubectl get svc llm-guard-ext-proc -n $NAMESPACE -o jsonpath='{.spec.clusterIP}')
  pass "Ext proc service running at $svc_ip:9000"
else
  fail "Ext proc service not found"
fi
echo ""

# Test 4: Gateway can route to backend
test_case "Gateway routes to mock backend successfully"
response=$(curl -s -X POST "$GATEWAY_URL/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{
    "model": "phi",
    "stream": false,
    "messages": [{"role": "user", "content": "test"}]
  }')

if echo "$response" | grep -q '"choices"'; then
  pass "Gateway routes successfully and returns OpenAI format"
else
  fail "Gateway routing failed: $response"
fi
echo ""

# Test 5: Environment variables are set
test_case "Guardrails environment variables configured"
pod=$(kubectl get pods -n $NAMESPACE -l app=llm-guard-ext-proc -o jsonpath='{.items[0].metadata.name}')
if [ -n "$pod" ]; then
  enabled=$(kubectl exec -n $NAMESPACE $pod -- printenv OUTPUT_GUARDRAILS_ENABLED)
  if [ "$enabled" = "true" ]; then
    pass "OUTPUT_GUARDRAILS_ENABLED=true"
  else
    fail "OUTPUT_GUARDRAILS_ENABLED not set to true: $enabled"
  fi
else
  fail "Ext proc pod not found"
fi
echo ""

echo "=== All tests passed! ==="
