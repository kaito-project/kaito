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
)

func TestMultiRoleInference_SupportedVerbs(t *testing.T) {
	m := &MultiRoleInference{}
	got := m.SupportedVerbs()
	assert.Equal(t, []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}, got)
}

func TestMultiRoleInference_SetDefaults(t *testing.T) {
	tests := []struct {
		name             string
		mri              *MultiRoleInference
		expectedReplicas []int32
	}{
		{
			name: "replicas should default to 1 when not set",
			mri: &MultiRoleInference{
				Spec: MultiRoleInferenceSpec{
					Roles: []MultiRoleInferenceRoleSpec{
						{Type: MultiRoleInferenceRolePrefill, Replicas: 0, InstanceType: "Standard_NC24ads_A100_v4"},
						{Type: MultiRoleInferenceRoleDecode, Replicas: 0, InstanceType: "Standard_NC24ads_A100_v4"},
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
						{Type: MultiRoleInferenceRolePrefill, Replicas: 2, InstanceType: "Standard_NC24ads_A100_v4"},
						{Type: MultiRoleInferenceRoleDecode, Replicas: 3, InstanceType: "Standard_NC24ads_A100_v4"},
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
				assert.Equal(t, expected, tt.mri.Spec.Roles[i].Replicas)
			}
		})
	}
}

func TestMultiRoleInference_Validate(t *testing.T) {
	validMRI := func() *MultiRoleInference {
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
					{Type: MultiRoleInferenceRolePrefill, Replicas: 1, InstanceType: "Standard_NC24ads_A100_v4"},
					{Type: MultiRoleInferenceRoleDecode, Replicas: 1, InstanceType: "Standard_NC24ads_A100_v4"},
				},
			},
		}
	}

	tests := []struct {
		name    string
		mri     *MultiRoleInference
		wantErr bool
	}{
		{
			name:    "valid MultiRoleInference",
			mri:     validMRI(),
			wantErr: false,
		},
		{
			name: "invalid DNS1123 name",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Name = "Invalid-Name"
				return m
			}(),
			wantErr: true,
		},
		{
			name: "missing model name",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.Model.Name = ""
				return m
			}(),
			wantErr: true,
		},
		{
			name: "missing labelSelector",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.LabelSelector = nil
				return m
			}(),
			wantErr: true,
		},
		{
			name: "wrong number of roles",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.Roles = m.Spec.Roles[:1]
				return m
			}(),
			wantErr: true,
		},
		{
			name: "duplicate role types (two prefill)",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.Roles[1].Type = MultiRoleInferenceRolePrefill
				return m
			}(),
			wantErr: true,
		},
		{
			name: "invalid replicas value",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.Roles[0].Replicas = 0
				return m
			}(),
			wantErr: true,
		},
		{
			name: "empty instanceType",
			mri: func() *MultiRoleInference {
				m := validMRI()
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

func TestMultiRoleInference_validateUpdate(t *testing.T) {
	validMRI := func() *MultiRoleInference {
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
					{Type: MultiRoleInferenceRolePrefill, Replicas: 1, InstanceType: "Standard_NC24ads_A100_v4"},
					{Type: MultiRoleInferenceRoleDecode, Replicas: 1, InstanceType: "Standard_NC24ads_A100_v4"},
				},
			},
		}
	}

	tests := []struct {
		name    string
		mri     *MultiRoleInference
		old     *MultiRoleInference
		wantErr bool
	}{
		{
			name:    "valid update",
			mri:     validMRI(),
			old:     validMRI(),
			wantErr: false,
		},
		{
			name: "update with empty model name should fail",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.Model.Name = ""
				return m
			}(),
			old:     validMRI(),
			wantErr: true,
		},
		{
			name: "update with empty instanceType should fail",
			mri: func() *MultiRoleInference {
				m := validMRI()
				m.Spec.Roles[0].InstanceType = ""
				return m
			}(),
			old:     validMRI(),
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
