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

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestInferenceSetValidate(t *testing.T) {
	original := ValidateInferenceSetWorkspace
	t.Cleanup(func() { ValidateInferenceSetWorkspace = original })
	ValidateInferenceSetWorkspace = func(context.Context, *InferenceSet) *apis.FieldError { return nil }

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-is",
			Namespace: "default",
		},
		Spec: InferenceSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}},
		},
	}
	errs := is.Validate(context.Background())
	assert.Nil(t, errs)
}

func TestInferenceSetValidateFailsClosedWithoutWorkspaceValidation(t *testing.T) {
	original := ValidateInferenceSetWorkspace
	t.Cleanup(func() { ValidateInferenceSetWorkspace = original })
	ValidateInferenceSetWorkspace = nil

	is := &InferenceSet{ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"}}
	errs := is.Validate(context.Background())
	assert.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "child Workspace validation is not configured")
}

func TestInferenceSetValidateDeletingSkipsWorkspaceValidation(t *testing.T) {
	original := ValidateInferenceSetWorkspace
	t.Cleanup(func() { ValidateInferenceSetWorkspace = original })

	workspaceValidationCalled := false
	ValidateInferenceSetWorkspace = func(context.Context, *InferenceSet) *apis.FieldError {
		workspaceValidationCalled = true
		return apis.ErrGeneric("referenced ConfigMap not found", "inference", "config")
	}

	now := metav1.Now()
	old := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-is",
			Namespace:  "default",
			Finalizers: []string{"inferenceset.kaito.sh/finalizer"},
		},
	}
	deleting := old.DeepCopy()
	deleting.DeletionTimestamp = &now
	deleting.Finalizers = nil

	errs := deleting.Validate(apis.WithinUpdate(context.Background(), old))
	assert.Nil(t, errs)
	assert.False(t, workspaceValidationCalled)
}

func TestInferenceSetSetDefaults(t *testing.T) {
	is := &InferenceSet{}
	is.SetDefaults(context.Background())
}

func TestInferenceSetSupportedVerbs(t *testing.T) {
	is := &InferenceSet{}
	verbs := is.SupportedVerbs()
	assert.Len(t, verbs, 2)
}

func TestValidateInferenceSetMaintenanceWindow(t *testing.T) {
	// nil autoUpgrade
	errs := validateInferenceSetMaintenanceWindow(nil)
	assert.Nil(t, errs)

	// valid window
	errs = validateInferenceSetMaintenanceWindow(&AutoUpgradePolicy{
		MaintenanceWindow: &MaintenanceWindow{
			Schedule: "0 2 * * 6",
		},
	})
	assert.Nil(t, errs)
}

func TestInferenceSetBenchmarkHelpers(t *testing.T) {
	oldCPUInferenceGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldCPUInferenceGate
	}()
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
		Spec: InferenceSetSpec{
			Template: InferenceSetTemplate{
				Resource: InferenceSetResourceSpec{InstanceType: "Standard_NC24ads_A100_v4"},
				Inference: InferenceSpec{
					Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}},
				},
			},
		},
	}
	assert.Equal(t, model.RuntimeNameVLLM, GetInferenceSetRuntimeName(is))
	assert.True(t, IsInferenceSetBenchmarkEnabled(is))
	assert.True(t, ShouldRunInferenceSetBenchmark(is))

	is.Spec.Template.Resource.InstanceType = "Standard_D8s_v5"
	assert.False(t, ShouldRunInferenceSetBenchmark(is), "basic CPU inference should skip parent benchmark applicability")

	is.Annotations[AnnotationDisableBenchmark] = "true"
	assert.False(t, ShouldRunInferenceSetBenchmark(is))
}

func TestInferenceSetValidatePassesBaselineToWorkspaceValidation(t *testing.T) {
	original := ValidateInferenceSetWorkspace
	t.Cleanup(func() { ValidateInferenceSetWorkspace = original })

	var gotBaseline any
	calls := 0
	ValidateInferenceSetWorkspace = func(ctx context.Context, _ *InferenceSet) *apis.FieldError {
		calls++
		gotBaseline = apis.GetBaseline(ctx)
		return nil
	}

	old := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
		Spec: InferenceSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}},
		},
	}
	updated := old.DeepCopy()

	assert.Nil(t, updated.Validate(context.Background()))
	assert.Equal(t, 1, calls)
	assert.Nil(t, gotBaseline)

	assert.Nil(t, updated.Validate(apis.WithinUpdate(context.Background(), old)))
	assert.Equal(t, 2, calls)
	assert.Same(t, old, gotBaseline)
}

