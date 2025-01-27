// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/machine"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// garbageCollectRAGEngine remove finalizer associated with ragengine object.
func (c *RAGEngineReconciler) garbageCollectRAGEngine(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) (ctrl.Result, error) {
	klog.InfoS("garbageCollectRAGEngine", "ragengine", klog.KObj(ragEngineObj))

	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		// Check if there are any nodeClaims associated with this ragengine.
		ncList, err := nodeclaim.ListNodeClaim(ctx, ragEngineObj, c.Client)
		if err != nil {
			return ctrl.Result{}, err
		}

		// We should delete all the nodeClaims that are created by this ragengine
		for i := range ncList.Items {
			if deleteErr := c.Delete(ctx, &ncList.Items[i], &client.DeleteOptions{}); deleteErr != nil {
				klog.ErrorS(deleteErr, "failed to delete the nodeClaim", "nodeClaim", klog.KObj(&ncList.Items[i]))
				return ctrl.Result{}, deleteErr
			}
		}
	} else {
		// Check if there are any machines associated with this ragengine.
		mList, err := machine.ListMachines(ctx, ragEngineObj, c.Client)
		if err != nil {
			return ctrl.Result{}, err
		}
		// We should delete all the machines that are created by this ragengine
		for i := range mList.Items {
			if deleteErr := c.Delete(ctx, &mList.Items[i], &client.DeleteOptions{}); deleteErr != nil {
				klog.ErrorS(deleteErr, "failed to delete the machine", "machine", klog.KObj(&mList.Items[i]))
				return ctrl.Result{}, deleteErr
			}
		}
	}

	if controllerutil.RemoveFinalizer(ragEngineObj, consts.RAGEngineFinalizer) {
		if updateErr := c.Update(ctx, ragEngineObj, &client.UpdateOptions{}); updateErr != nil {
			klog.ErrorS(updateErr, "failed to remove the finalizer from the ragengine",
				"ragengine", klog.KObj(ragEngineObj))
			return ctrl.Result{}, updateErr
		}
	}

	klog.InfoS("successfully removed the ragengine finalizers",
		"ragengine", klog.KObj(ragEngineObj))
	return ctrl.Result{}, nil
}
