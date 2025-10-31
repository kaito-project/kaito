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
	"reflect"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
)

type NodeManager struct {
	client.Client
}

func NewNodeManager(c client.Client) *NodeManager {
	return &NodeManager{
		Client: c,
	}
}

// CheckIfNodePluginsReady is used for ensuring node label(accelerator:nvidia) and GPU capacity on all auto-provisioned nodes for the workspace.
// It also updates the Node status condition accordingly.
func (c *NodeManager) CheckIfNodePluginsReady(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	// ensure Nvidia device plugins are ready for the workspace when instance type is known.
	knownGPUConfig, _ := utils.GetGPUConfigBySKU(wObj.Resource.InstanceType)
	if knownGPUConfig != nil {
		if areReady, err := c.checkNodePlugin(ctx, wObj, existingNodeClaims); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeStatus, metav1.ConditionFalse,
				"NodePluginsNotReady", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return false, updateErr
			}
			return false, err
		} else if !areReady {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeStatus, metav1.ConditionFalse,
				"NodePluginsNotReady", "waiting all node plugins to be ready"); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return false, updateErr
			}
			return false, nil
		}
	}

	return true, nil
}

// checkNodePlugin ensures that NVIDIA device plugins are ready on all nodes for the workspace
func (c *NodeManager) checkNodePlugin(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	nodes, err := c.getReadyNodesFromNodeClaims(ctx, wObj, existingNodeClaims)
	if err != nil {
		return false, fmt.Errorf("failed to get ready nodes from nodeClaims: %w", err)
	} else if len(nodes) != len(existingNodeClaims) {
		klog.Infof("node plugins not ready, # nodes (%d) is not equal to # nodeClaims (%d) for workspace %s/%s", len(nodes), len(existingNodeClaims), wObj.Namespace, wObj.Name)
		return false, nil
	}

	// Check each node for NVIDIA accelerator label and GPU capacity
	for _, node := range nodes {
		if accelerator, exists := node.Labels[resources.LabelKeyNvidia]; !exists || accelerator != resources.LabelValueNvidia {
			if node.Labels == nil {
				node.Labels = make(map[string]string)
			}
			node.Labels[resources.LabelKeyNvidia] = resources.LabelValueNvidia

			if err := c.Client.Update(ctx, node); err != nil {
				return false, fmt.Errorf("failed to update node %s with accelerator label: %w", node.Name, err)
			}
		}

		gpuCapacity := node.Status.Capacity[resources.CapacityNvidiaGPU]
		if gpuCapacity.IsZero() {
			klog.Infof("node plugins not ready, %s does not have GPU capacity for workspace %s/%s", node.Name, wObj.Namespace, wObj.Name)
			return false, nil
		}

		if node.Labels[corev1.LabelInstanceTypeStable] != wObj.Resource.InstanceType {
			klog.Infof("node plugins not ready, %s instance type label %s does not match workspace instance type %s", node.Name, node.Labels[corev1.LabelInstanceTypeStable], wObj.Resource.InstanceType)
			return false, nil
		}
	}

	klog.Infof("all node plugins are ready for workspace %s/%s", wObj.Namespace, wObj.Name)

	return true, nil
}

