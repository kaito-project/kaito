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
	"sort"

	"k8s.io/apimachinery/pkg/api/meta"
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
func (c *NodeClaimManager) DiffNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace) (bool, int, int, []*karpenterv1.NodeClaim, []string, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	var addedNodeClaimsCount int
	var deletedNodeClaimsCount int

	if !c.expectations.SatisfiedExpectations(c.logger, workspaceKey) {
		klog.V(4).InfoS("Waiting for NodeClaim expectations to be satisfied",
			"workspace", klog.KObj(wObj))
		return false, addedNodeClaimsCount, deletedNodeClaimsCount, nil, nil, nil
	}

	// Calculate the number of NodeClaims required (target - BYO nodes)
	readyNodes, targetNodeClaimsCount, err := nodeclaim.ResolveReadyNodesAndTargetNodeClaimCount(ctx, c.Client, wObj)
	if err != nil {
		return false, addedNodeClaimsCount, deletedNodeClaimsCount, nil, nil, fmt.Errorf("failed to get required NodeClaims: %w", err)
	}

	existingNodeClaims, err := nodeclaim.GetExistingNodeClaims(ctx, c.Client, wObj)
	if err != nil {
		return false, addedNodeClaimsCount, deletedNodeClaimsCount, nil, nil, fmt.Errorf("failed to get existing NodeClaims: %w", err)
	}

	if targetNodeClaimsCount > len(existingNodeClaims) {
		addedNodeClaimsCount = targetNodeClaimsCount - len(existingNodeClaims)
	} else if targetNodeClaimsCount < len(existingNodeClaims) {
		deletedNodeClaimsCount = len(existingNodeClaims) - targetNodeClaimsCount
	}

	return true, addedNodeClaimsCount, deletedNodeClaimsCount, existingNodeClaims, readyNodes, nil
}

// ScaleUpNodeClaims scales up the NodeClaims for the given workspace
// this function will be invoked before creating workloads for workspace in order to ensure nodes.
func (c *NodeClaimManager) ScaleUpNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, nodesToCreate int) (bool, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	klog.InfoS("Scaling up additional NodeClaims", "workspace", workspaceKey, "toCreate", nodesToCreate)

	if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
		"ScalingUpNodeClaims", fmt.Sprintf("Scaling up %d additional NodeClaims", nodesToCreate)); updateErr != nil {
		klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", workspaceKey)
		return false, fmt.Errorf("failed to update NodeClaim status condition: %w", updateErr)
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
	return true, nil
}

// MeetReadyNodeClaimsTarget is used for checking the number of ready nodeclaims(isNodeClaimReadyNotDeleting) meet the target count(workspace.Status.Inference.TargetNodeCount)
func (c *NodeClaimManager) MeetReadyNodeClaimsTarget(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	targetNodeCount := 1
	if wObj.Status.Inference != nil && wObj.Status.Inference.TargetNodeCount > 0 {
		targetNodeCount = int(wObj.Status.Inference.TargetNodeCount)
	}
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
		return true, nil
	} else {
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"NodeClaimNotReady", fmt.Sprintf("Ready NodeClaims are not enough (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeCount, readyCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition(NodeClaimNotReady): %w", updateErr)
		}

		return false, nil
	}
}

