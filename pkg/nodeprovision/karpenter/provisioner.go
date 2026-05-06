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
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	sigsyaml "sigs.k8s.io/yaml"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

// NodeClassConfig holds cloud-specific NodeClass reference info.
// Group, Kind, Version, and ResourceName are injected via CLI flags.
// DefaultName is derived by Start() from ConfigMap labels.
type NodeClassConfig struct {
	Group        string // e.g. "karpenter.azure.com"
	Kind         string // e.g. "AKSNodeClass"
	Version      string // e.g. "v1beta1"
	ResourceName string // plural resource name (e.g. "aksnodeclasses"); combined with Group for CRD lookup
	DefaultName  string // populated by Start(): name of entry with karpenter.kaito.sh/default=true
}

// KarpenterProvisioner implements NodeProvisioner using the cloud-agnostic
// Karpenter API (NodePool / NodeClaim). Cloud-specific details (NodeClass
// group, kind, name mapping) are provided via NodeClassConfig.
type KarpenterProvisioner struct {
	client              client.Client
	nodeClassConfig     NodeClassConfig
	nodeResourceManager *resource.NodeManager
}

var _ nodeprovision.NodeProvisioner = (*KarpenterProvisioner)(nil)

// NewKarpenterProvisioner creates a new KarpenterProvisioner.
func NewKarpenterProvisioner(c client.Client, cfg NodeClassConfig) *KarpenterProvisioner {
	return &KarpenterProvisioner{client: c, nodeClassConfig: cfg, nodeResourceManager: resource.NewNodeManager(c)}
}

// Name returns the provisioner name.
func (p *KarpenterProvisioner) Name() string { return "KarpenterProvisioner" }

const nodeClassConfigMapName = "kaito-nodeclasses"

// Start verifies that the Karpenter CRDs are installed, creates
// NodeClass resources from the ConfigMap, and derives DefaultName from labels.
// Returns an error if Karpenter is not installed.
func (p *KarpenterProvisioner) Start(ctx context.Context) error {
	// Check if the core Karpenter CRDs exist.
	coreCRDs := []string{
		"nodepools.karpenter.sh",
		"nodeclaims.karpenter.sh",
	}
	for _, crdName := range coreCRDs {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := p.client.Get(ctx, types.NamespacedName{Name: crdName}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("CRD %q not found — Karpenter must be installed before KAITO when node-provisioner=karpenter", crdName)
			}
			return fmt.Errorf("checking CRD %q: %w", crdName, err)
		}
	}

	// Check if the provider-specific NodeClass CRD exists.
	nodeClassCRDName := p.nodeClassConfig.ResourceName + "." + p.nodeClassConfig.Group
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: nodeClassCRDName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("NodeClass CRD %q not found — Karpenter provider must be installed before KAITO when node-provisioner=karpenter", nodeClassCRDName)
		}
		return fmt.Errorf("checking NodeClass CRD %q: %w", nodeClassCRDName, err)
	}

	releaseNS, err := utils.GetReleaseNamespace()
	if err != nil {
		return fmt.Errorf("resolving release namespace: %w", err)
	}

	// Read the ConfigMap containing NodeClass manifests.
	cm := &corev1.ConfigMap{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name: nodeClassConfigMapName, Namespace: releaseNS,
	}, cm); err != nil {
		return fmt.Errorf("reading NodeClass ConfigMap %q in namespace %q: %w",
			nodeClassConfigMapName, releaseNS, err)
	}

	// Create each NodeClass and derive DefaultName from labels.
	for key, raw := range cm.Data {
		obj := &unstructured.Unstructured{}
		if err := sigsyaml.Unmarshal([]byte(raw), &obj.Object); err != nil {
			return fmt.Errorf("decoding NodeClass %q from ConfigMap: %w", key, err)
		}

		name := obj.GetName()
		labels := obj.GetLabels()

		// Track the default NodeClass entry.
		if labels["karpenter.kaito.sh/default"] == "true" {
			if p.nodeClassConfig.DefaultName != "" {
				return fmt.Errorf("multiple NodeClass entries have karpenter.kaito.sh/default=true: %q and %q",
					p.nodeClassConfig.DefaultName, name)
			}
			p.nodeClassConfig.DefaultName = name
		}

		klog.InfoS("Creating NodeClass", "name", name, "kind", obj.GetKind())
		if err := p.client.Create(ctx, obj); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("creating NodeClass %q: %w", key, err)
			}
			klog.InfoS("NodeClass already exists", "name", name)
		}
	}

	if p.nodeClassConfig.DefaultName == "" {
		return fmt.Errorf("no NodeClass entry has label karpenter.kaito.sh/default=true")
	}

	// Wait for the default NodeClass to be ready.
	if err := p.waitForNodeClassReady(ctx, p.nodeClassConfig.DefaultName); err != nil {
		return fmt.Errorf("default NodeClass %q not ready: %w", p.nodeClassConfig.DefaultName, err)
	}
	klog.InfoS("NodeClass resources created",
		"count", len(cm.Data),
		"default", p.nodeClassConfig.DefaultName)
	return nil
}

