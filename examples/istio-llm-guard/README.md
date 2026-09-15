# Istio Gateway llm-guard PoC

This PoC shows that an Istio-managed Gateway can apply `llm-guard` to complete,
non-streaming OpenAI chat-completion responses without changing the model server
or RAGEngine. An Envoy Lua HTTP filter buffers JSON responses, calls a Python
scanner service, and replaces the response body with the scanned result.

## Why Lua for the PoC

Istio's `WasmPlugin` cannot load the Python `llm-guard` package; using it would
require reimplementing every scanner in Rust or C++. Envoy `ext_proc` is the
stronger long-term interface, but Python has no official pre-generated Envoy
protobuf package, so it adds a protobuf build and compatibility surface before
the integration itself can be tested. The built-in Lua filter supports full
response buffering, asynchronous HTTP calls, header mutation, and body
replacement, making it the smallest useful feasibility test.

## Scope

- Supports non-streaming `application/json` chat-completion responses.
- Demonstrates `ban_substrings` and `regex` scanners with block and redact
  actions using `llm-guard==0.3.16`.
- Skips SSE (`text/event-stream`) responses. Buffering SSE would remove token
  streaming and still require stateful event framing and holdback logic.
- Uses fail-open behavior when the scanner service is unavailable. Production
  use must make this an explicit policy decision and add metrics and alerts.
- Does not pass the original prompt to `scan_output`; scanners whose decision
  depends on request context require request-body capture or `ext_proc`.

## Run the PoC

Build the scanner image from the repository root and load it into the cluster's
container runtime. For a local `kind` cluster:

```bash
docker build \
  -f examples/istio-llm-guard/Dockerfile \
  -t llm-guard-scanner:poc .
kind load docker-image llm-guard-scanner:poc
```

Deploy the existing Istio Gateway API example, scanner service, and filter in
the same namespace:

```bash
kubectl apply -f examples/gateway-api-inference-extension/gateway.yaml
kubectl apply -f examples/istio-llm-guard/deployment.yaml
kubectl apply -f examples/istio-llm-guard/envoyfilter.yaml
kubectl rollout status deployment/llm-guard-scanner
```

The `EnvoyFilter` selects the generated Gateway workload by
`gateway.networking.k8s.io/gateway-name: inference-gateway`. If the resources
are deployed outside `default`, update the scanner service FQDN in both cluster
and Lua configurations.

Send a non-streaming request whose model output contains `INTERNAL_ONLY` or a
value such as `DEMO-1234`. The response header reports `allow`, `redact`, or
`block`:

```bash
curl -i "$GATEWAY_URL/v1/chat/completions" \
  -H 'content-type: application/json' \
  -d '{"model":"phi-4-mini-instruct","stream":false,"messages":[{"role":"user","content":"Reply with DEMO-1234"}]}'
```

## Validation

Run the scanner behavior tests with an environment that has the RAGEngine test
dependencies installed:

```bash
PYTHONPATH=examples/istio-llm-guard \
  python -m pytest examples/istio-llm-guard/test_scanner_service.py -q
```

Inspect the generated Gateway proxy configuration after applying the filter:

```bash
istioctl proxy-config listeners \
  -l gateway.networking.k8s.io/gateway-name=inference-gateway
istioctl proxy-config clusters \
  -l gateway.networking.k8s.io/gateway-name=inference-gateway \
  | grep llm_guard_scanner
```

## Production Direction

Use an Envoy `ext_proc` service for production. It provides a typed bidirectional
gRPC protocol, request and response context, body mutation, immediate responses,
timeouts, failure-mode controls, and native metrics. Reuse the existing
RAGEngine streaming framing and holdback logic in that service before enabling
SSE. Keep the Lua version only as a non-streaming feasibility prototype.

References:

- [Istio EnvoyFilter](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
- [Istio WasmPlugin](https://istio.io/latest/docs/reference/config/proxy_extensions/wasm-plugin/)
- [Envoy Lua HTTP filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/lua_filter)
- [Envoy external processing filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)