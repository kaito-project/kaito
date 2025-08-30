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

// EnsureNodeClaims ensures the correct number of NodeClaims for the workspace
// based on the TargetNodeCount in the workspace status, considering BYO nodes
// only when all required NodeClaims are ready, it will return true.
func (c *NodeClaimManager) EnsureNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace) (bool, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()

	if !c.expectations.SatisfiedExpectations(c.logger, workspaceKey) {
		klog.V(4).InfoS("Waiting for NodeClaim expectations to be satisfied",
			"workspace", klog.KObj(wObj))
		return false, nil
	}

	// Calculate the number of NodeClaims needed (target - preferred nodes)
	requiredNodeClaimsCount, err := nodeclaim.GetRequiredNodeClaimsCount(ctx, c.Client, wObj)
	if err != nil {
		return false, fmt.Errorf("failed to get required NodeClaims: %w", err)
	}

	existingNodeClaims, err := nodeclaim.GetExistingNodeClaims(ctx, c.Client, wObj)
	if err != nil {
		return false, fmt.Errorf("failed to get existing NodeClaims: %w", err)
	}

	currentNodeClaimCount := len(existingNodeClaims)

	if currentNodeClaimCount < requiredNodeClaimsCount {
		nodesToCreate := requiredNodeClaimsCount - currentNodeClaimCount
		klog.InfoS("Creating additional NodeClaims",
			"workspace", klog.KObj(wObj),
			"current", currentNodeClaimCount,
			"required", requiredNodeClaimsCount,
			"toCreate", nodesToCreate)

		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"CreatingNodeClaims", fmt.Sprintf("Creating %d additional NodeClaims (current: %d, required: %d)", nodesToCreate, currentNodeClaimCount, requiredNodeClaimsCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}

		c.expectations.ExpectCreations(c.logger, workspaceKey, nodesToCreate)

		nodeOSDiskSize := c.determineNodeOSDiskSize(wObj)

		for range nodesToCreate {
			var nodeClaim *karpenterv1.NodeClaim
			var err error
			created := false

			retry.RetryOnConflict(retry.DefaultRetry, func() error {
				nodeClaim = nodeclaim.GenerateNodeClaimManifest(nodeOSDiskSize, wObj)
				err = c.Client.Create(ctx, nodeClaim)

				if err == nil {
					created = true
					return nil
				}
				return err
			})

			if !created {
				// Failed to create, decrement expectations
				c.expectations.CreationObserved(c.logger, workspaceKey)
				if err != nil {
					c.recorder.Eventf(wObj, "Warning", "NodeClaimCreationFailed",
						"Failed to create NodeClaim %s for workspace %s: %v", nodeClaim.Name, wObj.Name, err)
				} else {
					c.recorder.Eventf(wObj, "Warning", "NodeClaimCreationFailed",
						"Failed to create NodeClaim for workspace %s after retries", wObj.Name)
				}
				continue // should not return here or expectations will leak
			}

			klog.InfoS("NodeClaim created successfully",
				"nodeClaim", nodeClaim.Name,
				"workspace", klog.KObj(wObj))

			c.recorder.Eventf(wObj, "Normal", "NodeClaimCreated",
				"Successfully created NodeClaim %s for workspace %s", nodeClaim.Name, wObj.Name)
		}
	} else if currentNodeClaimCount > requiredNodeClaimsCount {
		// Need to delete excess NodeClaims
		nodesToDelete := currentNodeClaimCount - requiredNodeClaimsCount
		klog.InfoS("Deleting excess NodeClaims",
			"workspace", klog.KObj(wObj),
			"current", currentNodeClaimCount,
			"required", requiredNodeClaimsCount,
			"toDelete", nodesToDelete)

		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"DeletingNodeClaims", fmt.Sprintf("Deleting %d excess NodeClaims (current: %d, required: %d)", nodesToDelete, currentNodeClaimCount, requiredNodeClaimsCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}

		c.expectations.ExpectDeletions(c.logger, workspaceKey, nodesToDelete)

		// Sort NodeClaims for deletion: deletion timestamp set first, then not ready ones,
		// then by creation timestamp (newest first)
		sort.Slice(existingNodeClaims, func(i, j int) bool {
			nodeClaimI := existingNodeClaims[i]
			nodeClaimJ := existingNodeClaims[j]

			deletingI := nodeClaimI.DeletionTimestamp != nil
			deletingJ := nodeClaimJ.DeletionTimestamp != nil
			if deletingI != deletingJ {
				return deletingI // being deleted comes first
			}

			readyI := c.isNodeClaimReady(nodeClaimI)
			readyJ := c.isNodeClaimReady(nodeClaimJ)
			if readyI != readyJ {
				return !readyI // not ready comes first (true when i is not ready)
			}

			return nodeClaimI.CreationTimestamp.After(nodeClaimJ.CreationTimestamp.Time)
		})

		claimsToDelete := min(nodesToDelete, len(existingNodeClaims))
		for _, nodeClaim := range existingNodeClaims[:claimsToDelete] {
			if nodeClaim.DeletionTimestamp.IsZero() {
				if err := c.Client.Delete(ctx, nodeClaim); err != nil {
					c.expectations.DeletionObserved(c.logger, workspaceKey)
					klog.ErrorS(err, "failed to delete NodeClaim",
						"nodeClaim", nodeClaim.Name,
						"workspace", klog.KObj(wObj))
					c.recorder.Eventf(wObj, "Warning", "NodeClaimDeletionFailed",
						"Failed to delete NodeClaim %s for workspace %s: %v", nodeClaim.Name, wObj.Name, err)
					continue // should not return here or expectations will leak
				}
				klog.InfoS("NodeClaim deleted successfully",
					"nodeClaim", nodeClaim.Name,
					"creationTimestamp", nodeClaim.CreationTimestamp,
					"workspace", klog.KObj(wObj))

				c.recorder.Eventf(wObj, "Normal", "NodeClaimDeleted",
					"Successfully deleted NodeClaim %s for workspace %s", nodeClaim.Name, wObj.Name)
			} else {
				c.expectations.DeletionObserved(c.logger, workspaceKey)
			}
		}
	} else {
		klog.V(4).InfoS("NodeClaim count matches required",
			"workspace", klog.KObj(wObj),
			"nodeClaimCount", currentNodeClaimCount)
		for _, nodeClaim := range existingNodeClaims {
			if !c.isNodeClaimReady(nodeClaim) {
				if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
					"NodeClaimNotReady", fmt.Sprintf("NodeClaim %s is not ready yet", nodeClaim.Name)); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
				}

				return false, nil
			}
		}
		// All NodeClaims are ready - update status condition to indicate success
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
			"NodeClaimsReady", fmt.Sprintf("All NodeClaims are ready (NodeClaims: %d)", currentNodeClaimCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition: %w", updateErr)
		}
		return true, nil
	}

	return false, nil
}

// isNodeClaimReady checks if a NodeClaim is in ready state
func (c *NodeClaimManager) isNodeClaimReady(nodeClaim *karpenterv1.NodeClaim) bool {
	for _, condition := range nodeClaim.Status.Conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}

	return nodeClaim.Status.NodeName != ""
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
