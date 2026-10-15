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

### Phase 1 CPU preset examples

Selected CPU-compatible preset example retained in Phase 1:

- NAP InferenceSet (default CPU `vllm`): `examples/inference/kaito_inferenceset_qwen_2.5_0.5b_instruct_cpu_vllm.yaml`
- NAP InferenceSet (CPU `transformers`/TFS runtime): `examples/inference/kaito_inferenceset_qwen_2.5_0.5b_instruct_cpu_tfs.yaml`

Notes:
- enable the `enableCPUInference` feature gate first
- these examples use `Qwen/Qwen2.5-0.5B-Instruct`
- the TFS example selects the Transformers runtime with `metadata.annotations.kaito.sh/runtime: transformers`
- built-in presets and generic Hugging Face model IDs are supported on the Phase 1 CPU preset path
- the post-load benchmark stage is automatically skipped on Phase 1 CPU paths

---

### Exposing Service via LoadBalancer (Testing Only)

For **testing** purposes, you may annotate your workspace resource with `kaito.sh/enablelb: "True"` to automatically create a `LoadBalancer` type service with a public IP for the inference service:

```yaml
metadata:
  annotations:
    kaito.sh/enablelb: "True"
```
> Important: This approach is **NOT** recommended for production environments. For production scenarios, use an [Ingress Controller](https://learn.microsoft.com/en-us/azure/aks/ingress-basic?tabs=azure-cli) to safely expose the service.

