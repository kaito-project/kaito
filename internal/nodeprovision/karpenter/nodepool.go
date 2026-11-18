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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
)

const (
	// Keeps the Azure VM name of each node, "aks-" + NodeClaim name, within
	// the 63-character hostname label limit: Karpenter names NodeClaims
	// "<nodepool>-" + 5 random characters, so 4 + 53 + 1 + 5 = 63. This also
	// fits the karpenter.sh/nodepool label on NodeClaims and Nodes.
	maxNodePoolNameLen = 53
	hashSuffixLen      = 9

	// karpenter-provider-azure placement labels.
	azurePlacementScopeLabel = "karpenter.azure.com/placement-scope"
	azurePlacementRegional   = "regional"
)

// truncatedName returns a deterministic, truncated string with a hash suffix
// for uniqueness when the input exceeds maxLen.
func truncatedName(workspaceNamespace, workspaceName string, maxLen int) string {
	full := workspaceNamespace + "-" + workspaceName
	if len(full) <= maxLen {
		return full
	}
	truncLen := maxLen - 1 - hashSuffixLen // 1 for dash separator
	h := sha256.Sum256([]byte(full))
	return full[:truncLen] + "-" + hex.EncodeToString(h[:])[:hashSuffixLen]
}

// NodePoolName returns a deterministic, label-safe name for the NodePool
// derived from the workspace namespace and name, with a hash suffix appended
// when truncation is needed.
func NodePoolName(workspaceNamespace, workspaceName string) string {
	return truncatedName(workspaceNamespace, workspaceName, maxNodePoolNameLen)
}

// resolveNodeClassName determines the NodeClass resource name for a Workspace.
func resolveNodeClassName(ws *kaitov1beta1.Workspace, cfg NodeClassConfig) (string, error) {
	name, ok := ws.Annotations[kaitov1beta1.AnnotationNodeClassName]
	if !ok || name == "" {
		return cfg.DefaultName, nil
	}
	allowed := featuregates.AllowedNodeClassNames()
	if !slices.Contains(allowed, name) {
		return "", fmt.Errorf("annotation %s=%q is not permitted: choose one of the KAITO-managed NodeClasses %v",
			kaitov1beta1.AnnotationNodeClassName, name, allowed)
	}
	return name, nil
}

// isInferenceSetWorkspace returns true if the Workspace was created by an InferenceSet.
func isInferenceSetWorkspace(ws *kaitov1beta1.Workspace) bool {
	_, ok := ws.Labels[kaitov1beta1.WorkspaceCreatedByInferenceSetLabel]
	return ok
}

func resolveCapacityType(ws *kaitov1beta1.Workspace) (string, error) {
	capacityType := ws.Annotations[kaitov1beta1.AnnotationCapacityType]
	if capacityType == "" {
		return karpenterv1.CapacityTypeOnDemand, nil
	}
	if !kaitov1beta1.IsSupportedKarpenterCapacityType(capacityType) {
		return "", fmt.Errorf("annotation %s=%q is invalid: supported values are %q and %q",
			kaitov1beta1.AnnotationCapacityType,
			capacityType,
			kaitov1beta1.CapacityTypeOnDemand,
			kaitov1beta1.CapacityTypeSpot)
	}
	return capacityType, nil
}

// nodePoolRequirements builds the NodePool requirements list.
// The instance-type and capacity-type requirements are always included. Provider-specific
// requirements (e.g. Azure placement scope) are added based on the
// NodeClassConfig group.
func nodePoolRequirements(ws *kaitov1beta1.Workspace, cfg NodeClassConfig, capacityType string) []karpenterv1.NodeSelectorRequirementWithMinValues {
	reqs := []karpenterv1.NodeSelectorRequirementWithMinValues{
		{
			Key:      corev1.LabelInstanceTypeStable,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{ws.Resource.InstanceType},
		},
		{
			Key:      karpenterv1.CapacityTypeLabelKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{capacityType},
		},
	}
	// Azure Karpenter requires regional placement scope.
	if cfg.Group == "karpenter.azure.com" {
		reqs = append(reqs, karpenterv1.NodeSelectorRequirementWithMinValues{
			Key:      azurePlacementScopeLabel,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{azurePlacementRegional},
		})
	}
	return reqs
}

