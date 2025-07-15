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

package garbagecollect

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/component-helpers/storage/volume"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type PersistentVolumeGCReconciler struct {
	client.Client
	Recorder record.EventRecorder
}

func NewPersistentVolumeGCReconciler(client client.Client, Recorder record.EventRecorder) *PersistentVolumeGCReconciler {
	return &PersistentVolumeGCReconciler{
		Client:   client,
		Recorder: Recorder,
	}
}

func (c *PersistentVolumeGCReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	pvObj := &corev1.PersistentVolume{}
	if err := c.Client.Get(ctx, req.NamespacedName, pvObj); err != nil {
		klog.ErrorS(err, "Failed to get PersistentVolume", "name", req.Name, "namespace", req.Namespace)
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if pvObj.Spec.ClaimRef == nil {
		// No claim reference, nothing to do
		return reconcile.Result{}, nil
	}

	pvcNamespacedName := types.NamespacedName{
		Namespace: pvObj.Spec.ClaimRef.Namespace,
		Name:      pvObj.Spec.ClaimRef.Name,
	}

	pvcObj := &corev1.PersistentVolumeClaim{}
	if err := c.Client.Get(ctx, pvcNamespacedName, pvcObj); err != nil {
		if client.IgnoreNotFound(err) == nil {
			klog.InfoS("PVC not found, force deleting PV", "pv", klog.KObj(pvObj), "pvc", pvcNamespacedName)
			return reconcile.Result{}, forceDeletePV(ctx, c.Client, pvObj)
		}
		klog.ErrorS(err, "Failed to get PersistentVolumeClaim", "pvc", pvcNamespacedName)
		return reconcile.Result{}, err
	}

	result, err := c.deleteOrphanedLocalPVIfNodeNotFound(ctx, pvObj, pvcObj)
	if err != nil {
		klog.ErrorS(err, "Failed to delete orphaned local PV", "pv", klog.KObj(pvObj), "pvc", klog.KObj(pvcObj))
		c.Recorder.Eventf(pvcObj, corev1.EventTypeWarning, "DeletionFailed", "Failed to delete orphaned local PV %s: %v", klog.KObj(pvObj), err)
		return result, err
	}

	return result, nil
}

// When a node with local volumes gets removed from a cluster before deleting those volumes,
// the PV and PVC objects may still exist. Force deleting objects in this case.
// Ref: https://github.com/kubernetes-csi/external-provisioner?tab=readme-ov-file#deleting-local-volumes-after-a-node-failure-or-removal
func (c *PersistentVolumeGCReconciler) deleteOrphanedLocalPVIfNodeNotFound(ctx context.Context, pv *corev1.PersistentVolume, pvc *corev1.PersistentVolumeClaim) (ctrl.Result, error) {
	if pvc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	nodeName := pvc.Annotations[volume.AnnSelectedNode]
	if nodeName == "" {
		return ctrl.Result{}, nil
	}

	// Check if the node exists
	node := &corev1.Node{}
	err := c.Client.Get(ctx, client.ObjectKey{Name: nodeName}, node)
	if err == nil {
		// Node exists, waiting for csi driver to clean up
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if client.IgnoreNotFound(err) != nil {
		klog.ErrorS(err, "Failed to get node", "node", nodeName, "pvc", klog.KObj(pvc))
		return ctrl.Result{}, err
	}

	// Node does not exist, force delete pv
	klog.InfoS("Node does not exist, deleting orphaned PV", "pv", klog.KObj(pv), "pvc", klog.KObj(pvc), "node", nodeName)

	return ctrl.Result{}, forceDeletePV(ctx, c.Client, pv)
}

func forceDeletePV(ctx context.Context, c client.Client, pv *corev1.PersistentVolume) error {
	// Delete the PV: kubectl delete pv <pv> --wait=false --grace-period=0 --force
	err := c.Delete(ctx, pv, &client.DeleteOptions{
		GracePeriodSeconds: ptr.To(int64(0)),
		PropagationPolicy:  ptr.To(metav1.DeletePropagationBackground),
	})
	if err != nil {
		return client.IgnoreNotFound(fmt.Errorf("failed to delete PV %s: %w", pv.Name, err))
	}
	klog.InfoS("Successfully deleted PV", "pv", klog.KObj(pv))

	// Remove finalizers: kubectl patch pv <pv> -p '{"metadata":{"finalizers":null}}'
	stored := pv.DeepCopy()
	pv.Finalizers = nil
	if !equality.Semantic.DeepEqual(stored, pv) {
		if err := c.Patch(ctx, pv, client.StrategicMergeFrom(stored)); err != nil {
			return client.IgnoreNotFound(fmt.Errorf("failed to patch PV %s to remove finalizers: %w", pv.Name, err))
		}
	}
	klog.InfoS("Removed finalizers from PV", "pv", klog.KObj(pv))

	return nil
}

func storageClassFilter() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		pv, ok := obj.(*corev1.PersistentVolume)
		if !ok {
			return false
		}
		if pv.Spec.StorageClassName != consts.LocalNVMeStorageClass {
			return false
		}
		return true
	})
}

// PVCToPVMapFunc maps a PVC event to its bound PV
var PVCToPVMapFunc = handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
	pvc, ok := o.(*corev1.PersistentVolumeClaim)
	if !ok {
		return nil
	}
	// Only process if PVC is bound to a PV
	if pvc.Spec.VolumeName == "" {
		return nil
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != consts.LocalNVMeStorageClass {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name: pvc.Spec.VolumeName,
		},
	}}
})

// SetupWithManager sets up the controller with the Manager.
func (c *PersistentVolumeGCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("PersistentVolumeGCController")

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.PersistentVolume{}, builder.WithPredicates(storageClassFilter())).
		Watches(&corev1.PersistentVolumeClaim{}, PVCToPVMapFunc).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	return builder.Complete(c)
}
