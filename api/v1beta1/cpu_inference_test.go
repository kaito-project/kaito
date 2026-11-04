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
	"fmt"
	"strings"
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
	oldDisableNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
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
				InstanceType:  "Standard_D8s_v5",
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

	t.Run("single node only", func(t *testing.T) {
		ws := baseWorkspace()
		ws.Resource.Count = &two
		err := ws.validateCPUInferenceConstraints(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected validation error when cpu workspace requests multiple nodes")
		}
	})

	t.Run("valid cpu workspace", func(t *testing.T) {
		ws := baseWorkspace()
		if !IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected workspace to be detected as cpu inference")
		}
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err != nil {
			t.Fatalf("expected cpu workspace validation to pass, got %v", err)
		}
	})

	t.Run("mig template on cpu instance type is detected as cpu inference", func(t *testing.T) {
		ws := baseWorkspace()
		ws.Inference.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1"),
		}
		ws.Inference.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1"),
		}
		if !IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected cpu instance type to select cpu inference regardless of mig resources")
		}
		ws.Resource.Count = &two
		if err := ws.validateCPUInferenceConstraints(context.Background(), nil); err == nil {
			t.Fatalf("expected cpu single-node validation regardless of mig resources")
		}
	})

	t.Run("init container gpu request does not override cpu instance type", func(t *testing.T) {
		ws := baseWorkspace()
		ws.Inference.Template.Spec.InitContainers = []corev1.Container{{
			Name:  "gpu-init",
			Image: "example.com/gpu-init:latest",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
				Limits:   corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
			},
		}}
		if !IsCPUInferenceWorkspace(ws) {
			t.Fatalf("expected cpu instance type to select cpu inference regardless of gpu init container")
		}
	})

	t.Run("invalid runtime annotation", func(t *testing.T) {
		ws := baseWorkspace()
		ws.Annotations[AnnotationWorkspaceRuntime] = "invalid-runtime"
		err := ws.validateCPUInferenceConstraints(context.Background(), nil)
		if err == nil {
			t.Fatalf("expected validation error for invalid cpu runtime annotation")
		}
	})

	t.Run("cpu-compatible preset with cpu instance type is detected", func(t *testing.T) {
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

}

func TestCPUInferencePresetRuntimeSelection(t *testing.T) {
	registerCPUValidationTestModels()
	oldVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldVLLM
	})
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
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	registerCPUValidationTestModels()
	for _, tc := range []struct {
		name, instance, runtime string
		want                    bool
	}{
		{"explicit CPU", "Standard_D16s_v6", "transformers", true},
		{"GPU", "Standard_NC6s_v3", "transformers", false},
		{"Azure ARM", "Standard_D16ps_v6", "transformers", true},
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
		})
	}
	ws := &Workspace{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationWorkspaceRuntime: "transformers"}},
		Resource:   ResourceSpec{InstanceType: "Standard_D16ps_v6"},
		Inference:  &InferenceSpec{Template: &corev1.PodTemplateSpec{}},
	}
	if IsExplicitTransformersCPUWorkspace(ws) {
		t.Fatal("custom templates must not qualify for the preset runtime exception")
	}
	ws.Inference = &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}
	t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)
	if IsExplicitTransformersCPUWorkspace(ws) {
		t.Fatal("AWS must not qualify for the CPU runtime exception")
	}
	if IsExplicitTransformersCPUWorkspace(nil) || IsExplicitTransformersCPUWorkspace(&Workspace{}) {
		t.Fatal("empty workspace cannot select CPU Transformers")
	}
}

func TestResolveCPUInferenceSetUsesChildNodeSelector(t *testing.T) {
	registerCPUValidationTestModels()
	oldBYO := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldBYO
	}()
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	kubeClient := newCPUValidationClient(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu", Labels: map[string]string{"pool": "cpu", corev1.LabelArchStable: "amd64"}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	})
	iObj := &InferenceSet{}
	iObj.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "workspaces"}}
	iObj.Spec.Template.Resource.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}
	iObj.Spec.Template.Inference.Preset = &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}
	got, err := ResolveCPUInferenceSetTarget(context.Background(), iObj, kubeClient)
	if err != nil || !got {
		t.Fatalf("distinct child node selector should resolve CPU, got %v, %v", got, err)
	}
	iObj.Spec.Template.Resource.LabelSelector = nil
	got, err = ResolveCPUInferenceSetTarget(context.Background(), iObj, kubeClient)
	if err != nil || got {
		t.Fatalf("BYO must not fall back to Workspace labels, got %v, %v", got, err)
	}
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	if InferenceSetNodeSelector(iObj) != iObj.Spec.Selector {
		t.Fatal("provisioned legacy selector fallback lost")
	}
}

