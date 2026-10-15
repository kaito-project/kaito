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

package v1beta1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestValidatePhase1CPUInference(t *testing.T) {
	test.RegisterTestModel()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldDisableNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldDisableNAP
	}()

	one := 1
	two := 2

	baseWorkspace := func() *Workspace {
		return &Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cpu-ws",
				Namespace: "default",
				Annotations: map[string]string{
					AnnotationWorkspaceRuntime: "transformers",
				},
			},
			Resource: ResourceSpec{
				Count:         &one,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "cpu",
							Image: "example.com/cpu:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
								Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
							},
						}},
					},
				},
			},
		}
	}

	t.Run("feature gate required", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = false
		ws := baseWorkspace()
		err := ws.validatePhase1CPUInference(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected validation error when enableCPUInference is disabled")
		}
	})

	t.Run("single node only", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		ws.Resource.Count = &two
		err := ws.validatePhase1CPUInference(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected validation error when cpu workspace requests multiple nodes")
		}
	})

	t.Run("valid cpu workspace", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		if !IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected workspace to be detected as cpu inference")
		}
		if err := ws.validatePhase1CPUInference(context.Background(), nil); err != nil {
			t.Fatalf("expected cpu workspace validation to pass, got %v", err)
		}
	})

	t.Run("invalid runtime annotation", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		ws.Annotations[AnnotationWorkspaceRuntime] = "invalid-runtime"
		err := ws.validatePhase1CPUInference(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected validation error for invalid cpu runtime annotation")
		}
	})

	t.Run("cpu-compatible preset with cpu instance type is detected", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := &Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset", Namespace: "default"},
			Resource: ResourceSpec{
				Count:         &one,
				InstanceType:  "Standard_D8s_v5",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-model")}},
			},
		}
		if !IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected preset workspace to be detected as cpu inference")
		}
		if err := ws.validatePhase1CPUInference(context.Background(), nil); err != nil {
			t.Fatalf("expected cpu preset validation to pass, got %v", err)
		}
	})

	t.Run("built-in preset on cpu instance type is allowed", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := &Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset", Namespace: "default"},
			Resource: ResourceSpec{
				Count:         &one,
				InstanceType:  "Standard_D8s_v5",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-distributed-model")}},
			},
		}
		if err := ws.validatePhase1CPUInference(context.Background(), nil); err != nil {
			t.Fatalf("expected validation to pass for built-in preset on cpu instance type, got %v", err)
		}
	})

	t.Run("generic hf model id with builtin mapping is allowed on cpu instance type", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := &Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-generic", Namespace: "default"},
			Resource: ResourceSpec{
				Count:         &one,
				InstanceType:  "Standard_D8s_v5",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{
				Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("microsoft/Phi-4-mini-instruct")}},
			},
		}
		if !IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected generic hf model id to be detected as cpu inference")
		}
		if err := ws.validatePhase1CPUInference(context.Background(), nil); err != nil {
			t.Fatalf("expected validation to pass for generic hf model id on cpu instance type, got %v", err)
		}
	})

	t.Run("cpu-compatible preset on byo cpu nodes is detected", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

		mockClient := test.NewClient()
		k8sclient.SetGlobalClient(mockClient)
		mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{{
				ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{"kaito.sh/cpu-inference": "true"}},
				Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
			}}
		}).Return(nil)

		ws := &Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-byo", Namespace: "default"},
			Resource: ResourceSpec{
				Count:         &one,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-model")}}},
		}
		if err := ws.validatePhase1CPUInference(context.Background(), mockClient); err != nil {
			t.Fatalf("expected byo cpu preset validation to pass, got %v", err)
		}
	})
}
