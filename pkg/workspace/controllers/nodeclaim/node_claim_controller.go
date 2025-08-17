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

package nodeclaim

import (
	"context"
	"fmt"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	ctrlutils "github.com/kaito-project/kaito/pkg/workspace/controllers/utils"
	"github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/pkg/workspace/estimator/skubasednodesestimator"
)

type NodeClaimReconciler struct {
	client.Client
	client.Reader
	Estimator    estimator.NodesEstimator
	Recorder     record.EventRecorder
	expectations *utils.ControllerExpectations
	logger       klog.Logger
}

func NewNodeClaimReconciler() *NodeClaimReconciler {
	return &NodeClaimReconciler{
		Estimator:    &skubasednodesestimator.SKUBasedNodesEstimator{},
		logger:       klog.NewKlogr().WithName("WorkspaceController"),
		expectations: utils.NewControllerExpectations(),
	}
}

func (c *NodeClaimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// get the workspace object
	workspaceObj := &kaitov1beta1.Workspace{}
	if err := c.Client.Get(ctx, req.NamespacedName, workspaceObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.logger, req.String())
			return reconcile.Result{}, nil
		}
		c.logger.Error(err, "failed to get workspace", "workspace", req.Name)
		return reconcile.Result{}, err
	}

	// Handle workspace termination
	if !workspaceObj.DeletionTimestamp.IsZero() {
		return c.finalizeWorkspace(ctx, workspaceObj)
	}

	// Add finalizer if not present for workspace that is not terminated
	if err := c.ensureFinalizer(ctx, workspaceObj); err != nil {
		return reconcile.Result{}, err
	}

	// ensure PerReplicaNodeCount and TargetNodeCount is configured for workspace inference
	if workspaceObj.Inference != nil {
		statusUpdated, err := c.ensureWorkspaceInferenceStatus(ctx, workspaceObj)
		if err != nil {
			return reconcile.Result{}, err
		}
		if statusUpdated {
			return reconcile.Result{}, nil
		}
	}

	// ensure nodeclaims for workspace
	if err := c.ensureNodeClaims(ctx, workspaceObj); err != nil {
		return reconcile.Result{}, err
	}

	// wait for all nodeclaims to be ready
	ready, err := c.waitForNodeClaimsReady(ctx, workspaceObj)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !ready {
		klog.V(4).InfoS("Waiting for NodeClaims to be ready", "workspace", klog.KObj(workspaceObj))
		// nodeclaim update can trigger this reconcile loop, so we return without requeuing
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

// ensureWorkspaceInferenceStatus ensures PerReplicaNodeCount and TargetNodeCount are configured for workspace
func (c *NodeClaimReconciler) ensureWorkspaceInferenceStatus(ctx context.Context, wObj *kaitov1beta1.Workspace) (bool, error) {
	// If inference is not configured, nothing to do
	if wObj.Inference == nil {
		return false, nil
	}

	inferenceStatusUpdated := false

	// Initialize inference status if it's nil
	if wObj.Status.Inference == nil {
		wObj.Status.Inference = &kaitov1beta1.InferenceStatus{}
		inferenceStatusUpdated = true
	}

	// Set default PerReplicaNodeCount if not set
	if wObj.Status.Inference.PerReplicaNodeCount == 0 {
		perReplicaNodeCount, err := c.Estimator.EstimateNodeCount(ctx, wObj)
		if err != nil {
			return false, fmt.Errorf("failed to calculate per-replica node count: %w", err)
		}
		wObj.Status.Inference.PerReplicaNodeCount = perReplicaNodeCount
		inferenceStatusUpdated = true
	}

	// Calculate TargetNodeCount based on Replicas and PerReplicaNodeCount
	desiredReplicas := wObj.Inference.Replicas

	if desiredReplicas != 0 && wObj.Status.Inference.TargetNodeCount != desiredReplicas*wObj.Status.Inference.PerReplicaNodeCount {
		wObj.Status.Inference.TargetNodeCount = desiredReplicas * wObj.Status.Inference.PerReplicaNodeCount
		inferenceStatusUpdated = true
	} else if desiredReplicas == 0 && wObj.Status.Inference.TargetNodeCount != wObj.Status.Inference.PerReplicaNodeCount {
		// If no replicas are requested, default to 1 replica worth of nodes
		wObj.Status.Inference.TargetNodeCount = wObj.Status.Inference.PerReplicaNodeCount
		inferenceStatusUpdated = true
	}

	// Update status if needed
	if inferenceStatusUpdated {
		klog.InfoS("Updating workspace inference status",
			"workspace", klog.KObj(wObj),
			"perReplicaNodeCount", wObj.Status.Inference.PerReplicaNodeCount,
			"targetNodeCount", wObj.Status.Inference.TargetNodeCount,
			"replicas", desiredReplicas)

		if err := c.Client.Status().Update(ctx, wObj); err != nil {
			return false, fmt.Errorf("failed to update workspace inference status: %w", err)
		}
		return true, nil
	}

	return false, nil
}

// ensureNodeClaims ensures the correct number of NodeClaims for the workspace
// based on the TargetNodeCount in the workspace status, considering preferred nodes
func (c *NodeClaimReconciler) ensureNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()

	// Check if we should wait for expectations to be satisfied
	if !c.expectations.SatisfiedExpectations(c.logger, workspaceKey) {
		klog.V(4).InfoS("Waiting for NodeClaim expectations to be satisfied",
			"workspace", klog.KObj(wObj))
		return nil
	}

	// Calculate the number of NodeClaims needed (target - preferred nodes)
	requiredNodeClaimsCount, err := ctrlutils.GetRequiredNodeClaimsCount(ctx, c.Client, wObj)
	if err != nil {
		return fmt.Errorf("failed to get required NodeClaims: %w", err)
	}

	klog.V(4).InfoS("NodeClaim calculation",
		"workspace", klog.KObj(wObj),
		"requiredNodeClaims", requiredNodeClaimsCount)

	// Get existing NodeClaims for this workspace
	existingNodeClaims, err := ctrlutils.GetExistingNodeClaims(ctx, c.Client, wObj)
	if err != nil {
		return fmt.Errorf("failed to get existing NodeClaims: %w", err)
	}

	currentNodeClaimCount := len(existingNodeClaims)

	if currentNodeClaimCount < requiredNodeClaimsCount {
		// Need to create more NodeClaims
		nodesToCreate := requiredNodeClaimsCount - currentNodeClaimCount
		klog.InfoS("Creating additional NodeClaims",
			"workspace", klog.KObj(wObj),
			"current", currentNodeClaimCount,
			"required", requiredNodeClaimsCount,
			"toCreate", nodesToCreate)

		// Update status condition to indicate NodeClaims are being created
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"CreatingNodeClaims", fmt.Sprintf("Creating %d additional NodeClaims (current: %d, required: %d)", nodesToCreate, currentNodeClaimCount, requiredNodeClaimsCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}

		// Set expectation for NodeClaim creations
		c.expectations.ExpectCreations(c.logger, workspaceKey, nodesToCreate)

		nodeOSDiskSize := c.determineNodeOSDiskSize(wObj)

		for range nodesToCreate {
			var nodeClaim *karpenterv1.NodeClaim
			var err error
			created := false

			// Retry loop to handle NodeClaim name conflicts
			for retryAttempt := range 5 {
				nodeClaim = nodeclaim.GenerateNodeClaimManifest(nodeOSDiskSize, wObj)
				err = c.Client.Create(ctx, nodeClaim)

				if err == nil {
					// Successfully created
					created = true
					break
				} else if apierrors.IsAlreadyExists(err) {
					// NodeClaim with this name already exists, wait and retry with new name
					klog.V(4).InfoS("NodeClaim already exists, generating new name and retrying",
						"nodeClaim", nodeClaim.Name,
						"workspace", klog.KObj(wObj),
						"retry", retryAttempt+1)
					time.Sleep(100 * time.Millisecond)
					continue
				} else {
					// Other error, don't retry
					break
				}
			}

			if !created {
				// Failed to create, decrement expectations
				c.expectations.CreationObserved(c.logger, workspaceKey)
				if err != nil {
					// Generate event for NodeClaim creation failure
					c.Recorder.Eventf(wObj, "Warning", "NodeClaimCreationFailed",
						"Failed to create NodeClaim %s for workspace %s: %v", nodeClaim.Name, wObj.Name, err)
					return fmt.Errorf("failed to create NodeClaim %s: %w", nodeClaim.Name, err)
				}
				// Generate event for NodeClaim creation failure after retries
				c.Recorder.Eventf(wObj, "Warning", "NodeClaimCreationFailed",
					"Failed to create NodeClaim for workspace %s after retries", wObj.Name)
				return fmt.Errorf("failed to create NodeClaim after retries: %w", err)
			}

			klog.InfoS("NodeClaim created successfully",
				"nodeClaim", nodeClaim.Name,
				"workspace", klog.KObj(wObj))

			// Generate event for successful NodeClaim creation
			c.Recorder.Eventf(wObj, "Normal", "NodeClaimCreated",
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

		// Update status condition to indicate NodeClaims are being deleted
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"DeletingNodeClaims", fmt.Sprintf("Deleting %d excess NodeClaims (current: %d, required: %d)", nodesToDelete, currentNodeClaimCount, requiredNodeClaimsCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}

		// Set expectation for NodeClaim deletions
		c.expectations.ExpectDeletions(c.logger, workspaceKey, nodesToDelete)

		// Sort NodeClaims for deletion: deletion timestamp set first, then not ready ones, then by creation timestamp (newest first)
		sort.Slice(existingNodeClaims, func(i, j int) bool {
			nodeClaimI := existingNodeClaims[i]
			nodeClaimJ := existingNodeClaims[j]

			// Priority 1: Check if NodeClaims have deletion timestamp set
			deletingI := nodeClaimI.DeletionTimestamp != nil
			deletingJ := nodeClaimJ.DeletionTimestamp != nil

			// If one is being deleted and the other is not, prioritize the one being deleted
			if deletingI != deletingJ {
				return deletingI // being deleted comes first
			}

			// Priority 2: If both have the same deletion status, check readiness
			readyI := c.isNodeClaimReady(nodeClaimI)
			readyJ := c.isNodeClaimReady(nodeClaimJ)

			// If one is ready and the other is not, prioritize deleting the not-ready one
			if readyI != readyJ {
				return !readyI // not ready comes first (true when i is not ready)
			}

			// Priority 3: If both have the same deletion and readiness status, sort by creation timestamp (newest first)
			return nodeClaimI.CreationTimestamp.After(nodeClaimJ.CreationTimestamp.Time)
		})

		// Delete the excess NodeClaims (prioritized by deletion timestamp, readiness, then creation time)
		claimsToDelete := min(nodesToDelete, len(existingNodeClaims))
		for _, nodeClaim := range existingNodeClaims[:claimsToDelete] {
			if nodeClaim.DeletionTimestamp.IsZero() {
				if err := c.Client.Delete(ctx, nodeClaim); err != nil {
					// Failed to delete, decrement expectations
					c.expectations.DeletionObserved(c.logger, workspaceKey)
					klog.ErrorS(err, "failed to delete NodeClaim",
						"nodeClaim", nodeClaim.Name,
						"workspace", klog.KObj(wObj))
					// Generate event for NodeClaim deletion failure
					c.Recorder.Eventf(wObj, "Warning", "NodeClaimDeletionFailed",
						"Failed to delete NodeClaim %s for workspace %s: %v", nodeClaim.Name, wObj.Name, err)
					return fmt.Errorf("failed to delete NodeClaim %s: %w", nodeClaim.Name, err)
				}
				klog.InfoS("NodeClaim deleted successfully",
					"nodeClaim", nodeClaim.Name,
					"creationTimestamp", nodeClaim.CreationTimestamp,
					"workspace", klog.KObj(wObj))

				// Generate event for successful NodeClaim deletion
				c.Recorder.Eventf(wObj, "Normal", "NodeClaimDeleted",
					"Successfully deleted NodeClaim %s for workspace %s", nodeClaim.Name, wObj.Name)
			} else {
				c.expectations.DeletionObserved(c.logger, workspaceKey)
			}
		}
	} else {
		// Current count matches required, no action needed
		klog.V(4).InfoS("NodeClaim count matches required",
			"workspace", klog.KObj(wObj),
			"nodeClaimCount", currentNodeClaimCount)
	}

	return nil
}

// waitForNodeClaimsReady checks if all NodeClaims are ready and match the target count
func (c *NodeClaimReconciler) waitForNodeClaimsReady(ctx context.Context, wObj *kaitov1beta1.Workspace) (bool, error) {
	targetNodeCount := 1
	if wObj.Status.Inference != nil {
		targetNodeCount = int(wObj.Status.Inference.TargetNodeCount)
	}

	// Find available BYO nodes
	availableBYONodes, err := ctrlutils.GetBringYourOwnNodes(ctx, c.Client, wObj)
	if err != nil {
		// Update status condition to indicate error getting BYO nodes
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"BYONodeListError", fmt.Sprintf("Failed to get BYO nodes: %v", err)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}
		return false, fmt.Errorf("failed to get available BYO nodes: %w", err)
	}

	// if node provision is disabled, user should ensure the number of BYO nodes more than target nodes.
	if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		if len(availableBYONodes) < targetNodeCount {
			if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
				"BYONodesNotEnough", fmt.Sprintf("BYO nodes is not enough(ready BYO nodes count: %d, target nodes count: %d", len(availableBYONodes), targetNodeCount)); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			}
			return false, fmt.Errorf("when node auto-provisioning is disabled, at least %d BYO nodes must match the label selector and be ready and not deleting, only have %d", targetNodeCount, len(availableBYONodes))
		}

		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
			"NodeClaimsReady", fmt.Sprintf("Node auto provisioning is disabled, so NodeClaims is not required(BYO nodes: %d)", len(availableBYONodes))); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update NodeClaim status condition: %w", updateErr)
		}
		return true, nil
	}

	// Calculate the number of NodeClaims needed
	requiredNodeClaims := max(0, targetNodeCount-len(availableBYONodes))

	// Get existing NodeClaims for this workspace
	existingNodeClaims, err := ctrlutils.GetExistingNodeClaims(ctx, c.Client, wObj)
	if err != nil {
		// Update status condition to indicate error
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"NodeClaimListError", fmt.Sprintf("Failed to get NodeClaims: %v", err)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}
		return false, fmt.Errorf("failed to get existing NodeClaims: %w", err)
	}

	currentNodeClaimCount := len(existingNodeClaims)

	// Check if the number of NodeClaims matches the required count
	if currentNodeClaimCount != requiredNodeClaims {
		// Update status condition to indicate NodeClaims are not ready
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"NodeClaimCountMismatch", fmt.Sprintf("NodeClaim count (%d) does not match required (%d, target: %d, BYO: %d)", currentNodeClaimCount, requiredNodeClaims, targetNodeCount, len(availableBYONodes))); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		}

		klog.V(4).InfoS("NodeClaim count does not match required, waiting for reconcile",
			"workspace", klog.KObj(wObj),
			"currentNodeClaims", currentNodeClaimCount,
			"requiredNodeClaims", requiredNodeClaims,
			"targetNodeCount", targetNodeCount,
			"preferredNodes", len(availableBYONodes))
		return false, nil
	}

	// Check if all NodeClaims are initialized (ready)
	for _, nodeClaim := range existingNodeClaims {
		if !c.isNodeClaimReady(nodeClaim) {
			// Update status condition to indicate NodeClaims are not ready
			if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
				"NodeClaimNotReady", fmt.Sprintf("NodeClaim %s is not ready yet", nodeClaim.Name)); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
			}

			klog.V(4).InfoS("NodeClaim is not ready yet",
				"workspace", klog.KObj(wObj),
				"nodeClaim", nodeClaim.Name,
				"status", nodeClaim.Status.Conditions)
			return false, nil
		}
	}

	// All NodeClaims are ready - update status condition to indicate success
	if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
		"NodeClaimsReady", fmt.Sprintf("All NodeClaims are ready (NodeClaims: %d, BYO nodes: %d)", currentNodeClaimCount, len(availableBYONodes))); updateErr != nil {
		klog.ErrorS(updateErr, "failed to update NodeClaim status condition", "workspace", klog.KObj(wObj))
		return false, fmt.Errorf("failed to update NodeClaim status condition: %w", updateErr)
	}

	klog.InfoS("All NodeClaims are ready",
		"workspace", klog.KObj(wObj),
		"nodeClaimCount", currentNodeClaimCount,
		"BYONodeCount", len(availableBYONodes),
		"totalNodes", currentNodeClaimCount+len(availableBYONodes))
	return true, nil
}

