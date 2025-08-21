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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
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

func (c *NodeManager) EnsureNodeResource(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim, workerNodes []string) (bool, error) {
	// ensure Nvidia device plugins are ready for the workspace when instance type is known.
	knownGPUConfig, _ := utils.GetGPUConfigBySKU(wObj.Resource.InstanceType)
	if knownGPUConfig != nil {
		readyNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(existingNodeClaims))
		for i := range existingNodeClaims {
			if nodeclaim.IsNodeClaimReadyNotDeleting(existingNodeClaims[i]) {
				readyNodeClaims = append(readyNodeClaims, existingNodeClaims[i])
			}
		}

		if isReady, err := c.ensureNodePlugin(ctx, wObj, readyNodeClaims); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"workspaceResourceStatusFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return isReady, updateErr
			}
			return isReady, err
		} else if !isReady {
			return isReady, nil
		}
	}

	// Add the ready nodes names to the WorkspaceStatus.WorkerNodes.
	sort.Strings(workerNodes)
	if !reflect.DeepEqual(wObj.Status.WorkerNodes, workerNodes) {
		if err := workspace.UpdateWorkspaceStatus(ctx, c.Client, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, func(status *kaitov1beta1.WorkspaceStatus) error {
			status.WorkerNodes = workerNodes
			return nil
		}); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"workspaceResourceStatusFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return false, updateErr
			}
			return false, err
		}
	}

	if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue,
		"workspaceResourceStatusSuccess", "workspace resource is ready"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return false, err
	}

	return true, nil
}

// ensureNodePlugin ensures that NVIDIA device plugins are ready on all nodes for the workspace
func (c *NodeManager) ensureNodePlugin(ctx context.Context, wObj *kaitov1beta1.Workspace, readyNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	nodes, err := c.getReadyNodesFromNodeClaims(ctx, wObj, readyNodeClaims)
	if err != nil {
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
			"NodeListError", fmt.Sprintf("Failed to get nodes for workspace: %v", err)); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
			return false, updateErr
		}
		return false, fmt.Errorf("failed to get nodes for workspace: %w", err)
	}

	// Check each node for NVIDIA accelerator label and GPU capacity
	for _, node := range nodes {
		if accelerator, exists := node.Labels[resources.LabelKeyNvidia]; !exists || accelerator != resources.LabelValueNvidia {
			if node.Labels == nil {
				node.Labels = make(map[string]string)
			}
			node.Labels[resources.LabelKeyNvidia] = resources.LabelValueNvidia

			if err := c.Client.Update(ctx, node); err != nil {
				if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
					"NodeUpdateError", fmt.Sprintf("Failed to update node %s with accelerator label: %v", node.Name, err)); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
				}
				return false, fmt.Errorf("failed to update node %s with accelerator label: %w", node.Name, err)
			}
		}

		gpuCapacity := node.Status.Capacity[resources.CapacityNvidiaGPU]
		if gpuCapacity.IsZero() {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"GPUCapacityNotReady", fmt.Sprintf("Node %s has zero GPU capacity", node.Name)); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update resource status condition", "workspace", klog.KObj(wObj))
				return false, fmt.Errorf("failed to update resource status condition: %w", updateErr)
			}

			return false, nil
		}
	}

	return true, nil
}

// getReadyNodesFromNodeClaims retrieves all ready nodes that are associated with NodeClaims for the workspace.
// This function excludes preferred nodes and only returns nodes that were provisioned through NodeClaims.
// It's primarily used for device plugin management where we need to ensure GPU nodes created by
// NodeClaims have the proper NVIDIA device plugins installed.
func (c *NodeManager) getReadyNodesFromNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, readyNodeClaims []*karpenterv1.NodeClaim) ([]*corev1.Node, error) {
	nodes := make([]*corev1.Node, 0, len(readyNodeClaims))
	for _, nodeClaim := range readyNodeClaims {
		if nodeClaim.Status.NodeName == "" {
			continue
		}

		node, err := resources.GetNode(ctx, nodeClaim.Status.NodeName, c.Client)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("failed to get node %s: %w", nodeClaim.Status.NodeName, err)
		}

		if !resources.NodeIsReadyAndNotDeleting(node) {
			klog.V(4).InfoS("Node is not ready, skipping",
				"node", node.Name,
				"workspace", klog.KObj(wObj))
			continue
		}

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
