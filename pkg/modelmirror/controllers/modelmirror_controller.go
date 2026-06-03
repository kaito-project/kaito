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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
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
	"github.com/kaito-project/kaito/pkg/modelmirror/storage"
)

const (
	jobRetryInterval = 5 * time.Minute
	giBBytes         = 1024 * 1024 * 1024
)

// ModelMirrorReconciler reconciles ModelMirror objects.
type ModelMirrorReconciler struct {
	client.Client
	Log           logr.Logger
	HTTPClient    *http.Client
	CloudProvider storage.CloudProvider
}

// NewModelMirrorReconciler creates a new reconciler instance.
func NewModelMirrorReconciler(c client.Client, log logr.Logger, cloudProvider storage.CloudProvider) *ModelMirrorReconciler {
	return &ModelMirrorReconciler{
		Client:        c,
		Log:           log,
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		CloudProvider: cloudProvider,
	}
}

// CRName derives the ModelMirror CR name from a model ID (first 6 hex chars of SHA-256).
func CRName(modelID string) string {
	h := sha256.Sum256([]byte(modelID))
	return hex.EncodeToString(h[:])[:6]
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
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 1: Validate CSIDriver
	csiDriverName := r.CloudProvider.CSIDriverName()
	csiDriver := &storagev1.CSIDriver{}
	if err := r.Get(ctx, types.NamespacedName{Name: csiDriverName}, csiDriver); err != nil {
		return r.setFailureAndRequeue(ctx, cr, fmt.Sprintf("CSIDriver %q not found: %v", csiDriverName, err))
	}

	// Step 2: Validate StorageClass
	sc := &storagev1.StorageClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Spec.Storage.StorageClassName}, sc); err != nil {
		return r.setFailureAndRequeue(ctx, cr, fmt.Sprintf("StorageClass %q not found: %v", cr.Spec.Storage.StorageClassName, err))
	}

	// Guard: PVCNamespace must be set (set by workspace controller in Phase 2)
	if cr.Status.PVCNamespace == "" {
		return r.setFailureAndRequeue(ctx, cr, "status.pvcNamespace not set; waiting for workspace controller")
	}

	// Step 3: Resolve storageSize if empty
	if cr.Spec.Storage.StorageSize == "" {
		size, err := r.resolveStorageSize(ctx, cr)
		if err != nil {
			return r.setFailureAndRequeue(ctx, cr, fmt.Sprintf("failed to resolve storage size: %v", err))
		}
		cr.Spec.Storage.StorageSize = size
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 4: Ensure PVC
	if err := r.ensurePVC(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	// Step 5: Ensure download Job
	if err := r.ensureDownloadJob(ctx, cr, log); err != nil {
		return ctrl.Result{}, err
	}

	// Step 6: Check Job status
	return r.checkJobStatus(ctx, cr, log)
}

func (r *ModelMirrorReconciler) handleDeletion(ctx context.Context, cr *kaitov1alpha1.ModelMirror) (ctrl.Result, error) {
	// Remove PVC finalizer
	if cr.Status.PVCName != "" && cr.Status.PVCNamespace != "" {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Name: cr.Status.PVCName, Namespace: cr.Status.PVCNamespace}, pvc); err == nil {
			if controllerutil.ContainsFinalizer(pvc, mmconsts.ModelMirrorPVCFinalizer) {
				controllerutil.RemoveFinalizer(pvc, mmconsts.ModelMirrorPVCFinalizer)
				if err := r.Update(ctx, pvc); err != nil {
					return ctrl.Result{}, err
				}
			}
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
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Status.PVCNamespace}, pvc)
	if err == nil {
		// PVC already exists — check if bound
		cr.Status.PVCName = pvcName
		if pvc.Status.Phase == corev1.ClaimBound {
			setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, "PVCBound", "PVC is bound")
		}
		return r.Status().Update(ctx, cr)
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create PVC
	storageSize := resource.MustParse(cr.Spec.Storage.StorageSize)
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:       pvcName,
			Namespace:  cr.Status.PVCNamespace,
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
	if err := r.Create(ctx, pvc); err != nil {
		return err
	}
	cr.Status.PVCName = pvcName
	return r.Status().Update(ctx, cr)
}

func (r *ModelMirrorReconciler) ensureDownloadJob(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) error {
	jobName := cr.Name + "-download"
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: cr.Status.PVCNamespace}, job)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	job = download.BuildDownloadJob(cr)
	log.Info("Creating download Job", "job", jobName, "namespace", cr.Status.PVCNamespace)
	return r.Create(ctx, job)
}

