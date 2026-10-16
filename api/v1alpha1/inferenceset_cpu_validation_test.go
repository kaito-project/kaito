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
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

type cpuInferenceSetTrackingClient struct {
	ctrlclient.Client
	listCalled *bool
	listErr    error
}

func (c *cpuInferenceSetTrackingClient) List(ctx context.Context, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
	if c.listCalled != nil {
		*c.listCalled = true
	}
	if c.listErr != nil {
		return c.listErr
	}
	return c.Client.List(ctx, list, opts...)
}

type cpuInferenceSetTestModel struct {
	name        string
	distributed bool
}

func (m *cpuInferenceSetTestModel) GetInferenceParameters() *model.PresetParam {
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

func (m *cpuInferenceSetTestModel) GetTuningParameters() *model.PresetParam {
	return &model.PresetParam{ReadinessTimeout: 30 * time.Minute}
}

func (m *cpuInferenceSetTestModel) SupportDistributedInference() bool { return m.distributed }
func (m *cpuInferenceSetTestModel) SupportTuning() bool               { return true }

func registerCPUInferenceSetTestModels() {
	plugin.KaitoModelRegister.Register(&plugin.Registration{Name: "test-model", Instance: &cpuInferenceSetTestModel{name: "test-model"}})
	plugin.KaitoModelRegister.Register(&plugin.Registration{Name: "test-distributed-model", Instance: &cpuInferenceSetTestModel{name: "test-distributed-model", distributed: true}})
}

func newCPUInferenceSetClient(t *testing.T, objects ...ctrlclient.Object) ctrlclient.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestInferenceSetValidateCreateCPUFeatureGate(t *testing.T) {
	registerCPUInferenceSetTestModels()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Template: InferenceSetTemplate{
				Inference: kaitoInferenceSpecForCPU(),
			},
		},
	}

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = false
	is.Annotations = map[string]string{AnnotationWorkspaceRuntime: "invalid-runtime"}
	if err := is.validateCreateWithContext(context.Background()); err != nil {
		t.Fatalf("expected cpu-specific validation to be skipped when feature gate is disabled, got %v", err)
	}

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	is.Annotations = nil
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
	registerCPUInferenceSetTestModels()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: int32Ptr(1),
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
	registerCPUInferenceSetTestModels()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldDisableNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldDisableNAP
	}()

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

	nodeClient := newCPUInferenceSetClient(t, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{"kaito.sh/cpu-inference": "true"}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	})
	k8sclient.SetGlobalClient(nodeClient)

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-preset-byo-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: int32Ptr(1),
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
	registerCPUInferenceSetTestModels()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-generic-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: int32Ptr(1),
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

func TestInferenceSetValidateCPUInferenceConstraints_SkipsNodeLookupForNonCPUPaths(t *testing.T) {
	registerCPUInferenceSetTestModels()
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldDisableNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldDisableNAP
	}()

	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

	listCalled := false
	trackingClient := &cpuInferenceSetTrackingClient{
		Client:     newCPUInferenceSetClient(t),
		listCalled: &listCalled,
		listErr:    errors.New("node lookup should not happen for non-cpu paths"),
	}
	k8sclient.SetGlobalClient(trackingClient)

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "non-cpu-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
			Template: InferenceSetTemplate{
				Inference: kaitov1beta1.InferenceSpec{
					Template: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name:  "mig",
						Image: "example.com/mig:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1")},
							Limits:   corev1.ResourceList{corev1.ResourceName("nvidia.com/mig-1g.10gb"): resource.MustParse("1")},
						},
					}}}},
				},
			},
		},
	}

	if err := is.validateCPUInferenceConstraints(context.Background()); err != nil {
		t.Fatalf("expected non-cpu mig path to skip cpu node lookup, got %v", err)
	}
	if listCalled {
		t.Fatalf("expected non-cpu path to skip node lookup")
	}
}
