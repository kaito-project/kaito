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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func int32Ptr(v int32) *int32 { return &v }

func newValidMRI() *MultiRoleInference {
	return &MultiRoleInference{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-name",
			Namespace: "default",
		},
		Spec: MultiRoleInferenceSpec{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Model: MultiRoleInferenceModelSpec{Name: "deepseek-ai/DeepSeek-V3"},
			Roles: []MultiRoleInferenceRoleSpec{
				{Type: MultiRoleInferenceRolePrefill, Replicas: int32Ptr(1), InstanceType: "Standard_NC24ads_A100_v4"},
				{Type: MultiRoleInferenceRoleDecode, Replicas: int32Ptr(1), InstanceType: "Standard_NC24ads_A100_v4"},
			},
		},
	}
}

func requireInstanceTypeChildValidation(_ context.Context, m *MultiRoleInference) *apis.FieldError {
	for i, role := range m.Spec.Roles {
		if role.InstanceType == "" {
			return apis.ErrMissingField(fmt.Sprintf("spec.roles[%d].instanceType", i))
		}
	}
	return nil
}

func setChildValidationStub(t *testing.T, fn func(context.Context, *MultiRoleInference) *apis.FieldError) {
	orig := ValidateMultiRoleInferenceChildren
	ValidateMultiRoleInferenceChildren = fn
	t.Cleanup(func() { ValidateMultiRoleInferenceChildren = orig })
}

func TestMultiRoleInference_SupportedVerbs(t *testing.T) {
	m := &MultiRoleInference{}
	got := m.SupportedVerbs()
	assert.Equal(t, []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}, got)
}

func TestMultiRoleInference_SetDefaults(t *testing.T) {
	two := int32(2)
	three := int32(3)
	tests := []struct {
		name             string
		mri              *MultiRoleInference
		expectedReplicas []int32
	}{
		{
			name: "replicas should default to 1 when nil",
			mri: &MultiRoleInference{
				Spec: MultiRoleInferenceSpec{
					Roles: []MultiRoleInferenceRoleSpec{
						{Type: MultiRoleInferenceRolePrefill, Replicas: nil, InstanceType: "Standard_NC24ads_A100_v4"},
						{Type: MultiRoleInferenceRoleDecode, Replicas: nil, InstanceType: "Standard_NC24ads_A100_v4"},
					},
				},
			},
			expectedReplicas: []int32{1, 1},
		},
		{
			name: "replicas should not change when already set",
			mri: &MultiRoleInference{
				Spec: MultiRoleInferenceSpec{
					Roles: []MultiRoleInferenceRoleSpec{
						{Type: MultiRoleInferenceRolePrefill, Replicas: &two, InstanceType: "Standard_NC24ads_A100_v4"},
						{Type: MultiRoleInferenceRoleDecode, Replicas: &three, InstanceType: "Standard_NC24ads_A100_v4"},
					},
				},
			},
			expectedReplicas: []int32{2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mri.SetDefaults(context.Background())
			for i, expected := range tt.expectedReplicas {
				assert.Equal(t, expected, *tt.mri.Spec.Roles[i].Replicas)
			}
		})
	}
}

