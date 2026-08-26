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
	"os"
	"sort"
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
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
	"github.com/kaito-project/kaito/pkg/modelmirror/download"
	"github.com/kaito-project/kaito/pkg/modelmirror/progress"
)

const (
	jobRetryInterval = 5 * time.Minute

	// deletionRetryInterval paces the wait for the mirror's children to disappear.
	deletionRetryInterval = 5 * time.Second
)

// ModelMirrorReconciler reconciles ModelMirror objects.
type ModelMirrorReconciler struct {
	client.Client
	// APIReader bypasses the informer cache for finalizer's fresh reads
	APIReader client.Reader
	Log       logr.Logger
	// DownloadResources sets the CPU/memory request==limit on the download Job container.
	DownloadResources mmconsts.DownloadJobResources
}

// NewModelMirrorReconciler creates a new reconciler instance.
func NewModelMirrorReconciler(c client.Client, apiReader client.Reader, log logr.Logger, downloadResources mmconsts.DownloadJobResources) *ModelMirrorReconciler {
	return &ModelMirrorReconciler{
		Client:            c,
		APIReader:         apiReader,
		Log:               log,
		DownloadResources: downloadResources,
	}
}

func (r *ModelMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("modelmirror", req.Name)

	cr := &kaitov1alpha1.ModelMirror{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cr.DeletionTimestamp.IsZero() {
		return r.finalizeMirror(ctx, cr, log)
	}

	// Static mirror (BYO storage): the model weights already exist in a pre-existing
	// location, so there is no PVC to provision and no weights to download. Mark Ready
	// immediately and never create a PVC or Job.
	if cr.Spec.Mode == kaitov1alpha1.ModelMirrorModeStatic {
		if cr.Status.Phase == kaitov1alpha1.ModelMirrorPhaseReady {
			return ctrl.Result{}, nil
		}
		cr.Status.Phase = kaitov1alpha1.ModelMirrorPhaseReady
		cr.Status.FailureMessage = ""
		setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionTrue, mmconsts.ReasonStaticMirror, "No download required")
		setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, mmconsts.ReasonStaticMirror, "No PVC required")
		return ctrl.Result{}, r.Status().Update(ctx, cr)
	}

	// Only a managed mirror provisions children, so only it needs the teardown ordering.
	if err := r.ensureFinalizer(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	if cr.Status.Phase == kaitov1alpha1.ModelMirrorPhaseReady {
		return ctrl.Result{}, nil
	}

	if cr.Spec.Source == nil || cr.Spec.Storage == nil {
		cr.Status.Phase = kaitov1alpha1.ModelMirrorPhasePending
		msg := "managed ModelMirror requires spec.source and spec.storage"
		cr.Status.FailureMessage = msg
		setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, mmconsts.ReasonInvalidSpec, msg)
		return ctrl.Result{}, r.Status().Update(ctx, cr)
	}

	if err := r.ensurePVC(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureDownloadJob(ctx, cr, log); err != nil {
		return ctrl.Result{}, err
	}

	return r.checkJobStatus(ctx, cr, log)
}

// ensureFinalizer adds the cleanup finalizer so the mirror outlives a delete request until
// its children are gone.
func (r *ModelMirrorReconciler) ensureFinalizer(ctx context.Context, cr *kaitov1alpha1.ModelMirror) error {
	if controllerutil.ContainsFinalizer(cr, mmconsts.ModelMirrorFinalizer) {
		return nil
	}
	// A plain merge patch replaces the finalizer array wholesale, so a stale read would
	// drop finalizers added since — including the API server's own foregroundDeletion.
	patch := client.MergeFromWithOptions(cr.DeepCopy(), client.MergeFromWithOptimisticLock{})
	controllerutil.AddFinalizer(cr, mmconsts.ModelMirrorFinalizer)
	return r.Patch(ctx, cr, patch)
}

// finalizeMirror deletes the mirror's children and only then releases the CR. Holding the
// name until the storage is gone is what stops a replacement from observing it.
func (r *ModelMirrorReconciler) finalizeMirror(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(cr, mmconsts.ModelMirrorFinalizer) {
		return ctrl.Result{}, nil
	}

	pending, err := r.deleteChildren(ctx, cr)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pending {
		return ctrl.Result{RequeueAfter: deletionRetryInterval}, nil
	}

	log.Info("ModelMirror children removed, releasing finalizer", "namespace", cr.Namespace)
	controllerutil.RemoveFinalizer(cr, mmconsts.ModelMirrorFinalizer)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, cr))
}

// deleteChildren removes the Jobs and PVC this mirror owns and reports whether any survive.
// The name and label are enough to identify them: the cleanup finalizer holds the CR until
// they are gone, so a live mirror can never share them with another generation.
func (r *ModelMirrorReconciler) deleteChildren(ctx context.Context, cr *kaitov1alpha1.ModelMirror) (bool, error) {
	jobsPending, err := r.deleteDownloadJobs(ctx, cr)
	if err != nil {
		return false, err
	}

	pvcPending, err := r.deletePVC(ctx, cr)
	if err != nil {
		return false, err
	}

	return jobsPending || pvcPending, nil
}

