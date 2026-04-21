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

package karpenter

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

// NodeClassConfig holds cloud-specific NodeClass reference info injected at
// construction time, making the provisioner cloud-agnostic.
type NodeClassConfig struct {
	Group            string            // e.g. "karpenter.azure.com"
	Kind             string            // e.g. "AKSNodeClass"
	DefaultName      string            // default NodeClass name (e.g. "image-family-ubuntu")
	ImageFamilyNames map[string]string // image family annotation value → NodeClass resource name
}

// KarpenterProvisioner implements NodeProvisioner using the cloud-agnostic
// Karpenter API (NodePool / NodeClaim). Cloud-specific details (NodeClass
// group, kind, name mapping) are provided via NodeClassConfig.
type KarpenterProvisioner struct {
	client          client.Client
	nodeClassConfig NodeClassConfig
}

var _ nodeprovision.NodeProvisioner = (*KarpenterProvisioner)(nil)

// NewKarpenterProvisioner creates a new KarpenterProvisioner.
func NewKarpenterProvisioner(c client.Client, cfg NodeClassConfig) *KarpenterProvisioner {
	return &KarpenterProvisioner{client: c, nodeClassConfig: cfg}
}

// Name returns the provisioner name.
func (p *KarpenterProvisioner) Name() string { return "KarpenterProvisioner" }

// Start is a no-op. The installer/Helm chart is responsible for ensuring
// NodeClass resources exist before KAITO starts.
func (p *KarpenterProvisioner) Start(_ context.Context) error { return nil }

// ProvisionNodes creates a NodePool for the Workspace. Idempotent — AlreadyExists is ignored.
func (p *KarpenterProvisioner) ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	np := generateNodePool(ws, p.nodeClassConfig)
	if err := p.client.Create(ctx, np); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating NodePool %q: %w", np.Name, err)
	}
	return nil
}

// DeleteNodes deletes the NodePool for the Workspace. Idempotent — NotFound is ignored.
// Karpenter cascades deletion: NodePool → NodeClaim → Node → VM.
func (p *KarpenterProvisioner) DeleteNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	nodePoolName := NodePoolName(ws.Namespace, ws.Name)
	np := &karpenterv1.NodePool{}
	np.Name = nodePoolName

	if err := p.client.Delete(ctx, np); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("deleting NodePool %q: %w", nodePoolName, err)
	}
	return nil
}

// EnsureNodesReady returns (true, false, nil) when all expected NodeClaims for
// the Workspace are present and in Ready state (not being deleted).
// When not ready, needRequeue is false because NodeClaim watch events trigger
// reconciliation — no polling requeue is needed.
func (p *KarpenterProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (bool, bool, error) {
	nodePoolName := NodePoolName(ws.Namespace, ws.Name)

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := p.client.List(ctx, nodeClaimList,
		client.MatchingLabels{karpenterv1.NodePoolLabelKey: nodePoolName},
	); err != nil {
		return false, false, fmt.Errorf("listing NodeClaims for NodePool %q: %w", nodePoolName, err)
	}

	if int32(len(nodeClaimList.Items)) < ws.Status.TargetNodeCount {
		return false, false, nil
	}

	for i := range nodeClaimList.Items {
		if !nodeclaim.IsNodeClaimReadyNotDeleting(&nodeClaimList.Items[i]) {
			return false, false, nil
		}
	}

	return true, false, nil
}

// EnableDriftRemediation sets the Drifted budget to "1", allowing karpenter to replace drifted nodes.
func (p *KarpenterProvisioner) EnableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return p.setDriftBudget(ctx, workspaceNamespace, workspaceName, "1")
}

// DisableDriftRemediation sets the Drifted budget to "0", blocking karpenter from replacing drifted nodes.
func (p *KarpenterProvisioner) DisableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return p.setDriftBudget(ctx, workspaceNamespace, workspaceName, "0")
}

