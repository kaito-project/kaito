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

package noderesource

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	ctrlutils "github.com/kaito-project/kaito/pkg/workspace/controllers/utils"
)

type NodeResourceReconciler struct {
	client.Client
}

func NewNodeResourceReconciler() *NodeResourceReconciler {
	return &NodeResourceReconciler{}
}

func (c *NodeResourceReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// get the workspace object
	workspaceObj := &kaitov1beta1.Workspace{}
	if err := c.Client.Get(ctx, req.NamespacedName, workspaceObj); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	nodeClaimCondition := meta.FindStatusCondition(workspaceObj.Status.Conditions, string(kaitov1beta1.ConditionTypeNodeClaimStatus))
	if nodeClaimCondition == nil || nodeClaimCondition.Status != metav1.ConditionTrue {
		klog.V(4).InfoS("Waiting for NodeClaims to be ready", "workspace", klog.KObj(workspaceObj))

		// Set ResourceStatus condition to false if NodeClaim condition is not true
		if err := c.resetResourceStatusConditionIfNeeded(ctx, workspaceObj); err != nil {
			klog.ErrorS(err, "failed to reset resource status condition", "workspace", klog.KObj(workspaceObj))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// ensure Nvidia device plugins are ready for the workspace
	knownGPUConfig, _ := utils.GetGPUConfigBySKU(workspaceObj.Resource.InstanceType)
	if knownGPUConfig != nil {
		devicePluginsReady, err := c.ensureNvidiaDevicePluginsReady(ctx, workspaceObj)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !devicePluginsReady {
			klog.V(4).InfoS("Waiting for NVIDIA device plugins to be ready", "workspace", klog.KObj(workspaceObj))
			return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
		}
	}

	// update the workspace status to indicate all resources are ready
	if err := c.updateWorkspaceStatusIfNeeded(ctx, workspaceObj); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(workspaceObj))
		return reconcile.Result{}, err
	}

	klog.InfoS("All resources are ready for workspace", "workspace", klog.KObj(workspaceObj))

	return reconcile.Result{}, nil
}

// ensureNvidiaDevicePluginsReady ensures that NVIDIA device plugins are ready on all nodes for the workspace
func (c *NodeResourceReconciler) ensureNvidiaDevicePluginsReady(ctx context.Context, wObj *kaitov1beta1.Workspace) (bool, error) {
	requiredProvisionedNodeCount, err := ctrlutils.GetRequiredNodeClaimsCount(ctx, c.Client, wObj)
	if err != nil {
		return false, fmt.Errorf("failed to get required node claims count: %w", err)
	}

	// Get all nodes for this workspace
	nodes, err := c.getReadyNodesFromNodeClaims(ctx, wObj)
	if err != nil {
		// Update status condition to indicate error getting nodes
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
			"NodeListError", fmt.Sprintf("Failed to get nodes for workspace: %v", err)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
		}
		return false, fmt.Errorf("failed to get nodes for workspace: %w", err)
	}

	// Check if the number of nodes matches the target count
	if len(nodes) != requiredProvisionedNodeCount {
		// Update status condition to indicate node count mismatch
		if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
			"NodeCountMismatch", fmt.Sprintf("Node count (%d) does not match target provisioned (%d)", len(nodes), requiredProvisionedNodeCount)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
		}

		klog.V(4).InfoS("Node count does not match target, waiting for nodes to be ready",
			"workspace", klog.KObj(wObj),
			"currentNodes", len(nodes),
			"targetProvisionedNodeCount", requiredProvisionedNodeCount)
		return false, nil
	}

	// Check each node for NVIDIA accelerator label and GPU capacity
	for _, node := range nodes {
		// Check if node has the required accelerator label
		if accelerator, exists := node.Labels[resources.LabelKeyNvidia]; !exists || accelerator != resources.LabelValueNvidia {
			klog.InfoS("Adding accelerator label to node",
				"workspace", klog.KObj(wObj),
				"node", node.Name,
				"currentAccelerator", accelerator)

			// Add the accelerator label to the node
			if node.Labels == nil {
				node.Labels = make(map[string]string)
			}
			node.Labels[resources.LabelKeyNvidia] = resources.LabelValueNvidia

			if err := c.Client.Update(ctx, node); err != nil {
				// Update status condition to indicate node update failure
				if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
					"NodeUpdateError", fmt.Sprintf("Failed to update node %s with accelerator label: %v", node.Name, err)); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
				}
				return false, fmt.Errorf("failed to update node %s with accelerator label: %w", node.Name, err)
			}

			klog.InfoS("Successfully added accelerator label to node",
				"workspace", klog.KObj(wObj),
				"node", node.Name)
		}

		// Check if node has GPU capacity (nvidia.com/gpu resource should be > 0)
		gpuCapacity := node.Status.Capacity[resources.CapacityNvidiaGPU]
		if gpuCapacity.IsZero() {
			// Update status condition to indicate GPU capacity is not ready
			if updateErr := ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"GPUCapacityNotReady", fmt.Sprintf("Node %s has zero GPU capacity", node.Name)); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
			}

			klog.V(4).InfoS("Node has zero GPU capacity",
				"workspace", klog.KObj(wObj),
				"node", node.Name,
				"gpuCapacity", gpuCapacity.String())
			return false, nil
		}
	}

	klog.InfoS("All nodes have NVIDIA device plugins ready",
		"workspace", klog.KObj(wObj),
		"nodeCount", len(nodes))
	return true, nil
}

