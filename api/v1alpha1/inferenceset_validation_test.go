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

	"github.com/stretchr/testify/assert"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestInferenceSet_SupportedVerbs(t *testing.T) {
	tests := []struct {
		name     string
		expected []admissionregistrationv1.OperationType
	}{
		{
			name: "should return Create and Update operations",
			expected: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := &InferenceSet{}
			got := is.SupportedVerbs()
			assert.Equal(t, tt.expected, got)
			assert.Len(t, got, 2)
			assert.Contains(t, got, admissionregistrationv1.Create)
			assert.Contains(t, got, admissionregistrationv1.Update)
		})
	}
}

func ptrInt32(i int32) *int32 { return &i }
func TestInferenceSet_SetDefaults(t *testing.T) {
	tests := []struct {
		name            string
		inferenceset    *InferenceSet
		expectedReplica *int32
	}{
		{
			name: "explicit replicas 0 is preserved by SetDefaults (scale-to-zero)",
			inferenceset: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-inferenceset",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					// Explicit 0 should not be overridden by SetDefaults.
					// The CRD schema default handles the truly-omitted case at admission time.
					Replicas: ptrInt32(0),
				},
			},
			expectedReplica: ptrInt32(0),
		},
		{
			name: "replicas should not change when already set",
			inferenceset: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-inferenceset",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(3),
				},
			},
			expectedReplica: ptrInt32(3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tt.inferenceset.SetDefaults(ctx)
			assert.Equal(t, tt.expectedReplica, tt.inferenceset.Spec.Replicas)
		})
	}
}

func TestInferenceSet_Validate(t *testing.T) {
	tests := []struct {
		name        string
		inferencSet *InferenceSet
		oldIS       *InferenceSet
		wantErr     bool
		errField    string
	}{
		{
			name: "valid DNS1123 label name on create",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			oldIS:   nil,
			wantErr: false,
		},
		{
			name: "invalid DNS1123 label name with uppercase",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Invalid-Name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "name",
		},
		{
			name: "invalid DNS1123 label name too long",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "this-is-a-very-long-name-that-exceeds-the-maximum-allowed-length-for-dns",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "name",
		},
		{
			name: "invalid DNS1123 label name with special chars",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid_name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "name",
		},
		{
			name: "valid DNS1123 label name on update",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			oldIS: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			wantErr: false,
		},
		{
			name: "valid replicas value 0 (scale-to-zero)",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(0),
				},
			},
			oldIS:   nil,
			wantErr: false,
		},
		{
			name: "invalid replicas negative value",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(-1),
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.oldIS != nil {
				ctx = apis.WithinUpdate(ctx, tt.oldIS)
			}
			err := tt.inferencSet.Validate(ctx)
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestInferenceSet_validateCreate(t *testing.T) {
	tests := []struct {
		name     string
		is       *InferenceSet
		wantErr  bool
		errField string
	}{
		{
			name: "valid replicas value",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(1),
				},
			},
			wantErr: false,
		},
		{
			name: "valid replicas value 0 (scale-to-zero)",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(0),
				},
			},
			wantErr: false,
		},
		{
			name: "valid replicas value greater than 1",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(5),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid negative replicas value",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: ptrInt32(-1),
				},
			},
			wantErr:  true,
			errField: "replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.is.validateCreate()
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestInferenceSet_validateUpdate(t *testing.T) {
	is := &InferenceSet{}
	old := &InferenceSet{}
	err := is.validateUpdate(old)
	assert.Nil(t, err)
}

func TestInferenceSet_validateSpeculativeDecoding(t *testing.T) {
	presetTpl := func(name string) InferenceSetTemplate {
		return InferenceSetTemplate{
			Inference: kaitov1beta1.InferenceSpec{Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: kaitov1beta1.ModelName(name)}}},
		}
	}
	tests := []struct {
		name        string
		annotations map[string]string
		template    InferenceSetTemplate
		wantErr     bool
	}{
		{
			name:        "annotation absent - pass",
			annotations: nil,
			template:    presetTpl("deepseek-r1-0528"),
			wantErr:     false,
		},
		{
			name:        "annotation false - pass",
			annotations: map[string]string{AnnotationEnableSpeculativeDecoding: "false"},
			template:    presetTpl("deepseek-r1-0528"),
			wantErr:     false,
		},
		{
			name:        "invalid annotation value - rejected",
			annotations: map[string]string{AnnotationEnableSpeculativeDecoding: "yes"},
			template:    presetTpl("deepseek-r1-0528"),
			wantErr:     true,
		},
		{
			name:        "true with supported preset - pass",
			annotations: map[string]string{AnnotationEnableSpeculativeDecoding: "true"},
			template:    presetTpl("deepseek-r1-0528"),
			wantErr:     false,
		},
		{
			name:        "true with unsupported preset - accepted (ngram fallback)",
			annotations: map[string]string{AnnotationEnableSpeculativeDecoding: "true"},
			template:    presetTpl("llama-3.1-8b-instruct"),
			wantErr:     false,
		},
		{
			name:        "true without preset - rejected",
			annotations: map[string]string{AnnotationEnableSpeculativeDecoding: "true"},
			template:    InferenceSetTemplate{},
			wantErr:     true,
		},
		{
			name: "true with non-vllm runtime annotation - rejected",
			annotations: map[string]string{
				AnnotationEnableSpeculativeDecoding: "true",
				AnnotationWorkspaceRuntime:          "transformers",
			},
			template: presetTpl("deepseek-r1-0528"),
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tpl := tc.template
			tpl.Annotations = tc.annotations
			is := &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "is", Namespace: "default"},
				Spec:       InferenceSetSpec{Template: tpl},
			}
			err := is.validateSpeculativeDecoding()
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSpeculativeDecoding() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}

	t.Run("vllm feature gate disabled - rejected", func(t *testing.T) {
		orig := featuregates.FeatureGates[consts.FeatureFlagVLLM]
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = false
		defer func() { featuregates.FeatureGates[consts.FeatureFlagVLLM] = orig }()

		tpl := presetTpl("deepseek-r1-0528")
		tpl.Annotations = map[string]string{AnnotationEnableSpeculativeDecoding: "true"}
		is := &InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "is", Namespace: "default"},
			Spec:       InferenceSetSpec{Template: tpl},
		}
		if err := is.validateSpeculativeDecoding(); err == nil {
			t.Fatalf("expected rejection when vLLM feature gate is disabled")
		}
	})
}

