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
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/custommodel"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
)

// reconcileResolvedModel resolves a bring-your-own model and records what it
// resolved to in status. It runs before sizing and provisioning so that both
// are driven by a model identity that has already been recorded.
func (c *WorkspaceReconciler) reconcileResolvedModel(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	record, err := custommodel.Reconcile(ctx, c.Client, wObj.Inference, wObj.Namespace,
		wObj.Status.ResolvedModel, wObj,
		workspaceModelStatusWriter{client: c.Client, key: client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}})
	if err != nil {
		return err
	}
	if record != nil {
		wObj.Status.ResolvedModel = record
	}
	return nil
}

// workspaceModelStatusWriter persists custom-model resolution outcomes onto a
// Workspace's status.
type workspaceModelStatusWriter struct {
	client client.Client
	key    client.ObjectKey
}

func (w workspaceModelStatusWriter) SetModelConfigCondition(ctx context.Context,
	status metav1.ConditionStatus, reason, message string,
) error {
	return workspace.UpdateWorkspaceStatus(ctx, w.client, &w.key,
		func(s *kaitov1beta1.WorkspaceStatus) error {
			meta.SetStatusCondition(&s.Conditions, metav1.Condition{
				Type:    string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady),
				Status:  status,
				Reason:  reason,
				Message: message,
			})
			return nil
		})
}

func (w workspaceModelStatusWriter) SaveResolved(ctx context.Context,
	record *kaitov1beta1.ResolvedModel, reason, message string,
) error {
	return workspace.UpdateWorkspaceStatus(ctx, w.client, &w.key,
		func(s *kaitov1beta1.WorkspaceStatus) error {
			s.ResolvedModel = record
			meta.SetStatusCondition(&s.Conditions, metav1.Condition{
				Type:    string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady),
				Status:  metav1.ConditionTrue,
				Reason:  reason,
				Message: message,
			})
			return nil
		})
}