func TestAzureCPUInstanceTypeCaseInsensitive(t *testing.T) {
	registerCPUValidationTestModels()
	oldVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldVLLM
	})
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	for _, tc := range []struct {
		instance string
		cpu, gpu bool
	}{
		{"Standard_B2s", true, false},
		{"standard_b2s", true, false},
		{"Standard_F16s", true, false},
		{"STANDARD_F16S", true, false},
		{"Standard_A1", true, false},
		{"Standard_DS4_v2", true, false},
		{"Standard_D16s_v6", true, false},
		{"standard_d16s_v6", true, false},
		{"STANDARD_D16S_V6", true, false},
		{"standard_d16ps_v6", true, false},
		{"StAnDaRd_D16pS_v6", true, false},
		{"standard_nc6s_v3", false, true},
		{"STANDARD_ND96ASR_V4", false, true},
		{"StAnDaRd_NV12s_v3", false, true},
		{"Standard_NC6", false, true},
		{"Standard_NV12", false, true},
		// ARM-style names follow the same CPU classification, with or without a version.
		{"Standard_D16ps", true, false},
	} {
		t.Run(tc.instance, func(t *testing.T) {
			if got := isAzureGPUOrAcceleratorSKU(tc.instance); got != tc.gpu {
				t.Fatalf("GPU classification = %v, want %v", got, tc.gpu)
			}
			if got := isAzureCPUInstanceType(tc.instance); got != tc.cpu {
				t.Fatalf("CPU classification = %v, want %v", got, tc.cpu)
			}
			inference := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}
			got, err := ResolveCPUInferenceTarget(context.Background(), inference, tc.instance, nil, nil)
			if err != nil || got != tc.cpu {
				t.Fatalf("CPU resolution = %v, %v; want %v", got, err, tc.cpu)
			}
			if tc.cpu {
				ws := &Workspace{Resource: ResourceSpec{InstanceType: tc.instance}, Inference: inference}
				err := ws.validateCPUInferenceConstraints(context.Background(), nil)
				if err != nil {
					t.Fatalf("CPU admission must not depend on architecture: %v", err)
				}
			}
		})
	}
}

func TestSingleValueExpressionCPUSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu", Labels: map[string]string{"pool": "cpu", corev1.LabelArchStable: "amd64"}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}).Build()
	selector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "pool", Operator: metav1.LabelSelectorOpIn, Values: []string{"cpu"}},
		{Key: LabelWorkspaceName, Operator: metav1.LabelSelectorOpIn, Values: []string{"reserved"}},
	}}
	labels := SanitizedMatchLabels(selector)
	if len(labels) != 1 || labels["pool"] != "cpu" {
		t.Fatalf("expected normalized non-reserved pool selector, got %v", labels)
	}
	labels["pool"] = "changed"
	if selector.MatchExpressions[0].Values[0] != "cpu" {
		t.Fatal("normalization mutated the original selector")
	}
	if cpu, err := AreLabelSelectedReadyNodesCPU(context.Background(), selector, "", c); err != nil || !cpu {
		t.Fatalf("single-value expression must resolve the CPU node: cpu=%v, err=%v", cpu, err)
	}
	selector.MatchExpressions[0].Values = []string{"cpu", "gpu"}
	if got := SanitizedMatchLabels(selector); got != nil {
		t.Fatalf("unsupported multi-value expression must not broaden node selection: %v", got)
	}
}

func TestAzureCPUInstanceTypeForNAPVersionSuffix(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	for _, tc := range []struct {
		instance string
		valid    bool
	}{
		{"Standard_B2s", true},
		{"standard_b2s", true},
		{"Standard_F16s", true},
		{"Standard_A1", true},
		{"Standard_DS4_v2", true},
		{"Standard_D16s_v6", true},
		{"Standard_E8s_v5", true},
		{"Standard_D16ps_v5", true},
		{"Standard_D16ps", true},
		{"Standard_NC6", false},
		{"Standard_NV12", false},
		{"Standard_ND96asr_v4", false},
		{"Standard_B", false},
		{"Standard_B2s_v", false},
		{"Standard_B2s_vx", false},
		{"Standard_B2s_extra", false},
		{"", false},
	} {
		t.Run(tc.instance, func(t *testing.T) {
			if err := ValidateCPUInstanceTypeForNAP(tc.instance); (err == nil) != tc.valid {
				t.Fatalf("NAP validation error = %v, want valid = %v", err, tc.valid)
			}
		})
	}
}

