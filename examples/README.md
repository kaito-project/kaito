# Notes:

### GPU Resource Requirement Bypass
To bypass GPU memory requirement checks, annotate your workspace resource as follows:
```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: my-workspace
  annotations:
    kaito.sh/bypass-resource-checks: "True"
  # ... rest of the workspace spec
```
> Caution: Using this annotation may lead to degraded performance or out-of-memory errors if GPU resources are insufficient for your model. Warnings will be logged accordingly.

---

### Basic CPU preset examples

Selected CPU-compatible preset example retained in basic CPU support:

- NAP InferenceSet (default CPU `vllm`): `examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml`

Notes:
- enable the `enableCPUInference` feature gate first
- CPU preset inference supports only the `vllm` runtime on amd64 nodes; the `transformers` runtime remains GPU-only
- these examples use `google/gemma-4-E4B-it`
- built-in presets and generic Hugging Face model IDs are supported on the basic CPU preset path
- the post-load benchmark stage is automatically skipped on basic CPU paths

---

### Exposing Service via LoadBalancer (Testing Only)

For **testing** purposes, you may annotate your workspace resource with `kaito.sh/enablelb: "True"` to automatically create a `LoadBalancer` type service with a public IP for the inference service:

```yaml
metadata:
  annotations:
    kaito.sh/enablelb: "True"
```
> Important: This approach is **NOT** recommended for production environments. For production scenarios, use an [Ingress Controller](https://learn.microsoft.com/en-us/azure/aks/ingress-basic?tabs=azure-cli) to safely expose the service.

