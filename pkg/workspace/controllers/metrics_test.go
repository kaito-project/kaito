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
