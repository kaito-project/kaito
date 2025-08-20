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

package utils

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// GetBringYourOwnNodes finds all BYO nodes that match the workspace's label selector
func GetBringYourOwnNodes(ctx context.Context, c client.Client, wObj *kaitov1beta1.Workspace) ([]*corev1.Node, error) {
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

	if err := c.List(ctx, nodeList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Create a set of preferred node names for fast lookup
	preferredNodeSet := sets.New(wObj.Resource.PreferredNodes...)

	// Filter nodes that are in the preferred nodes list and are ready
	availableBYONodes := make([]*corev1.Node, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		node := &nodeList.Items[i]

		// if node provision is disabled, preferred nodes will be ignored.
		if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
			// Check if this node is in the preferred nodes list
			if !preferredNodeSet.Has(node.Name) {
				continue
			}
		}

		// Check if the node is ready
		if !utils.IsNodeReady(node) {
			klog.V(4).InfoS("BYO node is not ready, skipping",
				"node", node.Name,
				"workspace", klog.KObj(wObj))
			continue
		}

		availableBYONodes = append(availableBYONodes, node)
	}

	klog.V(4).InfoS("Found available BYO nodes",
		"workspace", klog.KObj(wObj),
		"preferredNodesSpecified", len(wObj.Resource.PreferredNodes),
		"availableBYONodes", len(availableBYONodes))

	return availableBYONodes, nil
}

// getExistingNodeClaims retrieves all NodeClaims associated with the given workspace
func GetExistingNodeClaims(ctx context.Context, c client.Reader, wObj *kaitov1beta1.Workspace) ([]*karpenterv1.NodeClaim, error) {
	nodeClaimList := &karpenterv1.NodeClaimList{}

	// List NodeClaims with labels that match this workspace
	listOpts := []client.ListOption{
		client.InNamespace(wObj.Namespace),
		client.MatchingLabels{
			kaitov1beta1.LabelWorkspaceName: wObj.Name,
		},
	}

	if err := c.List(ctx, nodeClaimList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	// Convert to slice of pointers for easier manipulation
	nodeClaims := make([]*karpenterv1.NodeClaim, 0, len(nodeClaimList.Items))
	for i := range nodeClaimList.Items {
		nodeClaims = append(nodeClaims, &nodeClaimList.Items[i])
	}

	return nodeClaims, nil
}

func GetRequiredNodeClaimsCount(ctx context.Context, c client.Client, wObj *kaitov1beta1.Workspace) (int, error) {
	// if node provision is disabled, NodeClaims are not needed.
	if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		return 0, nil
	}
	// Find available preferred nodes
	availablePreferredNodes, err := GetBringYourOwnNodes(ctx, c, wObj)
	if err != nil {
		return 0, fmt.Errorf("failed to get available preferred nodes: %w", err)
	}

	// Configure targetNodeCount to 1 for non-inference workloads like tuning job.
	targetNodeCount := 1
	if wObj.Inference != nil && wObj.Status.Inference != nil {
		targetNodeCount = int(wObj.Status.Inference.TargetNodeCount)
	}

	// Calculate the number of NodeClaims needed (target - preferred nodes)
	return max(0, targetNodeCount-len(availablePreferredNodes)), nil
}

// UpdateStatusConditionIfNotMatch updates the workspace status condition if it doesn't match the current values
func UpdateStatusConditionIfNotMatch(ctx context.Context, c client.Client, wObj *kaitov1beta1.Workspace, cType kaitov1beta1.ConditionType,
	cStatus metav1.ConditionStatus, cReason, cMessage string) error {
	if curCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(cType)); curCondition != nil {
		if curCondition.Status == cStatus && curCondition.Reason == cReason && curCondition.Message == cMessage {
			// Nothing to change
			return nil
		}
	}
	klog.InfoS("updateStatusCondition", "workspace", klog.KObj(wObj), "conditionType", cType, "status", cStatus, "reason", cReason, "message", cMessage)
	cObj := metav1.Condition{
		Type:               string(cType),
		Status:             cStatus,
		Reason:             cReason,
		ObservedGeneration: wObj.GetGeneration(),
		Message:            cMessage,
		LastTransitionTime: metav1.Now(),
	}
	return UpdateWorkspaceStatus(ctx, c, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, &cObj)
}

// UpdateWorkspaceStatus updates the workspace status with the provided condition
func UpdateWorkspaceStatus(ctx context.Context, c client.Client, name *client.ObjectKey, condition *metav1.Condition) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			wObj := &kaitov1beta1.Workspace{}
			if err := c.Get(ctx, *name, wObj); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			if condition != nil {
				meta.SetStatusCondition(&wObj.Status.Conditions, *condition)
			}
			return c.Status().Update(ctx, wObj)
		})
}

// scaleDeploymentIfNeeded checks if the Deployment specified by key and scales it to match
// the workspace's target node count if necessary.
// Returns true if scaling was performed, false otherwise.
func ScaleDeploymentIfNeeded(ctx context.Context, c client.Client, key client.ObjectKey, workspace *kaitov1beta1.Workspace) bool {
	// Check if workspace has inference status with target node count
	if workspace.Status.Inference == nil {
		return false
	}

	// Check if the deployment already exists
	existingDeployment := &appsv1.Deployment{}
	err := c.Get(ctx, key, existingDeployment)
	if err != nil {
		// Deployment doesn't exist, no scaling needed
		return false
	}

	targetReplicas := workspace.Status.Inference.TargetNodeCount
	currentReplicas := *existingDeployment.Spec.Replicas

	// If replicas match, no scaling needed
	if currentReplicas == targetReplicas {
		return false
	}

	// Scale the deployment to match target node count
	existingDeployment.Spec.Replicas = &targetReplicas
	err = c.Update(ctx, existingDeployment)
	if err != nil {
		klog.ErrorS(err, "failed to scale deployment", "deployment", klog.KObj(existingDeployment),
			"currentReplicas", currentReplicas, "targetReplicas", targetReplicas)
		return false
	}

	klog.InfoS("Successfully scaled deployment", "deployment", klog.KObj(existingDeployment),
		"fromReplicas", currentReplicas, "toReplicas", targetReplicas)
	return true
}