// checkNodeClassReady performs a single point-in-time check that the named
// NodeClass exists and has a Ready=True condition. Unlike waitForNodeClassReady,
// it does not poll — it returns an error immediately if the resource is missing
// or not ready.
func (p *KarpenterProvisioner) checkNodeClassReady(ctx context.Context, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   p.nodeClassConfig.Group,
		Version: p.nodeClassConfig.Version,
		Kind:    p.nodeClassConfig.Kind,
	})
	if err := p.client.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		return fmt.Errorf("getting NodeClass %q: %w", name, err)
	}

	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf("reading conditions for NodeClass %q: %w", name, err)
	}
	if !found {
		return fmt.Errorf("NodeClass %q has no status conditions", name)
	}
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Ready" && cond["status"] == "True" {
			return nil
		}
	}
	return fmt.Errorf("NodeClass %q exists but is not Ready", name)
}

// waitForNodeClassReady polls until the NodeClass has a Ready=True condition.
func (p *KarpenterProvisioner) waitForNodeClassReady(ctx context.Context, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   p.nodeClassConfig.Group,
			Version: p.nodeClassConfig.Version,
			Kind:    p.nodeClassConfig.Kind,
		})
		if err := p.client.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil // not created yet
			}
			return false, err
		}

		conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
		if err != nil {
			return false, fmt.Errorf("reading conditions for NodeClass %q: %w", name, err)
		}
		if !found {
			return false, nil
		}
		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if cond["type"] == "Ready" && cond["status"] == "True" {
				klog.InfoS("NodeClass is ready", "name", name)
				return true, nil
			}
		}
		return false, nil
	})
}

// countCoveredNodes returns:
//   - coveredByNonKarpenter: nodes already handled outside karpenter. This includes:
//     (a) Ready BYO nodes (no NodeClaim, user-managed)
//     (b) Non-deleting legacy gpu-provisioner NodeClaims (whether their node is ready or not,
//     because karpenter will either self-heal them or delete them after a timeout,
//     at which point a reconcile is triggered via the NodeClaim watch)
//   - readyWithInstanceType: total ready nodes (BYO + legacy + karpenter) with correct instance type.
func countCoveredNodes(ctx context.Context, c client.Client, ws *kaitov1beta1.Workspace) (coveredByNonKarpenter int, readyWithInstanceType int, err error) {
	if ws.Resource.LabelSelector == nil {
		return 0, 0, nil
	}

	// Count ready nodes (all types).
	nodeList, err := resources.ListNodes(ctx, c, ws.Resource.LabelSelector.MatchLabels)
	if err != nil {
		return 0, 0, fmt.Errorf("listing nodes: %w", err)
	}
	byoCount := 0
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !resources.NodeIsReadyAndNotDeleting(node) {
			continue
		}
		if node.Labels[corev1.LabelInstanceTypeStable] != ws.Resource.InstanceType {
			continue
		}
		readyWithInstanceType++
		// BYO nodes: ready, correct instance type, no karpenter label, no legacy label.
		_, isKarpenter := node.Labels[consts.KarpenterWorkspaceNameKey]
		_, isLegacy := node.Labels[kaitov1beta1.LabelWorkspaceName]
		if !isKarpenter && !isLegacy {
			byoCount++
		}
	}

	// Count non-deleting legacy NodeClaims (whether ready or not).
	// These are covered because karpenter engine will self-heal the node or
	// delete the NodeClaim after a timeout (triggering a reconcile).
	legacyNodeClaimList := &karpenterv1.NodeClaimList{}
	if err := c.List(ctx, legacyNodeClaimList,
		client.MatchingLabels{
			kaitov1beta1.LabelWorkspaceName:      ws.Name,
			kaitov1beta1.LabelWorkspaceNamespace: ws.Namespace,
		},
	); err != nil {
		return 0, 0, fmt.Errorf("listing legacy NodeClaims: %w", err)
	}
	legacyCount := 0
	for i := range legacyNodeClaimList.Items {
		nc := &legacyNodeClaimList.Items[i]
		if nc.DeletionTimestamp.IsZero() {
			legacyCount++
		}
	}

	coveredByNonKarpenter = byoCount + legacyCount
	return coveredByNonKarpenter, readyWithInstanceType, nil
}

