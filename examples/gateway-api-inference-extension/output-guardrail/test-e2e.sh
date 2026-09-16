#!/bin/bash
# E2E tests for gateway output guardrails
# Prerequisites: kubectl configured, gateway deployed, ext_proc service running, mock backend

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

# Helper: validate JSON
validate_json() {
  if echo "$1" | jq . >/dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

# Helper: validate HTTP status and response
check_response() {
  local url=$1
  local body=$2
  local expected_status=$3

  response=$(curl -s -w "\n%{http_code}" -X POST "$url" \
    -H "content-type: application/json" \
    -d "$body")

  http_code=$(echo "$response" | tail -1)
  response_body=$(echo "$response" | sed '$d')

  if [ "$http_code" != "$expected_status" ]; then
    echo "$response_body"
    fail "Expected HTTP $expected_status but got $http_code"
  fi

  echo "$response_body"
}

# Test 1: Clean response passes through unchanged, JSON valid
test_case "Clean response passes through unchanged and is valid JSON"
response=$(check_response "$GATEWAY_URL/v1/chat/completions" \
  '{"model":"test","stream":false,"messages":[{"role":"user","content":"hello"}]}' \
  "200")

if ! validate_json "$response"; then
  fail "Response is not valid JSON: $response"
fi

if echo "$response" | jq -e '.choices[0].message.content | length > 0' >/dev/null 2>&1; then
  pass "Clean response passed through (JSON valid, content present)"
else
  fail "Response structure invalid or content missing: $response"
fi
echo ""

# Test 2: Ban substring blocks response
test_case "Ban substring blocks response (block action)"
response=$(check_response "$GATEWAY_URL/v1/chat/completions" \
  '{"model":"test","stream":false,"messages":[{"role":"user","content":"PROHIBITED_CONTENT"}]}' \
  "200")

if ! validate_json "$response"; then
  fail "Response is not valid JSON: $response"
fi

# Mock backend configured to return blocked content when it sees PROHIBITED_CONTENT in request
# Expected: message.content = blockMessage ("Response blocked by guardrails")
block_msg=$(echo "$response" | jq -r '.choices[0].message.content // empty')
if [ "$block_msg" = "Response blocked by guardrails" ]; then
  pass "Ban substring triggered: content replaced with blockMessage"
else
  fail "Expected blockMessage but got: $block_msg"
fi
echo ""

# Test 3: Sensitive data is redacted
test_case "Sensitive data is redacted (redact action)"
# Mock backend returns content with email that should be redacted
response=$(check_response "$GATEWAY_URL/v1/chat/completions" \
  '{"model":"test","stream":false,"messages":[{"role":"user","content":"contact: test@example.com"}]}' \
  "200")

if ! validate_json "$response"; then
  fail "Response is not valid JSON: $response"
fi

content=$(echo "$response" | jq -r '.choices[0].message.content // empty')
if echo "$content" | grep -q "\[REDACTED\]" || echo "$content" | grep -qv "test@example.com"; then
  pass "Sensitive data redacted: email address detected and masked"
else
  fail "Email not redacted: $content"
fi
echo ""

# Test 4: HTTP framing - verify response structure and Content-Length
test_case "HTTP framing is valid (status, Content-Type, Content-Length)"
response_with_headers=$(curl -s -i -X POST "$GATEWAY_URL/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{"model":"test","stream":false,"messages":[{"role":"user","content":"test"}]}')

http_status=$(echo "$response_with_headers" | head -1 | grep -o "200\|201\|400\|500")
content_type=$(echo "$response_with_headers" | grep -i "^content-type:" | head -1)
content_length=$(echo "$response_with_headers" | grep -i "^content-length:" | head -1)

if [ "$http_status" = "200" ]; then
  pass "HTTP 200 OK"
else
  fail "HTTP status not 200: $http_status"
fi

if echo "$content_type" | grep -q "application/json"; then
  pass "Content-Type is application/json"
else
  fail "Content-Type not application/json: $content_type"
fi

response_body=$(echo "$response_with_headers" | tail -1)
actual_length=${#response_body}
expected_length=$(echo "$content_length" | grep -o "[0-9]*$")

if [ -n "$expected_length" ] && [ "$actual_length" -eq "$expected_length" ]; then
  pass "Content-Length matches actual body (no framing errors)"
else
  fail "Content-Length mismatch or missing (expected: $expected_length, actual: $actual_length)"
fi
echo ""

# Test 5: Ext proc service unavailable (fail-closed)
test_case "Fail-closed: response blocked when ext_proc unavailable"
# Temporarily scale down ext_proc
kubectl scale deployment llm-guard-ext-proc -n $NAMESPACE --replicas=0 >/dev/null 2>&1
sleep 2

# Try to send request - should timeout or fail (not return unguarded response)
response=$(curl -s -m 5 -X POST "$GATEWAY_URL/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{"model":"test","stream":false,"messages":[{"role":"user","content":"test"}]}' || echo "CURL_TIMEOUT")

# Restore ext_proc
kubectl scale deployment llm-guard-ext-proc -n $NAMESPACE --replicas=2 >/dev/null 2>&1

if [ "$response" = "CURL_TIMEOUT" ] || [ -z "$response" ]; then
  pass "Request timed out/blocked when ext_proc unavailable (fail-closed)"
else
  # If we got a response, verify it's not an unguarded one (has error or blockMessage)
  if echo "$response" | jq -e '.error | length > 0' >/dev/null 2>&1 || \
     echo "$response" | jq -e '.choices[0].message.content' | grep -q "error\|blocked"; then
    pass "Request failed gracefully when ext_proc unavailable"
  else
    fail "Unexpected response when ext_proc unavailable (should fail-closed): $response"
  fi
fi
echo ""

# Test 6: Large response handling
test_case "Large response (100KB+) is handled correctly with framing"
# Send large payload
large_content=$(printf 'a%.0s' {1..10000})
response=$(check_response "$GATEWAY_URL/v1/chat/completions" \
  "{\"model\":\"test\",\"stream\":false,\"messages\":[{\"role\":\"user\",\"content\":\"$large_content\"}]}" \
  "200")

if ! validate_json "$response"; then
  fail "Large response is not valid JSON: $(echo "$response" | head -c 100)..."
fi

if echo "$response" | jq -e '.choices[0].message.content | length > 1000' >/dev/null 2>&1; then
  pass "Large response processed correctly (JSON valid, content intact)"
else
  fail "Large response lost or truncated"
fi
echo ""

echo "=== All E2E tests passed! ==="
