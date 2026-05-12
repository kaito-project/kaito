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
	"strings"
	"testing"

	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// --- NodePoolName tests ---

func TestNodePoolName_Normal(t *testing.T) {
	name := NodePoolName("default", "myworkspace")
	assert.Equal(t, "default-myworkspace", name)
}

func TestNodePoolName_Exactly253(t *testing.T) {
	ws := strings.Repeat("a", 250)
	name := NodePoolName("ns", ws)
	assert.Equal(t, 253, len(name))
	assert.Equal(t, "ns-"+ws, name)
}

func TestNodePoolName_Over253_Truncated(t *testing.T) {
	ws := strings.Repeat("b", 251)
	name := NodePoolName("ns", ws)
	assert.Assert(t, len(name) == 253, "expected length 253, got %d", len(name))
	assert.Assert(t, name[maxNodePoolNameLen-1-hashSuffixLen] == '-', "expected dash at truncation point")
	suffix := name[maxNodePoolNameLen-hashSuffixLen:]
	assert.Equal(t, hashSuffixLen, len(suffix))
}

func TestNodePoolName_Deterministic(t *testing.T) {
	name1 := NodePoolName("ns", strings.Repeat("c", 300))
	name2 := NodePoolName("ns", strings.Repeat("c", 300))
	assert.Equal(t, name1, name2)
}

// --- resolveNodeClassName tests ---

func TestResolveNodeClassName_FromAnnotation(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				kaitov1beta1.AnnotationNodeClassName: "my-custom-nodeclass",
			},
		},
	}
	name := resolveNodeClassName(ws, testConfig)
	assert.Equal(t, "my-custom-nodeclass", name)
}

func TestResolveNodeClassName_DefaultFallback(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{},
	}
	name := resolveNodeClassName(ws, testConfig)
	assert.Equal(t, "image-family-ubuntu", name)
}

func TestResolveNodeClassName_EmptyAnnotation_FallsBackToDefault(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				kaitov1beta1.AnnotationNodeClassName: "",
			},
		},
	}
	name := resolveNodeClassName(ws, testConfig)
	assert.Equal(t, "image-family-ubuntu", name)
}

func TestResolveNodeClassName_CustomConfig(t *testing.T) {
	cfg := NodeClassConfig{
		Group:        "karpenter.k8s.aws",
		Kind:         "EC2NodeClass",
		ResourceName: "ec2nodeclasses",
		DefaultName:  "default-ec2",
	}
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				kaitov1beta1.AnnotationNodeClassName: "al2023-nodeclass",
			},
		},
	}
	name := resolveNodeClassName(ws, cfg)
	assert.Equal(t, "al2023-nodeclass", name)
}

// --- isInferenceSetWorkspace tests ---

func TestIsInferenceSetWorkspace_True(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: "my-infset",
			},
		},
	}
	assert.Assert(t, isInferenceSetWorkspace(ws))
}

func TestIsInferenceSetWorkspace_False(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
		},
	}
	assert.Assert(t, !isInferenceSetWorkspace(ws))
}

// --- generateNodePool tests ---

func newTestWorkspace(ns, name, instanceType string, targetNodeCount int32, labels, annotations map[string]string) *kaitov1beta1.Workspace {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Labels:      labels,
			Annotations: annotations,
		},
		Resource: kaitov1beta1.ResourceSpec{
			InstanceType: instanceType,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"apps": "llm",
				},
			},
		},
		Status: kaitov1beta1.WorkspaceStatus{
			TargetNodeCount: targetNodeCount,
		},
	}
	return ws
}

