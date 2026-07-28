---
title: Prefill/Decode Disaggregation
---

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

### Enable this feature

```bash
helm repo add kaito https://kaito-project.github.io/kaito/charts/kaito
helm repo update

export CLUSTER_NAME="<your-cluster-name>"
helm upgrade --install kaito-workspace kaito/workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set clusterName="$CLUSTER_NAME" \
  --set featureGates.enableMultiRoleInferenceController=true \
  --set featureGates.gatewayAPIInferenceExtension=true \
  --wait \
  --take-ownership
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

1. The Gateway routes the incoming user request to the llm-d EPP scheduler
2. The EPP's `disagg-profile-handler` and `prefix-based-pd-decider` plugins determine whether the request needs prefill (based on prefix cache status) and communicate the decision via headers (set by `disagg-headers-handler`)
3. The EPP routes the request to a **decode pod** — the routing sidecar on port 5000 receives the request along with headers indicating the selected prefill pod
4. The routing sidecar orchestrates the prefill call: it contacts the designated **prefill pod** to process the input tokens and build the KV cache, then the prefill pod transfers the KV cache directly to the decode pod via NIXL (`VLLM_NIXL_SIDE_CHANNEL_HOST`)
5. Once the KV cache is available, the routing sidecar forwards the request to the local vLLM instance (port 5001) for autoregressive token generation
6. The response streams back through the Gateway to the client

> **Note:** The InferencePool targets port 5000 on all pods (prefill + decode). The EPP's internal `prefill-filter` / `decode-filter` plugins handle role-based selection, routing user traffic to decode pods while prefill pods are coordinated internally.

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

> ⚠️ **Security warning:** This DestinationRule uses `insecureSkipVerify: true`, which disables server identity verification. This is acceptable for **development and testing only**. For production, configure a trusted CA certificate or use cert-manager to provision proper TLS certificates for the EPP service.

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
- [llm-d router](https://github.com/llm-d/llm-d-router) — The EPP scheduling plugins used for P/D routing