// CPU inference is available without opt-in, but AWS targets keep the existing
// GPU path. Unknown-provider BYO nodes do not need an Azure SKU label.
func TestCPUInferenceProviderScope(t *testing.T) {
	registerCPUValidationTestModels()
	old := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = old })
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	for _, tc := range []struct {
		name, provider, instance string
		want                     bool
	}{
		{"Azure CPU", consts.AzureCloudName, "Standard_D8s_v5", true},
		{"Azure GPU", consts.AzureCloudName, "Standard_NC6s_v3", false},
		{"AWS CPU", consts.AWSCloudName, "m6i.2xlarge", false},
		{"AWS GPU", consts.AWSCloudName, "g5.xlarge", false},
		{"AWS label-only", consts.AWSCloudName, "", false},
		{"AWS SKU without provider", "", "m6i.2xlarge", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tc.provider)
			inf := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}
			if got := IsCPUInferenceTarget(inf, tc.instance); got != tc.want {
				t.Fatalf("CPU target=%v, want %v", got, tc.want)
			}
			got, err := ResolveCPUInferenceTarget(t.Context(), inf, tc.instance, nil, nil)
			if err != nil || got != tc.want {
				t.Fatalf("CPU resolution=%v, %v, want %v", got, err, tc.want)
			}
		})
	}
	t.Setenv("CLOUD_PROVIDER", "")
	for _, tc := range []struct {
		name, providerID, instance string
		want                       bool
	}{
		{"generic BYO", "", "", true},
		{"Azure BYO", "azure:///node", "Standard_D8s_v5", true},
		{"AWS BYO provider", "aws:///zone/node", "", false},
		{"generic BYO custom SKU", "", "custom.vm", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu", Labels: map[string]string{"pool": "cpu", corev1.LabelInstanceTypeStable: tc.instance, corev1.LabelArchStable: "amd64"}}, Spec: corev1.NodeSpec{ProviderID: tc.providerID}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
			got, err := ResolveCPUInferenceTarget(t.Context(), &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}, "", &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}, newCPUValidationClient(t, node))
			if got != tc.want || (tc.want && err != nil) {
				t.Fatalf("CPU resolution=%v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestCustomTemplateCPUInferenceUsesInstanceType(t *testing.T) {
	old := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = old })

	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}
	kubeClient := newCPUValidationClient(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: selector.MatchLabels},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	})
	for _, napDisabled := range []bool{false, true} {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = napDisabled
		for _, tc := range []struct {
			name, provider, instance string
			want                     bool
		}{
			{"Azure CPU", consts.AzureCloudName, "Standard_D8s_v5", true},
			{"Azure unversioned CPU", consts.AzureCloudName, "Standard_B2s", true},
			{"Azure ARM CPU", consts.AzureCloudName, "Standard_D8ps_v6", true},
			{"Azure GPU", consts.AzureCloudName, "Standard_NC6s_v3", false},
			{"empty instance type with CPU selector", consts.AzureCloudName, "", false},
			{"unknown instance type with CPU selector", consts.AzureCloudName, "custom.vm", false},
			{"AWS CPU", consts.AWSCloudName, "m6i.2xlarge", false},
			{"AWS provider excludes Azure CPU SKU", consts.AWSCloudName, "Standard_D8s_v5", false},
		} {
			t.Run(fmt.Sprintf("%s/NAPDisabled=%v", tc.name, napDisabled), func(t *testing.T) {
				t.Setenv("CLOUD_PROVIDER", tc.provider)
				for _, templateCase := range []struct {
					name string
					spec corev1.PodSpec
				}{
					{"no containers", corev1.PodSpec{}},
					{"no GPU resources", corev1.PodSpec{Containers: []corev1.Container{{Name: "custom"}}}},
					{"GPU request", corev1.PodSpec{Containers: []corev1.Container{{Name: "custom", Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
					}}}}},
					{"MIG limit", corev1.PodSpec{Containers: []corev1.Container{{Name: "custom", Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1")},
					}}}}},
					{"init GPU limit", corev1.PodSpec{Containers: []corev1.Container{{Name: "custom"}}, InitContainers: []corev1.Container{{Name: "init", Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceName(consts.NvidiaGPU): resource.MustParse("1")},
					}}}}},
				} {
					t.Run(templateCase.name, func(t *testing.T) {
						inf := &InferenceSpec{Template: &corev1.PodTemplateSpec{Spec: templateCase.spec}}
						if got := IsCPUInferenceTarget(inf, tc.instance); got != tc.want {
							t.Fatalf("CPU target=%v, want %v", got, tc.want)
						}
						got, err := ResolveCPUInferenceTarget(t.Context(), inf, tc.instance, selector, kubeClient)
						if err != nil || got != tc.want {
							t.Fatalf("CPU resolution=%v, %v, want %v", got, err, tc.want)
						}
					})
				}
			})
		}
	}
}

