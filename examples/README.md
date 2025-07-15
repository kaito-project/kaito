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

### Exposing Service via LoadBalancer (Testing Only)

For **testing** purposes, you may annotate your workspace resource with `kaito.sh/enablelb: "True"` to automatically create a `LoadBalancer` type service with a public IP for the inference service:

```yaml
metadata:
  annotations:
    kaito.sh/enablelb: "True"
```
> Important: This approach is **NOT** recommended for production environments. For production scenarios, use an [Ingress Controller](https://learn.microsoft.com/en-us/azure/aks/ingress-basic?tabs=azure-cli) to safely expose the service.

