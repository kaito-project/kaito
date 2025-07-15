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

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

// garbageCollectWorkspace remove finalizer associated with workspace object.
func (c *WorkspaceReconciler) garbageCollectWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (ctrl.Result, error) {
	klog.InfoS("garbageCollectWorkspace", "workspace", klog.KObj(wObj))

	// Check if there are any nodeClaims associated with this workspace.
	ncList, err := nodeclaim.ListNodeClaim(ctx, wObj, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// We should delete all the nodeClaims that are created by this workspace
	for i := range ncList.Items {
		if ncList.Items[i].DeletionTimestamp.IsZero() {
			klog.InfoS("Deleting associated NodeClaim...", "nodeClaim", ncList.Items[i].Name)
			if deleteErr := c.Delete(ctx, &ncList.Items[i], &client.DeleteOptions{}); deleteErr != nil {
				klog.ErrorS(deleteErr, "failed to delete the nodeClaim", "nodeClaim", klog.KObj(&ncList.Items[i]))
				return ctrl.Result{}, deleteErr
			}
		}
	}

	if controllerutil.RemoveFinalizer(wObj, consts.WorkspaceFinalizer) {
		if updateErr := c.Update(ctx, wObj, &client.UpdateOptions{}); updateErr != nil {
			klog.ErrorS(updateErr, "failed to remove the finalizer from the workspace",
				"workspace", klog.KObj(wObj))
			return ctrl.Result{}, updateErr
		}
		klog.InfoS("successfully removed the workspace finalizers", "workspace", klog.KObj(wObj))
	}

	return ctrl.Result{}, nil
}
