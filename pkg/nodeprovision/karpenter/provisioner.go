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
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

// NodeClassConfig holds cloud-specific NodeClass reference info injected at
// construction time, making the provisioner cloud-agnostic.
type NodeClassConfig struct {
	Group            string            // e.g. "karpenter.azure.com"
	Kind             string            // e.g. "AKSNodeClass"
	CRDName          string            // full CRD resource name (e.g. "aksnodeclasses.karpenter.azure.com")
	DefaultName      string            // default NodeClass name (e.g. "image-family-ubuntu")
	ImageFamilyNames map[string]string // image family annotation value → NodeClass resource name
	ConfigMapName    string            // ConfigMap containing NodeClass manifests (e.g. "kaito-nodeclasses")
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

// Start verifies that the Karpenter NodeClass CRD is installed and creates
// NodeClass resources from the configured ConfigMap. Returns an error if
// Karpenter is not installed — the operator cannot function without it when
// node-provisioner=karpenter is set.
func (p *KarpenterProvisioner) Start(ctx context.Context) error {
	if p.nodeClassConfig.ConfigMapName == "" {
		return nil
	}

	// Check if the NodeClass CRD exists.
	crdName := p.nodeClassConfig.CRDName
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: crdName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("NodeClass CRD %q not found — Karpenter must be installed before KAITO when node-provisioner=karpenter", crdName)
		}
		return fmt.Errorf("checking NodeClass CRD %q: %w", crdName, err)
	}

	releaseNS, err := utils.GetReleaseNamespace()
	if err != nil {
		return fmt.Errorf("resolving release namespace: %w", err)
	}

	// Read the ConfigMap containing NodeClass manifests.
	cm := &corev1.ConfigMap{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name: p.nodeClassConfig.ConfigMapName, Namespace: releaseNS,
	}, cm); err != nil {
		return fmt.Errorf("reading NodeClass ConfigMap %q in namespace %q: %w",
			p.nodeClassConfig.ConfigMapName, releaseNS, err)
	}

	// Create each NodeClass from the ConfigMap entries.
	nodeClassNames := make([]string, 0, len(cm.Data))
	for name, raw := range cm.Data {
		obj := &unstructured.Unstructured{}
		if err := sigsyaml.Unmarshal([]byte(raw), &obj.Object); err != nil {
			return fmt.Errorf("decoding NodeClass %q from ConfigMap: %w", name, err)
		}
		klog.InfoS("Creating NodeClass", "name", obj.GetName(), "kind", obj.GetKind())
		if err := p.client.Create(ctx, obj); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("creating NodeClass %q: %w", name, err)
			}
			klog.InfoS("NodeClass already exists", "name", obj.GetName())
		}
		nodeClassNames = append(nodeClassNames, obj.GetName())
	}

	// Wait for all NodeClasses to become Ready.
	for _, name := range nodeClassNames {
		if err := p.waitForNodeClassReady(ctx, name); err != nil {
			return err
		}
	}
	klog.InfoS("All NodeClasses are ready", "count", len(nodeClassNames))
	return nil
}

// waitForNodeClassReady polls until the NodeClass has a Ready=True condition.
func (p *KarpenterProvisioner) waitForNodeClassReady(ctx context.Context, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   p.nodeClassConfig.Group,
			Version: "v1beta1",
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

// EnsureNodesReady returns (true, false, nil) when all expected NodeClaims for
// the Workspace are present, in Ready state, and have GPU resources available.
// Returns needRequeue=true when NodeClaims are ready but GPU plugins are not,
// since GPU readiness is not event-driven.
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

	existingNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(nodeClaimList.Items))
	for i := range nodeClaimList.Items {
		if !nodeclaim.IsNodeClaimReadyNotDeleting(&nodeClaimList.Items[i]) {
			return false, false, nil
		}
		existingNodeClaims = append(existingNodeClaims, &nodeClaimList.Items[i])
	}

	// Verify GPU device plugins are ready on all provisioned nodes.
	pluginReady, err := p.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, existingNodeClaims)
	if err != nil {
		return false, false, fmt.Errorf("checking GPU plugin readiness: %w", err)
	}
	if !pluginReady {
		return false, true, nil // needRequeue=true, GPU not ready yet
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

	// Count ready NodeClaims and collect pointers for GPU check.
	readyCount := 0
	existingNodeClaims := make([]*karpenterv1.NodeClaim, 0, len(nodeClaimList.Items))
	for i := range nodeClaimList.Items {
		if nodeclaim.IsNodeClaimReadyNotDeleting(&nodeClaimList.Items[i]) {
			readyCount++
			existingNodeClaims = append(existingNodeClaims, &nodeClaimList.Items[i])
		}
	}

	targetCount := int(ws.Status.TargetNodeCount)
	if readyCount >= targetCount {
		nodeClaimCond.Status = metav1.ConditionTrue
		nodeClaimCond.Reason = "NodeClaimsReady"
		nodeClaimCond.Message = "Enough NodeClaims are ready"

		// Check GPU plugin readiness on the underlying nodes.
		pluginReady, err := p.nodeResourceManager.CheckIfNodePluginsReady(ctx, ws, existingNodeClaims)
		if err != nil {
			return nil, fmt.Errorf("checking GPU plugin readiness: %w", err)
		}
		if pluginReady {
			nodeCond.Status = metav1.ConditionTrue
			nodeCond.Reason = "NodesReady"
			nodeCond.Message = "Enough Nodes are ready with GPU resources"
		} else {
			nodeCond.Reason = "GPUPluginNotReady"
			nodeCond.Message = "GPU resources not yet available on nodes"
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
