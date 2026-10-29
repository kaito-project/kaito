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

CPU-compatible preset examples:

- NAP InferenceSet (default CPU `vllm`): [Gemma vLLM CPU](inference/kaito_inferenceset_gemma_4_e4b_it_cpu_vllm.yaml)
- NAP InferenceSet (explicit CPU `transformers`): [Gemma Transformers CPU, text only](inference/kaito_inferenceset_gemma_4_e4b_it_cpu_transformers.yaml)

Notes:
- enable the `enableCPUInference` feature gate first
- default CPU preset inference uses `vllm` on amd64 nodes and requires the `vllm` feature gate
- explicit `kaito.sh/runtime: transformers` with an amd64 CPU `instanceType` reuses the configured ordinary `kaito-base` image; no separate CPU-TFS image/build or `vllm` gate is needed
- Transformers runs one CPU process with `device_map=cpu` and `torch_dtype=bfloat16`; label-only BYO CPU selection remains vLLM-only
- these examples use `google/gemma-4-E4B-it`
- built-in presets and generic Hugging Face model IDs are supported on the basic CPU preset path
- the post-load benchmark stage is automatically skipped on basic CPU paths

For the gated Gemma model, accept its license on Hugging Face and create a Secret
in the InferenceSet namespace using a token with access to the model:

```bash
# Set HF_TOKEN securely in your environment; do not commit it to the manifest.
kubectl create secret generic hf-token --from-literal=HF_TOKEN="$HF_TOKEN"
kubectl apply -f examples/inference/kaito_inferenceset_gemma_4_e4b_it_cpu_transformers.yaml
```

The example passes `presetOptions.modelAccessSecret` through to the inference
container as `HF_TOKEN`. Transformers 5.15.0 in the existing base image maps the
Gemma 4 config through `AutoModelForCausalLM` to `Gemma4ForConditionalGeneration`.
This is **text-only serving, not text-only weight loading**: the existing loader
also instantiates the checkpoint's vision/audio components. Size memory for the
full checkpoint and runtime overhead, not only the E4B active parameter count.
`Standard_D16s_v6` is an example target, not a live-E2E or capacity guarantee.

---

### Exposing Service via LoadBalancer (Testing Only)

For **testing** purposes, you may annotate your workspace resource with `kaito.sh/enablelb: "True"` to automatically create a `LoadBalancer` type service with a public IP for the inference service:

```yaml
metadata:
  annotations:
    kaito.sh/enablelb: "True"
```
> Important: This approach is **NOT** recommended for production environments. For production scenarios, use an [Ingress Controller](https://learn.microsoft.com/en-us/azure/aks/ingress-basic?tabs=azure-cli) to safely expose the service.