func TestValidateMaintenanceWindow(t *testing.T) {
	tests := []struct {
		name    string
		policy  *AutoUpgradePolicy
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil policy - no error",
			policy:  nil,
			wantErr: false,
		},
		{
			name: "nil maintenance window - no error",
			policy: &AutoUpgradePolicy{
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "valid cron schedule without duration",
			policy: &AutoUpgradePolicy{
				Enabled: true,
				MaintenanceWindow: &MaintenanceWindow{
					Schedule: "0 2 * * 6",
				},
			},
			wantErr: false,
		},
		{
			name: "valid cron schedule with valid duration",
			policy: &AutoUpgradePolicy{
				Enabled: true,
				MaintenanceWindow: &MaintenanceWindow{
					Schedule: "0 0 * * *",
					Duration: &metav1.Duration{Duration: 2 * 3600000000000}, // 2h
				},
			},
			wantErr: false,
		},
		{
			name: "empty schedule",
			policy: &AutoUpgradePolicy{
				Enabled: true,
				MaintenanceWindow: &MaintenanceWindow{
					Schedule: "",
				},
			},
			wantErr: true,
			errMsg:  "schedule",
		},
		{
			name: "invalid cron expression",
			policy: &AutoUpgradePolicy{
				Enabled: true,
				MaintenanceWindow: &MaintenanceWindow{
					Schedule: "not-a-cron",
				},
			},
			wantErr: true,
			errMsg:  "schedule",
		},
		{
			name: "negative duration",
			policy: &AutoUpgradePolicy{
				Enabled: true,
				MaintenanceWindow: &MaintenanceWindow{
					Schedule: "0 2 * * 6",
					Duration: &metav1.Duration{Duration: -1 * 3600000000000}, // -1h
				},
			},
			wantErr: true,
			errMsg:  "duration",
		},
		{
			name: "zero duration",
			policy: &AutoUpgradePolicy{
				Enabled: true,
				MaintenanceWindow: &MaintenanceWindow{
					Schedule: "0 2 * * 6",
					Duration: &metav1.Duration{Duration: 0},
				},
			},
			wantErr: true,
			errMsg:  "duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMaintenanceWindow(tt.policy)
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestInferenceSet_validateInstanceType(t *testing.T) {
	tests := []struct {
		name            string
		nodeProvisioner string
		instanceType    string
		wantErr         bool
		errField        string
	}{
		{
			name:            "BYO mode with empty instanceType - valid",
			nodeProvisioner: consts.NodeProvisionerBYO,
			instanceType:    "",
			wantErr:         false,
		},
		{
			name:            "BYO mode with instanceType set - invalid",
			nodeProvisioner: consts.NodeProvisionerBYO,
			instanceType:    "Standard_NC6s_v3",
			wantErr:         true,
			errField:        "resource.instanceType",
		},
		{
			name:            "Karpenter mode with instanceType set - valid",
			nodeProvisioner: consts.NodeProvisionerKarpenter,
			instanceType:    "Standard_NC6s_v3",
			wantErr:         false,
		},
		{
			name:            "Karpenter mode with empty instanceType - invalid",
			nodeProvisioner: consts.NodeProvisionerKarpenter,
			instanceType:    "",
			wantErr:         true,
			errField:        "resource.instanceType",
		},
		{
			name:            "AzureGPU mode with instanceType set - valid",
			nodeProvisioner: consts.NodeProvisionerAzureGPU,
			instanceType:    "Standard_NC6s_v3",
			wantErr:         false,
		},
		{
			name:            "AzureGPU mode with empty instanceType - invalid",
			nodeProvisioner: consts.NodeProvisionerAzureGPU,
			instanceType:    "",
			wantErr:         true,
			errField:        "resource.instanceType",
		},
		{
			name:            "Unknown provisioner with instanceType - no error",
			nodeProvisioner: "",
			instanceType:    "Standard_NC6s_v3",
			wantErr:         false,
		},
		{
			name:            "Unknown provisioner without instanceType - no error",
			nodeProvisioner: "",
			instanceType:    "",
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore global state
			orig := consts.ActiveNodeProvisioner
			consts.ActiveNodeProvisioner = tt.nodeProvisioner
			defer func() { consts.ActiveNodeProvisioner = orig }()

			is := &InferenceSet{
				Spec: InferenceSetSpec{
					Template: InferenceSetTemplate{
						Resource: InferenceSetResourceSpec{
							InstanceType: tt.instanceType,
						},
					},
				},
			}
			err := is.validateInstanceType()
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
