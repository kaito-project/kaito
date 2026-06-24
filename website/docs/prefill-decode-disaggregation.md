---
title: Prefill/Decode Disaggregation
---

# Prefill/Decode (P/D) Disaggregation

Prefill/Decode disaggregation separates the two phases of LLM inference into independently scalable pod groups, improving throughput and GPU utilization for large models.

## Overview

In standard LLM inference, a single pod handles both phases:

- **Prefill** — the compute-intensive phase where all input tokens are processed in parallel to build the KV cache
- **Decode** — the memory-bandwidth-intensive phase where tokens are generated autoregressively one at a time

With P/D disaggregation via **MultiRoleInference (MRI)**, KAITO creates separate workloads for each role, allowing you to:

- **Scale independently** — adjust prefill and decode replicas based on workload patterns
- **Optimize GPU selection** — use compute-optimized instances for prefill and memory-bandwidth-optimized instances for decode
- **Improve throughput** — prefill pods process new prompts while decode pods generate tokens for previous requests
- **Transfer KV cache transparently** — NIXL handles direct pod-to-pod KV cache movement without gateway involvement

## Prerequisites

- **KAITO v0.11.0+** with the `enableMultiRoleInferenceController` feature gate enabled
- **Istio** installed as the Gateway API provider
- **Gateway API CRDs** v1.4.1+
- A GPU node pool with sufficient capacity for both prefill and decode pods

### Enable the Feature Gate

```bash
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set featureGates.enableMultiRoleInferenceController=true
```

## Architecture

### How MRI Works

When you create a MultiRoleInference resource, KAITO will:

1. Provision separate pod groups for each role (prefill and decode) via child InferenceSets
2. **Inject a routing sidecar** into decode pods — the [`llm-d-routing-sidecar`](https://github.com/llm-d/llm-d-routing-sidecar) container is automatically injected, listening on port 5000 and proxying to vLLM on port 5001
3. **Set `VLLM_NIXL_SIDE_CHANNEL_HOST`** to the Pod IP on both prefill and decode pods, enabling cross-pod KV cache transfer via vLLM's NIXL connector
4. Create a single InferencePool with an EPP (Endpoint Picker) deployment that uses llm-d scheduling plugins for P/D-aware routing
5. The EPP uses scheduling profiles (`prefill` and `decode`) with a `prefix-based-pd-decider` plugin to decide which role handles each request

### Port Architecture

| Component | Prefill Pod | Decode Pod |
|-----------|-------------|------------|
| vLLM | port 5000 (default) | port 5001 (moved) |
| Routing sidecar | — | port 5000 |
| InferencePool targetPort | 5000 (vLLM directly) | 5000 (via sidecar → vLLM:5001) |

### Request Flow

1. The Gateway routes the request to the llm-d EPP scheduler
2. The EPP's `prefix-based-pd-decider` plugin determines whether to route to prefill or decode based on the request's prefix cache status
3. For a new prompt: the request goes to a **prefill pod** (port 5000, vLLM directly), which processes all input tokens in parallel and builds the KV cache
4. The **decode pod's routing sidecar** (port 5000) receives the decode request and forwards it to vLLM (port 5001). The decode pod uses NIXL (`VLLM_NIXL_SIDE_CHANNEL_HOST`) to transfer the KV cache directly from the prefill pod
5. The decode pod performs autoregressive token generation using the transferred KV cache
6. The response streams back through the Gateway to the client

## Quickstart

> **Note:** This quickstart deploys all resources in the `kaito-workspace` namespace. Adjust the namespace if your setup differs.

### 1. Install Istio and Deploy Gateway

Add the Istio Helm repo and install Istio with Gateway API Inference Extension support:

```bash
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
helm upgrade -i istio-base istio/base \
  --version 1.28.3 \
  --namespace istio-system \
  --create-namespace
helm upgrade -i istiod istio/istiod \
  --version 1.28.3 \
  --namespace istio-system \
  --set pilot.env.ENABLE_GATEWAY_API_INFERENCE_EXTENSION="true" \
  --wait
```

Install Gateway API CRDs and deploy the Gateway:

```bash
kubectl apply -k "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v1.4.1"
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/gateway.yaml -n kaito-workspace
```

### 2. Deploy MultiRoleInference

Create a MultiRoleInference resource with prefill/decode roles:

```bash
kubectl apply -f - <<EOF
apiVersion: kaito.sh/v1alpha1
kind: MultiRoleInference
metadata:
  name: phi-4-mini
  namespace: kaito-workspace
spec:
  labelSelector:
    matchLabels:
      apps: phi-4-mini
  model:
    name: phi-4-mini-instruct
  roles:
  - type: prefill
    replicas: 1
    instanceType: Standard_NC24ads_A100_v4
  - type: decode
    replicas: 1
    instanceType: Standard_NC24ads_A100_v4
EOF
```

### 3. Verify Resources

Verify that both prefill and decode pods are running:

```bash
kubectl get pods -l multiroleinference.kaito.sh/created-by=phi-4-mini -n kaito-workspace
```

Expected output:

```
NAME                              READY   STATUS    RESTARTS   AGE
phi-4-mini-decode-25x54-0         2/2     Running   0          10h
phi-4-mini-prefill-qkrzk-0        1/1     Running   0          10h
```

> **Note:** Decode pods show `2/2` because they have both the vLLM container and the automatically injected `llm-d-routing-sidecar` container. Prefill pods show `1/1` with only the vLLM container.

Verify the InferencePool and EPP:

```bash
kubectl get inferencepool -n kaito-workspace
kubectl get pods -l inferencepool=phi-4-mini-inferencepool-epp -n kaito-workspace
```

### 4. Deploy DestinationRule and HTTPRoute

Since EPP runs with `--secure-serving=true` using a self-signed certificate, apply a DestinationRule to bypass TLS verification:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/destinationrule-phi-4-mini-instruct.yaml -n kaito-workspace
```

Create the HTTPRoute that targets the InferencePool:

```bash
kubectl apply -f https://raw.githubusercontent.com/kaito-project/kaito/refs/heads/main/examples/gateway-api-inference-extension/httproute.yaml -n kaito-workspace
```

### 5. Test Inference

Get the Gateway IP:

```bash
export GATEWAY_IP=$(kubectl get gateway inference-gateway -n kaito-workspace -o jsonpath='{.status.addresses[0].value}')
```

Send a request (the Gateway listens on port 80):

```bash
curl -s "http://${GATEWAY_IP}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "phi-4-mini-instruct",
    "messages": [{"role": "user", "content": "Explain prefill/decode disaggregation in LLM inference"}],
    "max_tokens": 100,
    "temperature": 0
  }' | jq .
