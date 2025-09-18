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
	"reflect"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/autoindexer/manifests"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (
	AutoIndexerHashAnnotation = "autoindexer.kaito.io/hash"
	AutoIndexerNameLabel      = "autoindexer.kaito.io/name"
)

// AutoIndexerReconciler reconciles an AutoIndexer object
type AutoIndexerReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// NewAutoIndexerReconciler creates a new AutoIndexer reconciler
func NewAutoIndexerReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, recorder record.EventRecorder) *AutoIndexerReconciler {
	return &AutoIndexerReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      log,
		Recorder: recorder,
	}
}

//+kubebuilder:rbac:groups=kaito.sh,resources=autoindexers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kaito.sh,resources=autoindexers/status,verbs=get;list;update;patch
//+kubebuilder:rbac:groups=kaito.sh,resources=autoindexers/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AutoIndexerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	autoIndexerObj := &kaitov1alpha1.AutoIndexer{}
	if err := r.Client.Get(ctx, req.NamespacedName, autoIndexerObj); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get AutoIndexer", "AutoIndexer", req.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	klog.InfoS("Reconciling", "AutoIndexer", req.NamespacedName)

	if autoIndexerObj.DeletionTimestamp.IsZero() {
		// if err := r.ensureFinalizer(ctx, autoIndexerObj); err != nil {
		// 	return ctrl.Result{}, err
		// }
	} else {
		// Handle deleting autoindexer, garbage collect all the resources.
		return r.deleteAutoIndexer(ctx, autoIndexerObj)
	}

	result, err := r.addAutoIndexer(ctx, autoIndexerObj)
	if err != nil {
		return result, err
	}

	return result, nil
}

// ensureFinalizer adds the finalizer if it doesn't exist
func (r *AutoIndexerReconciler) ensureFinalizer(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	if !controllerutil.ContainsFinalizer(autoIndexerObj, consts.AutoIndexerFinalizer) {
		patch := client.MergeFrom(autoIndexerObj.DeepCopy())
		controllerutil.AddFinalizer(autoIndexerObj, consts.AutoIndexerFinalizer)
		if err := r.Client.Patch(ctx, autoIndexerObj, patch); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer to the autoindexer", "autoindexer", klog.KObj(autoIndexerObj))
			return err
		}
	}
	return nil
}

// addAutoIndexer handles the reconciliation logic for creating/updating AutoIndexer
func (r *AutoIndexerReconciler) addAutoIndexer(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) (ctrl.Result, error) {
	// Check if suspend is true
	// if autoIndexerObj.Spec.Suspend != nil && *autoIndexerObj.Spec.Suspend {
	// 	klog.InfoS("AutoIndexer is suspended, skipping reconciliation", "autoindexer", klog.KObj(autoIndexerObj))
	// 	if err := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, kaitov1alpha1.AutoIndexerConditionTypeScheduled, metav1.ConditionFalse,
	// 		"Suspended", "AutoIndexer is suspended"); err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// 	return ctrl.Result{}, nil
	// }

	// Validate that referenced RAGEngine exists
	if err := r.validateRAGEngineRef(ctx, autoIndexerObj); err != nil {
		if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, kaitov1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue,
			"RAGEngineNotFound", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update autoindexer status", "autoindexer", klog.KObj(autoIndexerObj))
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: time.Minute * 5}, err
	}

	// Handle scheduled vs one-time execution
	if autoIndexerObj.Spec.Schedule != nil {
		// Handle scheduled execution (CronJob)
		if err := r.ensureCronJob(ctx, autoIndexerObj); err != nil {
			if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, kaitov1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue,
				"CronJobFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update autoindexer status", "autoindexer", klog.KObj(autoIndexerObj))
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, err
		}

		if err := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, kaitov1alpha1.AutoIndexerConditionTypeScheduled, metav1.ConditionTrue,
			"Scheduled", "AutoIndexer is scheduled successfully"); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Handle one-time execution (Job)
		if err := r.ensureJob(ctx, autoIndexerObj); err != nil {
			if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, kaitov1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue,
				"JobFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update autoindexer status", "autoindexer", klog.KObj(autoIndexerObj))
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, err
		}
	}

	if err := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, kaitov1alpha1.AutoIndexerConditionTypeSucceeded, metav1.ConditionTrue,
		"AutoIndexerSucceeded", "AutoIndexer succeeds"); err != nil {
		klog.ErrorS(err, "failed to update autoindexer status", "autoindexer", klog.KObj(autoIndexerObj))
		// Don't return error here as the main reconciliation succeeded
	}

	return ctrl.Result{}, nil
}

