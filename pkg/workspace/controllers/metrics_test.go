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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestDeterminePhase(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       string
	}{
		{
			name: "deleting phase",
			conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeDeleting),
					Status: metav1.ConditionTrue,
				},
			},
			want: "deleting",
		},
		{
			name: "succeeded phase",
			conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
					Status: metav1.ConditionTrue,
				},
			},
			want: "succeeded",
		},
		{
			name: "error phase",
			conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
					Status: metav1.ConditionFalse,
				},
			},
			want: "error",
		},
		{
			name:       "pending phase - no conditions",
			conditions: []metav1.Condition{},
			want:       "pending",
		},
		{
			name: "pending phase - unknown condition",
			conditions: []metav1.Condition{
				{
					Type:   "UnknownCondition",
					Status: metav1.ConditionTrue,
				},
			},
			want: "pending",
		},
		{
			name: "deleting false doesn't return deleting",
			conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeDeleting),
					Status: metav1.ConditionFalse,
				},
			},
			want: "pending",
		},
		{
			name: "succeeded unknown status",
			conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
					Status: metav1.ConditionUnknown,
				},
			},
			want: "pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeterminePhase(tt.conditions)
			if got != tt.want {
				t.Errorf("DeterminePhase() = %v, want %v, description: %v", got, tt.want, tt.name)
			}
		})
	}
}