// getReadyNodesFromNodeClaims retrieves all ready nodes that are associated with NodeClaims for the workspace.
// This function excludes preferred nodes and only returns nodes that were provisioned through NodeClaims.
// It's primarily used for device plugin management where we need to ensure GPU nodes created by
// NodeClaims have the proper NVIDIA device plugins installed.
func (c *NodeResourceReconciler) getReadyNodesFromNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace) ([]*corev1.Node, error) {
	// First get the NodeClaims for this workspace
	nodeClaims, err := ctrlutils.GetExistingNodeClaims(ctx, c.Client, wObj)
	if err != nil {
		return nil, fmt.Errorf("failed to get NodeClaims: %w", err)
	}

	nodes := make([]*corev1.Node, 0, len(nodeClaims))

	// For each NodeClaim, get the corresponding Node
	for _, nodeClaim := range nodeClaims {
		if nodeClaim.Status.NodeName == "" {
			// NodeClaim doesn't have a node assigned yet
			continue
		}

		node := &corev1.Node{}
		nodeKey := client.ObjectKey{Name: nodeClaim.Status.NodeName}
		if err := c.Client.Get(ctx, nodeKey, node); err != nil {
			if apierrors.IsNotFound(err) {
				// Node doesn't exist yet, skip
				continue
			}
			return nil, fmt.Errorf("failed to get node %s: %w", nodeClaim.Status.NodeName, err)
		}

		// skip if node is not ready
		if !utils.IsNodeReady(node) {
			klog.V(4).InfoS("Node is not ready, skipping",
				"node", node.Name,
				"workspace", klog.KObj(wObj))
			continue
		}

		// skip if node instance type does not match workspace
		if node.Labels[corev1.LabelInstanceTypeStable] != wObj.Resource.InstanceType {
			klog.V(4).InfoS("Node instance type does not match workspace, skipping",
				"node", node.Name,
				"workspace", klog.KObj(wObj))
			continue
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// getReadyNodesMatchingLabelSelector retrieves all ready nodes that match the workspace's label selector.
// This function includes both NodeClaim-provisioned nodes and preferred nodes, providing a comprehensive
// view of all nodes available for the workspace. It uses the workspace.Resource.LabelSelector to filter
// nodes across the entire cluster and is used for workspace status updates to reflect all available nodes.
func (c *NodeResourceReconciler) getReadyNodesMatchingLabelSelector(ctx context.Context, wObj *kaitov1beta1.Workspace) ([]*corev1.Node, error) {
	// List all nodes in the cluster
	nodeList := &corev1.NodeList{}
	listOpts := []client.ListOption{}

	// If there's a label selector, add it to the list options
	if wObj.Resource.LabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(wObj.Resource.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to convert label selector: %w", err)
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}

	if err := c.Client.List(ctx, nodeList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Filter nodes that are ready
	readyNodes := make([]*corev1.Node, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		// Check if the node is ready
		if !utils.IsNodeReady(node) {
			klog.V(4).InfoS("Node is not ready, skipping",
				"node", node.Name,
				"workspace", klog.KObj(wObj))
			continue
		}

		readyNodes = append(readyNodes, node)
	}

	klog.V(4).InfoS("Found ready nodes matching workspace label selector",
		"workspace", klog.KObj(wObj),
		"readyNodeCount", len(readyNodes))

	return readyNodes, nil
}

// updateWorkspaceStatusIfNeeded updates the workspace status when WorkerNodes change or ResourceStatus condition is not true
func (c *NodeResourceReconciler) updateWorkspaceStatusIfNeeded(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	// Get all ready nodes that match the workspace's label selector
	nodes, err := c.getReadyNodesMatchingLabelSelector(ctx, wObj)
	if err != nil {
		klog.ErrorS(err, "failed to get ready nodes for workspace status update", "workspace", klog.KObj(wObj))
		return fmt.Errorf("failed to get ready nodes for workspace: %w", err)
	}

	// Extract node names from ready nodes
	readyNodeNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		readyNodeNames = append(readyNodeNames, node.Name)
	}

	// Check if WorkerNodes need to be updated
	readyNodeSet := sets.New(readyNodeNames...)
	currentWorkerNodeSet := sets.New(wObj.Status.WorkerNodes...)
	needsWorkerNodeUpdate := !readyNodeSet.Equal(currentWorkerNodeSet)

	// Check if ResourceStatus condition is not true
	needsResourceStatusUpdate := true
	if resourceCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus)); resourceCondition != nil {
		if resourceCondition.Status == metav1.ConditionTrue && resourceCondition.Reason == "ResourcesReady" {
			needsResourceStatusUpdate = false
		}
	}

	// If no updates are needed, return early
	if !needsWorkerNodeUpdate && !needsResourceStatusUpdate {
		return nil
	}

	// Perform both updates in a single request
	klog.InfoS("Updating workspace status",
		"workspace", klog.KObj(wObj),
		"updateWorkerNodes", needsWorkerNodeUpdate,
		"updateResourceStatus", needsResourceStatusUpdate,
		"previousNodes", wObj.Status.WorkerNodes,
		"newNodes", readyNodeNames)

	return c.updateWorkspaceStatusWithBothUpdates(ctx, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace},
		readyNodeNames, needsWorkerNodeUpdate, needsResourceStatusUpdate)
}

