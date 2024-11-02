// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"reflect"
	"sort"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *WorkspaceReconciler) updateWorkspaceStatus(ctx context.Context, name *client.ObjectKey, condition *metav1.Condition, workerNodes []string) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			wObj := &kaitov1alpha1.Workspace{}
			if err := c.Client.Get(ctx, *name, wObj); err != nil {
				if !errors.IsNotFound(err) {
					return err
				}
				return nil
			}
			if condition != nil {
				meta.SetStatusCondition(&wObj.Status.Conditions, *condition)
			}
			if workerNodes != nil {
				wObj.Status.WorkerNodes = workerNodes
			}
			return c.Client.Status().Update(ctx, wObj)
		})
}

func (c *WorkspaceReconciler) updateStatusConditionIfNotMatch(ctx context.Context, wObj *kaitov1alpha1.Workspace, cType kaitov1alpha1.ConditionType,
	cStatus metav1.ConditionStatus, cReason, cMessage string) error {
	if curCondition := meta.FindStatusCondition(wObj.Status.Conditions, string(cType)); curCondition != nil {
		if curCondition.Status == cStatus && curCondition.Reason == cReason && curCondition.Message == cMessage {
			// Nonthing to change
			return nil
		}
	}
	klog.InfoS("updateStatusCondition", "workspace", klog.KObj(wObj), "conditionType", cType, "status", cStatus, "reason", cReason, "message", cMessage)
	cObj := metav1.Condition{
		Type:               string(cType),
		Status:             cStatus,
		Reason:             cReason,
		ObservedGeneration: wObj.GetGeneration(),
		Message:            cMessage,
	}
	return c.updateWorkspaceStatus(ctx, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, &cObj, nil)
}

func (c *WorkspaceReconciler) updateStatusNodeListIfNotMatch(ctx context.Context, wObj *kaitov1alpha1.Workspace, validNodeList []*corev1.Node) error {
	nodeNameList := lo.Map(validNodeList, func(v *corev1.Node, _ int) string {
		return v.Name
	})
	sort.Strings(wObj.Status.WorkerNodes)
	sort.Strings(nodeNameList)
	if reflect.DeepEqual(wObj.Status.WorkerNodes, nodeNameList) {
		return nil
	}
	klog.InfoS("updateStatusNodeList", "workspace", klog.KObj(wObj))
	return c.updateWorkspaceStatus(ctx, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, nil, nodeNameList)
}