func TestGenerateNodePool_Standalone(t *testing.T) {
	ws := newTestWorkspace("default", "llama-serve", "Standard_NC24ads_A100_v4", 2, nil, nil)
	np := generateNodePool(ws, testConfig)

	// Name
	assert.Equal(t, "default-llama-serve", np.Name)

	// Labels
	assert.Equal(t, consts.KarpenterManagedByValue, np.Labels[consts.KarpenterLabelManagedBy])

	// Template labels
	assert.Equal(t, "llama-serve", np.Spec.Template.Labels[consts.KarpenterWorkspaceNameKey])
	assert.Equal(t, "default", np.Spec.Template.Labels[consts.KarpenterWorkspaceNamespaceKey])

	// Standalone workspaces should NOT have InferenceSet labels
	_, hasInfSetLabel := np.Spec.Template.Labels[consts.KarpenterInferenceSetKey]
	assert.Assert(t, !hasInfSetLabel, "standalone workspace should not have InferenceSet label")

	// NodeClassRef — uses config values
	ref := np.Spec.Template.Spec.NodeClassRef
	assert.Equal(t, testConfig.Group, ref.Group)
	assert.Equal(t, testConfig.Kind, ref.Kind)
	assert.Equal(t, testConfig.DefaultName, ref.Name)

	// Requirements
	assert.Equal(t, 1, len(np.Spec.Template.Spec.Requirements))
	req := np.Spec.Template.Spec.Requirements[0]
	assert.Equal(t, corev1.LabelInstanceTypeStable, req.Key)
	assert.Equal(t, corev1.NodeSelectorOpIn, req.Operator)
	assert.Equal(t, "Standard_NC24ads_A100_v4", req.Values[0])

	// Taints
	assert.Equal(t, 1, len(np.Spec.Template.Spec.Taints))
	taint := np.Spec.Template.Spec.Taints[0]
	assert.Equal(t, consts.SKUString, taint.Key)
	assert.Equal(t, consts.GPUString, taint.Value)
	assert.Equal(t, corev1.TaintEffectNoSchedule, taint.Effect)

	// Disruption — standalone gets budget "1"
	assert.Equal(t, 1, len(np.Spec.Disruption.Budgets))
	budget := np.Spec.Disruption.Budgets[0]
	assert.Equal(t, "1", budget.Nodes)
	assert.Equal(t, 1, len(budget.Reasons))
	assert.Equal(t, karpenterv1.DisruptionReasonDrifted, budget.Reasons[0])

	// ConsolidateAfter is set
	expected := karpenterv1.MustParseNillableDuration("0s")
	assert.Assert(t, np.Spec.Disruption.ConsolidateAfter.Duration != nil, "ConsolidateAfter should not be nil")
	assert.Equal(t, *expected.Duration, *np.Spec.Disruption.ConsolidateAfter.Duration)
}

func TestGenerateNodePool_InferenceSet(t *testing.T) {
	labels := map[string]string{
		consts.WorkspaceCreatedByInferenceSetLabel: "my-infset",
	}
	ws := newTestWorkspace("prod", "llama-infset-0", "Standard_NC24ads_A100_v4", 1, labels, nil)
	np := generateNodePool(ws, testConfig)

	// InferenceSet workspace gets budget "0"
	assert.Equal(t, "0", np.Spec.Disruption.Budgets[0].Nodes)

	// Default NodeClass name (no annotation)
	assert.Equal(t, testConfig.DefaultName, np.Spec.Template.Spec.NodeClassRef.Name)

	// InferenceSet labels on template for drift controller mapping
	assert.Equal(t, "my-infset", np.Spec.Template.Labels[consts.KarpenterInferenceSetKey])
	assert.Equal(t, "prod", np.Spec.Template.Labels[consts.KarpenterInferenceSetNamespaceKey])
}

func TestGenerateNodePool_WithAnnotation(t *testing.T) {
	ws := newTestWorkspace("default", "ws1", "Standard_D4s_v3", 1, nil, map[string]string{
		kaitov1beta1.AnnotationNodeClassName: "image-family-azure-linux",
	})
	np := generateNodePool(ws, testConfig)
	assert.Equal(t, "image-family-azure-linux", np.Spec.Template.Spec.NodeClassRef.Name)
}

func TestGenerateNodePool_CustomCloudConfig(t *testing.T) {
	cfg := NodeClassConfig{
		Group:       "karpenter.k8s.aws",
		Kind:        "EC2NodeClass",
		DefaultName: "default-ec2",
	}
	ws := newTestWorkspace("default", "ws1", "m5.xlarge", 1, nil, nil)
	np := generateNodePool(ws, cfg)

	ref := np.Spec.Template.Spec.NodeClassRef
	assert.Equal(t, "karpenter.k8s.aws", ref.Group)
	assert.Equal(t, "EC2NodeClass", ref.Kind)
	assert.Equal(t, "default-ec2", ref.Name)
}