func TestMultiRoleInference_Validate(t *testing.T) {
	orig := consts.ActiveNodeProvisioner
	consts.ActiveNodeProvisioner = consts.NodeProvisionerKarpenter
	defer func() { consts.ActiveNodeProvisioner = orig }()
	setChildValidationStub(t, requireInstanceTypeChildValidation)

	tests := []struct {
		name    string
		mri     *MultiRoleInference
		wantErr bool
	}{
		{
			name:    "valid MultiRoleInference",
			mri:     newValidMRI(),
			wantErr: false,
		},
		{
			name: "invalid DNS1123 name",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Name = "Invalid-Name"
				return m
			}(),
			wantErr: true,
		},
		{
			name: "name at the maximum length",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Name = strings.Repeat("a", 55)
				return m
			}(),
			wantErr: false,
		},
		{
			name: "name too long for child InferenceSet names",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Name = strings.Repeat("a", 56)
				return m
			}(),
			wantErr: true,
		},
		{
			name: "missing model name",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Model.Name = ""
				return m
			}(),
			wantErr: true,
		},
		{
			name: "missing labelSelector",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.LabelSelector = nil
				return m
			}(),
			wantErr: true,
		},
		{
			name: "empty labelSelector",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.LabelSelector = &metav1.LabelSelector{}
				return m
			}(),
			wantErr: true,
		},
		{
			name: "wrong number of roles",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Roles = m.Spec.Roles[:1]
				return m
			}(),
			wantErr: true,
		},
		{
			name: "duplicate role types (two prefill)",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Roles[1].Type = MultiRoleInferenceRolePrefill
				return m
			}(),
			wantErr: true,
		},
		{
			name: "invalid replicas value",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Roles[0].Replicas = int32Ptr(0)
				return m
			}(),
			wantErr: true,
		},
		{
			name: "empty instanceType",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Roles[0].InstanceType = ""
				return m
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mri.Validate(context.Background())
			if tt.wantErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestMultiRoleInference_Validate_BYOAllowsInstanceType(t *testing.T) {
	orig := consts.ActiveNodeProvisioner
	consts.ActiveNodeProvisioner = consts.NodeProvisionerBYO
	defer func() { consts.ActiveNodeProvisioner = orig }()
	setChildValidationStub(t, func(context.Context, *MultiRoleInference) *apis.FieldError { return nil })

	assert.Nil(t, newValidMRI().Validate(context.Background()))
}

func TestMultiRoleInference_validateUpdate(t *testing.T) {
	orig := consts.ActiveNodeProvisioner
	consts.ActiveNodeProvisioner = consts.NodeProvisionerKarpenter
	defer func() { consts.ActiveNodeProvisioner = orig }()
	setChildValidationStub(t, requireInstanceTypeChildValidation)

	tests := []struct {
		name    string
		mri     *MultiRoleInference
		old     *MultiRoleInference
		wantErr bool
	}{
		{
			name:    "valid update",
			mri:     newValidMRI(),
			old:     newValidMRI(),
			wantErr: false,
		},
		{
			name: "update with empty model name should fail",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Model.Name = ""
				return m
			}(),
			old:     newValidMRI(),
			wantErr: true,
		},
		{
			name: "update with empty instanceType should fail",
			mri: func() *MultiRoleInference {
				m := newValidMRI()
				m.Spec.Roles[0].InstanceType = ""
				return m
			}(),
			old:     newValidMRI(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := apis.WithinUpdate(context.Background(), tt.old)
			err := tt.mri.Validate(ctx)
			if tt.wantErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestMultiRoleInferenceValidateFailsClosedWithoutChildValidation(t *testing.T) {
	orig := ValidateMultiRoleInferenceChildren
	t.Cleanup(func() { ValidateMultiRoleInferenceChildren = orig })
	ValidateMultiRoleInferenceChildren = nil

	err := newValidMRI().Validate(context.Background())
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "child InferenceSet validation is not configured")
}

func TestMultiRoleInferenceValidatePassesBaselineToChildValidation(t *testing.T) {
	var gotBaseline any
	calls := 0
	setChildValidationStub(t, func(ctx context.Context, _ *MultiRoleInference) *apis.FieldError {
		calls++
		gotBaseline = apis.GetBaseline(ctx)
		return nil
	})

	old := newValidMRI()
	updated := newValidMRI()

	assert.Nil(t, updated.Validate(context.Background()))
	assert.Equal(t, 1, calls)
	assert.Nil(t, gotBaseline)

	assert.Nil(t, updated.Validate(apis.WithinUpdate(context.Background(), old)))
	assert.Equal(t, 2, calls)
	assert.Same(t, old, gotBaseline)
}

func TestMultiRoleInferenceValidateDeletingSkipsChildValidation(t *testing.T) {
	called := false
	setChildValidationStub(t, func(context.Context, *MultiRoleInference) *apis.FieldError {
		called = true
		return apis.ErrGeneric("referenced ConfigMap not found", "spec", "roles[0]")
	})

	now := metav1.Now()
	old := newValidMRI()
	deleting := old.DeepCopy()
	deleting.DeletionTimestamp = &now

	err := deleting.Validate(apis.WithinUpdate(context.Background(), old))
	assert.Nil(t, err)
	assert.False(t, called)
}

func TestMultiRoleInferenceValidateSpeculativeDecoding(t *testing.T) {
	newMRI := func(annotations map[string]string, modelName string) *MultiRoleInference {
		return &MultiRoleInference{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "mri",
				Namespace:   "default",
				Annotations: annotations,
			},
			Spec: MultiRoleInferenceSpec{
				Model: MultiRoleInferenceModelSpec{Name: modelName},
			},
		}
	}
	tests := []struct {
		name        string
		annotations map[string]string
		modelName   string
		wantErr     bool
	}{
		{"no annotation - pass", nil, "deepseek-r1-0528", false},
		{"false - pass", map[string]string{AnnotationEnableSpeculativeDecoding: "false"}, "deepseek-r1-0528", false},
		{"invalid value - rejected", map[string]string{AnnotationEnableSpeculativeDecoding: "yes"}, "deepseek-r1-0528", true},
		{"true supported preset - pass", map[string]string{AnnotationEnableSpeculativeDecoding: "true"}, "deepseek-r1-0528", false},
		{"true second supported preset - pass", map[string]string{AnnotationEnableSpeculativeDecoding: "true"}, "deepseek-v3-0324", false},
		{"true unsupported preset - accepted (ngram fallback)", map[string]string{AnnotationEnableSpeculativeDecoding: "true"}, "llama-3.1-8b-instruct", false},
		{"true empty model - rejected", map[string]string{AnnotationEnableSpeculativeDecoding: "true"}, "", true},
		{
			name: "true with non-vllm runtime - rejected",
			annotations: map[string]string{
				AnnotationEnableSpeculativeDecoding: "true",
				AnnotationWorkspaceRuntime:          "transformers",
			},
			modelName: "deepseek-r1-0528",
			wantErr:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newMRI(tc.annotations, tc.modelName)
			errs := m.validateSpeculativeDecoding()
			if (errs != nil) != tc.wantErr {
				t.Errorf("validateSpeculativeDecoding() err=%v wantErr=%v", errs, tc.wantErr)
			}
		})
	}

	// Feature-gate-off case: with the vLLM feature gate disabled,
	// EffectiveInferenceRuntime resolves to HuggingFace regardless of the
	// annotation, so the MRI-level spec-decoding opt-in must be rejected.
	t.Run("vllm feature gate disabled - rejected", func(t *testing.T) {
		orig := featuregates.FeatureGates[consts.FeatureFlagVLLM]
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = false
		defer func() { featuregates.FeatureGates[consts.FeatureFlagVLLM] = orig }()

		m := newMRI(
			map[string]string{AnnotationEnableSpeculativeDecoding: "true"},
			"deepseek-r1-0528",
		)
		if errs := m.validateSpeculativeDecoding(); errs == nil {
			t.Fatalf("expected rejection when vLLM feature gate is disabled")
		}
	})
}
