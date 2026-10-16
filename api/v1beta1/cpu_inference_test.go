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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

type cpuValidationTrackingClient struct {
	ctrlclient.Client
	listCalled *bool
	listErr    error
}

func (c *cpuValidationTrackingClient) List(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
	if c.listCalled != nil {
		*c.listCalled = true
	}
	if c.listErr != nil {
		return c.listErr
	}
	return c.Client.List(ctx, list, opts...)
}

type cpuValidationTestModel struct {
	name        string
	distributed bool
}

func (m *cpuValidationTestModel) GetInferenceParameters() *model.PresetParam {
	return &model.PresetParam{
		Metadata:                model.Metadata{Name: m.name, Tag: "1.0.0"},
		DiskStorageRequirement:  "100Gi",
		TotalSafeTensorFileSize: "8Gi",
		RuntimeParam: model.RuntimeParam{
			VLLM: model.VLLMParam{
				BaseCommand:    "python3 /workspace/vllm/inference_api.py",
				ModelRunParams: map[string]string{},
			},
			Transformers: model.HuggingfaceTransformersParam{
				BaseCommand:       "accelerate launch",
				InferenceMainFile: "/workspace/tfs/inference_api.py",
				AccelerateParams:  map[string]string{},
				ModelRunParams:    map[string]string{},
			},
		},
		ReadinessTimeout: 30 * time.Minute,
	}
}

func (m *cpuValidationTestModel) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{ReadinessTimeout: 30 * time.Minute}
}

func (m *cpuValidationTestModel) SupportDistributedInference() bool { return m.distributed }
func (m *cpuValidationTestModel) SupportTuning() bool               { return true }

func registerCPUValidationTestModels() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{Name: "test-model", Instance: &cpuValidationTestModel{name: "test-model"}})
	plugin.KaitoModelRegister.Register(&plugin.Registration{Name: "test-distributed-model", Instance: &cpuValidationTestModel{name: "test-distributed-model", distributed: true}})
}

func newCPUValidationClient(t *testing.T, objects ...ctrlclient.Object) ctrlclient.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestValidateCPUInferenceConstraints(t *testing.T) {
	registerCPUValidationTestModels()
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

	t.Run("feature gate disabled skips cpu-specific validation", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = false
		ws := baseWorkspace()
		ws.Resource.Count = &two
		ws.Annotations[AnnotationWorkspaceRuntime] = "invalid-runtime"
		if IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected workspace to not be detected as cpu inference when feature gate is disabled")
		}
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
			t.Fatalf("expected cpu-specific validation to be skipped when enableCPUInference is disabled, got %v", err)
		}
	})

	t.Run("single node only", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		ws.Resource.Count = &two
		err := ws.validateCPUInferenceConstraints(context.Background(), nil)
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
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
			t.Fatalf("expected cpu workspace validation to pass, got %v", err)
		}
	})

	t.Run("mig template is not detected as cpu inference", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		ws.Inference.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1"),
		}
		ws.Inference.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1"),
		}
		if IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected mig-backed template workspace to not be detected as cpu inference")
		}
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
			t.Fatalf("expected non-cpu mig template to skip cpu validation, got %v", err)
		}
	})

	t.Run("invalid runtime annotation", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		ws.Annotations[AnnotationWorkspaceRuntime] = "invalid-runtime"
		err := ws.validateCPUInferenceConstraints(context.Background(), nil)
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
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
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
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
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
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
			t.Fatalf("expected validation to pass for generic hf model id on cpu instance type, got %v", err)
		}
	})

	t.Run("cpu-compatible preset on byo cpu nodes is detected", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

		nodeClient := newCPUValidationClient(t, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
		})

		ws := &Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-byo", Namespace: "default"},
			Resource: ResourceSpec{
				Count:         &one,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-model")}}},
		}
		if err := ws.validateCPUInferenceConstraints(context.Background(), nodeClient); err != nil {
			t.Fatalf("expected byo cpu preset validation to pass, got %v", err)
		}
	})
}
