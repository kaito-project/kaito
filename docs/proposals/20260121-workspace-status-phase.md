# Workspace Status Field Proposal

## Background

Currently, the `Workspace` Custom Resource (CR) status relies heavily on a list of `Conditions` (e.g., `ResourceReady`, `InferenceReady`, `JobStarted`) to represent the state of the system. While `Conditions` are excellent for programmatic consumption and detailed status reporting, they present a few challenges for end-users:

1.  **Poor Readability**: Users must inspect the `status.conditions` array to understand the current state.
2.  **No High-Level Summary**: There is no single field that answers "Is my workspace working?" 
3.  **CLI Output**: `kubectl get workspace` typically requires complex `JSONPath` or custom printers to show meaningful status, often resulting in multiple columns (`ResourceReady`, `InferenceReady`) instead of one status column.

To address this, we propose adding a `Status` field to the `WorkspaceStatus`. This field effectively aggregates the various conditions into a single, high-level finite state machine (FSM) enumeration, similar to how standard Kubernetes resources like `Pod` (Pending, Running, Succeeded, Failed) or `PersistentVolumeClaim` (Bound, Available) work.

## Design Proposal

We propose adding a `Status` field to `WorkspaceStatus`.

### Workspace API Change

```go
// WorkspaceState indicates the high-level state of the workspace.
// +kubebuilder:validation:Enum=Pending;Ready;NotReady;Running;Succeeded;Failed
type WorkspaceState string

const (
    // Common State
    WorkspaceStatePending WorkspaceState = "Pending"

    // Inference States
    WorkspaceStateReady    WorkspaceState = "Ready"
    WorkspaceStateNotReady WorkspaceState = "NotReady"

    // Fine-tuning States
    WorkspaceStateRunning   WorkspaceState = "Running"
    WorkspaceStateSucceeded WorkspaceState = "Succeeded"
    WorkspaceStateFailed    WorkspaceState = "Failed"
)

type WorkspaceStatus struct {
    // ... existing fields
    
    // Status represents the current high-level state of the workspace.
    // +optional
    Status WorkspaceState `json:"status,omitempty"`
}
```

### Inference Workspace Status Workflow

For inference workloads, the `Status` indicates whether the service is available.

*   **Pending**: The workspace is in the initialization phase (e.g., provisioning infrastructure, pulling images).
    *   *Constraint*: Can only transition to `Ready`. If initialization fails, it remains `Pending`.
    *   *Note*: Once the workspace leaves `Pending`, it **cannot** return to `Pending`.
*   **Ready**: The workspace is fully operational and serving requests.
*   **NotReady**: The workspace is currently unable to serve requests due to runtime issues (e.g., NodeClaim lost, OOM, crash).
    *   *Transition*: Can transition back to `Ready` if the issue resolves (e.g., node auto-healing).
    *   *Debugging*: When `Status` is `NotReady`, users should check `WorkspaceStatus.Conditions`. Any condition with `Status=False` serves as the root cause (e.g., `InferenceReady=False` due to `CrashLoopBackOff`).


![inference status workflow](../../website/static/img/workspace-inference-status-workflow.png)

### Fine-tuning Workspace Status Workflow

For fine-tuning workloads, the `Status` tracks the execution lifecycle of the job.

*   **Pending**: The workspace is in the initialization phase.
*   **Running**: The fine-tuning job is actively executing.
*   **Succeeded**: The tuning job completed successfully. (Terminal State)
*   **Failed**: The tuning job failed. (Terminal State)

![fine-tuning status workflow](../../website/static/img/workspace-fine-tuning-status-workflow.png)

## Pros and Cons

### Pros
*   **Simplified User View**: Provides a single reliable field for checking availability (`Ready`) or job status.
*   **Clear Separation**: Distinct states for Service availability (Ready/NotReady) vs Job lifecycle (Running/Succeeded).
*   **Stable Initialization**: `Pending` implies "not established yet". Once established, we track availability via `Ready`/`NotReady`.
*   **Forward Compatibility**: Future requirements can introduce new `Conditions` without needing UI updates. The UI only needs to check the `Status` field, as the workspace controller abstracts the complexity.

### Cons
*   **"Pending" Trap**: Permanent provisioning failures manifest as infinite `Pending`.
*   **NotReady Ambiguity**: Collapses various runtime failures into one state.
