# KAITO Demo Frontend Helm Chart
## Install
Before deploying the Demo front-end, you must set the `workspaceServiceURL` environment variable to point to your Workspace Service inference endpoint.

To set this value, modify the `values.override.yaml` file or use the `--set` flag during Helm install/upgrade:

```bash
helm install inference-frontend ./charts/DemoUI/inference --set env.workspaceServiceURL="http://<CLUSTER_IP>:80"
```

Or through a custom `values` file (`values.override.yaml`): 
```bash
helm install inference-frontend ./charts/DemoUI/inference -f values.override.yaml
```

## Values

| Key                           | Type   | Default                 | Description                                           |
|-------------------------------|--------|-------------------------|-------------------------------------------------------|
| `replicaCount`                | int    | `1`                     | Number of replicas                                    |
| `image.repository`            | string | `"python"`              | Image repository                                      |
| `image.pullPolicy`            | string | `"IfNotPresent"`        | Image pull policy                                     |
| `image.tag`                   | string | `"3.8"`                 | Image tag                                             |
| `imagePullSecrets`            | list   | `[]`                    | Specify image pull secrets                            |
| `podAnnotations`              | object | `{}`                    | Annotations to add to the pod                         |
| `serviceAccount.create`       | bool   | `false`                 | Specifies whether a service account should be created |
| `serviceAccount.name`         | string | `""`                    | The name of the service account to use                |
| `service.type`                | string | `"ClusterIP"`           | Service type                                          |
| `service.port`                | int    | `8000`                  | Service port                                          |
| `env.workspaceServiceURL`     | string | `"<YOUR_SERVICE_URL>"`  | Workspace Service URL for the inference endpoint      |
| `resources.limits.cpu`        | string | `"500m"`                | CPU limit                                             |
| `resources.limits.memory`     | string | `"256Mi"`               | Memory limit                                          |
| `resources.requests.cpu`      | string | `"10m"`                 | CPU request                                           |
| `resources.requests.memory`   | string | `"128Mi"`               | Memory request                                        |
| `livenessProbe.exec.command`  | list   | `["pgrep", "chainlit"]` | Command for liveness probe                            |
| `readinessProbe.exec.command` | list   | `["pgrep", "chainlit"]` | Command for readiness probe                           |
| `nodeSelector`                | object | `{}`                    | Node labels for pod assignment                        |
| `tolerations`                 | list   | `[]`                    | Tolerations for pod assignment                        |
| `affinity`                    | object | `{}`                    | Affinity for pod assignment                           |
| `ingress.enabled`             | bool   | `false`                 | Enable or disable ingress                             |

### Liveness and Readiness Probes

The `livenessProbe` and `readinessProbe` are configured to check if the Chainlit application is running by using `pgrep` to find the process. Adjust these probes as necessary for your deployment.
