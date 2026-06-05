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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
	"github.com/kaito-project/kaito/pkg/modelmirror/download"
)

const (
	jobRetryInterval = 5 * time.Minute
)

// ModelMirrorReconciler reconciles ModelMirror objects.
type ModelMirrorReconciler struct {
	client.Client
	Log logr.Logger
}

// NewModelMirrorReconciler creates a new reconciler instance.
func NewModelMirrorReconciler(c client.Client, log logr.Logger) *ModelMirrorReconciler {
	return &ModelMirrorReconciler{
		Client: c,
		Log:    log,
	}
}

func (r *ModelMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("modelmirror", req.Name)

	cr := &kaitov1alpha1.ModelMirror{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !cr.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cr)
	}

	// Step 0: If already Ready, no-op
	if cr.Status.Phase == kaitov1alpha1.ModelMirrorPhaseReady {
		return ctrl.Result{}, nil
	}

	// Step 0a: Ensure finalizer
	if !controllerutil.ContainsFinalizer(cr, mmconsts.ModelMirrorFinalizer) {
		controllerutil.AddFinalizer(cr, mmconsts.ModelMirrorFinalizer)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Step 1: Ensure PVC
	if err := r.ensurePVC(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	// Step 3: Ensure download Job
	if err := r.ensureDownloadJob(ctx, cr, log); err != nil {
		return ctrl.Result{}, err
	}

	// Step 4: Check Job status
	return r.checkJobStatus(ctx, cr, log)
}

func (r *ModelMirrorReconciler) handleDeletion(ctx context.Context, cr *kaitov1alpha1.ModelMirror) (ctrl.Result, error) {
	ns := cr.Spec.JobNamespace

	// Delete all download Jobs for this CR (cluster-scoped CR cannot own
	// namespaced resources, so ownerReference-based GC does not work)
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList,
		client.InNamespace(ns),
		client.MatchingLabels{mmconsts.LabelModelMirrorName: cr.Name},
	); err == nil {
		for i := range jobList.Items {
			if err := r.Delete(ctx, &jobList.Items[i], client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	// Delete PVC
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: ns}, pvc); err == nil {
		if controllerutil.ContainsFinalizer(pvc, mmconsts.ModelMirrorPVCFinalizer) {
			controllerutil.RemoveFinalizer(pvc, mmconsts.ModelMirrorPVCFinalizer)
			if err := r.Update(ctx, pvc); err != nil {
				return ctrl.Result{}, err
			}
		}
		if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// Remove CR finalizer
	controllerutil.RemoveFinalizer(cr, mmconsts.ModelMirrorFinalizer)
	if err := r.Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ModelMirrorReconciler) ensurePVC(ctx context.Context, cr *kaitov1alpha1.ModelMirror) error {
	pvcName := cr.Name
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Spec.JobNamespace}, pvc)
	if err == nil {
		// PVC already exists
		if pvc.Status.Phase == corev1.ClaimBound {
			setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, "PVCBound", "PVC is bound")
			return r.Status().Update(ctx, cr)
		}
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create PVC
	storageSize := resource.MustParse(cr.Spec.Storage.Size)
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       pvcName,
			Namespace:  cr.Spec.JobNamespace,
			Finalizers: []string{mmconsts.ModelMirrorPVCFinalizer},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: ptr.To(cr.Spec.Storage.StorageClassName),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}
	return r.Create(ctx, pvc)
}

func (r *ModelMirrorReconciler) ensureDownloadJob(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) error {
	// Check if an active (non-failed) Job already exists
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList,
		client.InNamespace(cr.Spec.JobNamespace),
		client.MatchingLabels{mmconsts.LabelModelMirrorName: cr.Name},
	); err != nil {
		return err
	}

	var latestFailTime *metav1.Time
	for i := range jobList.Items {
		job := &jobList.Items[i]
		if !isJobFailed(job) {
			return nil // Active or succeeded Job exists
		}
		// Track the most recent failure time
		for _, cond := range job.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				if latestFailTime == nil || cond.LastTransitionTime.After(latestFailTime.Time) {
					latestFailTime = &cond.LastTransitionTime
				}
			}
		}
	}

	// If a Job failed recently, wait before retrying
	if latestFailTime != nil && time.Since(latestFailTime.Time) < jobRetryInterval {
		return nil
	}

	job := download.BuildDownloadJob(cr)
	log.Info("Creating download Job", "namespace", cr.Spec.JobNamespace)
	return r.Create(ctx, job)
}

func isJobFailed(job *batchv1.Job) bool {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *ModelMirrorReconciler) checkJobStatus(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) (ctrl.Result, error) {
	// Find the latest active Job for this CR
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList,
		client.InNamespace(cr.Spec.JobNamespace),
		client.MatchingLabels{mmconsts.LabelModelMirrorName: cr.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Find the most recent non-failed Job
	var activeJob *batchv1.Job
	for i := range jobList.Items {
		job := &jobList.Items[i]
		if !isJobFailed(job) {
			activeJob = job
		}
	}

	if activeJob == nil {
		// No active Job. A new job will be recreated by ensureDownloadJob() on next reconcile
		return ctrl.Result{RequeueAfter: jobRetryInterval}, nil
	}

	// Check for success
	if activeJob.Status.Succeeded > 0 {
		return r.handleJobSuccess(ctx, cr, log)
	}

	// Check for failure (all retries exhausted on this Job)
	if isJobFailed(activeJob) {
		msg := "Download job failed"
		for _, cond := range activeJob.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				msg = fmt.Sprintf("Download job failed: %s", cond.Message)
			}
		}
		cr.Status.FailureMessage = msg
		setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, "DownloadFailed", msg)
		if err := r.Status().Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: jobRetryInterval}, nil
	}

	// Job still running
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *ModelMirrorReconciler) handleJobSuccess(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) (ctrl.Result, error) {
	modelID := cr.Spec.Source.ModelID
	cr.Status.Phase = kaitov1alpha1.ModelMirrorPhaseReady
	cr.Status.ModelPath = "/models/" + modelID
	cr.Status.FailureMessage = ""
	cr.Status.LastDownloadTime = ptr.To(metav1.Now())

	setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionTrue, "DownloadSucceeded", "Model download completed")
	setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, "PVCBound", "PVC is bound")

	log.Info("ModelMirror is Ready", "modelPath", cr.Status.ModelPath)
	return ctrl.Result{}, r.Status().Update(ctx, cr)
}

func setCondition(cr *kaitov1alpha1.ModelMirror, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range cr.Status.Conditions {
		if c.Type == condType {
			if c.Status != status {
				cr.Status.Conditions[i].LastTransitionTime = now
			}
			cr.Status.Conditions[i].Status = status
			cr.Status.Conditions[i].Reason = reason
			cr.Status.Conditions[i].Message = message
			return
		}
	}
	cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}

// SetupWithManager registers the controller with the manager.
func (r *ModelMirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.ModelMirror{}).
		Complete(r)
}
