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
					AnnotationWorkspaceRuntime: "vllm",
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

	t.Run("init container gpu request is not detected as cpu inference", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		ws := baseWorkspace()
		ws.Inference.Template.Spec.InitContainers = []corev1.Container{{
			Name:  "gpu-init",
			Image: "example.com/gpu-init:latest",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
				Limits:   corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
			},
		}}
		if IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected template with gpu init container to not be detected as cpu inference")
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
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{"kaito.sh/cpu-inference": "true", corev1.LabelArchStable: "amd64"}},
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

	t.Run("cpu node detection sanitizes reserved selector labels", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

		nodeClient := newCPUValidationClient(t, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{"kaito.sh/cpu-inference": "true", corev1.LabelArchStable: "amd64"}},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
		})

		selector := &metav1.LabelSelector{MatchLabels: map[string]string{
			"kaito.sh/cpu-inference": "true",
			LabelWorkspaceName:       "other-workspace",
		}}
		isCPU, err := AreLabelSelectedReadyNodesCPU(context.Background(), selector, "", nodeClient)
		if err != nil {
			t.Fatalf("expected sanitized selector lookup to succeed, got %v", err)
		}
		if !isCPU {
			t.Fatalf("expected sanitized selector lookup to detect cpu-ready nodes")
		}
	})

	t.Run("gpu preset with nil selector does not scan the whole cluster for cpu detection", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

		nodeClient := newCPUValidationClient(t,
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{"kaito.sh/cpu-inference": "true", corev1.LabelArchStable: "amd64"}},
				Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
			},
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "gpu-node", Labels: map[string]string{corev1.LabelInstanceTypeStable: "g5.xlarge", consts.NvidiaGPUProduct: "NVIDIA L4"}},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
					Capacity:   corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
				},
			},
		)

		inference := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-model")}}}
		isCPU, err := ResolveCPUInferenceTarget(context.Background(), inference, "g5.xlarge", nil, nodeClient)
		if err != nil {
			t.Fatalf("expected gpu preset with nil selector to skip cpu resolution, got %v", err)
		}
		if isCPU {
			t.Fatalf("expected gpu preset with nil selector to remain on gpu path")
		}
	})

	t.Run("gpu sku without discovery labels is still treated as gpu for byo cpu resolution", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

		nodeClient := newCPUValidationClient(t, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gpu-sku-node",
				Labels: map[string]string{
					"kaito.sh/cpu-inference":       "true",
					corev1.LabelInstanceTypeStable: "Standard_NC24ads_A100_v4",
					corev1.LabelArchStable:         "amd64",
				},
			},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
		})

		inference := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-model")}}}
		isCPU, err := ResolveCPUInferenceTarget(context.Background(), inference, "", &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}}, nodeClient)
		if err != nil {
			t.Fatalf("expected gpu sku without discovery labels to resolve cleanly, got %v", err)
		}
		if isCPU {
			t.Fatalf("expected known gpu sku to stay on gpu path even before discovery labels appear")
		}
	})

	t.Run("vllm preset on arm64 byo cpu nodes is rejected", func(t *testing.T) {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

		nodeClient := newCPUValidationClient(t, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "cpu-arm-node",
				Labels: map[string]string{"kaito.sh/cpu-inference": "true", corev1.LabelArchStable: "arm64"},
			},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
		})

		ws := &Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cpu-preset-byo-arm",
				Namespace: "default",
				Annotations: map[string]string{
					AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
				},
			},
			Resource: ResourceSpec{
				Count:         &one,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			},
			Inference: &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("test-model")}}},
		}
		if err := ws.validateCPUInferenceConstraints(context.Background(), nodeClient); err == nil {
			t.Fatalf("expected arm64 byo nodes to be rejected for vllm cpu preset path")
		}
	})
}

func TestCPUInferencePresetRuntimeSelection(t *testing.T) {
	registerCPUValidationTestModels()
	oldCPU := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldCPU
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldVLLM
	})
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	for _, tc := range []struct {
		name, runtime          string
		vllmEnabled, wantError bool
	}{
		{"default vllm accepted", "", true, false},
		{"explicit vllm accepted", "vllm", true, false},
		{"explicit transformers", "transformers", true, false},
		{"explicit transformers without vllm gate", "transformers", false, false},
		{"default requires vllm gate", "", false, true},
		{"explicit vllm requires gate", "vllm", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregates.FeatureGates[consts.FeatureFlagVLLM] = tc.vllmEnabled
			ws := &Workspace{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationWorkspaceRuntime: tc.runtime}},
				Resource:   ResourceSpec{InstanceType: "Standard_D8s_v5"},
				Inference:  &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}},
			}
			err := ws.validateCPUInferenceConstraints(context.Background(), nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("validation error = %v, want error = %v", err, tc.wantError)
			}
		})
	}
}

func TestIsExplicitTransformersCPUWorkspace(t *testing.T) {
	registerCPUValidationTestModels()
	old := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = old })
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	for _, tc := range []struct {
		name, instance, runtime string
		want                    bool
	}{
		{"explicit CPU", "Standard_D16s_v6", "transformers", true},
		{"GPU", "Standard_NC6s_v3", "transformers", false},
		{"Azure ARM", "Standard_D16ps_v6", "transformers", false},
		{"AWS ARM", "m7g.4xlarge", "transformers", false},
		{"label only BYO", "", "transformers", false},
		{"default runtime", "Standard_D16s_v6", "", false},
		{"vllm runtime", "Standard_D16s_v6", "vllm", false},
		{"unknown runtime", "Standard_D16s_v6", "other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := &Workspace{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationWorkspaceRuntime: tc.runtime}},
				Resource:   ResourceSpec{InstanceType: tc.instance},
				Inference:  &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}},
			}
			if got := IsExplicitTransformersCPUWorkspace(ws); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = false
			if IsExplicitTransformersCPUWorkspace(ws) {
				t.Fatal("CPU gate must be enabled")
			}
			featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
		})
	}
	if IsExplicitTransformersCPUWorkspace(nil) || IsExplicitTransformersCPUWorkspace(&Workspace{}) {
		t.Fatal("empty workspace cannot select CPU Transformers")
	}
}
