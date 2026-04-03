# KAITO LLM Output Filtering - Design Proposal

> Analysis of [kaito-project/kaito#1310](https://github.com/kaito-project/kaito/issues/1310)
>
> Date: 2026-04-03

## Problem

In agentic AI scenarios, LLM responses may contain malicious URLs, IP addresses, or other harmful content that downstream agents could blindly act upon. KAITO needs a mechanism to filter/block such content before it reaches the caller.

## Candidate Solutions

| Solution | Project | Type |
|----------|---------|------|
| **LLM Guard** | [protectai/llm-guard](https://github.com/protectai/llm-guard) | Python library with pluggable output scanners |
| **Llama Firewall** | [meta-llama/PurpleLlama](https://github.com/meta-llama/PurpleLlama/tree/main/LlamaFirewall) | Meta's safety framework, uses PromptGuard model |
| **NeMo Guardrails** | [NVIDIA/NeMo-Guardrails](https://github.com/NVIDIA/NeMo-Guardrails) | NVIDIA's dialog-level guardrails with Colang DSL |
| **MCP Context Protector** | [trailofbits/mcp-context-protector](https://github.com/trailofbits/mcp-context-protector) | MCP protocol-specific protection |
| **Custom pattern matching** | Self-built | Simple regex-based URL/IP/keyword blocking |

## Recommendation: LLM Guard

LLM Guard is the best fit for KAITO. Here's why:

### 1. Architecture Alignment

KAITO's inference runtime is Python-based (vLLM's `inference_api.py`). LLM Guard is a Python library that can be embedded directly as middleware in the vLLM response pipeline — no extra sidecar containers or external services required.

### 2. Rich Out-of-the-Box Scanners

LLM Guard provides ready-to-use [output scanners](https://protectai.github.io/llm-guard/output_scanners/) that directly address the issue requirements:

- **`MaliciousURLs`** — Detects and blocks malicious URLs (the core ask in the issue)
- **`Regex`** — Custom pattern matching for IPs, keywords, etc.
- **`BanTopics`** — Topic-based content blocking
- **`Sensitive`** — PII/sensitive data detection
- **`Toxicity`**, **`Bias`** — Additional safety layers

No need to build and maintain custom regex patterns that inevitably miss edge cases (encoded URLs, URL shorteners, unicode tricks, etc.).

### 3. Lightweight — No Extra GPU Required

Most LLM Guard scanners use rule-based/pattern matching without requiring additional model inference. This is critical for KAITO where GPU resources are already dedicated to the served model.

### 4. Why Not the Others?

| Solution | Reason for Not Choosing |
|----------|------------------------|
| **NeMo Guardrails** | Heavyweight — requires defining Colang dialog flows; designed for conversational systems rather than API serving; introduces NVIDIA ecosystem dependency |
| **Llama Firewall** | Requires running PromptGuard safety model for classification — additional GPU/CPU overhead; complex deployment |
| **MCP Context Protector** | Designed specifically for the MCP protocol; does not align with KAITO's OpenAI-compatible API serving model |
| **Custom pattern matching** | Feasible for an MVP but high maintenance cost; prone to bypasses via URL encoding, short links, unicode variants; LLM Guard already handles these edge cases |

## Proposed Integration

### Data Flow

```
User Request → vLLM Inference → LLM Guard Output Scanners → Filtered Response
```

### Configuration via ConfigMap

Leverage KAITO's existing ConfigMap mechanism (`inference_config.yaml`) to allow users to configure guardrails declaratively:

```yaml
# inference_config.yaml
vllm:
  # existing vllm args...

guardrails:
  enabled: true
  output_scanners:
    - name: MaliciousURLs
      threshold: 0.5
    - name: Regex
      patterns:
        - "\\b\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\b"  # block raw IPs
    - name: BanTopics
      topics: ["violence"]
```

This allows users to enable/disable scanners and customize rules through the ConfigMap without rebuilding the container image.

### Implementation Steps

1. **Add `llm-guard` as a dependency** in KAITO's vLLM inference image
2. **Create a guardrails middleware** in `inference_api.py` that intercepts vLLM responses
3. **Parse guardrails config** from `inference_config.yaml`
4. **Instantiate configured scanners** at startup
5. **Apply scanners** to each response before returning to the client
6. **Log/metric** blocked responses for observability

### Streaming Considerations

For streaming responses (`stream=true`), the scanner needs to operate on accumulated chunks or use a sliding window approach. LLM Guard supports scanning partial text, but latency tradeoffs should be documented.

## Configuration Explained

```yaml
guardrails:
  enabled: true                    # Master switch — set to true to enable output filtering
  output_scanners:                 # List of scanners to apply (executed in order)

    - name: MaliciousURLs          # Scanner 1: Malicious URL detection
      threshold: 0.5               # Confidence threshold (0~1). Scores >= 0.5 are flagged as malicious.
                                   # Lower = stricter (more URLs blocked), higher = more permissive.

    - name: Regex                  # Scanner 2: Custom regex pattern matching
      patterns:
        - "\\b\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\b"
          # Matches raw IP addresses like 192.168.1.1 or 10.0.0.5
          # \\b = word boundary (prevents matching version strings like "v1.2.3.4xxx")
          # \\d{1,3} = 1-3 digit number
          # Any match triggers a block.

    - name: BanTopics              # Scanner 3: Topic-based content blocking
      topics: ["violence"]         # Block responses discussing "violence"
                                   # Extensible: ["violence", "drugs", "weapons"]
```

### Execution Flow

```
vLLM generates response
  │
  ├─ Scanner 1: MaliciousURLs → finds http://evil.com       → ❌ BLOCKED
  ├─ Scanner 2: Regex         → finds 192.168.1.100         → ❌ BLOCKED
  ├─ Scanner 3: BanTopics     → detects violent content      → ❌ BLOCKED
  │
  └─ All scanners pass                                       → ✅ Response returned to user
```

### MaliciousURLs Threshold Behavior

| Threshold | Effect |
|-----------|--------|
| `0.1` | Very strict — almost all URLs are blocked |
| `0.5` | Balanced — reasonable tradeoff between false positives and missed detections |
| `0.9` | Very permissive — only highly suspicious URLs are blocked |

The MaliciousURLs scanner scores URLs across multiple dimensions (domain reputation, path patterns, known malicious domain lists, etc.). URLs scoring at or above the threshold are blocked.

### Examples

| LLM Output | Triggered Scanner | Result |
|------------|-------------------|--------|
| `"Visit http://malware-site.xyz/payload"` | MaliciousURLs | ❌ Blocked |
| `"Server address is 10.0.0.5"` | Regex (IP match) | ❌ Blocked |
| `"Here's how to build a weapon..."` | BanTopics (violence) | ❌ Blocked |
| `"Python version is 3.12"` | None (`3.12` has only 2 segments, doesn't match IP regex) | ✅ Passed |
| `"See docs at https://docs.python.org"` | MaliciousURLs (legitimate domain, score < 0.5) | ✅ Passed |

## References

- Issue: https://github.com/kaito-project/kaito/issues/1310
- LLM Guard docs: https://protectai.github.io/llm-guard/
- LLM Guard output scanners: https://protectai.github.io/llm-guard/output_scanners/
- Malicious URL scanner: https://protectai.github.io/llm-guard/output_scanners/malicious_urls/
