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
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/custommodel"
	"github.com/kaito-project/kaito/pkg/utils/inferenceset"
)

// reconcileResolvedModel resolves a bring-your-own model and records what it
// resolved to in status, before any replica Workspace is created from it.
//
// Resolving here as well as in the Workspace controller is deliberate: the
// InferenceSet is where the model identity is fixed for the whole set, so a
// replaced ConfigMap must be caught before it can be propagated into new
// replicas that would then disagree with the existing ones.
func (c *InferenceSetReconciler) reconcileResolvedModel(ctx context.Context, iObj *kaitov1beta1.InferenceSet) error {
	record, err := custommodel.Reconcile(ctx, c.Client, &iObj.Spec.Template.Inference, iObj.Namespace,
		iObj.Status.ResolvedModel, iObj, inferenceSetModelStatusWriter{client: c.Client, obj: iObj})
	if err != nil {
		return err
	}
	if record != nil {
		iObj.Status.ResolvedModel = record
	}
	return nil
}

// inferenceSetModelStatusWriter persists custom-model resolution outcomes onto
// an InferenceSet's status.
type inferenceSetModelStatusWriter struct {
	client client.Client
	obj    *kaitov1beta1.InferenceSet
}

func (w inferenceSetModelStatusWriter) SetModelConfigCondition(ctx context.Context,
	status metav1.ConditionStatus, reason, message string,
) error {
	return inferenceset.UpdateStatusConditionIfNotMatch(ctx, w.client, w.obj,
		kaitov1beta1.WorkspaceConditionTypeModelConfigReady, status, reason, message)
}

func (w inferenceSetModelStatusWriter) SaveResolved(ctx context.Context,
	record *kaitov1beta1.ResolvedModel, reason, message string,
) error {
	return inferenceset.UpdateInferenceSetStatus(ctx, w.client,
		&client.ObjectKey{Name: w.obj.Name, Namespace: w.obj.Namespace},
		func(status *kaitov1beta1.InferenceSetStatus) error {
			status.ResolvedModel = record
			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type:    string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: message,
			})
			return nil
		})
}
