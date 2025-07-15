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
	"reflect"
	"sort"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

func (c *RAGEngineReconciler) updateRAGEngineStatus(ctx context.Context, name *client.ObjectKey, condition *metav1.Condition, workerNodes []string) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			ragObj := &kaitov1alpha1.RAGEngine{}
			if err := c.Client.Get(ctx, *name, ragObj); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			if condition != nil {
				meta.SetStatusCondition(&ragObj.Status.Conditions, *condition)
			}
			if workerNodes != nil {
				ragObj.Status.WorkerNodes = workerNodes
			}
			return c.Client.Status().Update(ctx, ragObj)
		})
}

func (c *RAGEngineReconciler) updateStatusConditionIfNotMatch(ctx context.Context, ragObj *kaitov1alpha1.RAGEngine, cType kaitov1alpha1.ConditionType,
	cStatus metav1.ConditionStatus, cReason, cMessage string) error {
	if curCondition := meta.FindStatusCondition(ragObj.Status.Conditions, string(cType)); curCondition != nil {
		if curCondition.Status == cStatus && curCondition.Reason == cReason && curCondition.Message == cMessage {
			// Nonthing to change
			return nil
		}
	}
	klog.InfoS("updateStatusCondition", "ragengine", klog.KObj(ragObj), "conditionType", cType, "status", cStatus, "reason", cReason, "message", cMessage)
	cObj := metav1.Condition{
		Type:               string(cType),
		Status:             cStatus,
		Reason:             cReason,
		ObservedGeneration: ragObj.GetGeneration(),
		Message:            cMessage,
	}
	return c.updateRAGEngineStatus(ctx, &client.ObjectKey{Name: ragObj.Name, Namespace: ragObj.Namespace}, &cObj, nil)
}

func (c *RAGEngineReconciler) updateStatusNodeListIfNotMatch(ctx context.Context, ragObj *kaitov1alpha1.RAGEngine, validNodeList []*corev1.Node) error {
	nodeNameList := lo.Map(validNodeList, func(v *corev1.Node, _ int) string {
		return v.Name
	})
	sort.Strings(ragObj.Status.WorkerNodes)
	sort.Strings(nodeNameList)
	if reflect.DeepEqual(ragObj.Status.WorkerNodes, nodeNameList) {
		return nil
	}
	klog.InfoS("updateStatusNodeList", "ragengine", klog.KObj(ragObj))
	return c.updateRAGEngineStatus(ctx, &client.ObjectKey{Name: ragObj.Name, Namespace: ragObj.Namespace}, nil, nodeNameList)
}