// updateWorkspaceStatusWithBothUpdates updates both WorkerNodes and ResourceStatus condition in a single request
func (c *NodeResourceReconciler) updateWorkspaceStatusWithBothUpdates(ctx context.Context, name *client.ObjectKey,
	workerNodes []string, updateWorkerNodes bool, updateResourceStatus bool) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			wObj := &kaitov1beta1.Workspace{}
			if err := c.Client.Get(ctx, *name, wObj); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}

			// Update WorkerNodes if needed
			if updateWorkerNodes {
				wObj.Status.WorkerNodes = workerNodes
			}

			// Update ResourceStatus condition if needed
			if updateResourceStatus {
				condition := metav1.Condition{
					Type:               string(kaitov1beta1.ConditionTypeResourceStatus),
					Status:             metav1.ConditionTrue,
					Reason:             "ResourcesReady",
					ObservedGeneration: wObj.GetGeneration(),
					Message:            "All resources are ready and nodes are available",
					LastTransitionTime: metav1.Now(),
				}
				meta.SetStatusCondition(&wObj.Status.Conditions, condition)
			}

			return c.Client.Status().Update(ctx, wObj)
		})
}

// resetResourceStatusConditionIfNeeded resets the ResourceStatus condition to false if it's currently true
// when NodeClaim condition is not ready
func (c *NodeResourceReconciler) resetResourceStatusConditionIfNeeded(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	// Check if ResourceStatus condition is currently true
	resourceCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
	if resourceCondition == nil || resourceCondition.Status != metav1.ConditionTrue {
		// Already false or doesn't exist, no need to update
		return nil
	}

	// Update ResourceStatus condition to false since NodeClaim condition is not ready
	klog.InfoS("Resetting ResourceStatus condition to false due to NodeClaim not ready",
		"workspace", klog.KObj(wObj))

	return ctrlutils.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
		"NodeClaimNotReady", "Waiting for NodeClaims to be ready before resources can be provisioned")
}

// SetupWithManager sets up the controller with the Manager.
func (c *NodeResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Client = mgr.GetClient()

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1beta1.Workspace{}).
		Watches(&karpenterv1.NodeClaim{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Get the workspace name from NodeClaim labels
				workspaceName, exists := obj.GetLabels()[kaitov1beta1.LabelWorkspaceName]
				if !exists {
					klog.V(4).InfoS("NodeClaim does not have workspace label, skipping", "nodeClaim", klog.KObj(obj))
					return nil
				}

				// Construct workspace key directly from labels
				workspaceKey := client.ObjectKey{
					Name:      workspaceName,
					Namespace: obj.GetNamespace(),
				}

				// Enqueue the workspace for reconciliation
				return []reconcile.Request{{NamespacedName: workspaceKey}}
			}),
			builder.WithPredicates(nodeclaim.NodeClaimPredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 50})

	return builder.Complete(c)
}
