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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestDetermineWorkspacePhase(t *testing.T) {
	tests := []struct {
		name     string
		ws       *kaitov1beta1.Workspace
		expected string
	}{
		{
			name: "workspace is deleting",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeDeleting),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "deleting",
		},
		{
			name: "workspace succeeded",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "succeeded",
		},
		{
			name: "workspace error",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expected: "error",
		},
		{
			name: "workspace pending with no conditions",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expected: "pending",
		},
		{
			name: "workspace pending with unknown condition",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
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
			name: "deleting takes precedence over succeeded",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeDeleting),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
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
			result := DetermineWorkspacePhase(tt.ws)
			assert.Equal(t, tt.expected, result)
		})
	}
}