// validateRAGEngineRef validates that the referenced RAGEngine exists
func (r *AutoIndexerReconciler) validateRAGEngineRef(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	ragEngine := &kaitov1alpha1.RAGEngine{}
	ragEngineKey := client.ObjectKey{
		Name:      autoIndexerObj.Spec.RAGEngineRef.Name,
		Namespace: autoIndexerObj.Spec.RAGEngineRef.Namespace,
	}

	// If namespace is not specified in the ref, use the AutoIndexer's namespace
	if ragEngineKey.Namespace == "" {
		ragEngineKey.Namespace = autoIndexerObj.Namespace
	}

	if err := r.Client.Get(ctx, ragEngineKey, ragEngine); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("referenced RAGEngine %s/%s not found", ragEngineKey.Namespace, ragEngineKey.Name)
		}
		return fmt.Errorf("failed to get referenced RAGEngine: %w", err)
	}

	return nil
}

// ensureCronJob creates or updates a CronJob for scheduled indexing
func (r *AutoIndexerReconciler) ensureCronJob(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	// Generate the CronJob manifest
	config := manifests.JobConfig{
		AutoIndexer:     autoIndexerObj,
		JobName:         fmt.Sprintf("%s-cronjob", autoIndexerObj.Name),
		JobType:         "scheduled-indexing",
		Image:           getImageConfig().GetImage(),
		ImagePullPolicy: "IfNotPresent",
	}

	cronJob := manifests.GenerateIndexingCronJobManifest(config)

	// Set the AutoIndexer as the owner of the CronJob
	if err := controllerutil.SetControllerReference(autoIndexerObj, cronJob, r.Scheme); err != nil {
		return err
	}

	// Check if CronJob already exists
	existingCronJob := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: cronJob.Name, Namespace: cronJob.Namespace}, existingCronJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the CronJob
			klog.InfoS("Creating CronJob", "cronjob", klog.KObj(cronJob), "autoindexer", klog.KObj(autoIndexerObj))
			return r.Create(ctx, cronJob)
		}
		return err
	}

	// Update the existing CronJob if needed
	if !equalCronJobs(existingCronJob, cronJob) {
		klog.InfoS("Updating CronJob", "cronjob", klog.KObj(cronJob), "autoindexer", klog.KObj(autoIndexerObj))
		existingCronJob.Spec = cronJob.Spec
		return r.Update(ctx, existingCronJob)
	}

	return nil
}

// ensureJob creates or updates a Job for one-time indexing
func (r *AutoIndexerReconciler) ensureJob(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	// Generate the Job manifest
	config := manifests.JobConfig{
		AutoIndexer:     autoIndexerObj,
		JobName:         fmt.Sprintf("%s-job", autoIndexerObj.Name),
		JobType:         "one-time-indexing",
		Image:           getImageConfig().GetImage(),
		ImagePullPolicy: "IfNotPresent",
	}

	job := manifests.GenerateIndexingJobManifest(config)

	// Set the AutoIndexer as the owner of the Job
	if err := controllerutil.SetControllerReference(autoIndexerObj, job, r.Scheme); err != nil {
		return err
	}

	// Check if Job already exists
	existingJob := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, existingJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the Job
			klog.InfoS("Creating Job", "job", klog.KObj(job), "autoindexer", klog.KObj(autoIndexerObj))
			return r.Create(ctx, job)
		}
		return err
	}

	// Jobs are immutable once created, so we don't update existing ones
	klog.InfoS("Job already exists", "job", klog.KObj(existingJob), "autoindexer", klog.KObj(autoIndexerObj))
	return nil
}

