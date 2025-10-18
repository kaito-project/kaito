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

package workspace

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const WorkspaceNameSuffixLength = 12

var (
	InferenceSetSelector, _ = metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: consts.WorkspaceCreatedByInferenceSetLabel, Operator: metav1.LabelSelectorOpExists},
		},
	})

	WorkspacePredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			workspace, ok := e.Object.(*kaitov1beta1.Workspace)
			if !ok {
				return false
			}
			if !InferenceSetSelector.Matches(labels.Set(workspace.GetLabels())) {
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
			if !InferenceSetSelector.Matches(labels.Set(oldWorkspace.GetLabels())) {
				return false
			}

			if !InferenceSetSelector.Matches(labels.Set(newWorkspace.GetLabels())) {
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
			if !InferenceSetSelector.Matches(labels.Set(workspace.GetLabels())) {
				return false
			}
			return true
		},
	}
)

// UpdateStatusConditionIfNotMatch updates the workspace status condition if it doesn't match the current values
func UpdateStatusConditionIfNotMatch(ctx context.Context, c client.Client, wObj *kaitov1beta1.Workspace, cType kaitov1beta1.ConditionType,
	cStatus metav1.ConditionStatus, cReason, cMessage string) error {
	if curCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(cType)); curCondition != nil {
		if curCondition.Status == cStatus && curCondition.Reason == cReason && curCondition.Message == cMessage {
			// Nothing to change
			return nil
		}
	}
	klog.InfoS("updateStatusCondition", "workspace", klog.KObj(wObj), "conditionType", cType, "status", cStatus, "reason", cReason, "message", cMessage)
	condition := metav1.Condition{
		Type:               string(cType),
		Status:             cStatus,
		Reason:             cReason,
		ObservedGeneration: wObj.GetGeneration(),
		Message:            cMessage,
		LastTransitionTime: metav1.Now(),
	}
	return UpdateWorkspaceStatus(ctx, c, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, func(status *kaitov1beta1.WorkspaceStatus) error {
		meta.SetStatusCondition(&status.Conditions, condition)
		return nil
	})
}

// UpdateWorkspaceStatus updates the workspace status with the provided condition
func UpdateWorkspaceStatus(ctx context.Context, c client.Client, name *client.ObjectKey, modifyFn func(*kaitov1beta1.WorkspaceStatus) error) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			wObj := &kaitov1beta1.Workspace{}
			if err := c.Get(ctx, *name, wObj); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			if modifyFn != nil {
				if err := modifyFn(&wObj.Status); err != nil {
					return err
				}
			}
			return c.Status().Update(ctx, wObj)
		})
}

// UpdateWorkspaceWithRetry gets the latest workspace object, applies the modify function, and retries on conflict
func UpdateWorkspaceWithRetry(ctx context.Context, c client.Client, wObj *kaitov1beta1.Workspace, modifyFn func(*kaitov1beta1.Workspace) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestWorkspace := &kaitov1beta1.Workspace{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(wObj), latestWorkspace); err != nil {
			return err
		}
		if err := modifyFn(latestWorkspace); err != nil {
			return err
		}
		return c.Update(ctx, latestWorkspace)
	})
}

// generateRandomString generates a random string of the specified length using digits and lowercase letters.
func generateRandomString(length int) string {
	if length <= 0 {
		return ""
	}

	charset := "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

func GetWorkspaceNameWithRandomSuffix(baseName string) string {
	return fmt.Sprintf("%s-%s", baseName, generateRandomString(WorkspaceNameSuffixLength))
}