// ProvisionNodes creates or updates a NodePool for the Workspace.
// Computes delta-based replicas: desiredReplicas = max(0, targetNodeCount - coveredByNonKarpenterCount).
// If no NodePool exists and desiredReplicas is 0, no NodePool is created.
// If a NodePool exists, replicas are only increased (never decreased) to avoid
// disrupting running karpenter nodes when BYO nodes appear.
func (p *KarpenterProvisioner) ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	nodeClassName := resolveNodeClassName(ws, p.nodeClassConfig)
	if err := p.checkNodeClassReady(ctx, nodeClassName); err != nil {
		return fmt.Errorf("NodeClass %q is not ready: %w", nodeClassName, err)
	}

	// Count non-karpenter ready nodes to compute delta.
	coveredCount, _, err := countCoveredNodes(ctx, p.client, ws)
	if err != nil {
		return fmt.Errorf("counting non-karpenter nodes: %w", err)
	}

	desiredReplicas := int64(ws.Status.TargetNodeCount) - int64(coveredCount)
	if desiredReplicas <= 0 {
		return nil
	}

	nodePoolName := NodePoolName(ws.Namespace, ws.Name)
	existing := &karpenterv1.NodePool{}
	err = p.client.Get(ctx, types.NamespacedName{Name: nodePoolName}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting NodePool %q: %w", nodePoolName, err)
		}
		np := generateNodePool(ws, p.nodeClassConfig)
		np.Spec.Replicas = lo.ToPtr(desiredReplicas)
		if err := p.client.Create(ctx, np); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("creating NodePool %q: %w", np.Name, err)
		}
		klog.InfoS("Created NodePool", "nodePool", np.Name, "replicas", desiredReplicas, "workspace", klog.KObj(ws))
		return nil
	}

	// NodePool exists — only increase replicas, never decrease.
	// This protects running karpenter nodes when BYO nodes appear after provisioning.
	currentReplicas := int64(0)
	if existing.Spec.Replicas != nil {
		currentReplicas = *existing.Spec.Replicas
	}
	if desiredReplicas <= currentReplicas {
		return nil
	}
	existing.Spec.Replicas = lo.ToPtr(desiredReplicas)
	if err := p.client.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating NodePool %q replicas to %d: %w", nodePoolName, desiredReplicas, err)
	}
	klog.InfoS("Updated NodePool replicas",
		"nodePool", nodePoolName,
		"desiredReplicas", desiredReplicas,
		"coveredByNonKarpenter", coveredCount,
		"targetNodeCount", ws.Status.TargetNodeCount)
	return nil
}

// DeleteNodes deletes the NodePool for the Workspace. Idempotent — NotFound is ignored.
// Karpenter cascades deletion: NodePool → NodeClaim → Node → VM.
func (p *KarpenterProvisioner) DeleteNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	nodePoolName := NodePoolName(ws.Namespace, ws.Name)
	np := &karpenterv1.NodePool{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: nodePoolName}, np); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("getting NodePool %q: %w", nodePoolName, err)
	}
	if np.DeletionTimestamp != nil {
		return nil
	}
	if err := p.client.Delete(ctx, np); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("deleting NodePool %q: %w", nodePoolName, err)
	}
	return nil
}

// nodeReadinessSnapshot holds pre-computed data about node and NodeClaim readiness
// for a workspace.
type nodeReadinessSnapshot struct {
	// readyWithInstanceTypeCount is the total number of ready nodes (BYO + legacy + karpenter)
	// that have the correct instance type for the workspace. Used to determine overall node readiness.
	readyWithInstanceTypeCount int
	// coveredByNonKarpenterCount is the number of nodes already handled outside karpenter:
	// ready BYO nodes + non-deleting legacy gpu-provisioner NodeClaims (regardless of node readiness).
	coveredByNonKarpenterCount int
	// targetNodeClaimCount is the number of karpenter NodeClaims needed:
	// max(0, ws.Status.TargetNodeCount - coveredByNonKarpenterCount).
	targetNodeClaimCount int
	// readyNodeClaims is the subset of karpenter-managed NodeClaims that are Ready and not deleting.
	// Used for GPU plugin readiness checks.
	readyNodeClaims []*karpenterv1.NodeClaim
}

