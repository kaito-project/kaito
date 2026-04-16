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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// NodeProvisioner interface. It creates NodeClaims directly (the legacy
// path) and has no drift support.
type AzureGPUProvisioner struct {
	nodeClaimManager    *resource.NodeClaimManager
	nodeResourceManager *resource.NodeManager
}

var _ nodeprovision.NodeProvisioner = (*AzureGPUProvisioner)(nil)

// NewAzureGPUProvisioner creates an AzureGPUProvisioner that delegates to the existing
// NodeClaimManager and NodeManager.
func NewAzureGPUProvisioner(ncm *resource.NodeClaimManager, nm *resource.NodeManager) *AzureGPUProvisioner {
	return &AzureGPUProvisioner{
		nodeClaimManager:    ncm,
		nodeResourceManager: nm,
	}
}

// Name returns the provisioner name.
func (g *AzureGPUProvisioner) Name() string { return "AzureGPUProvisioner" }

// Start is a no-op for AzureGPUProvisioner.
func (g *AzureGPUProvisioner) Start(ctx context.Context) error { return nil }

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

	return g.nodeClaimManager.CreateUpNodeClaims(ctx, ws, numNodeClaimsToCreate)
}

// DeleteNodes deletes all NodeClaims associated with the workspace.
func (g *AzureGPUProvisioner) DeleteNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
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

// EnableDriftRemediation is a no-op for Azure gpu-provisioner (no drift support).
func (g *AzureGPUProvisioner) EnableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

// DisableDriftRemediation is a no-op for Azure gpu-provisioner (no drift support).
func (g *AzureGPUProvisioner) DisableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

// EnsureNodesReady checks that:
//  1. All expected NodeClaims are in Ready state -> (false, false) if not.
//  2. Enough Nodes with the correct instance type are ready -> (false, true) if not.
//  3. GPU device plugins are installed on provisioned nodes -> (false, true) if not.
func (g *AzureGPUProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (bool, bool, error) {
	// List nodes once and derive both readyNodes (for NodeClaim check) and
	// readyCount with correct instance type (for node readiness check).
	nodeList, err := resources.ListNodes(ctx, g.nodeClaimManager.Client, ws.Resource.LabelSelector.MatchLabels)
	if err != nil {
		return false, false, fmt.Errorf("failed to list nodes: %w", err)
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
		return false, false, fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	existingNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(ncList.Items))
	for i := range ncList.Items {
		existingNodeClaims = append(existingNodeClaims, &ncList.Items[i])
	}

	nodeClaimsReady, err := g.nodeClaimManager.EnsureNodeClaimsReady(ctx, ws, readyNodes, existingNodeClaims)
	if err != nil {
		return false, false, err
	}
	if !nodeClaimsReady {
		return false, false, nil
	}

	// Step 2: Check that enough Nodes with the correct instance type are ready.
	targetNodeCount := int(ws.Status.TargetNodeCount)
	if readyWithInstanceType < targetNodeCount {
		klog.InfoS("Not enough Nodes are ready for workspace",
			"workspace", client.ObjectKeyFromObject(ws).String(),
			"targetNodes", targetNodeCount, "currentReadyNodes", readyWithInstanceType)
		return false, true, nil
	}

	// Step 3: Check GPU device plugins on provisioned nodes.
	pluginReady, err := g.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, existingNodeClaims)
	if err != nil {
		return false, true, fmt.Errorf("failed to check node plugin readiness: %w", err)
	}
	if !pluginReady {
		return false, true, nil
	}

	return true, false, nil
}

// CollectNodeStatusInfo gathers status conditions for workspace status.
func (g *AzureGPUProvisioner) CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	nodeCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeNodeStatus), Status: metav1.ConditionFalse,
		Reason: "NodeNotReady", Message: "Not enough Nodes are ready",
	}
	nodeClaimCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeNodeClaimStatus), Status: metav1.ConditionFalse,
		Reason: "NodeClaimNotReady", Message: "Ready NodeClaims are not enough",
	}
	resourceCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeResourceStatus), Status: metav1.ConditionFalse,
		Reason: "workspaceResourceStatusNotReady", Message: "node claim or node status condition not ready",
	}

	nodeList, err := resources.ListNodes(ctx, g.nodeClaimManager.Client, ws.Resource.LabelSelector.MatchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	var readyNodes []*corev1.Node
	readyWithInstanceType := 0
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if resources.NodeIsReadyAndNotDeleting(node) {
			readyNodes = append(readyNodes, node)
			if it, ok := node.Labels[corev1.LabelInstanceTypeStable]; ok && it == ws.Resource.InstanceType {
				readyWithInstanceType++
			}
		}
	}

	// NodeClaim readiness.
	ncList, err := nodeclaim.ListNodeClaim(ctx, ws, g.nodeClaimManager.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to list NodeClaims: %w", err)
	}
	existingNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(ncList.Items))
	for i := range ncList.Items {
		existingNodeClaims = append(existingNodeClaims, &ncList.Items[i])
	}
	targetNodeClaimCount := g.nodeClaimManager.GetNumNodeClaimsNeeded(ctx, ws, readyNodes)
	readyNodeClaimCount := 0
	for _, claim := range existingNodeClaims {
		if nodeclaim.IsNodeClaimReadyNotDeleting(claim) {
			readyNodeClaimCount++
		}
	}
	if readyNodeClaimCount >= targetNodeClaimCount {
		nodeClaimCond.Status = metav1.ConditionTrue
		nodeClaimCond.Reason = "NodeClaimsReady"
		nodeClaimCond.Message = "Enough NodeClaims are ready"
	}

	// Node readiness.
	targetNodeCount := int(ws.Status.TargetNodeCount)
	if readyWithInstanceType >= targetNodeCount {
		nodeCond.Status = metav1.ConditionTrue
		nodeCond.Reason = "NodesReady"
		nodeCond.Message = "Enough Nodes are ready"
		pluginReady, pluginErr := g.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, existingNodeClaims)
		if pluginErr != nil {
			nodeCond.Status = metav1.ConditionFalse
			nodeCond.Reason = "NodePluginsNotReady"
			nodeCond.Message = pluginErr.Error()
		} else if !pluginReady {
			nodeCond.Status = metav1.ConditionFalse
			nodeCond.Reason = "NodePluginsNotReady"
			nodeCond.Message = "waiting all node plugins to be ready"
		}
	}

	// Derive resource condition.
	if nodeCond.Status == metav1.ConditionTrue && nodeClaimCond.Status == metav1.ConditionTrue {
		resourceCond.Status = metav1.ConditionTrue
		resourceCond.Reason = "workspaceResourceStatusSuccess"
		resourceCond.Message = "workspace resource is ready"
	} else if nodeClaimCond.Status != metav1.ConditionTrue {
		resourceCond.Reason = nodeClaimCond.Reason
		resourceCond.Message = nodeClaimCond.Message
	} else {
		resourceCond.Reason = nodeCond.Reason
		resourceCond.Message = nodeCond.Message
	}

	return []metav1.Condition{nodeCond, nodeClaimCond, resourceCond}, nil
}