// generateNodePool builds a karpenter NodePool manifest for the given Workspace.
func generateNodePool(ws *kaitov1beta1.Workspace, cfg NodeClassConfig, nodeClassName, capacityType string) *karpenterv1.NodePool {
	nodePoolName := NodePoolName(ws.Namespace, ws.Name)

	// Drift budget: InferenceSet workspaces start with "0" (blocked),
	// standalone workspaces use "1" (karpenter handles autonomously).
	driftBudgetNodes := "1"
	if isInferenceSetWorkspace(ws) {
		driftBudgetNodes = "0"
	}

	// Template labels propagated to NodeClaims and Nodes.
	templateLabels := map[string]string{
		kaitov1beta1.LabelKarpenterWorkspaceName:      ws.Name,
		kaitov1beta1.LabelKarpenterWorkspaceNamespace: ws.Namespace,
	}
	// Include the user's matchLabels so that inference pods' nodeAffinity
	// (built from matchLabels) is satisfied. KAITO-reserved keys are stripped
	// to avoid clobbering controller-managed labels.
	for k, v := range kaitov1beta1.SanitizedMatchLabels(ws.Resource.LabelSelector) {
		templateLabels[k] = v
	}
	// InferenceSet workspaces get additional labels so the drift controller
	// can map NodeClaim events back to the owning InferenceSet.
	if isInferenceSetWorkspace(ws) {
		templateLabels[kaitov1beta1.LabelKarpenterInferenceSet] = ws.Labels[kaitov1beta1.WorkspaceCreatedByInferenceSetLabel]
		templateLabels[kaitov1beta1.LabelKarpenterInferenceSetNamespace] = ws.Namespace
	}

	// NodePool-level labels for management and lookup.
	nodePoolLabels := map[string]string{
		kaitov1beta1.LabelKarpenterManagedBy:          kaitov1beta1.KarpenterManagedByKAITO,
		kaitov1beta1.LabelKarpenterWorkspaceName:      ws.Name,
		kaitov1beta1.LabelKarpenterWorkspaceNamespace: ws.Namespace,
	}
	// InferenceSet workspaces get labels on NodePool ObjectMeta so the drift
	// controller can List NodePools by InferenceSet directly.
	if isInferenceSetWorkspace(ws) {
		nodePoolLabels[kaitov1beta1.LabelKarpenterInferenceSet] = ws.Labels[kaitov1beta1.WorkspaceCreatedByInferenceSetLabel]
		nodePoolLabels[kaitov1beta1.LabelKarpenterInferenceSetNamespace] = ws.Namespace
	}

	nodePoolTaints := []corev1.Taint{{
		Key:    kaitov1beta1.LabelSKU,
		Value:  kaitov1beta1.SKUGPU,
		Effect: corev1.TaintEffectNoSchedule,
	}}
	if kaitov1beta1.IsCPUInferenceWorkspace(ws) {
		templateLabels[kaitov1beta1.LabelSKU] = kaitov1beta1.SKUCPU
		nodePoolTaints = []corev1.Taint{{
			Key:    kaitov1beta1.LabelSKU,
			Value:  kaitov1beta1.SKUCPU,
			Effect: corev1.TaintEffectNoSchedule,
		}}
	}

	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodePoolName,
			Labels: nodePoolLabels,
		},
		Spec: karpenterv1.NodePoolSpec{
			Replicas: lo.ToPtr(int64(ws.Status.TargetNodeCount)),
			Template: karpenterv1.NodeClaimTemplate{
				ObjectMeta: karpenterv1.ObjectMeta{
					Labels: templateLabels,
				},
				Spec: karpenterv1.NodeClaimTemplateSpec{
					NodeClassRef: &karpenterv1.NodeClassReference{
						Group: cfg.Group,
						Kind:  cfg.Kind,
						Name:  nodeClassName,
					},
					Requirements: nodePoolRequirements(ws, cfg, capacityType),
					Taints:       nodePoolTaints,
				},
			},
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   driftBudgetNodes,
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonDrifted},
					},
				},
			},
		},
	}

	return np
}
