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

package inferenceset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

func TestDetermineInferenceSetPhase(t *testing.T) {
	tests := []struct {
		name     string
		is       *kaitov1alpha1.InferenceSet
		expected string
	}{
		{
			name: "inferenceset is deleting",
			is: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1alpha1.InferenceSetConditionTypeDeleting),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "deleting",
		},
		{
			name: "inferenceset ready",
			is: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1alpha1.InferenceSetConditionTypeReady),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "ready",
		},
		{
			name: "inferenceset error",
			is: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1alpha1.InferenceSetConditionTypeReady),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expected: "error",
		},
		{
			name: "inferenceset pending with no conditions",
			is: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expected: "pending",
		},
		{
			name: "inferenceset pending with unknown condition",
			is: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "UnknownCondition",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "pending",
		},
		{
			name: "deleting takes precedence over ready",
			is: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1alpha1.InferenceSetConditionTypeDeleting),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1alpha1.InferenceSetConditionTypeReady),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "deleting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineInferenceSetPhase(tt.is)
			assert.Equal(t, tt.expected, result)
		})
	}
}
