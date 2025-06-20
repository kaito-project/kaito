// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"time"

	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
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

	res, err := c.garbagePersistentVolume(ctx, wObj)
	if err != nil {
		return res, err
	} else if res.Requeue {
		// Waiting for deletion from csi driver
		return res, nil
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

// garbagePersistentVolume ensures that all prersistent volumes are deleted
func (c *WorkspaceReconciler) garbagePersistentVolume(ctx context.Context, wObj *kaitov1beta1.Workspace) (ctrl.Result, error) {
	klog.InfoS("garbagePersistentVolume", "workspace", klog.KObj(wObj))

	// List PVCs in the same namespace with a label matching workspace name
	pvcList := &corev1.PersistentVolumeClaimList{}
	listOpts := []client.ListOption{
		client.InNamespace(wObj.Namespace),
		client.MatchingLabels{kaitov1beta1.LabelWorkspaceName: wObj.Name},
	}

	if err := c.List(ctx, pvcList, listOpts...); err != nil {
		klog.ErrorS(err, "failed to list PVCs for workspace", "workspace", klog.KObj(wObj))
		return ctrl.Result{}, err
	}

	if len(pvcList.Items) == 0 {
		klog.InfoS("No PVCs found for workspace", "workspace", klog.KObj(wObj))
		return ctrl.Result{}, nil
	}

	// Process the found PVCs
	var errs error
	localpvCount := 0
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName == consts.LocalNVMeStorageClass {
			localpvCount++
			if err := deleteOrphanedLocalPV(ctx, c.Client, pvc); err != nil {
				errs = multierr.Append(errs, err)
			}
		}
	}

	if errs != nil {
		return ctrl.Result{Requeue: true}, errs
	}

	if localpvCount > 0 {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// When a node with local volumes gets removed from a cluster before deleting those volumes,
// the PV and PVC objects may still exist. Force deleting objects in this case.
// Ref: https://github.com/kubernetes-csi/external-provisioner?tab=readme-ov-file#deleting-local-volumes-after-a-node-failure-or-removal
func deleteOrphanedLocalPV(ctx context.Context, c client.Client, pvc *corev1.PersistentVolumeClaim) error {
	nodeName := pvc.Annotations["volume.kubernetes.io/selected-node"]
	// If pvc is not bound, there's nothing to do
	if nodeName == "" || pvc.Status.Phase != corev1.ClaimBound {
		return nil
	}

	// Check if the node exists
	node := &corev1.Node{}
	err := c.Get(ctx, client.ObjectKey{Name: nodeName}, node)
	if err == nil {
		// Node exists, waiting for csi driver to clean up
		return nil
	}
	if client.IgnoreNotFound(err) != nil {
		klog.ErrorS(err, "Failed to get node", "node", nodeName, "pvc", klog.KObj(pvc))
		return err
	}

	// Node does not exist, force delete pv
	klog.InfoS("Node does not exist, deleting orphaned PVC", "pvc", klog.KObj(pvc), "node", nodeName)
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		klog.Warningf("PVC %s does not have a bound PV, nothing to do", klog.KObj(pvc))
		return nil
	}

	pv := &corev1.PersistentVolume{}
	if err := c.Get(ctx, client.ObjectKey{Name: pvName}, pv); err == nil {
		// PV exists, set finalizers to nil
		pv.Finalizers = nil
		if err := c.Update(ctx, pv); err != nil {
			klog.ErrorS(err, "Failed to update PV finalizers", "pv", klog.KObj(pv))
			return err
		}
		klog.InfoS("Removed finalizers from PV", "pv", klog.KObj(pv))
	} else if client.IgnoreNotFound(err) != nil {
		klog.ErrorS(err, "Failed to get PV", "pv", pvName)
		return err
	}

	// Delete the orphaned PVC
	if err := c.Delete(ctx, pvc); err != nil {
		klog.ErrorS(err, "Failed to delete orphaned PVC", "pvc", klog.KObj(pvc))
		return err
	}
	klog.InfoS("Successfully deleted orphaned PVC", "pvc", klog.KObj(pvc))

	return nil
}
