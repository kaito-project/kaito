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

package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestInferenceSetValidateCreateCPUFeatureGate(t *testing.T) {
	test.RegisterTestModel()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: 1,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Template: InferenceSetTemplate{
				Inference: kaitoInferenceSpecForCPU(),
			},
		},
	}

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = false
	if err := is.validateCreateWithContext(context.Background()); err == nil {
		t.Fatalf("expected cpu inferenceset validation to fail when feature gate is disabled")
	}

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	if err := is.validateCreateWithContext(context.Background()); err != nil {
		t.Fatalf("expected cpu inferenceset validation to pass when feature gate is enabled, got %v", err)
	}

	is.Annotations = map[string]string{AnnotationWorkspaceRuntime: "invalid-runtime"}
	if err := is.validateCreateWithContext(context.Background()); err == nil {
		t.Fatalf("expected cpu inferenceset validation to fail for invalid runtime annotation")
	}
}

func kaitoInferenceSpecForCPU() kaitov1beta1.InferenceSpec {
	return kaitov1beta1.InferenceSpec{
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
	}
}

func TestInferenceSetValidateCreateCPUPreset(t *testing.T) {
	test.RegisterTestModel()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: 1,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Template: InferenceSetTemplate{
				Resource: InferenceSetResourceSpec{InstanceType: "Standard_D8s_v5"},
				Inference: kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "test-model"}},
				},
			},
		},
	}
	if err := is.validateCreateWithContext(context.Background()); err != nil {
		t.Fatalf("expected cpu preset inferenceset validation to pass, got %v", err)
	}

	is.Spec.Template.Inference.Preset.PresetMeta.Name = "test-distributed-model"
	if err := is.validateCreateWithContext(context.Background()); err != nil {
		t.Fatalf("expected cpu preset inferenceset validation to pass for built-in preset, got %v", err)
	}
}

func TestInferenceSetValidateCreateBYOCPUPreset(t *testing.T) {
	test.RegisterTestModel()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldDisableNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldDisableNAP
	}()

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

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-byo-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: 1,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Template: InferenceSetTemplate{
				Inference: kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "test-model"}},
				},
			},
		},
	}

	if err := is.validateCreateWithContext(context.Background()); err != nil {
		t.Fatalf("expected BYO cpu preset inferenceset validation to pass, got %v", err)
	}
}

func TestInferenceSetValidateCreateCPUGenericHFID(t *testing.T) {
	test.RegisterTestModel()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-generic-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: 1,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Template: InferenceSetTemplate{
				Resource: InferenceSetResourceSpec{InstanceType: "Standard_D8s_v5"},
				Inference: kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "microsoft/Phi-4-mini-instruct"}},
				},
			},
		},
	}
	if err := is.validateCreateWithContext(context.Background()); err != nil {
		t.Fatalf("expected generic hf model id cpu inferenceset validation to pass, got %v", err)
	}
}
