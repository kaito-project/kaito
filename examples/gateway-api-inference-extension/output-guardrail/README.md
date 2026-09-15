# Gateway Output Guardrail PoC - PR1: Minimal ext_proc

## Objective

Prove that Istio Gateway can intercept LLM response and mutate it via Envoy ext_proc.

This PR implements:
1. **gRPC server** - Minimal ext_proc service that appends a test string to response body
2. **EnvoyFilter** - Istio configuration to insert ext_proc into Gateway filter chain
3. **Kubernetes manifests** - Deployment, Service for the ext_proc service

## What This PR Does

- ✅ Receives response body from Envoy (via bidirectional gRPC stream)
- ✅ Appends `" [GATEWAY_TEST]"` to the body
- ✅ Returns modified body to Envoy
- ✅ Proves response mutation chain works end-to-end

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
| `processor/server.py` | gRPC server (~100 LOC) |
| `processor/requirements.txt` | Python dependencies |
| `envoyfilter.yaml` | Istio EnvoyFilter configuration |
| `deployment.yaml` | Kubernetes Deployment + Service |
| `Dockerfile` | Container image definition |

## How to Build and Deploy

### Step 1: Build Docker Image

```bash
cd examples/gateway-api-inference-extension/output-guardrail/

docker build -t your-registry/llm-guard-ext-proc:v1 .
docker push your-registry/llm-guard-ext-proc:v1
```

### Step 2: Update Image in deployment.yaml

Edit `deployment.yaml` and change:
```yaml
image: your-registry/llm-guard-ext-proc:v1
```

### Step 3: Deploy to Kubernetes

```bash
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
# Check if filter is in the listener config
istioctl proxy-config listener \
  -l gateway.networking.k8s.io/gateway-name=inference-gateway \
  -n default | grep -A 10 "ext_proc"

# Check the cluster was added
istioctl proxy-config cluster \
  -l gateway.networking.k8s.io/gateway-name=inference-gateway | grep llm_guard
```

## Test

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

## Success Criteria for This PR

- ✅ ext_proc service starts without errors
- ✅ EnvoyFilter is successfully applied to Gateway
- ✅ Response body is received by processor
- ✅ Response is modified and returned
- ✅ Client receives modified response with `[GATEWAY_TEST]` appended
- ✅ EPP (Endpoint Picker) routing still works (regression check)

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
