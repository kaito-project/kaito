### Benchmark Results

We evaluated RAGEngine output guardrails with 380 prompts across three Azure AI Foundry models: Phi-4-mini-instruct (3.8B), mistral-small-2503 (~24B), and Mistral-Large-3 (123B). The benchmark compared Azure Content Safety alone with a block-only RAGEngine policy and a combined redaction-and-blocking policy.

#### Guardrail processing adds little overhead

For Phi-4-mini and mistral-small, median end-to-end latency remained within normal run-to-run variation after enabling RAGEngine guardrails:

| Model | Baseline P50 | Block only P50 | Redact + block P50 |
|-------|-------------|---------------|-------------------|
| Phi-4-mini | 3.67 s | 3.60 s | 3.51 s |
| mistral-small | 2.84 s | 2.83 s | 2.82 s |

A deterministic unit-level benchmark isolates the guardrail runtime from model and network variance. Block-only processing added 0.48 ms P50, while the three-scanner redact-and-block policy added 3.24 ms P50. Sequential throughput was also unchanged for both models.

Mistral-Large-3 showed higher and more variable end-to-end latency in guarded runs. Because the isolated guardrail benchmark measured only millisecond-scale processing overhead, these results reflect end-to-end variability on serverless endpoints rather than guardrail execution cost.

#### RAGEngine complements Azure Content Safety

Azure Content Safety and RAGEngine guardrails address different classes of risk. Across this benchmark, adding RAGEngine blocking and redaction increased the number of detected or sanitized cases:

| Model | Azure Content Safety alone | With RAGEngine redact + block | Increase |
|-------|--------------------------|------------------------------|----------|
| Phi-4-mini | 14 | 24 | +71% |
| mistral-small | 15 | 24 | +60% |
| Mistral-Large | 18 | 32 | +78% |

RAGEngine additionally redacted detected email addresses, phone numbers, credit-card numbers, and other configured sensitive values that Azure Content Safety does not handle as PII redaction.

#### Streaming checks prevented cross-chunk leakage

All 12 correctness checks passed, including prohibited values split at different SSE chunk boundaries, with zero bytes of detected policy-violating content released before enforcement.

Scanner cost varies by implementation. Lightweight string and pattern checks add only milliseconds, while more complex scanners cost more. Applications should select scanners based on their policy requirements and latency budget.