// deletePVC removes the mirror's PVC and reports whether it survives.
func (r *ModelMirrorReconciler) deletePVC(ctx context.Context, cr *kaitov1alpha1.ModelMirror) (bool, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(cr), pvc); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	if pvc.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
			return false, err
		}
	}
	return true, nil
}

// deleteDownloadJobs removes the mirror's download Jobs and reports whether any survive.
func (r *ModelMirrorReconciler) deleteDownloadJobs(ctx context.Context, cr *kaitov1alpha1.ModelMirror) (bool, error) {
	jobList := &batchv1.JobList{}
	if err := r.APIReader.List(ctx, jobList,
		client.InNamespace(cr.Namespace),
		client.MatchingLabels{mmconsts.LabelModelMirrorName: cr.Name},
	); err != nil {
		return false, err
	}

	for i := range jobList.Items {
		job := &jobList.Items[i]
		if !job.DeletionTimestamp.IsZero() {
			continue
		}
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !errors.IsNotFound(err) {
			return false, err
		}
	}
	return len(jobList.Items) > 0, nil
}

func (r *ModelMirrorReconciler) ensurePVC(ctx context.Context, cr *kaitov1alpha1.ModelMirror) error {
	pvcName := cr.Name
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Namespace}, pvc)
	if err == nil {
		if pvc.Status.Phase == corev1.ClaimBound {
			setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, mmconsts.ReasonPVCBound, "PVC is bound")
			return r.Status().Update(ctx, cr)
		}
		setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionFalse, mmconsts.ReasonPVCPending,
			fmt.Sprintf("PVC %s/%s is %s; check the StorageClass parameters and that the storage identity has write access",
				cr.Namespace, pvcName, pvc.Status.Phase))
		return r.Status().Update(ctx, cr)
	}
	if !errors.IsNotFound(err) {
		return err
	}

	storageSize := resource.MustParse(cr.Spec.Storage.Size)
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cr.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: cr.Spec.Storage.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(cr, pvc, r.Scheme()); err != nil {
		return err
	}
	if err := r.Create(ctx, pvc); err != nil {
		setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionFalse, mmconsts.ReasonPVCCreateFailed,
			fmt.Sprintf("failed to create PVC %s/%s: %v", cr.Namespace, pvcName, err))
		if updateErr := r.Status().Update(ctx, cr); updateErr != nil {
			r.Log.Error(updateErr, "failed to update ModelMirror status", "modelmirror", cr.Name)
			return updateErr
		}
		return err
	}
	return nil
}

func (r *ModelMirrorReconciler) ensureDownloadJob(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) error {
	// Uncached: same-key reconciles are serialised, so a read the API server answers cannot
	// miss a Job the previous pass created and would otherwise start a duplicate download.
	jobList := &batchv1.JobList{}
	if err := r.APIReader.List(ctx, jobList,
		client.InNamespace(cr.Namespace),
		client.MatchingLabels{mmconsts.LabelModelMirrorName: cr.Name},
	); err != nil {
		setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, mmconsts.ReasonJobCreateFailed,
			fmt.Sprintf("failed to list download jobs: %v", err))
		if updateErr := r.Status().Update(ctx, cr); updateErr != nil {
			log.Error(updateErr, "failed to update ModelMirror status", "modelmirror", cr.Name)
			return updateErr
		}
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

	job := download.BuildDownloadJob(cr, r.DownloadResources, download.WorkloadIdentityPodLabels(os.Getenv("CLOUD_PROVIDER")))
	if err := controllerutil.SetControllerReference(cr, job, r.Scheme()); err != nil {
		return err
	}
	log.Info("Creating download Job", "namespace", cr.Namespace)
	if err := r.Create(ctx, job); err != nil {
		setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, mmconsts.ReasonJobCreateFailed,
			fmt.Sprintf("failed to create download Job in namespace %s: %v", cr.Namespace, err))
		if updateErr := r.Status().Update(ctx, cr); updateErr != nil {
			log.Error(updateErr, "failed to update ModelMirror status", "modelmirror", cr.Name)
			return updateErr
		}
		return err
	}
	return nil
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
		client.InNamespace(cr.Namespace),
		client.MatchingLabels{mmconsts.LabelModelMirrorName: cr.Name},
	); err != nil {
		return ctrl.Result{}, err
	}

	// Find the most recent job
	var activeJob *batchv1.Job
	for i := range jobList.Items {
		job := &jobList.Items[i]
		if activeJob == nil || job.CreationTimestamp.After(activeJob.CreationTimestamp.Time) {
			activeJob = job
		}
	}

	if activeJob == nil {
		// No Job yet. One will be created by ensureDownloadJob() on the next reconcile.
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
		reason := mmconsts.ReasonDownloadFailed
		if classifiedReason, classifiedMsg := r.classifyDownloadFailure(ctx, cr, activeJob.Name); classifiedReason != "" {
			reason, msg = classifiedReason, classifiedMsg
		}
		cr.Status.FailureMessage = msg
		setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, reason, msg)
		if err := r.Status().Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: jobRetryInterval}, nil
	}

	// Job still running
	r.updateDownloadProgress(ctx, cr, activeJob.Name, log)
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// classifyDownloadFailure inspects the download pods' status and returns a
// specific reason and message, or empty strings when the cause is not
// attributable from pod status alone (the caller then falls back to the generic
// DownloadFailed reason).
func (r *ModelMirrorReconciler) classifyDownloadFailure(ctx context.Context, cr *kaitov1alpha1.ModelMirror, jobName string) (reason, message string) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(cr.Namespace),
		client.MatchingLabels{"job-name": jobName},
	); err != nil {
		return "", ""
	}

	ordered := make([]*corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		ordered = append(ordered, &pods.Items[i])
	}
	sort.Slice(ordered, func(a, b int) bool {
		ta, tb := ordered[a].CreationTimestamp, ordered[b].CreationTimestamp
		if ta.Equal(&tb) {
			return ordered[a].Name > ordered[b].Name
		}
		return tb.Before(&ta)
	})

	for _, pod := range ordered {
		for _, cs := range pod.Status.ContainerStatuses {
			for _, t := range []*corev1.ContainerStateTerminated{cs.State.Terminated, cs.LastTerminationState.Terminated} {
				if t != nil && t.Reason == "OOMKilled" {
					return mmconsts.ReasonDownloadOOMKilled,
						fmt.Sprintf("download container was OOMKilled (exit code %d); the model may need a larger memory request", t.ExitCode)
				}
			}
		}

		if pod.Status.Phase == corev1.PodFailed && pod.Status.Reason == "Evicted" {
			return mmconsts.ReasonDownloadEvicted,
				fmt.Sprintf("download pod was evicted: %s", pod.Status.Message)
		}
	}

	return "", ""
}

