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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// UpdateStatusConditionIfNotMatch updates the inferenceset status condition if it doesn't match the current values
func UpdateStatusConditionIfNotMatch(ctx context.Context, c client.Client, iObj *kaitov1alpha1.InferenceSet, cType kaitov1alpha1.ConditionType,
	cStatus metav1.ConditionStatus, cReason, cMessage string) error {
	if curCondition := meta.FindStatusCondition(iObj.Status.Conditions, string(cType)); curCondition != nil {
		if curCondition.Status == cStatus && curCondition.Reason == cReason && curCondition.Message == cMessage {
			// Nothing to change
			return nil
		}
	}
	klog.InfoS("updateStatusCondition", "inferenceset", klog.KObj(iObj), "conditionType", cType, "status", cStatus, "reason", cReason, "message", cMessage)
	condition := metav1.Condition{
		Type:               string(cType),
		Status:             cStatus,
		Reason:             cReason,
		ObservedGeneration: iObj.GetGeneration(),
		Message:            cMessage,
		LastTransitionTime: metav1.Now(),
	}
	return UpdateInferenceSetStatus(ctx, c, &client.ObjectKey{Name: iObj.Name, Namespace: iObj.Namespace}, func(status *kaitov1alpha1.InferenceSetStatus) error {
		meta.SetStatusCondition(&status.Conditions, condition)
		return nil
	})
}

// UpdateInferenceSetStatus updates the inferenceset status with the provided condition
func UpdateInferenceSetStatus(ctx context.Context, c client.Client, name *client.ObjectKey, modifyFn func(*kaitov1alpha1.InferenceSetStatus) error) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			iObj := &kaitov1alpha1.InferenceSet{}
			if err := c.Get(ctx, *name, iObj); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			if modifyFn != nil {
				if err := modifyFn(&iObj.Status); err != nil {
					return err
				}
			}
			return c.Status().Update(ctx, iObj)
		})
}

// UpdateInferenceSetWithRetry gets the latest inferenceset object, applies the modify function, and retries on conflict
func UpdateInferenceSetWithRetry(ctx context.Context, c client.Client, iObj *kaitov1alpha1.InferenceSet, modifyFn func(*kaitov1alpha1.InferenceSet) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestInferenceSet := &kaitov1alpha1.InferenceSet{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(iObj), latestInferenceSet); err != nil {
			return err
		}
		if err := modifyFn(latestInferenceSet); err != nil {
			return err
		}
		return c.Update(ctx, latestInferenceSet)
	})
}

// ListWorkspaces lists all workspace objects in the cluster that are created by the given InferenceSet.
func ListWorkspaces(ctx context.Context, iObj *kaitov1alpha1.InferenceSet, kubeClient client.Client) (*kaitov1beta1.WorkspaceList, error) {
	if iObj == nil {
		return nil, fmt.Errorf("InferenceSet object is nil")
	}
	workspaceList := &kaitov1beta1.WorkspaceList{}

	// List all the workspaces that are created by this inferenceset.
	// We use label selector to find the workspaces.
	// The label is "inferenceset.kaito.sh/created-by": <inferenceset-name>
	ls := labels.Set{
		consts.WorkspaceCreatedByInferenceSetLabel: iObj.Name,
	}

	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubeClient.List(ctx, workspaceList, &client.MatchingLabelsSelector{Selector: ls.AsSelector()})
	})
	return workspaceList, err
}

// GetWorkspace retrieves a workspace by name from a list of workspaces.
// Returns nil if the workspace is not found.
func GetWorkspace(workspaceName string, workspaceList *kaitov1beta1.WorkspaceList) *kaitov1beta1.Workspace {
	if workspaceName == "" || workspaceList == nil {
		return nil
	}
	for _, ws := range workspaceList.Items {
		if ws.Name == workspaceName {
			return &ws
		}
	}
	return nil
}

func ComputeInferenceSetHash(iObj *kaitov1alpha1.InferenceSet) string {
	if iObj == nil {
		return ""
	}

	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	encoder.Encode(iObj.Spec)
	return hex.EncodeToString(hasher.Sum(nil))
}

func MarshalInferenceSetFields(iObj *kaitov1alpha1.InferenceSet) ([]byte, error) {
	if iObj == nil {
		return nil, fmt.Errorf("InferenceSet object is nil")
	}

	partialMap := map[string]interface{}{
		"Spec": iObj.Spec,
	}

	jsonData, err := json.Marshal(partialMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal selected fields: %w", err)
	}

	return jsonData, nil
}