// buildNodeReadinessSnapshot lists karpenter NodeClaims and all workspace nodes,
// returning counts needed for readiness decisions.
func (p *KarpenterProvisioner) buildNodeReadinessSnapshot(ctx context.Context, ws *kaitov1beta1.Workspace) (*nodeReadinessSnapshot, error) {
	nodePoolName := NodePoolName(ws.Namespace, ws.Name)

	// List karpenter NodeClaims.
	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := p.client.List(ctx, nodeClaimList,
		client.MatchingLabels{karpenterv1.NodePoolLabelKey: nodePoolName},
	); err != nil {
		return nil, fmt.Errorf("listing NodeClaims for NodePool %q: %w", nodePoolName, err)
	}

	readyNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(nodeClaimList.Items))
	for i := range nodeClaimList.Items {
		if nodeclaim.IsNodeClaimReadyNotDeleting(&nodeClaimList.Items[i]) {
			readyNodeClaims = append(readyNodeClaims, &nodeClaimList.Items[i])
		}
	}

	// Count all ready nodes matching workspace labels (BYO + legacy + karpenter).
	coveredByNonKarpenterCount, readyWithInstanceTypeCount, err := countCoveredNodes(ctx, p.client, ws)
	if err != nil {
		return nil, fmt.Errorf("counting ready nodes: %w", err)
	}

	targetNodeClaimCount := max(0, int(ws.Status.TargetNodeCount)-coveredByNonKarpenterCount)

	return &nodeReadinessSnapshot{
		readyWithInstanceTypeCount: readyWithInstanceTypeCount,
		coveredByNonKarpenterCount: coveredByNonKarpenterCount,
		targetNodeClaimCount:       targetNodeClaimCount,
		readyNodeClaims:            readyNodeClaims,
	}, nil
}

// EnsureNodesReady checks whether enough nodes are ready for the workspace.
// Counts all node types (BYO, legacy, karpenter) against targetNodeCount.
// Returns:
//   - ready: true when all expected nodes are present, Ready, and have GPU resources available.
//   - needRequeue: true when the caller should poll again because there is no watch/event
//     that will trigger reconciliation (e.g., Node registration or GPU plugin installation).
//   - err: non-nil on API errors.
func (p *KarpenterProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (bool, bool, error) {
	snap, err := p.buildNodeReadinessSnapshot(ctx, ws)
	if err != nil {
		return false, false, err
	}

	// Step 1: Check NodeClaim readiness.
	if len(snap.readyNodeClaims) < snap.targetNodeClaimCount {
		return false, false, nil
	}

	// Step 2: Check that enough Nodes with the correct instance type are ready.
	if snap.readyWithInstanceTypeCount < int(ws.Status.TargetNodeCount) {
		return false, true, nil
	}

	// Step 3: Check GPU device plugins on karpenter nodes.
	if len(snap.readyNodeClaims) > 0 {
		pluginReady, err := p.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, snap.readyNodeClaims)
		if err != nil {
			return false, true, fmt.Errorf("checking GPU plugin readiness: %w", err)
		}
		if !pluginReady {
			return false, true, nil
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
					if np.Spec.Disruption.Budgets[i].Nodes == nodes {
						return nil
					}
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
// Counts all ready nodes (BYO + legacy + karpenter) against targetNodeCount.
// GPU plugin readiness is checked only on karpenter NodeClaims.
func (p *KarpenterProvisioner) CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
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

	snap, err := p.buildNodeReadinessSnapshot(ctx, ws)
	if err != nil {
		return nil, err
	}

	targetCount := int(ws.Status.TargetNodeCount)

	// NodeClaim condition: are enough karpenter NodeClaims ready?
	// Non-karpenter nodes (BYO, legacy gpu-provisioner) reduce the target —
	// they don't have karpenter NodeClaims, so we only need NodeClaims for the remainder.
	// Legacy gpu-provisioner nodes also have NodeClaims, but those are already stable
	// and not managed by this provisioner, so we treat them like BYO here.
	if len(snap.readyNodeClaims) >= snap.targetNodeClaimCount {
		nodeClaimCond.Status = metav1.ConditionTrue
		nodeClaimCond.Reason = "NodeClaimsReady"
		nodeClaimCond.Message = "Enough NodeClaims are ready"
	}

	// Node condition: are enough nodes ready with GPU resources?
	// Uses total ready node count (all types). GPU plugin check only applies
	// to karpenter NodeClaims — BYO/legacy nodes are assumed GPU-ready.
	if snap.readyWithInstanceTypeCount >= targetCount {
		if len(snap.readyNodeClaims) > 0 {
			pluginReady, err := p.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, snap.readyNodeClaims)
			if err != nil {
				return nil, fmt.Errorf("checking GPU plugin readiness: %w", err)
			}
			if pluginReady {
				nodeCond.Status = metav1.ConditionTrue
				nodeCond.Reason = "NodesReady"
				nodeCond.Message = "Enough Nodes are ready with GPU resources"
			} else {
				nodeCond.Reason = "NodePluginsNotReady"
				nodeCond.Message = "waiting all node plugins to be ready"
			}
		} else {
			// No karpenter NodeClaims — all nodes are BYO/legacy, assume GPU ready.
			nodeCond.Status = metav1.ConditionTrue
			nodeCond.Reason = "NodesReady"
			nodeCond.Message = "Enough Nodes are ready with GPU resources"
		}
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
