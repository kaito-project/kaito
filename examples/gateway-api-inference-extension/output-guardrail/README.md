# Gateway Output Guardrail PoC - PR1: Minimal ext_proc (Go)

## Objective

Prove that Istio Gateway can intercept LLM response and mutate it via Envoy ext_proc.

This PR implements:
1. **gRPC server** (Go) - Minimal ext_proc service that appends a test string to response body
2. **EnvoyFilter** - Istio configuration to insert ext_proc into Gateway filter chain
3. **Kubernetes manifests** - Deployment, Service for the ext_proc service

## What This PR Does

- ✅ Receives response body from Envoy (via bidirectional gRPC stream)
- ✅ Appends `" [GATEWAY_TEST]"` to the body
- ✅ Returns modified body to Envoy
- ✅ End-to-end validation: Gateway response gets mutated through mock backend

## What This PR Does NOT Do

- ❌ Parse JSON responses
- ❌ Call llm-guard or any scanner
- ❌ Understand OpenAI format
- ❌ Support streaming

Those come in PR2, PR3, etc.

## Architecture

```
Client sends request
    ↓
Istio Gateway → KAITO backend
    ↓
Model response
    ↓
Istio Envoy (response path only)
    ↓
[ext_proc filter intercepts response_body]
    │
    └─→ gRPC to llm-guard-ext-proc service
        ↓
    Append [GATEWAY_TEST] to body
        ↓
    Return modified body to Envoy
    ↓
Envoy replaces response body
    ↓
Client receives mutated response
```

**Note**: Request path is NOT intercepted (request_body_mode: NONE).

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

# Look for [GATEWAY_TEST] appended to the response
```

### What You Should See

**Before guard:**
```json
{"choices":[{"message":{"content":"hello world"}}]}
```

**After guard (with this PoC):**
```json
{"choices":[{"message":{"content":"hello world"}}]} [GATEWAY_TEST]
```

The `[GATEWAY_TEST]` string is appended to the entire JSON body (yes, it breaks JSON - that's OK for this PoC, we'll fix parsing in PR2).

## Current Status

✅ **PoC Verified: Gateway API Gateway Can Intercept and Mutate Responses**

### Verification Results

- ✅ ext_proc gRPC service: Listening on :9000
- ✅ Network connectivity: Gateway pod can reach ext_proc service
- ✅ Mock backend: HTTPRoute routing works
- ✅ **EnvoyFilter application**: ext_proc cluster appears in Envoy config
- ✅ **Response mutation**: [GATEWAY_TEST] successfully appended to response
- ✅ **Full chain verified**: Client → Gateway → mock-llm → ext_proc → mutated response

### Key Insight

HTTP/2 protocol must be explicitly configured in the upstream cluster for gRPC communication to work correctly with Envoy ext_proc. Without `typed_extension_protocol_options` with `http2_protocol_options`, Envoy doesn't properly negotiate with the gRPC service.

## Known Limitations

- ⚠️ Appends test string to raw bytes (breaks JSON format)
- ⚠️ No error handling for non-UTF8 responses
- ⚠️ No metrics or logging for production use
- ⚠️ No streaming support
- ⚠️ Fail-closed mode: if ext_proc is down, requests fail

These are all intentional for the PoC. PR2+ will improve these.

## Next Steps

- PR2: Parse OpenAI JSON responses properly
- PR3: Integrate actual llm-guard scanners
- PR4: Add comprehensive tests and benchmarks

## References

- [Envoy External Processing Filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
- [Istio EnvoyFilter](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
- [go-control-plane](https://github.com/envoyproxy/go-control-plane)
