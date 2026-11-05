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

### Basic CPU preset example

[Gemma vLLM CPU InferenceSet](inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml)
uses `google/gemma-4-E4B-it` with `Standard_D16s_v6`.

- CPU preset inference supports **vLLM only** and requires the `vllm` feature gate.
- Declare a non-GPU, non-ARM Azure `resource.instanceType`; label-only BYO and custom-template CPU inference are not supported.
- The CPU image is x86_64-only. AWS CPU and ARM64 are not supported.
- Basic CPU inference is single-node and skips the post-load benchmark.
- For gated models, accept the Hugging Face license and configure `presetOptions.modelAccessSecret` with an `HF_TOKEN` secret. Size memory for the full checkpoint and runtime overhead.

---

### Exposing Service via LoadBalancer (Testing Only)

For **testing** purposes, you may annotate your workspace resource with `kaito.sh/enablelb: "True"` to automatically create a `LoadBalancer` type service with a public IP for the inference service:

```yaml
metadata:
  annotations:
    kaito.sh/enablelb: "True"
```
> Important: This approach is **NOT** recommended for production environments. For production scenarios, use an [Ingress Controller](https://learn.microsoft.com/en-us/azure/aks/ingress-basic?tabs=azure-cli) to safely expose the service.

