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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// DetermineWorkspacePhase returns the phase of a Workspace based on its conditions.
// Used by the InferenceSet controller to check workspace readiness.
func DetermineWorkspacePhase(ws *kaitov1beta1.Workspace) string {
	for _, cond := range ws.Status.Conditions {
		switch kaitov1beta1.ConditionType(cond.Type) {
		case kaitov1beta1.WorkspaceConditionTypeDeleting:
			if cond.Status == metav1.ConditionTrue {
				return "deleting"
			}
		case kaitov1beta1.WorkspaceConditionTypeSucceeded:
			if cond.Status == metav1.ConditionTrue {
				return "succeeded"
			}
			if cond.Status == metav1.ConditionFalse {
				return "error"
			}
		}
	}
	return "pending"
}