func (r *ModelMirrorReconciler) checkJobStatus(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) (ctrl.Result, error) {
	jobName := cr.Name + "-download"
	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: cr.Status.PVCNamespace}, job); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// Check for success
	if job.Status.Succeeded > 0 {
		return r.handleJobSuccess(ctx, cr, log)
	}

	// Check for failure
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			log.Info("Download Job failed, deleting for retry", "job", jobName)
			if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			msg := fmt.Sprintf("Download job failed: %s", cond.Message)
			cr.Status.FailureMessage = msg
			setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, "DownloadFailed", msg)
			if err := r.Status().Update(ctx, cr); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: jobRetryInterval}, nil
		}
	}

	// Job still running
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *ModelMirrorReconciler) handleJobSuccess(ctx context.Context, cr *kaitov1alpha1.ModelMirror, log logr.Logger) (ctrl.Result, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Status.PVCName, Namespace: cr.Status.PVCNamespace}, pvc); err != nil {
		return ctrl.Result{}, err
	}

	if pvc.Spec.VolumeName == "" {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	pv := &corev1.PersistentVolume{}
	if err := r.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
		return ctrl.Result{}, err
	}

	if pv.Spec.CSI == nil {
		return ctrl.Result{}, fmt.Errorf("PV %s has no CSI spec", pv.Name)
	}

	accountName, containerName, err := r.CloudProvider.ParseVolumeHandle(pv.Spec.CSI.VolumeHandle)
	if err != nil {
		return ctrl.Result{}, err
	}

	modelID := cr.Spec.Source.ModelID
	cr.Status.Phase = kaitov1alpha1.ModelMirrorPhaseReady
	cr.Status.StorageURI = r.CloudProvider.BuildStorageURI(containerName, modelID)
	cr.Status.AccountName = accountName
	cr.Status.ModelPath = "/models/" + modelID
	cr.Status.FailureMessage = ""
	cr.Status.LastDownloadTime = ptr.To(metav1.Now())

	setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionTrue, "DownloadSucceeded", "Model download completed")
	setCondition(cr, mmconsts.ConditionTypeStorageReady, metav1.ConditionTrue, "PVCBound", "PVC is bound")

	log.Info("ModelMirror is Ready", "storageURI", cr.Status.StorageURI)
	return ctrl.Result{}, r.Status().Update(ctx, cr)
}

func (r *ModelMirrorReconciler) setFailureAndRequeue(ctx context.Context, cr *kaitov1alpha1.ModelMirror, msg string) (ctrl.Result, error) {
	cr.Status.FailureMessage = msg
	cr.Status.Phase = kaitov1alpha1.ModelMirrorPhasePending
	setCondition(cr, mmconsts.ConditionTypeReady, metav1.ConditionFalse, "ReconcileError", msg)
	if err := r.Status().Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// resolveStorageSize queries HuggingFace tree API and computes total safetensors size.
func (r *ModelMirrorReconciler) resolveStorageSize(ctx context.Context, cr *kaitov1alpha1.ModelMirror) (string, error) {
	url := fmt.Sprintf(mmconsts.HFTreeAPITemplate, cr.Spec.Source.ModelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	// Add auth token if access secret provided
	if cr.Spec.Source.AccessSecret != nil {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cr.Spec.Source.AccessSecret.Name,
			Namespace: cr.Spec.Source.AccessSecret.Namespace,
		}, secret); err == nil {
			if token, ok := secret.Data["token"]; ok {
				req.Header.Set("Authorization", "Bearer "+string(token))
			}
		}
	}

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HuggingFace tree API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("HuggingFace tree API returned %d: %s", resp.StatusCode, string(body))
	}

	var entries []struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", fmt.Errorf("failed to decode HF tree API response: %w", err)
	}

	var totalBytes int64
	for _, e := range entries {
		if e.Type == "file" && strings.HasSuffix(e.Path, ".safetensors") && !strings.Contains(e.Path, "/") {
			totalBytes += e.Size
		}
	}

	if totalBytes == 0 {
		return "", fmt.Errorf("no .safetensors files found for model %s", cr.Spec.Source.ModelID)
	}

	// Add 10% buffer and round up to nearest GiB
	buffered := float64(totalBytes) * 1.1
	gib := int64(math.Ceil(buffered / float64(giBBytes)))
	return fmt.Sprintf("%dGi", gib), nil
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
		Owns(&batchv1.Job{}).
		Complete(r)
}
