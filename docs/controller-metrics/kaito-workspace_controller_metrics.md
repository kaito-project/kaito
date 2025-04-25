# Prometheus Metrics

Workspace controller exposes Prometheus metrics for monitoring workspace states.

> [!NOTE]
> In addition to the metrics listed below, the workspace controller also exposes all metrics provided by the Kubernetes controller-runtime. For more information, see the [controller-runtime metrics package](https://github.com/kubernetes-sigs/controller-runtime/tree/main/pkg/metrics).

## Workspace Metrics

The workspace controller exports metrics at the `/metrics` endpoint. These metrics provide insights into the number of workspaces in different phases, helping you monitor the overall status of workspaces in your system.

| Category | Metric Name | Type | Description |
|----------|-------------|------|-------------|
| Workspace Stats | `kaito_workspace_count` | Gauge | Number of Workspaces in a certain phase |

### Labels

The `kaito_workspace_count` metric includes the following labels:

| Label | Description |
|-------|-------------|
| `phase` | Workspace phase: `succeeded`, `error`, `pending`, or `deleting` |

### Phase Definitions

- `succeeded`: Workspaces that have successfully completed their execution
- `error`: Workspaces that have encountered errors during execution 
- `pending`: Workspaces that are in progress or waiting to be processed
- `deleting`: Workspaces that are in the process of being deleted