// deleteAutoIndexer handles cleanup when AutoIndexer is being deleted
func (r *AutoIndexerReconciler) deleteAutoIndexer(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) (ctrl.Result, error) {
	klog.InfoS("Deleting AutoIndexer", "autoindexer", klog.KObj(autoIndexerObj))

	// Clean up owned resources (Jobs, CronJobs, etc.)
	if err := r.cleanupOwnedResources(ctx, autoIndexerObj); err != nil {
		klog.ErrorS(err, "failed to cleanup owned resources", "autoindexer", klog.KObj(autoIndexerObj))
		return ctrl.Result{}, err
	}

	// Remove finalizer
	patch := client.MergeFrom(autoIndexerObj.DeepCopy())
	controllerutil.RemoveFinalizer(autoIndexerObj, consts.AutoIndexerFinalizer)
	if err := r.Client.Patch(ctx, autoIndexerObj, patch); err != nil {
		klog.ErrorS(err, "failed to remove finalizer from autoindexer", "autoindexer", klog.KObj(autoIndexerObj))
		return ctrl.Result{}, err
	}

	klog.InfoS("AutoIndexer deleted successfully", "autoindexer", klog.KObj(autoIndexerObj))
	return ctrl.Result{}, nil
}

// cleanupOwnedResources removes all resources owned by this AutoIndexer
func (r *AutoIndexerReconciler) cleanupOwnedResources(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	// Clean up Jobs
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{
		AutoIndexerNameLabel: autoIndexerObj.Name,
	}); err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, job := range jobs.Items {
		if err := r.Client.Delete(ctx, &job); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete job %s: %w", job.Name, err)
		}
	}

	// Clean up CronJobs
	cronJobs := &batchv1.CronJobList{}
	if err := r.Client.List(ctx, cronJobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{
		AutoIndexerNameLabel: autoIndexerObj.Name,
	}); err != nil {
		return fmt.Errorf("failed to list cronjobs: %w", err)
	}

	for _, cronJob := range cronJobs.Items {
		if err := r.Client.Delete(ctx, &cronJob); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete cronjob %s: %w", cronJob.Name, err)
		}
	}

	return nil
}

// updateStatusConditionIfNotMatch updates the status condition if it doesn't match
func (r *AutoIndexerReconciler) updateStatusConditionIfNotMatch(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer, conditionType kaitov1alpha1.ConditionType, status metav1.ConditionStatus, reason, message string) error {
	// Find existing condition
	var existingCondition *metav1.Condition
	for i := range autoIndexerObj.Status.Conditions {
		if autoIndexerObj.Status.Conditions[i].Type == string(conditionType) {
			existingCondition = &autoIndexerObj.Status.Conditions[i]
			break
		}
	}

	// Check if update is needed
	if existingCondition != nil && existingCondition.Status == status && existingCondition.Reason == reason && existingCondition.Message == message {
		return nil // No update needed
	}

	// Update or add condition
	newCondition := metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	if existingCondition != nil {
		*existingCondition = newCondition
	} else {
		autoIndexerObj.Status.Conditions = append(autoIndexerObj.Status.Conditions, newCondition)
	}

	// Update status
	if err := r.Client.Status().Update(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to update autoindexer status: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutoIndexerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("AutoIndexer")

	return ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.AutoIndexer{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Complete(r)
}

// equalCronJobs compares two CronJob specs for equality
func equalCronJobs(existing, desired *batchv1.CronJob) bool {
	return reflect.DeepEqual(existing.Spec, desired.Spec)
}

type ImageConfig struct {
	RegistryName string
	ImageName    string
	ImageTag     string
}

func (ic ImageConfig) GetImage() string {
	return fmt.Sprintf("%s/%s:%s", ic.RegistryName, ic.ImageName, ic.ImageTag)
}

func getImageConfig() ImageConfig {
	return ImageConfig{
		RegistryName: getEnv("PRESET_AUTO_INDEXER_REGISTRY_NAME", "aimodelsregistrytest.azurecr.io"),
		ImageName:    getEnv("PRESET_AUTO_INDEXER_IMAGE_NAME", "kaito-autoindexer"),
		ImageTag:     getEnv("PRESET_AUTO_INDEXER_IMAGE_TAG", "0.6.0"),
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
