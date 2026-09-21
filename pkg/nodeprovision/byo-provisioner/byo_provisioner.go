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

package byoprovisioner

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
)

// BYOProvisioner is a no-op NodeProvisioner for BYO (Bring Your Own) node
// scenarios where node auto-provisioning is disabled. ProvisionNodes and
// DeleteNodes are no-ops. EnsureNodesReady only checks that enough
// matching Nodes are ready (no instance type validation, no GPU plugin checks).
type BYOProvisioner struct {
	client client.Client
}

var _ nodeprovision.NodeProvisioner = (*BYOProvisioner)(nil)

func NewBYOProvisioner(c client.Client) *BYOProvisioner {
	return &BYOProvisioner{client: c}
}

// Name returns the provisioner name.
func (n *BYOProvisioner) Name() string { return "BYOProvisioner" }

// Start is a no-op for BYOProvisioner.
func (n *BYOProvisioner) Start(ctx context.Context) error { return nil }

func (n *BYOProvisioner) ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	return nil
}

func (n *BYOProvisioner) DeleteNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	return nil
}

func (n *BYOProvisioner) EnableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

func (n *BYOProvisioner) DisableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return nil
}

// EnsureNodesReady checks that enough matching Nodes are ready for the
// Workspace. In BYO mode there are no provisioning resources, so needRequeue
// is always true when nodes are not ready.
func (n *BYOProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (bool, bool, error) {
	nodeList, err := nodeprovision.ListWorkspaceNodes(ctx, n.client, n, ws)
	if err != nil {
		return false, true, err
	}

	targetNodeCount := int(ws.Status.TargetNodeCount)
	readyCount := 0
	for i := range nodeList.Items {
		if nodes.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
			readyCount++
		}
	}

	if readyCount >= targetNodeCount {
		return true, false, nil
	}

	klog.InfoS("Not enough Nodes are ready for workspace (BYO mode)",
		"workspace", client.ObjectKeyFromObject(ws).String(),
		"targetNodes", targetNodeCount, "currentReadyNodes", readyCount)
	return false, true, nil
}

// CollectNodeStatusInfo gathers status conditions for workspace status.
// In BYO mode, no NodeClaimStatus condition is returned.
func (n *BYOProvisioner) CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	nodeCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeNodeStatus), Status: metav1.ConditionFalse,
		Reason: "NodeNotReady", Message: "Not enough Nodes are ready",
	}
	resourceCond := metav1.Condition{
		Type: string(kaitov1beta1.ConditionTypeResourceStatus), Status: metav1.ConditionFalse,
		Reason: "workspaceResourceStatusNotReady", Message: "node status condition not ready",
	}

	nodeList, err := nodeprovision.ListWorkspaceNodes(ctx, n.client, n, ws)
	if err != nil {
		return nil, err
	}
	readyCount := 0
	for i := range nodeList.Items {
		if nodes.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
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
	} else if readyCount == 0 {
		// An explicit instance type remains a hard filter with no fallback. A missing
		// match is reported until a suitable node becomes available.
		msg := "no ready GPU nodes match the workspace"
		if it, itErr := n.effectiveInstanceType(ctx, ws); itErr != nil {
			klog.ErrorS(itErr, "failed to resolve effective instance type for status message",
				"workspace", client.ObjectKeyFromObject(ws).String())
		} else if it != "" {
			msg = fmt.Sprintf("no ready GPU nodes of instance type %q match the workspace", it)
		}
		resourceCond.Reason = "NoAvailableGPUNodes"
		resourceCond.Message = msg
		nodeCond.Reason = "NoAvailableGPUNodes"
		nodeCond.Message = msg
	}

	// Enrich NodesReady with a node-pressure warning (diagnostic only; status unchanged).
	if w := nodes.NodePressureWarning(nodeList); w != "" {
		nodeCond.Message = nodeCond.Message + "; warning: " + w
	}

	// BYO mode: no NodeClaimStatus condition.
	return []metav1.Condition{nodeCond, resourceCond}, nil
}

