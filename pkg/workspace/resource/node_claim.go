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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
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

// DiffNodeClaims compares the current state of NodeClaims with the desired state
// the bool return value indicates whether the current reconciliation loop should exit or not.
// if return true, it means the nodeclaims sync has already completed, so the reconciliation should exit.
// if return false, it means the nodeclaims sync has not completed yet, so the reconciliation should not exit.
func (c *NodeClaimManager) DiffNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace) (bool, int, []*karpenterv1.NodeClaim, []string, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	var addedNodeClaimsCount int

	if !c.expectations.SatisfiedExpectations(c.logger, workspaceKey) {
		klog.V(4).InfoS("Waiting for NodeClaim expectations to be satisfied",
			"workspace", klog.KObj(wObj))
		return true, addedNodeClaimsCount, nil, nil, nil
	}

	// Calculate the number of NodeClaims required (target - BYO nodes)
	readyNodes, targetNodeClaimsCount, err := nodeclaim.ResolveReadyNodesAndTargetNodeClaimCount(ctx, c.Client, wObj)
	if err != nil {
		return true, addedNodeClaimsCount, nil, nil, fmt.Errorf("failed to get required NodeClaims: %w", err)
	}

	existingNodeClaims, err := nodeclaim.GetExistingNodeClaims(ctx, c.Client, wObj)
	if err != nil {
		return true, addedNodeClaimsCount, nil, nil, fmt.Errorf("failed to get existing NodeClaims: %w", err)
	}

	if targetNodeClaimsCount > len(existingNodeClaims) {
		addedNodeClaimsCount = targetNodeClaimsCount - len(existingNodeClaims)
	}

	return false, addedNodeClaimsCount, existingNodeClaims, readyNodes, nil
}

// ProvisionUpNodeClaims creates a specified number of NodeClaims as defined by nodesToCreate for the given workspace.
// this function will be invoked before creating workloads for workspace in order to ensure nodes.
// the bool return value indicates whether the current reconciliation loop should exit or not.
// if return true, it means the nodeclaims provision has not completed yet, so the reconciliation should exit.
// if return false, it means the nodeclaims provision has already completed, so the reconciliation should not exit.
func (c *NodeClaimManager) ProvisionUpNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, nodesToCreate int) (bool, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	klog.InfoS("Provisioning up additional NodeClaims", "workspace", workspaceKey, "toCreate", nodesToCreate)

	if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
		"ProvisioningUpNodeClaims", fmt.Sprintf("Provisioning up %d additional NodeClaims", nodesToCreate)); updateErr != nil {
		klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", workspaceKey)
		return true, fmt.Errorf("failed to update NodeClaim status condition: %w", updateErr)
	}

	c.expectations.ExpectCreations(c.logger, workspaceKey, nodesToCreate)

	nodeOSDiskSize := c.determineNodeOSDiskSize(wObj)

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
	return false, nil
}

// MeetReadyNodeClaimsTarget is used for checking the number of ready nodeclaims(isNodeClaimReadyNotDeleting) meet the target count(workspace.Status.TargetNodeCount)
// the bool return value indicates whether the current reconciliation loop should exit or not.
// if return true, it means the number of ready nodeclaims are not enough, so the reconciliation should exit.
// if return false, it means the number of ready nodeclaims are enough, so the reconciliation should not exit.
func (c *NodeClaimManager) MeetReadyNodeClaimsTarget(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	targetNodeCount := int(wObj.Status.TargetNodeCount)
	readyCount := 0
	for _, claim := range existingNodeClaims {
		if nodeclaim.IsNodeClaimReadyNotDeleting(claim) {
			readyCount++
		}
	}

	if readyCount >= targetNodeCount {
		// Enough NodeClaims are ready - update status condition to indicate success
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
			"NodeClaimsReady", fmt.Sprintf("Enough NodeClaims are ready (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeCount, readyCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition(NodeClaimsReady): %w", updateErr)
		}
		return false, nil
	} else {
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"NodeClaimNotReady", fmt.Sprintf("Ready NodeClaims are not enough (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeCount, readyCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition(NodeClaimNotReady): %w", updateErr)
		}

		return true, nil
	}
}

// determineNodeOSDiskSize returns the appropriate OS disk size for the workspace
func (c *NodeClaimManager) determineNodeOSDiskSize(wObj *kaitov1beta1.Workspace) string {
	var nodeOSDiskSize string
	if wObj.Inference != nil && wObj.Inference.Preset != nil && wObj.Inference.Preset.Name != "" {
		presetName := string(wObj.Inference.Preset.Name)
		nodeOSDiskSize = plugin.KaitoModelRegister.MustGet(presetName).
			GetInferenceParameters().DiskStorageRequirement
	}
	if nodeOSDiskSize == "" {
		nodeOSDiskSize = "1024Gi" // The default OS size is used
	}
	return nodeOSDiskSize
}