// Routing/admission tests do not establish runtime image support for ARM64.
func TestBYOCPUInferenceIsArchitectureAgnostic(t *testing.T) {
	registerCPUValidationTestModels()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	oldBYO := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	oldVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldBYO
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldVLLM
	})
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	for _, tc := range []struct {
		name               string
		architectures      []string
		gpuResource        corev1.ResourceName
		aws                bool
		wantCPU, wantError bool
	}{
		{name: "amd64", architectures: []string{"amd64"}, wantCPU: true},
		{name: "arm64", architectures: []string{"arm64"}, wantCPU: true},
		{name: "missing architecture", architectures: []string{""}, wantCPU: true},
		{name: "mixed CPU architectures", architectures: []string{"amd64", "arm64", ""}, wantCPU: true},
		{name: "ARM NVIDIA GPU", architectures: []string{"arm64"}, gpuResource: consts.NvidiaGPU},
		{name: "ARM NVIDIA MIG", architectures: []string{"arm64"}, gpuResource: "nvidia.com/mig-1g.10gb"},
		{name: "mixed CPU GPU", architectures: []string{"amd64", "arm64"}, gpuResource: consts.NvidiaGPU, wantError: true},
		{name: "AWS ARM remains excluded", architectures: []string{"arm64"}, aws: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nodes []ctrlclient.Object
			for i, arch := range tc.architectures {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i), Labels: map[string]string{"pool": "cpu"}},
					Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				}
				if arch != "" {
					node.Labels[corev1.LabelArchStable] = arch
				}
				if tc.gpuResource != "" && i == len(tc.architectures)-1 {
					node.Status.Capacity = corev1.ResourceList{tc.gpuResource: resource.MustParse("1")}
				}
				if tc.aws {
					node.Spec.ProviderID = "aws:///zone/node"
				}
				nodes = append(nodes, node)
			}
			c := newCPUValidationClient(t, nodes...)
			ws := &Workspace{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationWorkspaceRuntime: "vllm"}},
				Resource:   ResourceSpec{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}},
				Inference:  &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}},
			}
			cpu, err := ResolveCPUInferenceWorkspace(t.Context(), ws, c)
			if cpu != tc.wantCPU || (err != nil) != tc.wantError {
				t.Fatalf("CPU resolution = %v, %v; want CPU=%v error=%v", cpu, err, tc.wantCPU, tc.wantError)
			}
			if err := ws.validateCPUInferenceConstraints(t.Context(), c); (err != nil) != tc.wantError {
				t.Fatalf("CPU constraints = %v; want error=%v", err, tc.wantError)
			}
			if tc.wantCPU {
				ws.Annotations[AnnotationWorkspaceRuntime] = "transformers"
				if err := ws.validateCPUInferenceConstraints(t.Context(), c); err == nil {
					t.Fatal("label-only CPU targets must still require vllm")
				}
			}
		})
	}
}

func TestResolveCPUInferenceTargetUncataloguedAzureAccelerators(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	old := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = old })
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	inference := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "facebook/opt-125m"}}}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "byo"}}

	for _, instanceType := range []string{"Standard_NC6s_v3", "standard_nc6s_v3", "Standard_ND999s_v99", "Standard_NV999s_v99"} {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mixed=%t", instanceType, mixed), func(t *testing.T) {
				gpuNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "gpu", Labels: map[string]string{"pool": "byo", corev1.LabelInstanceTypeStable: instanceType}},
					Spec:       corev1.NodeSpec{ProviderID: "azure:///gpu"},
					// Ready can precede NVIDIA device-plugin capacity and discovery labels.
					Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				}
				objects := []ctrlclient.Object{gpuNode}
				if mixed {
					cpuNode := gpuNode.DeepCopy()
					cpuNode.Name = "cpu"
					cpuNode.Spec.ProviderID = "azure:///cpu"
					cpuNode.Labels[corev1.LabelInstanceTypeStable] = "Standard_D8s_v5"
					objects = append(objects, cpuNode)
				}
				isCPU, err := ResolveCPUInferenceTarget(context.Background(), inference, "", selector, newCPUValidationClient(t, objects...))
				if isCPU {
					t.Fatal("Azure N-series node without discovery signals must not select CPU inference")
				}
				if mixed {
					if err == nil || !strings.Contains(err.Error(), "mix of CPU-only and GPU nodes") {
						t.Fatalf("expected mixed CPU/GPU selector error, got %v", err)
					}
				} else if err != nil {
					t.Fatalf("GPU-only selector should stay on the GPU path without error, got %v", err)
				}
			})
		}
	}
}
