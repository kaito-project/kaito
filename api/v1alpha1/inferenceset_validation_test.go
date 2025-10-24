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
			},
			oldIS: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
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
	is := &InferenceSet{}
	err := is.validateCreate()
	assert.Nil(t, err)
}

func TestInferenceSet_validateUpdate(t *testing.T) {
	is := &InferenceSet{}
	old := &InferenceSet{}
	err := is.validateUpdate(old)
	assert.Nil(t, err)
}
