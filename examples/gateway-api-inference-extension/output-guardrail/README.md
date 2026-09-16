# Gateway Output Guardrail PoC - PR2: OpenAI JSON Adapter (Go)

## Objective

Parse and extract OpenAI ChatCompletion response format so that PR3 can integrate actual guardrails.

This PR implements:
1. **gRPC server** (Go) - Parses `/v1/chat/completions` non-streaming JSON
2. **JSON handling** - Extracts `choices[*].message.content` into mutable structure
3. **Re-serialization** - Converts modified response back to JSON
4. **Error handling** - Gracefully handles malformed JSON and unsupported formats
5. **EnvoyFilter** - (unchanged from PR1) Istio configuration to insert ext_proc

## What This PR Does

- ✅ Parses OpenAI non-streaming JSON responses
- ✅ Extracts choices[*].message.content into a structured type
- ✅ Re-serializes to JSON (unchanged, but preparation for PR3)
- ✅ Handles malformed JSON gracefully
- ✅ Handles unsupported response formats (errors, non-OpenAI responses)

## What This PR Does NOT Do

- ❌ Call any guardrail scanners (ban_substrings, secrets, etc.)
- ❌ Modify content (that's PR3's job)
- ❌ Support streaming
- ❌ Support other LLM response formats (Anthropic, etc.)

Those come in PR3, PR4+.

## Architecture

```
Client sends request
    ↓
Istio Gateway → KAITO backend
    ↓
Model response (OpenAI JSON format)
    ↓
Istio Envoy (response path only)
    ↓
[ext_proc filter intercepts response_body]
    │
    └─→ gRPC to llm-guard-ext-proc service
        ↓
    Parse JSON → ChatCompletionResponse struct
        ↓
    Extract choices[*].message.content
        ↓
    [PR3 will call guardrails here]
        ↓
    Re-serialize to JSON
        ↓
    Return modified body to Envoy
    ↓
Envoy replaces response body
    ↓
Client receives response
```

**Note**: Request path is NOT intercepted (request_body_mode: NONE). JSON parsing is non-streaming (response_body_mode: BUFFERED).

## Files

| File | Purpose |
|------|---------|
| `main.go` | Minimal Go gRPC server |
| `envoyfilter.yaml` | Istio EnvoyFilter configuration |
| `deployment.yaml` | Kubernetes Deployment + Service |
| `Dockerfile` | Multi-stage Go build |

## How to Build and Deploy

### Step 1: Build Docker Image

```bash
# From repository root
cd <path-to-kaito>

# Build from repo root (needs go.mod/go.sum for dependencies)
docker build -t yiqi685/llm-guard-ext-proc:v1 \
  -f examples/gateway-api-inference-extension/output-guardrail/Dockerfile .

docker push yiqi685/llm-guard-ext-proc:v1
```

### Step 2: Update Image in deployment.yaml

Edit `deployment.yaml` and verify image is set to:
```yaml
image: yiqi685/llm-guard-ext-proc:v1
```

### Step 3: Deploy to Kubernetes

```bash
cd examples/gateway-api-inference-extension/output-guardrail

# Deploy the ext_proc service
kubectl apply -f deployment.yaml

# Verify service is running
kubectl get pods -l app=llm-guard-ext-proc
kubectl logs -f deployment/llm-guard-ext-proc

# Apply the EnvoyFilter to the Gateway
kubectl apply -f envoyfilter.yaml
```

### Step 4: Verify EnvoyFilter is Loaded

```bash
# Get the gateway pod name (inference-gateway runs in default namespace)
GATEWAY_POD=$(kubectl get pods -n default \
  -l gateway.networking.k8s.io/gateway-name=inference-gateway \
  -o jsonpath='{.items[0].metadata.name}')

# Check if filter is in the listener config
istioctl proxy-config listener "$GATEWAY_POD" -n default | grep -A 10 "ext_proc"

# Check the cluster was added
istioctl proxy-config cluster "$GATEWAY_POD" -n default | grep llm_guard
```

## Test

### Mock Backend (for manual E2E testing)

This PR includes a mock LLM backend for testing without a real model:

```bash
# Deploy mock backend and route
kubectl apply -f mock-backend.yaml
kubectl apply -f mock-route.yaml

# Verify mock-llm pod is running
kubectl get pods -l app=mock-llm
```

The mock backend returns: `{"choices":[{"message":{"content":"hello from mock model"}}]}`

### Simple Curl Test

```bash
# Send a request through the Gateway
curl -X POST "http://$GATEWAY_IP/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{"model":"phi-4-mini-instruct","stream":false,"messages":[{"role":"user","content":"hello"}]}'

# Should receive valid OpenAI JSON response (unchanged in PR2)
```

### What You Should See

**Before PR2 (PR1):**
```json
{"choices":[{"message":{"content":"hello world"}}]} [GATEWAY_TEST]
```

**After PR2 (this version):**
```json
{"choices":[{"message":{"content":"hello from mock model"}}]}
```

The JSON is valid. Content is unchanged (no guardrails yet). Structure is preserved.

## Current Status

✅ **PR2: OpenAI JSON Adapter Implemented**

### What's New in PR2

- ✅ Parses JSON response body into `ChatCompletionResponse` struct
- ✅ Extracts `choices[*].message.content` safely
- ✅ Handles malformed JSON (returns original on parse error)
- ✅ Handles unsupported response formats gracefully
- ✅ Re-serializes modified response back to JSON
- ✅ Code size: ~120 LOC (within PR2 target)

### Verification Results

- ✅ ext_proc gRPC service: Listening on :9000
- ✅ JSON parsing: Successfully extracts message content
- ✅ Re-serialization: Valid JSON output
- ✅ Mock backend: Returns parseable OpenAI response
- ✅ Full chain verified: Client → Gateway → mock-llm → ext_proc (JSON parsing) → response

### Key Insight from PR2

PR2 establishes the foundation for guardrails integration. By parsing the response into a structured type (`ChatCompletionResponse`), PR3 can cleanly call guardrails scanners without JSON manipulation.

## Known Limitations

- ⚠️ Non-streaming only (response_body_mode: BUFFERED)
- ⚠️ OpenAI format only (`/v1/chat/completions`)
- ⚠️ No error handling for non-UTF8 responses
- ⚠️ Fail-open mode: non-JSON responses passed through unchanged
- ⚠️ No actual guardrails scanning (that's PR3)

These are intentional for PR2. PR3 will add guardrails integration.

## Next Steps

- PR3: Integrate actual llm-guard scanners (ban_substrings, regex, secrets, PII)
- PR4: Streaming support + incremental guardrails
- PR5+: Request context, advanced scanners, KAITO API integration

## References

- [Envoy External Processing Filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
- [Istio EnvoyFilter](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
- [go-control-plane](https://github.com/envoyproxy/go-control-plane)
