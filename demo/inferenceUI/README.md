## KAITO InferenceUI Demo

The KAITO InferenceUI Demo provides a sample front-end application that demonstrates
how to interface with the KAITO Workspace for inference tasks. 
This guide covers deploying the front-end as a Helm chart in a Kubernetes environment 
as well as how to run the Python application independently.

### Prerequisites

- A Kubernetes cluster with Helm installed
- Access to the KAITO Workspace Service endpoint

## Deployment with Helm
Deploy the KAITO InferenceUI Demo by setting the 
workspaceServiceURL environment variable to your 
Workspace Service endpoint.


### Configuring the Workspace Service URL
- Using the --set flag:

   ```
   helm install inference-frontend ./charts/DemoUI/inference --set env.workspaceServiceURL="http://<SERVICE_NAME>.<NAMESPACE>.svc.cluster.local:80"
   ```
 - Using a custom `values.override.yaml` file:
   ```
   env:
      workspaceServiceURL: "http://<SERVICE_NAME>.<NAMESPACE>.svc.cluster.local:80"
   ```
   Then deploy with custom values file:
    ```
    helm install inference-frontend ./charts/DemoUI/inference -f ./charts/DemoUI/inference/values.override.yaml
    ```

Replace `<SERVICE_NAME>` and `<NAMESPACE>` with your service's name and Kubernetes namespace.
This DNS naming convention ensures reliable service resolution within your cluster.

## Accessing the Application
After deploying, access the KAITO InferenceUI based on your service type:
- NodePort
   ```
  export NODE_PORT=$(kubectl get --namespace default -o jsonpath="{.spec.ports[0].nodePort}" services inference-frontend)
  export NODE_IP=$(kubectl get nodes --namespace default -o jsonpath="{.items[0].status.addresses[0].address}")
  echo "Access your application at http://$NODE_IP:$NODE_PORT"
   ```
- LoadBalancer (It may take a few minutes for the LoadBalancer IP to be available):
   ```
   export SERVICE_IP=$(kubectl get svc --namespace default inference-frontend --template "{{ range (index .status.loadBalancer.ingress 0) }}{{.}}{{ end }}")
   echo "Access your application at http://$SERVICE_IP:8000"
   ```
- ClusterIP (Use port-forwarding to access your application locally):
   ```
   export POD_NAME=$(kubectl get pods --namespace default -l "app.kubernetes.io/name=inference" -o jsonpath="{.items[0].metadata.name}")
   kubectl --namespace default port-forward $POD_NAME 8080:8000
   echo "Visit http://127.0.0.1:8080 to use your application"
   ```

---

For additional support or to report issues, please contact the development 
team at <kaito-dev@microsoft.com>.
