// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nodeprovision

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// NodeProvisioner abstracts node provisioning for a Workspace.
// Callers pass the Workspace object directly — all internal resources
// (NodePool, NodeClaim, AKSNodeClass) are managed by the implementation.
// The caller only needs to call ProvisionNodes and EnsureNodesReady,
// without knowing whether the backend uses NodeClaims, NodePools, or
// any other mechanism.
//
// Three implementations:
//   - AzureGPUProvisioner: wraps Azure gpu-provisioner (https://github.com/Azure/gpu-provisioner) logic.
//   - KarpenterProvisioner (future): creates NodePool with static replicas.
//   - NopProvisioner: no-op for BYO mode.
type NodeProvisioner interface {
	// Name returns the name of this provisioner implementation.
	Name() string

	// ProvisionNodes ensures all node resources for the Workspace exist
	// and are progressing toward Ready.
	//
	// AzureGPUProvisioner: creates NodeClaims via Azure gpu-provisioner.
	// KarpenterProvisioner (future): creates NodePool with replicas.
	// NopProvisioner: no-op.
	ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error

	// DeleteNodes removes all node resources for the Workspace.
	//
	// AzureGPUProvisioner: deletes NodeClaims.
	// KarpenterProvisioner (future): deletes NodePool (cascades).
	// NopProvisioner: no-op.
	DeleteNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error

	// EnsureNodesReady checks whether all nodes are ready and fully
	// initialized for the Workspace. Returns two booleans:
	//
	//   - ready: true if all nodes are ready, proceed with workload deployment.
	//   - needRequeue: true if the controller should requeue with a delay
	//     (e.g., nodes not yet registered, GPU plugins missing).
	//     When false and ready is false, wait for events (no requeue needed).
	//
	// Each implementation encapsulates its own readiness criteria internally.
	EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (ready bool, needRequeue bool, err error)

	// EnableDriftRemediation enables drift replacement for the Workspace's nodes.
	//
	// AzureGPUProvisioner: no-op.
	// KarpenterProvisioner (future): patches NodePool budget nodes="0" → "1".
	// NopProvisioner: no-op.
	EnableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error

	// DisableDriftRemediation disables drift replacement for the Workspace's nodes.
	//
	// AzureGPUProvisioner: no-op.
	// KarpenterProvisioner (future): patches NodePool budget nodes="1" → "0".
	// NopProvisioner: no-op.
	DisableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error

	// CollectNodeStatusInfo gathers node-related status conditions for the
	// Workspace. Each provisioner returns only the condition types it manages
	// (e.g., NodeStatus, NodeClaimStatus, ResourceStatus). The controller
	// merges these into the workspace status and removes any known node
	// condition types that are absent from the returned slice.
	CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error)
}