```

With P/D disaggregation active, the prefill pod processes the input tokens and builds the KV cache, then the decode pod receives the KV cache via NIXL and generates the output tokens.

### 6. Validate P/D Disaggregation

To confirm that disaggregation is working, check the EPP logs for the `disagg-profile-handler` scheduling profile:

```bash
kubectl logs -l inferencepool=phi-4-mini-inferencepool-epp -n kaito-workspace | grep "disagg-profile-handler"
```

Check prefill pod logs for prompt throughput:

```bash
kubectl logs phi-4-mini-prefill-<id>-0 -n kaito-workspace | grep "Avg prompt throughput"
```

Check decode pod logs for KV cache transfer metrics:

```bash
kubectl logs phi-4-mini-decode-<id>-0 -n kaito-workspace | grep "Num successful transfers"
```

## Scaling Recommendations

| Workload Pattern | Prefill Replicas | Decode Replicas | Notes |
|-----------------|-----------------|-----------------|-------|
| Long prompts, short responses | More prefill | Fewer decode | Prefill is the bottleneck |
| Short prompts, long responses | Fewer prefill | More decode | Decode is the bottleneck |
| Balanced | Equal | Equal | Good starting point |
| High concurrency | Scale both | Scale both | Monitor EPP routing metrics |

## Limitations

- **Alpha feature** — API may change in future releases
- Requires Istio as the Gateway API provider
- KV cache transfer via NIXL requires GPU-to-GPU connectivity between prefill and decode pods
- Currently supports vLLM runtime only

## Related Resources

- [Gateway API Inference Extension](./gateway-api-inference-extension.md) — Full GWIE documentation including InferenceSet
- [llm-d inference scheduler](https://github.com/llm-d/llm-d-inference-scheduler) — The EPP scheduling plugins used for P/D routing
- [NIXL](https://github.com/ai-dynamo/nixl) — The KV cache transfer library
- [llm-d routing sidecar](https://github.com/llm-d/llm-d-routing-sidecar) — The sidecar injected into decode pods