// getReadyNodesFromNodeClaims retrieves all ready nodes that are associated with NodeClaims for the workspace.
// This function excludes preferred nodes and only returns nodes that were provisioned through NodeClaims.
// It's primarily used for device plugin management where we need to ensure GPU nodes created by
// NodeClaims have the proper NVIDIA device plugins installed.
func (c *NodeManager) getReadyNodesFromNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) ([]*corev1.Node, error) {
	nodes := make([]*corev1.Node, 0, len(existingNodeClaims))
	for _, nodeClaim := range existingNodeClaims {
		if nodeClaim.Status.NodeName == "" {
			return nodes, nil
		}

		node, err := resources.GetNode(ctx, nodeClaim.Status.NodeName, c.Client)
		if err != nil {
			klog.Errorf("Failed to get node %s for nodeClaim %s: %v", nodeClaim.Status.NodeName, nodeClaim.Name, err)
			return nil, fmt.Errorf("failed to get node %s: %w", nodeClaim.Status.NodeName, err)
		}

		if !resources.NodeIsReadyAndNotDeleting(node) {
			return nodes, nil
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// EnsureNodesReady is used for checking the number of ready nodes meet the target node count. Updates the status condition accordingly.
func (c *NodeManager) EnsureNodesReady(ctx context.Context, wObj *kaitov1beta1.Workspace, matchingNodes []*corev1.Node, nodeClaims []*karpenterv1.NodeClaim) (ready bool, retErr error) {
	targetNodeCount := int(wObj.Status.TargetNodeCount)
	readyCount := 0

	for _, node := range matchingNodes {
		if resources.NodeIsReadyAndNotDeleting(node) {
			readyCount++
		}

		if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
			// If NAP is enabled, ensure the nodes have the instance type label set correctly and that it matches the workspace instance type.
			instanceType, ok := node.Labels[corev1.LabelInstanceTypeStable]
			var message string
			if !ok {
				message = fmt.Sprintf("Node %s is missing required instance type label", node.Name)
			} else if instanceType != wObj.Resource.InstanceType {
				message = fmt.Sprintf("Node %s instance type label %s does not match workspace instance type %s", node.Name, instanceType, wObj.Resource.InstanceType)
			}

			if message != "" {
				if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeStatus, metav1.ConditionFalse,
					"NodeNotReady", message); updateErr != nil {
					return false, fmt.Errorf("failed to update Node status condition(NodeNotReady): %w", updateErr)
				}
			}
		}
	}

	defer func() {
		// Update the list of nodes, but don't block setting the condition on it.
		if err := c.UpdateWorkerNodesInStatus(ctx, wObj, matchingNodes); err != nil {
			klog.Error("failed to update worker nodes in workspace status", "workspace", klog.KObj(wObj), "error", err)
			retErr = err
		}
	}()

	if readyCount >= targetNodeCount { // Enough nodes are ready.
		// If NAP is enabled, ensure node plugins are ready.
		if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
			ready, err := c.CheckIfNodePluginsReady(ctx, wObj, nodeClaims)
			if err != nil {
				return false, fmt.Errorf("failed to check node plugin readiness: %w", err)
			}
			if !ready {
				return false, nil
			}
		}

		// Enough Nodes are ready, mark condition as true.
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeStatus, metav1.ConditionTrue,
			"NodesReady", fmt.Sprintf("Enough Nodes are ready (TargetNodes: %d, CurrentReadyNodes: %d, SelectedNodes: %d)", targetNodeCount, readyCount, len(matchingNodes))); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update Node status condition NodesReady to true", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update Node status condition(NodesReady): %w", updateErr)
		}
		return true, nil
	} else {
		klog.InfoS("Not enough Nodes are ready for workspace", "workspace", client.ObjectKeyFromObject(wObj).String(),
			"targetNodes", targetNodeCount, "currentReadyNodes", readyCount)
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeNodeStatus, metav1.ConditionFalse,
			"NodeNotReady", fmt.Sprintf("Not enough Nodes are ready (TargetNodes: %d, CurrentReadyNodes: %d, SelectedNodes: %d)", targetNodeCount, readyCount, len(matchingNodes))); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update Node status condition NodesReady to false", "workspace", klog.KObj(wObj))
			return false, fmt.Errorf("failed to update Node status condition(NodeNotReady): %w", updateErr)
		}

		return false, nil
	}
}

// VerifyOwnedConditions checks the owned conditions, and if any one is false, it sets the condition to false with the reason and message of the first false condition found.
func (c *NodeManager) VerifyOwnedConditions(ctx context.Context, wObj *kaitov1beta1.Workspace, condition kaitov1beta1.ConditionType, conditionTypes []kaitov1beta1.ConditionType) (bool, error) {
	for _, cType := range conditionTypes {
		ownedCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(cType))
		if ownedCondition == nil {
			continue // Condition not found, skip to the next one
		} else if ownedCondition.Status != metav1.ConditionTrue {
			// Set the owned condition to false with the reason and message of the first false condition found.
			if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, condition, metav1.ConditionFalse, ownedCondition.Reason, ownedCondition.Message); err != nil {
				klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return false, err
			}
			return false, nil
		}
	}

	return true, nil
}

// SetResourceReadyConditionByStatus updates the status of the resource ready condition based on the statuses of owned conditions.
func (c *NodeManager) SetResourceReadyConditionByStatus(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	ownedConditions := []kaitov1beta1.ConditionType{
		kaitov1beta1.ConditionTypeNodeClaimStatus,
		kaitov1beta1.ConditionTypeNodeStatus,
	}

	// If any owned condition is false, set the resource condition to false and return.
	if conditionTrue, err := c.VerifyOwnedConditions(ctx, wObj, kaitov1beta1.ConditionTypeResourceStatus, ownedConditions); err != nil {
		return err
	} else if conditionTrue {
		// All owned conditions are true, set the resource condition to true.
		if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue,
			"workspaceResourceStatusSuccess", "workspace resource is ready"); err != nil {
			klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return err
		}
	}

	return nil
}

// UpdateWorkerNodesInStatus updates the worker nodes list in workspace status.
func (c *NodeManager) UpdateWorkerNodesInStatus(ctx context.Context, wObj *kaitov1beta1.Workspace, readyNodes []*corev1.Node) error {
	nodeNames := make([]string, 0, len(readyNodes))
	for _, node := range readyNodes {
		nodeNames = append(nodeNames, node.Name)
	}
	sort.Strings(nodeNames)

	if !reflect.DeepEqual(wObj.Status.WorkerNodes, nodeNames) {
		if err := workspace.UpdateWorkspaceStatus(ctx, c.Client, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, func(status *kaitov1beta1.WorkspaceStatus) error {
			status.WorkerNodes = nodeNames
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}
