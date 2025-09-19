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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
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

// AreNodePluginsReady is used for ensuring node label(accelerator:nvidia) and GPU capacity on all auto-provisioned nodes for the workspace.
func (c *NodeManager) AreNodePluginsReady(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	// ensure Nvidia device plugins are ready for the workspace when instance type is known.
	knownGPUConfig, _ := utils.GetGPUConfigBySKU(wObj.Resource.InstanceType)
	if knownGPUConfig != nil {
		if areReady, err := c.checkNodePlugin(ctx, wObj, existingNodeClaims); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"nodePluginIsNotReady", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return false, updateErr
			}
			return false, err
		} else if !areReady {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"nodePluginIsNotReady", "waiting all node plugins to be ready"); updateErr != nil {
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
			return false, nil
		}

		if node.Labels[corev1.LabelInstanceTypeStable] != wObj.Resource.InstanceType {
			return false, nil
		}
	}

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

// UpdateWorkerNodesInStatus updates the worker nodes list in workspace status.
func (c *NodeManager) UpdateWorkerNodesInStatus(ctx context.Context, wObj *kaitov1beta1.Workspace, workerNodes []string) error {
	sort.Strings(workerNodes)
	if !reflect.DeepEqual(wObj.Status.WorkerNodes, workerNodes) {
		if err := workspace.UpdateWorkspaceStatus(ctx, c.Client, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, func(status *kaitov1beta1.WorkspaceStatus) error {
			status.WorkerNodes = workerNodes
			return nil
		}); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
				"workspaceResourceStatusFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return updateErr
			}
			return err
		}
	}

	if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue,
		"workspaceResourceStatusSuccess", "workspace resource is ready"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}

	return nil
}
