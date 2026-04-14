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

package noprovisioner

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/resources"
)

// NopProvisioner is a no-op NodeProvisioner for BYO (Bring Your Own) node
// scenarios where node auto-provisioning is disabled. ProvisionNodes and
// DeprovisionNodes are no-ops. EnsureNodesReady only checks that enough
// matching Nodes are ready (no instance type validation, no GPU plugin checks).
type NopProvisioner struct {
	client client.Client
}

var _ nodeprovision.NodeProvisioner = (*NopProvisioner)(nil)

func NewNopProvisioner(c client.Client) *NopProvisioner {
	return &NopProvisioner{client: c}
}

// Name returns the provisioner name.
func (n *NopProvisioner) Name() string { return "NopProvisioner" }

func (n *NopProvisioner) ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	return nil
}

func (n *NopProvisioner) DeprovisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	return nil
}

func (n *NopProvisioner) EnableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

func (n *NopProvisioner) DisableDrift(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

// EnsureNodesReady checks that enough matching Nodes are ready for the
// Workspace. In BYO mode there are no provisioning resources, so this
// never returns ProvisioningNotReady — only NodesReady or NodesNotReady.
func (n *NopProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (nodeprovision.NodeReadiness, error) {
	var matchLabels client.MatchingLabels
	if ws.Resource.LabelSelector != nil {
		matchLabels = ws.Resource.LabelSelector.MatchLabels
	}

	nodeList, err := resources.ListNodes(ctx, n.client, matchLabels)
	if err != nil {
		return nodeprovision.NodesNotReady, err
	}

	targetNodeCount := int(ws.Status.TargetNodeCount)
	readyCount := 0
	for i := range nodeList.Items {
		if resources.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
			readyCount++
		}
	}

	if readyCount >= targetNodeCount {
		return nodeprovision.NodesReady, nil
	}

	klog.InfoS("Not enough Nodes are ready for workspace (BYO mode)",
		"workspace", client.ObjectKeyFromObject(ws).String(),
		"targetNodes", targetNodeCount, "currentReadyNodes", readyCount)
	return nodeprovision.NodesNotReady, nil
}

// CollectNodeStatusInfo gathers status conditions for workspace status.
// In BYO mode, no NodeClaimStatus condition is returned.
func (n *NopProvisioner) CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	nodeCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeNodeStatus), Status: metav1.ConditionFalse,
		Reason: "NodeNotReady", Message: "Not enough Nodes are ready",
	}
	resourceCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeResourceStatus), Status: metav1.ConditionFalse,
		Reason: "workspaceResourceStatusNotReady", Message: "node status condition not ready",
	}

	var matchLabels client.MatchingLabels
	if ws.Resource.LabelSelector != nil {
		matchLabels = ws.Resource.LabelSelector.MatchLabels
	}
	nodeList, err := resources.ListNodes(ctx, n.client, matchLabels)
	if err != nil {
		return nil, err
	}
	readyCount := 0
	for i := range nodeList.Items {
		if resources.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
			readyCount++
		}
	}
	if readyCount >= int(ws.Status.TargetNodeCount) {
		nodeCond.Status = metav1.ConditionTrue
		nodeCond.Reason = "NodesReady"
		nodeCond.Message = "Enough Nodes are ready"
		resourceCond.Status = metav1.ConditionTrue
		resourceCond.Reason = "workspaceResourceStatusSuccess"
		resourceCond.Message = "workspace resource is ready"
	}

	// BYO mode: no NodeClaimStatus condition.
	return []metav1.Condition{nodeCond, resourceCond}, nil
}
