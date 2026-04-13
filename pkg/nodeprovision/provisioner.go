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

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// NodeReadiness represents the readiness state of nodes for a Workspace.
type NodeReadiness int

const (
	// NodesReady indicates all nodes are fully ready for the Workspace.
	NodesReady NodeReadiness = iota

	// ProvisioningNotReady indicates the provisioning resources (e.g., NodeClaims,
	// NodePools) are not yet ready. The controller should wait for events from the
	// provisioning backend (no requeue needed).
	ProvisioningNotReady

	// NodesNotReady indicates the underlying Nodes are not yet ready (e.g., node
	// not yet registered, GPU plugins not installed). The controller should requeue
	// with a delay since node changes do not trigger workspace reconciliation.
	NodesNotReady
)

// NodesProvisioner abstracts node provisioning for a Workspace.
// Callers pass the Workspace object directly — all internal resources
// (NodePool, NodeClaim, AKSNodeClass) are managed by the implementation.
// The caller only needs to call ProvisionNodes and EnsureNodesReady,
// without knowing whether the backend uses NodeClaims, NodePools, or
// any other mechanism.
//
// Three implementations:
//   - GpuProvisioner: wraps existing gpu-provisioner logic.
//   - KarpenterProvisioner (future): creates NodePool with static replicas.
//   - NopProvisioner: no-op for BYO mode.
type NodesProvisioner interface {
	// ProvisionNodes ensures all node resources for the Workspace exist
	// and are progressing toward Ready.
	//
	// GpuProvisioner: creates NodeClaims via gpu-provisioner.
	// KarpenterProvisioner (future): creates NodePool with replicas.
	// NopProvisioner: no-op.
	ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error

	// DeprovisionNodes removes all node resources for the Workspace.
	//
	// GpuProvisioner: deletes NodeClaims.
	// KarpenterProvisioner (future): deletes NodePool (cascades).
	// NopProvisioner: no-op.
	DeprovisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error

	// EnsureNodesReady checks whether all nodes are ready and fully
	// initialized for the Workspace. Returns a NodeReadiness enum:
	//
	//   - NodesReady: all nodes are ready, proceed with workload deployment.
	//   - ProvisioningNotReady: provisioning resources (NodeClaims/NodePools)
	//     not yet ready. Wait for events (no requeue needed).
	//   - NodesNotReady: nodes not yet ready (not registered, GPU plugins
	//     missing, etc.). Requeue with delay.
	//
	// Each implementation encapsulates its own readiness criteria internally.
	EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (NodeReadiness, error)

	// EnableDrift enables drift replacement for the Workspace's nodes.
	//
	// GpuProvisioner: no-op.
	// KarpenterProvisioner (future): patches NodePool budget nodes="0" → "1".
	// NopProvisioner: no-op.
	EnableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error

	// DisableDrift disables drift replacement for the Workspace's nodes.
	//
	// GpuProvisioner: no-op.
	// KarpenterProvisioner (future): patches NodePool budget nodes="1" → "0".
	// NopProvisioner: no-op.
	DisableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error
}
