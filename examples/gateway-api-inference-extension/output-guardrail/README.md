# Gateway Output Guardrail PoC - PR3: Reuse Existing OutputGuardrails (Python)

## Objective

Integrate KAITO's existing OutputGuardrails into the ext_proc pipeline.

This PR implements:
1. **Python ext_proc server** - Direct integration with OutputGuardrails.guard_response()
2. **Response-only guardrails** - Applies scanner_schemas without request context
3. **Full scanner support** - ban_substrings, secrets, sensitive (PII) via existing implementation
4. **Actions via guardrails** - allow/redact/block determined by policy (not hardcoded)

## What This PR Does

- ✅ Reuses existing `OutputGuardrails.guard_response()`
- ✅ Applies all response-only scanners from scanner_schemas:
  - `ban_substrings` (with match_type, case_sensitive options)
  - `secrets` (AWS keys, tokens, etc.)
  - `sensitive` (email, phone, credit card, IP address)
- ✅ Respects policy actions: allow → pass through, redact → mask, block → replace with blockMessage
- ✅ Preserves all original JSON fields and structure
- ✅ Response-only mode: no request context passed to scanners requiring it

## What This PR Does NOT Do

- ❌ Implement scanners (uses existing implementation)
- ❌ Support request-context scanners (FactualConsistency, Relevance)
- ❌ Support streaming
- ❌ Parse policy YAML (uses GuardrailsReloader which does)

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
    └─→ gRPC to ext_proc service (Python)
        ↓
    Parse JSON
        ↓
    Extract choices[*].message.content
        ↓
    Call: OutputGuardrails.guard_response(response, request_metadata={})
        ↓
        scanner_schemas
            ↓
        SCANNER_REGISTRY
            ↓
        [ban_substrings] → substrings configured via policy YAML
        [secrets] → LLM-Guard built-in secret patterns
        [sensitive] → Email, phone, credit card, IP address detection
            ↓
        Apply actions per scanner
        (allow/redact/block)
        ↓
    Return guarded response
        ↓
    Update message["content"]
        ↓
    Re-serialize JSON
    ↓
Envoy sends modified response
    ↓
Client receives scanned response
```

## Code Structure

- `ext_proc_server.py`: ~150 LOC
  - `ExtProcService` class (gRPC handler)
  - `Process()` method (bidirectional streaming)
  - `_process_response_body()` (JSON handling)
  - `_apply_guardrails()` (thin wrapper around OutputGuardrails)

**Core glue logic** (~20 LOC):
```python
# Convert dict to ChatCompletionResponse
response = ChatCompletionResponse(**response_obj)

# Call existing OutputGuardrails
guarded = guardrails.guard_response(
    response,
    request={},  # Response-only mode
)

# Convert back to dict
return guarded.model_dump(mode="python")
```

## Configuration

Policy file at `/etc/kaito/guardrails/policy.yaml` (mounted via ConfigMap):

```yaml
enabled: true
blockMessage: "Response blocked by guardrails"
scanners:
  - type: ban_substrings
    action: block
    substrings: ["INTERNAL_ONLY", "SECRET_KEY"]
    match_type: word
    case_sensitive: false

  - type: secrets
    action: redact

  - type: sensitive
    action: redact
    detectors: [email, phone, credit_card]
```

## Deployment

```bash
# Update Dockerfile to reference Python version
docker build -t your-registry/llm-guard-ext-proc:pr3 .

# Update deployment.yaml image reference
kubectl set image deployment/llm-guard-ext-proc \
  llm-guard=your-registry/llm-guard-ext-proc:pr3

# Mount policy ConfigMap (already in deployment.yaml)
kubectl apply -f deployment.yaml
```

## Test

```bash
# Deploy mock backend
kubectl apply -f mock-backend.yaml

# Send clean response (should pass through)
curl -X POST "http://$GATEWAY_IP/v1/chat/completions" \
  -H "content-type: application/json" \
  -d '{"model":"phi","stream":false,"messages":[{"role":"user","content":"hello"}]}'

# Expected: {"choices": [{"message": {"content": "hello from mock model"}}]}

# If policy blocks "hello from mock":
# Expected: {"choices": [{"message": {"content": "Response blocked by guardrails"}}]}
```

## Verification

✅ **PR3: OutputGuardrails Integration Complete**

- ✅ Uses existing OutputGuardrails.guard_response()
- ✅ Applies scanner_schemas (ban_substrings, secrets, sensitive)
- ✅ Respects policy actions (allow/redact/block)
- ✅ No custom scanner implementation (reuses existing)
- ✅ Response-only mode (no prompt context)
- ✅ Thin glue layer: ~150 LOC (30 LOC core logic)
- ✅ Preserves all JSON fields and response structure

## Key Differences from Naive Go Implementation

| Aspect | PR3 (Python) | Alternative (Go) |
|--------|------|---|
| Scanners | Reuse scanner_schemas | Reimplement ban_substrings |
| Actions | Respect policy (allow/redact/block) | Hardcode block only |
| Complexity | Substrings, secrets, PII, regex | Substrings only |
| Maintenance | Update policy YAML | Modify Go code |
| Consistency | Same as RAGEngine | Diverge from RAGEngine |

## Known Limitations

- ⚠️ Response-only (no prompt context for advanced scanners)
- ⚠️ Non-streaming only
- ⚠️ Python runtime required (not minimal static binary)
- ⚠️ GuardrailsReloader expects file system (not suitable for all deployments)

These are acceptable for PoC. PR4+ can address as needed.

## Next Steps

- PR4: Add request context support for prompt-aware scanners
- PR5: Streaming support with holdback buffer
- PR6: KAITO API integration (InferenceSet.spec.guardrails)