// EffectiveInstanceType resolves the live GPU SKU filter for a BYO workspace.
//
// The labelSelector x instanceType matrix is:
//   - instanceType set: use it as a hard filter, even when no matching node exists
//   - instanceType unset, labelSelector nil: select the largest GPU-memory SKU
//     among healthy nodes; return empty when no usable GPU node exists
//   - instanceType unset, labelSelector set: add no SKU filter and preserve the
//     label selector's legacy placement behavior
//
// Auto-selection is recomputed from current nodes and is not persisted.
func EffectiveInstanceType(ctx context.Context, c client.Client, ws *kaitov1beta1.Workspace) (string, error) {
	if ws.Resource.InstanceType != "" {
		return ws.Resource.InstanceType, nil
	}
	if ws.Resource.LabelSelector == nil {
		return SelectInstanceType(ctx, c, ws)
	}
	return "", nil
}

func (n *BYOProvisioner) effectiveInstanceType(ctx context.Context, ws *kaitov1beta1.Workspace) (string, error) {
	return EffectiveInstanceType(ctx, n.client, ws)
}

// nodeHasAllocatableGPU reports registered schedulable GPU capacity, not currently
// free GPUs.
func nodeHasAllocatableGPU(node *corev1.Node) bool {
	if node.Status.Allocatable == nil {
		return false
	}
	return !node.Status.Allocatable.Name(nodes.CapacityNvidiaGPU, "").IsZero()
}

// SelectInstanceType returns the SKU with the most per-node GPU memory among ready,
// non-deleting nodes with allocatable GPUs and usable labels. Ties are resolved by
// instance type for deterministic selection; no usable node returns an empty string.
func SelectInstanceType(ctx context.Context, c client.Client, ws *kaitov1beta1.Workspace) (string, error) {
	nodeList := &corev1.NodeList{}
	if err := c.List(ctx, nodeList); err != nil {
		return "", fmt.Errorf("failed to list nodes for instance type selection: %w", err)
	}

	type skuGroup struct {
		instanceType string
		gpuMem       resource.Quantity
	}
	groups := map[string]*skuGroup{}
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !nodes.NodeIsReadyAndNotDeleting(node) {
			continue
		}
		if !nodeHasAllocatableGPU(node) {
			continue
		}
		instanceType := node.Labels[corev1.LabelInstanceTypeStable]
		if instanceType == "" {
			continue
		}
		if _, ok := groups[instanceType]; ok {
			continue
		}
		gpuConfig, err := sku.GetGPUConfigFromNodeLabels(node)
		if err != nil {
			continue
		}
		groups[instanceType] = &skuGroup{instanceType: instanceType, gpuMem: gpuConfig.GPUMem}
	}
	if len(groups) == 0 {
		return "", nil
	}

	ordered := make([]*skuGroup, 0, len(groups))
	for _, g := range groups {
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if cmp := ordered[i].gpuMem.Cmp(ordered[j].gpuMem); cmp != 0 {
			return cmp > 0
		}
		return ordered[i].instanceType < ordered[j].instanceType
	})
	return ordered[0].instanceType, nil
}

// BuildNodeSelector constrains pod placement (and, via WorkspaceNodeSelector, node
// listing) to the workspace's effective instance type in BYO mode. Returning the
// node.kubernetes.io/instance-type requirement here is the single execution point for
// both the user-specified hard filter (resource.instanceType) and the live
// nil-labelSelector auto-selection. It returns nil when no instance type is in effect,
// preserving pure label-selector matching. On a transient list error it logs and
// returns nil (best-effort; the next reconcile re-resolves).
func (n *BYOProvisioner) BuildNodeSelector(ctx context.Context, ws *kaitov1beta1.Workspace) []corev1.NodeSelectorRequirement {
	it, err := n.effectiveInstanceType(ctx, ws)
	if err != nil {
		klog.ErrorS(err, "failed to resolve effective instance type; skipping node selector",
			"workspace", client.ObjectKeyFromObject(ws).String())
		return nil
	}
	if it == "" {
		return nil
	}
	return []corev1.NodeSelectorRequirement{
		{
			Key:      corev1.LabelInstanceTypeStable,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{it},
		},
	}
}
