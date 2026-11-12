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
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

var (
	inferenceSetSelector, _ = metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: kaitov1beta1.WorkspaceCreatedByInferenceSetLabel, Operator: metav1.LabelSelectorOpExists},
		},
	})

	workspacePredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			workspace, ok := e.Object.(*kaitov1beta1.Workspace)
			if !ok {
				return false
			}
			if !inferenceSetSelector.Matches(labels.Set(workspace.GetLabels())) {
				return false
			}
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldWorkspace, ok := e.ObjectOld.(*kaitov1beta1.Workspace)
			if !ok {
				return false
			}

			newWorkspace, ok := e.ObjectNew.(*kaitov1beta1.Workspace)
			if !ok {
				return false
			}
			if !inferenceSetSelector.Matches(labels.Set(oldWorkspace.GetLabels())) {
				return false
			}

			if !inferenceSetSelector.Matches(labels.Set(newWorkspace.GetLabels())) {
				return false
			}

			oldWorkspaceCopy := oldWorkspace.DeepCopy()
			newWorkspaceCopy := newWorkspace.DeepCopy()

			oldWorkspaceCopy.ResourceVersion = ""
			newWorkspaceCopy.ResourceVersion = ""
			return !reflect.DeepEqual(oldWorkspaceCopy, newWorkspaceCopy)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			workspace, ok := e.Object.(*kaitov1beta1.Workspace)
			if !ok {
				return false
			}
			if !inferenceSetSelector.Matches(labels.Set(workspace.GetLabels())) {
				return false
			}
			return true
		},
	}
)