// ScaleDownNodeClaims scales down the NodeClaims for a workspace.
// This function will be invoked after scaling down workloads of workspace in order to ensure nodes without pods can be deleted.
func (c *NodeClaimManager) ScaleDownNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim, nodesToDelete int) error {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	klog.InfoS("Scaling down excess NodeClaims", "workspace", workspaceKey, "toDelete", nodesToDelete)

	if nodesToDelete == 0 {
		if curCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(kaitov1beta1.ConditionTypeScalingDownStatus)); curCondition != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeScalingDownStatus, metav1.ConditionTrue, "ScalingDownNodeClaimsCompleted", "Scaling down excess NodeClaims completed"); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update scaling down status condition(ScalingDownNodeClaimsCompleted)", "workspace", workspaceKey)
				return fmt.Errorf("failed to update scaling down status condition(ScalingDownNodeClaimsCompleted): %w", updateErr)
			}
		}
		return nil
	}

	if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeScalingDownStatus, metav1.ConditionFalse, "ScalingDownNodeClaims", "Scaling down excess NodeClaims"); updateErr != nil {
		klog.ErrorS(updateErr, "failed to update scaling down status condition(ScalingDownNodeClaims)", "workspace", workspaceKey)
		return fmt.Errorf("failed to update scaling down status condition(ScalingDownNodeClaims): %w", updateErr)
	}

	// filter nodeclaims that has no pod of workspace running on the node
	// only this kind of nodeclaims can be deleted
	claimsWithoutPods := make([]*karpenterv1.NodeClaim, 0, len(existingNodeClaims))
	for _, claim := range existingNodeClaims {
		if !nodeclaim.HasPodRunningOnNode(ctx, c.Client, wObj, claim) {
			claimsWithoutPods = append(claimsWithoutPods, claim)
		}
	}
	claimsToDelete := min(nodesToDelete, len(claimsWithoutPods))

	c.expectations.ExpectDeletions(c.logger, workspaceKey, claimsToDelete)

	// Sort NodeClaims for deletion: deletion timestamp set first, then not ready ones,
	// then by creation timestamp (newest first)
	sort.Slice(claimsWithoutPods, func(i, j int) bool {
		nodeClaimI := claimsWithoutPods[i]
		nodeClaimJ := claimsWithoutPods[j]

		deletingI := nodeClaimI.DeletionTimestamp != nil
		deletingJ := nodeClaimJ.DeletionTimestamp != nil
		if deletingI != deletingJ {
			return deletingI // being deleted comes first
		}

		readyI := nodeclaim.IsNodeClaimReadyNotDeleting(nodeClaimI)
		readyJ := nodeclaim.IsNodeClaimReadyNotDeleting(nodeClaimJ)
		if readyI != readyJ {
			return !readyI // not ready comes first (true when i is not ready)
		}

		return nodeClaimI.CreationTimestamp.After(nodeClaimJ.CreationTimestamp.Time)
	})

	for _, nodeClaim := range claimsWithoutPods[:claimsToDelete] {
		if nodeClaim.DeletionTimestamp.IsZero() {
			if err := c.Client.Delete(ctx, nodeClaim); err != nil {
				c.expectations.DeletionObserved(c.logger, workspaceKey)
				klog.ErrorS(err, "failed to delete NodeClaim",
					"nodeClaim", nodeClaim.Name,
					"workspace", workspaceKey)
				c.recorder.Eventf(wObj, "Warning", "NodeClaimDeletionFailed",
					"Failed to delete NodeClaim %s for workspace %s: %v", nodeClaim.Name, wObj.Name, err)
				continue // should not return here or expectations will leak
			}
			klog.InfoS("NodeClaim deleted successfully",
				"nodeClaim", nodeClaim.Name,
				"creationTimestamp", nodeClaim.CreationTimestamp,
				"workspace", workspaceKey)

			c.recorder.Eventf(wObj, "Normal", "NodeClaimDeleted",
				"Successfully deleted NodeClaim %s for workspace %s", nodeClaim.Name, wObj.Name)
		} else {
			c.expectations.DeletionObserved(c.logger, workspaceKey)
		}
	}

	if nodesToDelete > claimsToDelete {
		klog.InfoS("Not enough NodeClaims can be deleted because some NodeClaims still have pods running or are being deleted",
			"workspace", workspaceKey,
			"requestedToDelete", nodesToDelete,
			"actualDeleted", claimsToDelete)
		return fmt.Errorf("not enough NodeClaims can be deleted because some NodeClaims still have pods running or are being deleted, return error to trigger reconcile fastly")
	}
	return nil
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
