# Gateway Output Guardrail PoC - PR3: Guardrails Scanner Integration (Go)

## Objective

Integrate response-only guardrail scanners (ban_substrings) into the ext_proc pipeline.

This PR implements:
1. **gRPC server** (Go) - ext_proc service with integrated guardrails scanning
2. **Guardrails engine** - Response-only scanners (ban_substrings with block action)
3. **Configuration** - Load guardrails config from environment variables
4. **Message mutation** - Replace or block content based on scan results
5. **Test coverage** - Unit tests for core scanning logic

## What This PR Does

- ✅ Scans all choices[*].message.content for banned substrings
- ✅ Blocks responses that contain banned substrings (returns blockMessage)
- ✅ Preserves all original response structure and fields
- ✅ Case-insensitive substring detection
- ✅ Configuration via environment variables (GUARDRAILS_ENABLED, BANNED_SUBSTRINGS)
- ✅ Fail-closed: blocks if scan fails

## What This PR Does NOT Do

- ❌ Support request context (prompt-dependent scanners)
- ❌ Streaming responses
- ❌ Multiple scanner types (only ban_substrings for now)
- ❌ Redaction (only block action in v1)
- ❌ Integration with KAITO OutputGuardrails Python library (future PR)

Those come in PR4+.

## Architecture

```
Client request
    ↓
Istio Gateway → KAITO backend
    ↓
Model response (OpenAI JSON)
    ↓
Istio Envoy (response path)
    ↓
[ext_proc filter intercepts]
    │
    └─→ gRPC to ext_proc service
        ↓
    Parse JSON (map[string]any)
        ↓
    Extract choices[*].message.content
        ↓
    Apply GuardrailsEngine.ScanContent()
        ├─ Check for banned substrings
        └─ Block if detected
        ↓
    Update message["content"]
        ↓
    Re-serialize JSON
    ↓
Envoy sends modified response
    ↓
Client receives scanned response
```

## Configuration

Environment variables:
- `GUARDRAILS_ENABLED=true` - Enable/disable scanning
- `BANNED_SUBSTRINGS="SECRET,PASSWORD,API_KEY"` - Comma-separated banned strings
- `BLOCK_MESSAGE="Response blocked by guardrails"` - Message on block

## Test

```bash
# Deploy mock backend
kubectl apply -f mock-backend.yaml
kubectl apply -f mock-route.yaml

# Deploy with guardrails enabled
export GUARDRAILS_ENABLED=true
export BANNED_SUBSTRINGS="INTERNAL_SECRET,PASSWORD"
docker build -t your-registry/llm-guard-ext-proc:v3 .
docker push your-registry/llm-guard-ext-proc:v3

kubectl set env deployment/llm-guard-ext-proc \
  GUARDRAILS_ENABLED=true \
  BANNED_SUBSTRINGS="INTERNAL_SECRET"

# Test clean response
curl -X POST "http://$GATEWAY_IP/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{"model":"phi","stream":false,"messages":[{"role":"user","content":"hello"}]}'

# Expected: normal response passes through

# Test blocked response
# (If backend outputs "INTERNAL_SECRET", it gets blocked)
```

## Current Status

✅ **PR3: Response-Only Guardrails Scanners Implemented**

### What's New in PR3

- ✅ `GuardrailsEngine` with `ScanContent()` method
- ✅ Ban_substrings scanner with block action
- ✅ Environment-based configuration (no YAML parsing)
- ✅ Case-insensitive matching
- ✅ 4 comprehensive unit tests (clean/banned/case/multiple substrings)
- ✅ Core adapter: 203 LOC (130+ new for guardrails logic)

### Verification Results

- ✅ Unit tests: 4/4 passing
- ✅ Scans all choices in response
- ✅ Preserves JSON structure on block
- ✅ Case-insensitive detection works
- ✅ Multiple banned substrings supported

## Known Limitations

- ⚠️ Ban_substrings only (no secrets, PII, regex yet)
- ⚠️ Block action only (no redact/allow yet)
- ⚠️ No request context (prompt-dependent scanners deferred)
- ⚠️ No streaming support
- ⚠️ Environment variable configuration (not production ConfigMap yet)

These are intentional for PR3 v1. PR4 will add more scanners and actions.

## Next Steps

- PR4: Add more scanners (secrets, regex, PII detection)
- PR5: Support redact/allow actions in addition to block
- PR6: Integration with KAITO OutputGuardrails Python library
- PR7: Request context support for advanced scanners
- PR8: Streaming support