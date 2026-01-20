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

package resource

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

type NodeClaimManager struct {
	client.Client
	recorder     record.EventRecorder
	expectations *utils.ControllerExpectations
	logger       klog.Logger
}

func NewNodeClaimManager(c client.Client, recorder record.EventRecorder, expectations *utils.ControllerExpectations) *NodeClaimManager {
	return &NodeClaimManager{
		Client:       c,
		recorder:     recorder,
		expectations: expectations,
		logger:       klog.NewKlogr().WithName("NodeClaim"),
	}
}

// GetNumNodeClaimsNeeded calculates how many NodeClaims are needed to meet the target node count for the workspace.
func (c *NodeClaimManager) GetNumNodeClaimsNeeded(ctx context.Context, wObj *kaitov1beta1.Workspace, readyNodes []*corev1.Node) int {
	targetNodeCount := int(wObj.Status.TargetNodeCount)

	// Count ready nodes that do NOT have a corresponding NodeClaim
	readyNodesWithoutNodeClaim := 0

	for _, node := range readyNodes {
		if _, ok := node.Labels[consts.LabelNodePool]; !ok {
			readyNodesWithoutNodeClaim++
		}
	}

	// Calculate how many NodeClaims we need, including those already provisioned.
	targetNodeClaimCount := max(0, targetNodeCount-readyNodesWithoutNodeClaim)

	return targetNodeClaimCount
}

// CheckNodeClaims checks the current state of NodeClaims for the given workspace and determines how many additional NodeClaims need to be created to meet the target node count.
func (c *NodeClaimManager) CheckNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, readyNodes []*corev1.Node) (int, []*karpenterv1.NodeClaim, error) {
	// We don't care in this case if the ready nodes come from NodeClaims, meaning ready nodes could come from BYO if the right size and properly labeled.
	ncList, err := nodeclaim.ListNodeClaim(ctx, wObj, c.Client)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get existing NodeClaims: %w", err)
	}

	nodeClaims := make([]*karpenterv1.NodeClaim, 0, len(ncList.Items))
	for i := range ncList.Items {
		nodeClaims = append(nodeClaims, &ncList.Items[i])
	}

	klog.InfoS("Existing NodeClaims fetched", "count", len(nodeClaims), "workspace", klog.KObj(wObj))

	// Calculate the total number of NodeClaims needed.
	numNodeClaimsNeeded := c.GetNumNodeClaimsNeeded(ctx, wObj, readyNodes)
	klog.InfoS("NodeClaims needed calculation", "needed", numNodeClaimsNeeded, "workspace", klog.KObj(wObj))

	// Then, the number of NodeClaims to create is the difference between the total number needed and number of existing NodeClaims.
	numNodeClaimsToCreate := max(0, numNodeClaimsNeeded-len(nodeClaims))

	klog.InfoS("Number of NodeClaims to create", "count", numNodeClaimsToCreate)

	return numNodeClaimsToCreate, nodeClaims, nil
}

// CreateUpNodeClaims creates a specified number of NodeClaims as defined by nodesToCreate for the given workspace.
// this function will be invoked before creating workloads for workspace in order to ensure nodes.
func (c *NodeClaimManager) CreateUpNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, nodesToCreate int) error {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	klog.InfoS("Creating additional NodeClaims", "workspace", workspaceKey, "toCreate", nodesToCreate)
	if nodesToCreate <= 0 {
		return nil
	}

	if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
		"CreatingUpNodeClaims", fmt.Sprintf("Creating up %d additional NodeClaims", nodesToCreate)); updateErr != nil {
		klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", workspaceKey)
		return fmt.Errorf("failed to update NodeClaim status condition: %w", updateErr)
	}

	c.expectations.ExpectCreations(c.logger, workspaceKey, nodesToCreate)

	nodeOSDiskSize := c.determineNodeOSDiskSize(ctx, wObj)

	for range nodesToCreate {
		var nodeClaim *karpenterv1.NodeClaim

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			nodeClaim = nodeclaim.GenerateNodeClaimManifest(nodeOSDiskSize, wObj)
			return c.Client.Create(ctx, nodeClaim)
		})

		if err != nil {
			// Failed to create, decrement expectations
			c.expectations.CreationObserved(c.logger, workspaceKey)
			c.recorder.Eventf(wObj, "Warning", "NodeClaimCreationFailed", "Failed to create NodeClaim %s for workspace %s: %v", nodeClaim.Name, wObj.Name, err)
			continue // should not return here or expectations will leak
		}

		klog.InfoS("NodeClaim created successfully", "nodeClaim", nodeClaim.Name, "workspace", workspaceKey)

		c.recorder.Eventf(wObj, "Normal", "NodeClaimCreated",
			"Successfully created NodeClaim %s for workspace %s", nodeClaim.Name, workspaceKey)
	}
	return nil
}

// EnsureNodeClaimsReady is used for checking the number of ready nodeclaims(isNodeClaimReadyNotDeleting) meet the target NodeClaim count needed. Updates the
func (c *NodeClaimManager) EnsureNodeClaimsReady(ctx context.Context, wObj *kaitov1beta1.Workspace, readyNodes []*corev1.Node, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	targetNodeClaimCount := c.GetNumNodeClaimsNeeded(ctx, wObj, readyNodes)

	readyCount := 0
	for _, claim := range existingNodeClaims {
		if nodeclaim.IsNodeClaimReadyNotDeleting(claim) {
			readyCount++
		}
	}

	klog.InfoS("NodeClaim readiness check",
		"workspace", klog.KObj(wObj),
		"targetNodeCount", wObj.Status.TargetNodeCount,
		"targetNodeClaimCount", targetNodeClaimCount,
		"readyNodeClaimCount", readyCount,
		"totalExistingNodeClaims", len(existingNodeClaims))

	if readyCount >= targetNodeClaimCount {
		// Enough NodeClaims are ready - update status condition to indicate success
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
			"NodeClaimsReady", fmt.Sprintf("Enough NodeClaims are ready (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeClaimCount, readyCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition NodeClaimsReady to true", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition(NodeClaimsReady): %w", updateErr)
		}
		return true, nil
	} else {
		klog.InfoS("Ready nodeClaims for workspace are not enough currently", "workspace", client.ObjectKeyFromObject(wObj).String(),
			"targetNodeClaims", targetNodeClaimCount, "currentReadyNodeClaims", readyCount)
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"NodeClaimNotReady", fmt.Sprintf("Ready NodeClaims are not enough (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeClaimCount, readyCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition NodeClaimsReady to false", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition(NodeClaimNotReady): %w", updateErr)
		}

		return false, nil
	}
}

// determineNodeOSDiskSize returns the appropriate OS disk size for the workspace
func (c *NodeClaimManager) determineNodeOSDiskSize(ctx context.Context, wObj *kaitov1beta1.Workspace) string {
	var nodeOSDiskSize string
	if wObj.Inference != nil && wObj.Inference.Preset != nil && wObj.Inference.Preset.Name != "" {
		presetName := string(wObj.Inference.Preset.Name)
		secretName := wObj.Inference.Preset.PresetOptions.ModelAccessSecret

		model, err := models.GetModelByName(ctx, presetName, secretName, wObj.Namespace, c.Client)
		if err == nil {
			nodeOSDiskSize = model.GetInferenceParameters().DiskStorageRequirement
		} else {
			klog.ErrorS(err, "failed to get model by name when determining Node OS disk size", "model", presetName, "workspace", klog.KObj(wObj))
		}
	}
	if nodeOSDiskSize == "" {
		nodeOSDiskSize = "1024Gi" // The default OS size is used
	}
	return nodeOSDiskSize
}
