# RAGEngine Guardrails Integration Guide

## Overview

The new KAITO guardrails core (PR #2380-2383) provides a reusable, dependency-agnostic guardrail engine that can be integrated into RAGEngine with minimal changes.

## Integration Points

### 1. Current RAGEngine Guardrails Architecture

**File:** `pkg/ragengine/controllers/preset_rag.go`

Currently, RAGEngine:
- Mounts guardrails policy ConfigMap from `ragengine-guardrails-policy-template`
- Passes policy path to container via `OUTPUT_GUARDRAILS_POLICY_PATH` env var
- The actual scanning logic runs inside the kaito-rag-service container (Python)

### 2. New Guardrails Core in KAITO

**Packages:**
- `pkg/guardrails/` — Core interfaces and Engine
- `pkg/guardrails/scanners/` — Native scanners (Regex, Secrets)
- `pkg/guardrails/providers/` — Provider abstraction (LLMGuard wrapper)

**Key Components:**
- `Engine` — Orchestrates multiple scanners
- `ScannerFactory` — Creates Scanner instances from config
- `Config` / `ScannerConfig` — YAML-compatible configuration

## Integration Strategy

### Option A: Minimal (Recommended for Phase 1)

No changes needed in kaito core repository. The guardrail core is now **available as a library** for projects to import:

```go
import "github.com/kaito-project/kaito/pkg/guardrails"

// In RAGEngine container or external service:
engine := guardrails.NewEngine()
factory := guardrails.NewFactory()

cfg := guardrails.EngineConfig{...}
engine.Configure(cfg, factory)

output, err := engine.ScanText(ctx, input)
```

### Option B: RAGEngine Builtin (Future)

Modify RAGEngine to embed the guardrail engine:

**Steps:**
1. Create `pkg/guardrails/service.go` — HTTP API wrapper
2. Add guardrails as RAGEngine init container or sidecar
3. Call guardrails service from main RAGEngine endpoints
4. Update `ragengine_controller.go` to inject guardrails config

**Files to modify:**
- `pkg/ragengine/controllers/preset_rag.go` — Add guardrails init container
- `pkg/ragengine/manifests/manifests.go` — Add guardrails sidecar config

## Configuration Format

The new guardrails core uses the same YAML format as current RAGEngine:

```yaml
blockMessage: "The model output was blocked by output guardrails."
scanners:
  - type: regex
    action: redact
    patterns:
      - '\bsk-[A-Za-z0-9]{20,}\b'
  - type: secrets
    action: redact
    redactMode: partial
  - type: pii           # Requires LLMGuard provider
    action: redact
    detectors:
      - email
      - credit_card
```

This is **100% backward compatible** with existing guardrails.yaml policies.

## Migration Path

### Phase 1 (Current)
- ✅ Core library delivered (PR #2380-2383)
- Production-stack uses library for Envoy ext_proc
- RAGEngine continues existing guardrails

### Phase 2 (Planned)
- Evaluate embedding guardrail engine in RAGEngine
- Reuse policy ConfigMaps and YAML format
- Potential sidecar or init-container approach

### Phase 3 (Future)
- Unified guardrails API across RAGEngine and ext_proc
- Shared secret patterns, PII models
- Common telemetry and logging

## Benefits of New Core

1. **Decoupled**: No hard dependency on llm-guard in core package
2. **Composable**: Mix native + provider-based scanners
3. **Testable**: Pure Go, no external service dependencies
4. **Reusable**: Can be imported by external projects
5. **Backward Compatible**: Existing YAML policies work unchanged

## No Breaking Changes Required

The new guardrails core is **purely additive**:
- RAGEngine existing behavior unchanged
- guardrails.yaml format unchanged
- Container API unchanged

This PR 5 ("Update RAGEngine") is actually a **readiness verification** that the core library integrates cleanly with existing KAITO patterns.

## Related Files

- Policy template: `charts/kaito/ragengine/templates/guardrails-policy-configmap.yaml`
- RAGEngine config: `api/v1beta1/ragengine_types.go` (GuardrailsSpec)
- Controller integration: `pkg/ragengine/controllers/preset_rag.go`

## Next Steps

1. **Merge PR #2380-2383** — Core library ready for use
2. **Test in production-stack** — Envoy ext_proc uses core (ongoing)
3. **Evaluate RAGEngine embedding** — Design sidecar approach
4. **Update kaito-rag-service** — Import guardrails core for in-process scanning
