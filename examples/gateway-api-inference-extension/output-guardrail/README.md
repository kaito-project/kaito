# Gateway Output Guardrail PoC - PR3+PR4: Integration & E2E Validation (Python)

## Objective

Integrate KAITO's existing OutputGuardrails into the ext_proc pipeline.

This PR implements:
1. **Python ext_proc server** - Direct integration with OutputGuardrails.guard_response()
2. **Response-only guardrails** - Applies scanner_schemas without request context
3. **Full scanner support** - ban_substrings, secrets, sensitive (PII) via existing implementation
4. **Actions via guardrails** - allow/redact/block determined by policy (not hardcoded)

## What This PR Does

- ✅ Reuses existing `OutputGuardrails.guard_response()`
- ✅ Applies all response-only scanners to all choices (handles multiple choices automatically)
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
    Call: OutputGuardrails.guard_response(response, request={})
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

### E2E Tests (Full Flow)

```bash
# Make the test script executable
chmod +x test-e2e.sh

# Run E2E tests (requires deployed gateway and ext_proc service)
./test-e2e.sh
```

**Test cases:**
- ✅ Clean response passes through unchanged (JSON valid)
- ✅ Ban substring blocks response (unsafe content replaced with blockMessage)
- ✅ Sensitive data redacted (email/PII masked)
- ✅ HTTP framing valid (status, content-type, content-length, JSON parseable)
- ✅ Fail-closed: ext_proc unavailable blocks request (doesn't leak unguarded response)
- ✅ Large response (100KB+) handled correctly with proper framing

### Performance Benchmarking

```bash
chmod +x benchmark-latency.sh
./benchmark-latency.sh
```

Automatically measures baseline vs with-guardrails overhead. See [Performance Benchmarking](#performance-benchmarking) section.

### Regression Testing

```bash
chmod +x test-regression-epp.sh
./test-regression-epp.sh
```

Verifies EPP/GWIE routing unaffected. See [Regression Testing](#regression-testing) section.

### Unit Tests (Component-Level)

```bash
# Run unit tests for ext_proc server
python -m pytest test_ext_proc.py -v
```

Tests:
- Guard response disabled path
- Guard response allowed path (mock guardrails)
- Fail-closed behavior (all choices blocked on scanner error)

## Verification

✅ **PR3: OutputGuardrails Integration Complete for the Current PoC Scope**

- ✅ Uses existing OutputGuardrails.guard_response()
- ✅ Applies scanner_schemas (ban_substrings, secrets, sensitive) to all choices
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

- PR5: Add request context support for prompt-aware scanners
- PR6: Streaming support with holdback buffer
- PR7: KAITO API integration (InferenceSet.spec.guardrails)

## E2E Testing

Run end-to-end tests to verify the complete flow:

```bash
# Make the test script executable
chmod +x test-e2e.sh

# Run E2E tests (requires deployed gateway and ext_proc service)
./test-e2e.sh
```

**Tests:**
- Clean response passes through unchanged
- Ban substring configuration loads
- Ext proc service running and accessible
- Gateway routes to mock backend
- Guardrails environment variables configured

## Performance Benchmarking

Measure latency baseline and ext_proc overhead automatically:

```bash
# Make the benchmark script executable
chmod +x benchmark-latency.sh

# Run benchmark (automatically disables/enables ext_proc to compare)
./benchmark-latency.sh

# Customize:
# GATEWAY_URL=http://gateway.example.com NUM_REQUESTS=500 CONCURRENCY=10 ./benchmark-latency.sh
```

**What this measures:**
- **Baseline**: Full request path with ext_proc filter disabled (no guardrails scanning)
- **With guardrails**: Same path with ext_proc scanning enabled
- **Overhead calculation**: (with-guardrails latency) - (baseline latency)

**Report includes:**
- Min/Avg/Max latency for each phase
- Request success/failure rates
- Guidance on interpreting results for your environment

**Notes:**
- Results depend on: response size, scanner types (ban_substrings vs secrets), concurrency, backend latency
- Run warm-up requests before benchmarking (Python startup overhead)
- For accurate measurement, ensure gateway is not under other load during test

## Regression Testing

Verify that the ext_proc filter doesn't break existing GWIE/EPP routing:

```bash
# Make the regression test script executable
chmod +x test-regression-epp.sh

# Run regression tests (requires deployed gateway and backends)
./test-regression-epp.sh
```

**What this verifies:**
- ✅ Requests still route to backend pods (no routing breakage)
- ✅ Load distribution: requests go to multiple pods (when replicas > 1)
- ✅ No persistent connection errors
- ✅ Endpoint picker still active (Envoy load balancing working)
- ✅ Original GWIE/EPP semantics unaffected by ext_proc filter

**Manual verification (if script unavailable):**
```bash
# Check Envoy config includes original EPP filters
istioctl proxy-config listener <gateway-pod> | grep -E "endpoint_picker|ext_proc"

# Send requests and verify distribution
for i in {1..20}; do
  curl -s "$GATEWAY_URL/v1/chat/completions" ... | jq .id
done
# Should see successful responses, no errors
```