func (r *ModelMirrorReconciler) handleJobSuccess(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) (ctrl.Result, error) {
	modelID := cr.Spec.Source.ModelID
	cr.Status.Phase = kaitov1alpha1.ModelMirrorPhaseReady
	cr.Status.ModelPath = "/models/" + modelID
	cr.Status.FailureMessage = ""
	cr.Status.LastDownloadTime = ptr.To(metav1.Now())

	cr.Status.Download = &kaitov1alpha1.ModelMirrorDownloadStatus{
		SpeedBytesPerSecond: 0,
		RemainingSeconds:    0,
	}

	setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionTrue, mmconsts.ReasonDownloadSucceeded, "Model download completed")
	setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, mmconsts.ReasonPVCBound, "PVC is bound")

	log.Info("ModelMirror is Ready", "modelPath", cr.Status.ModelPath)
	return ctrl.Result{}, r.Status().Update(ctx, cr)
}

// selectSamplerPod returns the name of the pod whose sampler should be polled:
// the newest pod that is running or pending. A retried Job leaves failed pods
// listed alongside the live one. Returns "" when no live pod exists.
func (r *ModelMirrorReconciler) selectSamplerPod(ctx context.Context, namespace, jobName string) string {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(namespace),
		client.MatchingLabels{"job-name": jobName},
	); err != nil {
		return ""
	}

	var newest *corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending {
			continue
		}
		if newest == nil || p.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = p
		}
	}
	if newest == nil {
		return ""
	}
	return newest.Name
}

// updateDownloadProgress polls the sampler sidecar and copies its two values to
// status. It runs on the reconcile tick while the Job is active.
//
// Every failure path is non-fatal and leaves status untouched.
func (r *ModelMirrorReconciler) updateDownloadProgress(ctx context.Context, cr *kaitov1alpha1.ModelMirror, jobName string, log logr.Logger) {
	cs := k8sclient.GetGlobalClientGoClient()
	if cs == nil {
		return
	}

	podName := r.selectSamplerPod(ctx, cr.Namespace, jobName)
	if podName == "" {
		return
	}

	p, err := progress.Fetch(ctx, cs, cr.Namespace, podName)
	if err != nil {
		log.V(1).Info("skipping download progress", "pod", podName, "error", err)
		return
	}

	cur := cr.Status.Download
	if cur != nil && cur.SpeedBytesPerSecond == p.SpeedBytesPerSecond && cur.RemainingSeconds == p.RemainingSeconds {
		return
	}

	cr.Status.Download = &kaitov1alpha1.ModelMirrorDownloadStatus{
		SpeedBytesPerSecond: p.SpeedBytesPerSecond,
		RemainingSeconds:    p.RemainingSeconds,
	}
	if err := r.Status().Update(ctx, cr); err != nil {
		log.V(1).Info("download progress status update failed", "error", err)
	}
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
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Complete(r)
}