// setDriftBudget updates the Drifted budget entry in the NodePool.
// Uses RetryOnConflict with Get+Update inside the retry closure for optimistic concurrency.
func (p *KarpenterProvisioner) setDriftBudget(ctx context.Context, workspaceNamespace, workspaceName, nodes string) error {
	nodePoolName := NodePoolName(workspaceNamespace, workspaceName)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		np := &karpenterv1.NodePool{}
		if err := p.client.Get(ctx, types.NamespacedName{Name: nodePoolName}, np); err != nil {
			return err
		}

		found := false
		for i := range np.Spec.Disruption.Budgets {
			for _, reason := range np.Spec.Disruption.Budgets[i].Reasons {
				if reason == karpenterv1.DisruptionReasonDrifted {
					np.Spec.Disruption.Budgets[i].Nodes = nodes
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return fmt.Errorf("NodePool %q has no budget entry with Drifted reason", nodePoolName)
		}

		return p.client.Update(ctx, np)
	})
}

// CollectNodeStatusInfo gathers status conditions for workspace status.
// For karpenter, we check NodeClaim readiness and derive NodeStatus and
// ResourceStatus from the same data.
func (p *KarpenterProvisioner) CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	nodePoolName := NodePoolName(ws.Namespace, ws.Name)

	nodeClaimCond := metav1.Condition{
		Type:    string(kaitov1beta1.ConditionTypeNodeClaimStatus),
		Status:  metav1.ConditionFalse,
		Reason:  "NodeClaimNotReady",
		Message: "Ready NodeClaims are not enough",
	}
	nodeCond := metav1.Condition{
		Type:    string(kaitov1beta1.ConditionTypeNodeStatus),
		Status:  metav1.ConditionFalse,
		Reason:  "NodeNotReady",
		Message: "Not enough Nodes are ready",
	}
	resourceCond := metav1.Condition{
		Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
		Status:  metav1.ConditionFalse,
		Reason:  "workspaceResourceStatusNotReady",
		Message: "node claim or node status condition not ready",
	}

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := p.client.List(ctx, nodeClaimList,
		client.MatchingLabels{karpenterv1.NodePoolLabelKey: nodePoolName},
	); err != nil {
		return nil, fmt.Errorf("listing NodeClaims for NodePool %q: %w", nodePoolName, err)
	}

	// Count ready NodeClaims.
	readyCount := 0
	for i := range nodeClaimList.Items {
		if nodeclaim.IsNodeClaimReadyNotDeleting(&nodeClaimList.Items[i]) {
			readyCount++
		}
	}

	targetCount := int(ws.Status.TargetNodeCount)
	if readyCount >= targetCount {
		nodeClaimCond.Status = metav1.ConditionTrue
		nodeClaimCond.Reason = "NodeClaimsReady"
		nodeClaimCond.Message = "Enough NodeClaims are ready"

		// For karpenter, NodeClaim readiness implies node readiness because
		// karpenter only marks a NodeClaim Ready after the underlying node
		// is registered and ready.
		nodeCond.Status = metav1.ConditionTrue
		nodeCond.Reason = "NodesReady"
		nodeCond.Message = "Enough Nodes are ready"
	}

	// Derive resource condition.
	if nodeCond.Status == metav1.ConditionTrue && nodeClaimCond.Status == metav1.ConditionTrue {
		resourceCond.Status = metav1.ConditionTrue
		resourceCond.Reason = "workspaceResourceStatusSuccess"
		resourceCond.Message = "workspace resource is ready"
	} else if nodeClaimCond.Status != metav1.ConditionTrue {
		resourceCond.Reason = nodeClaimCond.Reason
		resourceCond.Message = nodeClaimCond.Message
	} else {
		resourceCond.Reason = nodeCond.Reason
		resourceCond.Message = nodeCond.Message
	}

	return []metav1.Condition{nodeCond, nodeClaimCond, resourceCond}, nil
}