func TestInferenceSetValidateSelector(t *testing.T) {
	original := ValidateInferenceSetWorkspace
	t.Cleanup(func() { ValidateInferenceSetWorkspace = original })
	ValidateInferenceSetWorkspace = func(context.Context, *InferenceSet) *apis.FieldError { return nil }

	newIS := func(selector *metav1.LabelSelector, templateLabels map[string]string) *InferenceSet {
		return &InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
			Spec: InferenceSetSpec{
				Selector: selector,
				Template: InferenceSetTemplate{ObjectMeta: metav1.ObjectMeta{Labels: templateLabels}},
			},
		}
	}
	appIn := func(values ...string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: values},
		}}
	}

	tests := []struct {
		name    string
		old     *InferenceSet
		is      *InferenceSet
		wantErr string
	}{
		{
			name:    "nil selector is rejected on create",
			is:      newIS(nil, nil),
			wantErr: "missing field(s): spec.labelSelector",
		},
		{
			name:    "empty selector is rejected on create",
			is:      newIS(&metav1.LabelSelector{}, nil),
			wantErr: "labelSelector must not be empty",
		},
		{
			name: "matchLabels without template labels is accepted",
			is:   newIS(&metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}}, nil),
		},
		{
			name:    "template label conflicting with matchLabels is rejected",
			is:      newIS(&metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}}, map[string]string{"app": "other"}),
			wantErr: "spec.template.metadata.labels[app]",
		},
		{
			name: "matchExpressions are not checked against template labels",
			is:   newIS(appIn("llm"), nil),
		},
		{
			name: "existing empty selector remains updatable",
			old:  newIS(&metav1.LabelSelector{}, nil),
			is:   newIS(&metav1.LabelSelector{}, nil),
		},
		{
			name: "template labels are not rechecked on update",
			old:  newIS(&metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}}, nil),
			is:   newIS(&metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}}, map[string]string{"app": "other"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.old != nil {
				ctx = apis.WithinUpdate(ctx, tt.old)
			}
			errs := tt.is.Validate(ctx)
			if tt.wantErr == "" {
				assert.Nil(t, errs)
				return
			}
			assert.NotNil(t, errs)
			assert.Contains(t, errs.Error(), tt.wantErr)
		})
	}
}

func TestResolveInferenceSetBenchmarkApplicability_BYOCPUPresetSkipsParentBenchmark(t *testing.T) {
	oldCPU := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldCPU
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldNAP
	}()
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

	scheme := runtime.NewScheme()
	assert.NoError(t, corev1.AddToScheme(scheme))
	nodeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cpu-node",
			Labels: map[string]string{
				"kaito.sh/cpu-inference": "true",
				corev1.LabelArchStable:   "amd64",
			},
		},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}).Build()

	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
		Spec: InferenceSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "workspaces"}},
			Template: InferenceSetTemplate{
				Resource:  InferenceSetResourceSpec{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}}},
				Inference: InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}},
			},
		},
	}

	benchmarkApplicable, err := ResolveInferenceSetBenchmarkApplicability(context.Background(), is, nodeClient)
	assert.NoError(t, err)
	assert.False(t, benchmarkApplicable, "BYO CPU presets with empty instanceType should skip parent benchmark applicability")
}

func TestInferenceSetMIGImmutable(t *testing.T) {
	makeIS := func(profile string) *InferenceSet {
		var p *PartitionSpec
		if profile != "" {
			p = &PartitionSpec{Mode: PartitionModeMIG, Profile: profile}
		}
		return &InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
			Spec: InferenceSetSpec{
				Template: InferenceSetTemplate{
					Resource: InferenceSetResourceSpec{Partition: p},
				},
			},
		}
	}

	// Unchanged partition is allowed.
	errs := makeIS("1g.10gb").validateUpdate(makeIS("1g.10gb"))
	assert.Nil(t, errs)

	// Changing the profile is rejected.
	errs = makeIS("2g.20gb").validateUpdate(makeIS("1g.10gb"))
	assert.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "field is immutable")

	// Adding a partition to a non-partitioned set is rejected.
	errs = makeIS("1g.10gb").validateUpdate(makeIS(""))
	assert.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "field is immutable")
}
