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

package gpuprovisioner

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

// AzureGPUProvisioner wraps the Azure gpu-provisioner
// (https://github.com/Azure/gpu-provisioner) logic behind the
// NodesProvisioner interface. It creates NodeClaims directly (the legacy
// path) and has no drift support.
type AzureGPUProvisioner struct {
	nodeClaimManager    *resource.NodeClaimManager
	nodeResourceManager *resource.NodeManager
}

var _ nodeprovision.NodesProvisioner = (*AzureGPUProvisioner)(nil)

// NewAzureGPUProvisioner creates an AzureGPUProvisioner that delegates to the existing
// NodeClaimManager and NodeManager.
func NewAzureGPUProvisioner(ncm *resource.NodeClaimManager, nm *resource.NodeManager) *AzureGPUProvisioner {
	return &AzureGPUProvisioner{
		nodeClaimManager:    ncm,
		nodeResourceManager: nm,
	}
}

// ProvisionNodes creates NodeClaims via the Azure gpu-provisioner backend.
func (g *AzureGPUProvisioner) ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	readyNodes, err := resources.GetReadyNodes(ctx, g.nodeClaimManager.Client, ws)
	if err != nil {
		return fmt.Errorf("failed to list ready nodes: %w", err)
	}

	numNodeClaimsToCreate, _, err := g.nodeClaimManager.CheckNodeClaims(ctx, ws, readyNodes)
	if err != nil {
		return err
	}
	klog.InfoS("NodeClaims to create", "count", numNodeClaimsToCreate, "workspace", klog.KObj(ws))

	if err := g.nodeClaimManager.CreateUpNodeClaims(ctx, ws, numNodeClaimsToCreate); err != nil {
		return err
	}

	return nil
}

// DeprovisionNodes deletes all NodeClaims associated with the workspace.
func (g *AzureGPUProvisioner) DeprovisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	ncList, err := nodeclaim.ListNodeClaim(ctx, ws, g.nodeClaimManager.Client)
	if err != nil {
		return err
	}

	for i := range ncList.Items {
		if ncList.Items[i].DeletionTimestamp.IsZero() {
			klog.InfoS("Deleting associated NodeClaim...", "nodeClaim", ncList.Items[i].Name)
			if deleteErr := g.nodeClaimManager.Delete(ctx, &ncList.Items[i], &client.DeleteOptions{}); deleteErr != nil {
				klog.ErrorS(deleteErr, "failed to delete the nodeClaim", "nodeClaim", klog.KObj(&ncList.Items[i]))
				return deleteErr
			}
		}
	}
	return nil
}

// EnableDrift is a no-op for Azure gpu-provisioner (no drift support).
func (g *AzureGPUProvisioner) EnableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

// DisableDrift is a no-op for Azure gpu-provisioner (no drift support).
func (g *AzureGPUProvisioner) DisableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

// EnsureNodesReady checks that:
//  1. All expected NodeClaims are in Ready state -> ProvisioningNotReady if not.
//  2. Enough Nodes with the correct instance type are ready -> NodesNotReady if not.
//  3. GPU device plugins are installed on provisioned nodes -> NodesNotReady if not.
func (g *AzureGPUProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (nodeprovision.NodeReadiness, error) {
	// List nodes once and derive both readyNodes (for NodeClaim check) and
	// readyCount with correct instance type (for node readiness check).
	nodeList, err := resources.ListNodes(ctx, g.nodeClaimManager.Client, ws.Resource.LabelSelector.MatchLabels)
	if err != nil {
		return nodeprovision.ProvisioningNotReady, fmt.Errorf("failed to list nodes: %w", err)
	}

	var readyNodes []*corev1.Node
	readyWithInstanceType := 0
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if resources.NodeIsReadyAndNotDeleting(node) {
			readyNodes = append(readyNodes, node)
			if instanceType, ok := node.Labels[corev1.LabelInstanceTypeStable]; ok && instanceType == ws.Resource.InstanceType {
				readyWithInstanceType++
			}
		}
	}

	// Step 1: Check NodeClaims readiness.
	ncList, err := nodeclaim.ListNodeClaim(ctx, ws, g.nodeClaimManager.Client)
	if err != nil {
		return nodeprovision.ProvisioningNotReady, fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	existingNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(ncList.Items))
	for i := range ncList.Items {
		existingNodeClaims = append(existingNodeClaims, &ncList.Items[i])
	}

	nodeClaimsReady, err := g.nodeClaimManager.EnsureNodeClaimsReady(ctx, ws, readyNodes, existingNodeClaims)
	if err != nil {
		return nodeprovision.ProvisioningNotReady, err
	}
	if !nodeClaimsReady {
		return nodeprovision.ProvisioningNotReady, nil
	}

	// Step 2: Check that enough Nodes with the correct instance type are ready.
	targetNodeCount := int(ws.Status.TargetNodeCount)
	if readyWithInstanceType < targetNodeCount {
		klog.InfoS("Not enough Nodes are ready for workspace",
			"workspace", client.ObjectKeyFromObject(ws).String(),
			"targetNodes", targetNodeCount, "currentReadyNodes", readyWithInstanceType)
		return nodeprovision.NodesNotReady, nil
	}

	// Step 3: Check GPU device plugins on provisioned nodes.
	ready, err := g.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, existingNodeClaims)
	if err != nil {
		return nodeprovision.NodesNotReady, fmt.Errorf("failed to check node plugin readiness: %w", err)
	}
	if !ready {
		return nodeprovision.NodesNotReady, nil
	}

	return nodeprovision.NodesReady, nil
}
