#!/bin/bash
# Deploy 3 Kaito model workspaces for e2e guardrails benchmarking.
#
# Prerequisites:
#   - AKS cluster with Kaito operator installed
#   - kubectl configured to the cluster
#   - For Llama: AI_MODELS_REGISTRY secret configured
#
# Usage:
#   ./deploy_models.sh [namespace]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NAMESPACE="${1:-default}"

echo "=== Deploying Kaito Benchmark Workspaces ==="
echo "  Namespace: $NAMESPACE"
echo ""

# Deploy each model
for yaml in workspace-phi3-mini.yaml workspace-phi4.yaml workspace-llama70b.yaml; do
    echo "Deploying $yaml..."
    kubectl apply -n "$NAMESPACE" -f "$SCRIPT_DIR/$yaml"
done

echo ""
echo "Workspaces created. Waiting for readiness..."
echo ""

# Wait for each workspace
for ws in bench-phi3-mini bench-phi4 bench-llama70b; do
    echo "Waiting for $ws..."
    kubectl wait --for=condition=ResourceReady workspace/$ws \
        -n "$NAMESPACE" --timeout=30m 2>/dev/null || \
        echo "  WARNING: $ws not ready yet. Check: kubectl get workspace $ws -n $NAMESPACE"
done

echo ""
echo "=== Service Endpoints ==="
for ws in bench-phi3-mini bench-phi4 bench-llama70b; do
    IP=$(kubectl get svc "$ws" -n "$NAMESPACE" -o jsonpath='{.spec.clusterIP}' 2>/dev/null || echo "N/A")
    echo "  $ws: http://$IP/v1/chat/completions"
done

echo ""
echo "To port-forward for local benchmarking:"
echo "  kubectl port-forward svc/bench-phi3-mini 8081:80 -n $NAMESPACE &"
echo "  kubectl port-forward svc/bench-phi4 8082:80 -n $NAMESPACE &"
echo "  kubectl port-forward svc/bench-llama70b 8083:80 -n $NAMESPACE &"
echo ""
echo "Then record traces:"
echo "  python -m benchmarks.ragengine_guardrails.bench_e2e record \\"
echo "    --model-url http://localhost:8081/v1/chat/completions \\"
echo "    --prompts datasets/benchmark_prompts.jsonl \\"
echo "    --output traces/phi3-mini/"
