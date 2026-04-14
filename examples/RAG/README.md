# RAGEngine Example Operations Notes

This directory contains concrete `RAGEngine` example manifests. The examples define the core CRD shape, but the current output-guardrails and remote-audit integration still relies on runtime environment variables on the generated RAG service deployment.

## First-Time Setup Flow

1. Apply an example manifest such as `kaito_ragengine_phi_3.yaml`.
2. Wait for the generated deployment to be created. Its name matches the `RAGEngine.metadata.name`.
3. Set the output-guardrail and audit env vars on that generated deployment.
4. Verify the deployment rollout completed and the env vars are present.

Example:

```bash
kubectl apply -f examples/RAG/kaito_ragengine_phi_3.yaml

kubectl rollout status deployment/ragengine-start -n default

kubectl set env deployment/ragengine-start -n default \
  OUTPUT_GUARDRAILS_ENABLED=true \
  OUTPUT_GUARDRAILS_FAIL_OPEN=true \
  OUTPUT_GUARDRAILS_ACTION_ON_HIT=redact \
  OUTPUT_GUARDRAILS_REGEX_PATTERNS='https?://\\S+' \
  OUTPUT_GUARDRAILS_AUDIT_SINKS=log,remote \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL=https://audit.example.com/events \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL=https://audit.example.com/reports \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_HEADER=X-Event-Auth \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_TOKEN=<event-token> \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_HEADER=X-Report-Auth \
  OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_TOKEN=<report-token>

kubectl rollout status deployment/ragengine-start -n default
kubectl describe deployment/ragengine-start -n default | grep OUTPUT_GUARDRAILS
```

## What To Configure

- `OUTPUT_GUARDRAILS_ENABLED`: enable output scanning.
- `OUTPUT_GUARDRAILS_FAIL_OPEN`: pass through the original output if scanning fails.
- `OUTPUT_GUARDRAILS_ACTION_ON_HIT`: choose `redact` or `block`.
- `OUTPUT_GUARDRAILS_REGEX_PATTERNS` and `OUTPUT_GUARDRAILS_BANNED_SUBSTRINGS`: define matchers.
- `OUTPUT_GUARDRAILS_AUDIT_SINKS`: choose `log`, `file`, `remote`, or a combination.
- `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_URL` and `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_URL`: split staged events from final reports.
- `OUTPUT_GUARDRAILS_AUDIT_REMOTE_EVENT_AUTH_*` and `OUTPUT_GUARDRAILS_AUDIT_REMOTE_REPORT_AUTH_*`: use separate credentials per endpoint when needed.
- `OUTPUT_GUARDRAILS_AUDIT_MAX_IN_FLIGHT_BACKGROUND_TASKS`: cap concurrent background audit deliveries so slow remote sinks cannot create an unbounded backlog.
- `OUTPUT_GUARDRAILS_AUDIT_BACKPRESSURE_POLICY`: choose `drop` to preserve request latency or `block` to preserve delivery at the cost of request-path waiting.

The default policy is `drop`. That is usually the safer starting point for a serving path, because it bounds audit backlog growth without letting audit sink slowness directly dominate chat latency.

## Monitoring Examples

This directory now includes basic monitoring examples for the generated RAG service:

- `monitoring/ragengine-output-guardrails-prometheusrule.yaml`: warning alerts for shutdown drain timeouts and cancelled audit deliveries.
- `monitoring/ragengine-output-guardrails-dashboard.json`: the Grafana dashboard source file for timeout rate, cancelled deliveries, shutdown drain latency, and in-flight background audit tasks.
- `monitoring/kustomization.yaml`: the base Kustomize entrypoint that generates the dashboard ConfigMap from the JSON file.

These examples assume the generated RAG service metrics are already being scraped into Prometheus.

The example manifests intentionally do not hard-code `metadata.namespace`. Apply them into each user's target namespace instead, for example:

```bash
kubectl apply -n <user-namespace> -f examples/RAG/monitoring/ragengine-output-guardrails-prometheusrule.yaml
kubectl apply -k examples/RAG/monitoring
```

If a team prefers Kustomize, Helm, or another overlay mechanism, the namespace can also be injected there.

A ready-to-use Kustomize overlay example is included in `monitoring/kustomize/`.

It does two things:

- injects a user-specific namespace via `namespace: user-namespace`
- overrides both alert severities from `warning` to `critical`
- overrides the Grafana Prometheus datasource UID from `${DS_PROMETHEUS}` to `prometheus`

Example flow:

```bash
cd examples/RAG/monitoring/kustomize

# edit kustomization.yaml and set your namespace
# edit prometheusrule-severity-patch.yaml and set your desired severity labels
# edit ragengine-output-guardrails-dashboard.json and set your Grafana Prometheus datasource UID

kubectl apply -k .
```

The overlay replaces the generated dashboard ConfigMap from a standalone JSON file. This keeps datasource customization in normal JSON rather than in a YAML-embedded JSON string.

## Current Limitation

These settings are not part of `RAGEngine.spec` yet. The controller currently derives the generated deployment env from the CRD fields it knows about. If a later reconcile rewrites the deployment template, operators should confirm their manually injected env overrides are still present.

The recommended long-term direction is to model these settings in the CRD and let the controller own them declaratively instead of relying on post-create `kubectl set env` steps.