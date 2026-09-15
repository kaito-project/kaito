# AKS Cluster Manual Setup for Guardrails Benchmark

## Prerequisites

- Azure subscription with GPU quota (NCasT4_v3 series)
- `az` CLI authenticated (`az login`)
- `kubectl` installed
- `helm` installed

---

## Step 1: Set Variables

```bash
export RG=rg-guardrails-bench
export LOCATION=eastus
export CLUSTER=aks-guardrails-bench
export SUB=ff05f55d-22b5-44a7-b704-f9a8efd493ed
```

---

## Step 2: Login & Set Subscription

```bash
az login --use-device-code
az account set --subscription $SUB
```

---

## Step 3: Create Resource Group

```bash
az group create --name $RG --location $LOCATION
```

---

## Step 4: Check GPU Quota

```bash
az vm list-usage --location $LOCATION -o table | grep -i "NCasT4"
```

If "CurrentValue" is at limit, request increase in portal:
Portal → Subscriptions → Usage + quotas → Request increase → NCasT4_v3 → 4 cores minimum

---

## Step 5: Create AKS Cluster

```bash
az aks create \
  --resource-group $RG \
  --name $CLUSTER \
  --location $LOCATION \
  --node-count 1 \
  --node-vm-size Standard_D4s_v5 \
  --generate-ssh-keys \
  --enable-managed-identity \
  --network-plugin azure \
  --tier standard
```

Takes ~3-5 minutes.

---

## Step 6: Add GPU Node Pool

```bash
az aks nodepool add \
  --resource-group $RG \
  --cluster-name $CLUSTER \
  --name gpupool \
  --node-count 1 \
  --node-vm-size Standard_NC4as_T4_v3 \
  --node-taints sku=gpu:NoSchedule \
  --labels sku=gpu
```

Takes ~3-5 minutes. If T4 not available, alternatives:

| VM Size | GPU | VRAM | Cost/hr |
|---------|-----|------|---------|
| Standard_NC4as_T4_v3 | 1× T4 | 16 GB | ~$0.53 |
| Standard_NC6s_v3 | 1× V100 | 16 GB | ~$3.06 |
| Standard_NC24ads_A100_v4 | 1× A100 | 80 GB | ~$3.67 |

---

## Step 7: Get Kubeconfig

```bash
az aks get-credentials --resource-group $RG --name $CLUSTER --overwrite-existing
kubectl get nodes -o wide
```

Verify: you should see 2 nodes (1 system + 1 GPU).

---

## Step 8: Install NVIDIA Device Plugin (if not auto-installed)

```bash
# Check if GPU is recognized
kubectl get nodes -l sku=gpu -o jsonpath='{.items[*].status.allocatable.nvidia\.com/gpu}'

# If empty, install manually:
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.14.1/nvidia-device-plugin.yml
```

---

## Step 9: Install KAITO Operator

```bash
helm repo add kaito https://azure.github.io/kaito/charts
helm repo update

helm install kaito-workspace kaito/kaito-workspace \
  --namespace kaito-workspace \
  --create-namespace \
  --set webhook.enabled=true

# Verify
kubectl get pods -n kaito-workspace
```

---

## Step 10: Deploy Model (phi-3-mini)

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: phi3-mini
spec:
  resource:
    instanceType: Standard_NC4as_T4_v3
    labelSelector:
      matchLabels:
        sku: gpu
  inference:
    preset:
      name: phi-3-mini-128k-instruct
EOF
```

Wait for workspace ready (~5-10 min for image pull + model load):

```bash
kubectl get workspace phi3-mini -w
# Wait until WORKSPACEREADY=True and INFERENCEREADY=True
```

Test inference works:

```bash
kubectl port-forward svc/phi3-mini 8000:80 &
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"phi-3-mini-128k-instruct","messages":[{"role":"user","content":"Hello"}],"max_tokens":50}'
kill %1
```

---

## Step 11: Deploy RAGEngine with Guardrails

### 11a. Create guardrails policy ConfigMap

```bash
kubectl create configmap guardrails-bench-policy \
  --from-file=guardrails.yaml=benchmarks/ragengine_guardrails/policies/redact_block.yaml
```

### 11b. Deploy RAGEngine

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: kaito.sh/v1beta1
kind: RAGEngine
metadata:
  name: ragengine-bench
spec:
  compute:
    instanceType: Standard_D4s_v5
    count: 1
  inferenceService:
    url: "http://phi3-mini.default.svc.cluster.local:80/v1"
  embedding:
    local:
      modelID: BAAI/bge-small-en-v1.5
  guardrails:
    enabled: true
    configMapRef: guardrails-bench-policy
EOF
```

Wait for RAGEngine ready:

```bash
kubectl get ragengine ragengine-bench -w
```

---

## Step 12: Run Benchmark

### 12a. Port-forward

```bash
kubectl port-forward svc/ragengine-bench 8080:80 &
```

### 12b. Quick sanity check

```bash
# Non-streaming
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"phi-3-mini-128k-instruct","messages":[{"role":"user","content":"What is Kubernetes?"}],"max_tokens":200}'

# Streaming
curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"phi-3-mini-128k-instruct","messages":[{"role":"user","content":"What is Kubernetes?"}],"max_tokens":200,"stream":true}'
```

### 12c. Run integration benchmark

```bash
PYTHONPATH=presets:$PYTHONPATH python -m benchmarks.ragengine_guardrails.benchmark \
  --iterations 100 --warmup 10 -v
```

### 12d. Collect container metrics

```bash
kubectl top pods -l app=ragengine-bench --containers
```

---

## Step 13: Switch Policies & Re-run

```bash
# Block-only
kubectl create configmap guardrails-bench-policy \
  --from-file=guardrails.yaml=benchmarks/ragengine_guardrails/policies/block_only.yaml \
  --dry-run=client -o yaml | kubectl apply -f -
sleep 3  # wait for hot-reload

# Disabled (baseline)
kubectl create configmap guardrails-bench-policy \
  --from-file=guardrails.yaml=benchmarks/ragengine_guardrails/policies/simple.yaml \
  --dry-run=client -o yaml | kubectl apply -f -
sleep 3
```

---

## Step 14: Cleanup (IMPORTANT — stops billing)

```bash
# Delete everything
az group delete --name $RG --yes --no-wait

# Verify
az group show --name $RG 2>&1 | grep -i "not found"
```

---

## Cost Estimate

| Resource | Rate | Duration | Cost |
|----------|------|----------|------|
| AKS control plane (Standard tier) | $0.10/hr | 3 hr | $0.30 |
| System node (D4s_v5) | $0.19/hr | 3 hr | $0.57 |
| GPU node (NC4as_T4_v3) | $0.53/hr | 3 hr | $1.59 |
| RAGEngine node (D4s_v5) | $0.19/hr | 3 hr | $0.57 |
| **Total** | | **~3 hr** | **~$3** |

Add network egress + disk: total realistic cost is **~$5-10** for a full benchmark session.

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| GPU quota insufficient | Portal → Quotas → request NCasT4_v3 in eastus |
| Workspace stuck Pending | `kubectl describe workspace phi3-mini` — check Events |
| Model OOM on T4 | Switch to V100 (Standard_NC6s_v3) |
| Port-forward drops | Re-run `kubectl port-forward` command |
| RAGEngine not starting | Check `kubectl logs` of the ragengine pod |
| Guardrails not reloading | Check `kubectl logs` for `output_guardrails_reload` messages |