// isNodeClaimReady checks if a NodeClaim is in ready state
func (c *NodeClaimReconciler) isNodeClaimReady(nodeClaim *karpenterv1.NodeClaim) bool {
	// Check if NodeClaim has Ready condition set to True
	for _, condition := range nodeClaim.Status.Conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}

	// Alternative check: if NodeClaim has been launched successfully
	// Check if the NodeClaim has a node name assigned (indicates it's provisioned)
	if nodeClaim.Status.NodeName != "" {
		return true
	}

	// If no Ready condition found and no node assigned, consider it not ready
	return false
}

// determineNodeOSDiskSize returns the appropriate OS disk size for the workspace
func (c *NodeClaimReconciler) determineNodeOSDiskSize(wObj *kaitov1beta1.Workspace) string {
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

// ensureFinalizer adds the workspace finalizer if it's not already present
func (c *NodeClaimReconciler) ensureFinalizer(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) error {
	if !controllerutil.ContainsFinalizer(workspaceObj, consts.WorkspaceFinalizer) {
		patch := client.MergeFrom(workspaceObj.DeepCopy())
		controllerutil.AddFinalizer(workspaceObj, consts.WorkspaceFinalizer)
		if err := c.Client.Patch(ctx, workspaceObj, patch); err != nil {
			c.logger.Error(err, "failed to add finalizer to workspace", "workspace", klog.KObj(workspaceObj))
			// Generate event for finalizer addition failure
			c.Recorder.Eventf(workspaceObj, "Warning", "FinalizerAdditionFailed",
				"Failed to add finalizer to workspace %s: %v", workspaceObj.Name, err)
			return err
		}
		klog.InfoS("Added finalizer to workspace", "workspace", klog.KObj(workspaceObj), "finalizer", consts.WorkspaceFinalizer)
		// Generate event for successful finalizer addition
		c.Recorder.Eventf(workspaceObj, "Normal", "FinalizerAdded",
			"Successfully added finalizer to workspace %s", workspaceObj.Name)
	}
	return nil
}

// finalizeWorkspace handles workspace deletion by cleaning up NodeClaims and removing the finalizer
func (c *NodeClaimReconciler) finalizeWorkspace(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	klog.InfoS("Handling workspace termination", "workspace", klog.KObj(workspaceObj))

	// Get all NodeClaims associated with this workspace from kube-apiserver directly.
	existingNodeClaims, err := ctrlutils.GetExistingNodeClaims(ctx, c.Reader, workspaceObj)
	if err != nil {
		c.logger.Error(err, "failed to get existing NodeClaims during workspace termination", "workspace", klog.KObj(workspaceObj))
		return reconcile.Result{}, err
	}

	// Delete all NodeClaims that are not already being deleted
	nodeClaimsToDelete := 0
	for _, nodeClaim := range existingNodeClaims {
		if nodeClaim.DeletionTimestamp.IsZero() {
			nodeClaimsToDelete++
			if err := c.Client.Delete(ctx, nodeClaim); err != nil {
				c.logger.Error(err, "failed to delete NodeClaim during workspace termination",
					"nodeClaim", nodeClaim.Name, "workspace", klog.KObj(workspaceObj))
				// Generate event for NodeClaim deletion failure during termination
				c.Recorder.Eventf(workspaceObj, "Warning", "NodeClaimDeletionFailed",
					"Failed to delete NodeClaim %s during workspace termination: %v", nodeClaim.Name, err)
				return reconcile.Result{}, err
			}
			klog.InfoS("Deleted NodeClaim during workspace termination",
				"nodeClaim", nodeClaim.Name, "workspace", klog.KObj(workspaceObj))
			// Generate event for successful NodeClaim deletion during termination
			c.Recorder.Eventf(workspaceObj, "Normal", "NodeClaimDeleted",
				"Successfully deleted NodeClaim %s during workspace termination", nodeClaim.Name)
		}
	}

	// If there are still NodeClaims being deleted, wait for them to be cleaned up
	if len(existingNodeClaims) > 0 {
		klog.InfoS("Waiting for NodeClaims to be deleted", "workspace", klog.KObj(workspaceObj), "remaining", len(existingNodeClaims))
		// Requeue to check again later
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// All NodeClaims have been cleaned up, remove the finalizer
	if controllerutil.ContainsFinalizer(workspaceObj, consts.WorkspaceFinalizer) {
		patch := client.MergeFrom(workspaceObj.DeepCopy())
		controllerutil.RemoveFinalizer(workspaceObj, consts.WorkspaceFinalizer)
		if err := c.Client.Patch(ctx, workspaceObj, patch); err != nil {
			c.logger.Error(err, "failed to remove finalizer from workspace", "workspace", klog.KObj(workspaceObj))
			// Generate event for finalizer removal failure
			c.Recorder.Eventf(workspaceObj, "Warning", "FinalizerRemovalFailed",
				"Failed to remove finalizer from workspace %s: %v", workspaceObj.Name, err)
			return reconcile.Result{}, err
		}
		klog.InfoS("Removed finalizer from workspace", "workspace", klog.KObj(workspaceObj), "finalizer", consts.WorkspaceFinalizer)
		// Generate event for successful finalizer removal
		c.Recorder.Eventf(workspaceObj, "Normal", "FinalizerRemoved",
			"Successfully removed finalizer from workspace %s", workspaceObj.Name)
	}

	klog.InfoS("Successfully handled workspace termination", "workspace", klog.KObj(workspaceObj))
	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *NodeClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("NodeClaimController")
	c.Client = mgr.GetClient()
	c.Reader = mgr.GetAPIReader()

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1beta1.Workspace{}).
		Watches(&karpenterv1.NodeClaim{},
			&nodeClaimEventHandler{
				logger:         c.logger,
				expectations:   c.expectations,
				enqueueHandler: enqueueWorkspaceForNodeClaim,
			},
			builder.WithPredicates(nodeclaim.NodeClaimPredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 50})

	return builder.Complete(c)
}
