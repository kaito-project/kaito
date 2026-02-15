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
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
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
func (c *NodeManager) CheckIfNodePluginsReady(ctx context.Context, wObj *kaitov1beta1.Workspace, existingNodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	// ensure Nvidia device plugins are ready for the workspace when instance type is known.
	knownGPUConfig, _ := utils.GetGPUConfigBySKU(wObj.Resource.InstanceType)
	if knownGPUConfig != nil {
		if areReady, err := c.checkNodePlugin(ctx, wObj, existingNodeClaims); err != nil {
			return false, err
		} else if !areReady {
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

// EnsureNodesReady is used for checking the number of ready nodes meet the target node count.
func (c *NodeManager) EnsureNodesReady(ctx context.Context, wObj *kaitov1beta1.Workspace, matchingNodes []*corev1.Node, nodeClaims []*karpenterv1.NodeClaim) (bool, error) {
	targetNodeCount := int(wObj.Status.TargetNodeCount)
	readyCount := 0

	for _, node := range matchingNodes {
		if resources.NodeIsReadyAndNotDeleting(node) {
			if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
				// If NAP is enabled, ensure the nodes have the instance type label set correctly and that it matches the workspace instance type.
				instanceType, ok := node.Labels[corev1.LabelInstanceTypeStable]
				if ok && instanceType == wObj.Resource.InstanceType {
					readyCount++
				}
			} else {
				readyCount++
			}
		}
	}

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

		return true, nil
	} else {
		klog.InfoS("Not enough Nodes are ready for workspace", "workspace", client.ObjectKeyFromObject(wObj).String(),
			"targetNodes", targetNodeCount, "currentReadyNodes", readyCount)
		return false, nil
	}
}
