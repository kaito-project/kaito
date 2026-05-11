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

package multiroleinference

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (
	// MultiRoleInferenceFinalizer is the finalizer for MultiRoleInference objects.
	MultiRoleInferenceFinalizer = "multiroleinference.kaito.sh/finalizer"

	// ConditionTypeDeleting indicates the MRI is being deleted.
	ConditionTypeDeleting = "Deleting"
)

// MultiRoleInferenceReconciler reconciles a MultiRoleInference object.
type MultiRoleInferenceReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// NewMultiRoleInferenceReconciler creates a new reconciler.
func NewMultiRoleInferenceReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, recorder record.EventRecorder) *MultiRoleInferenceReconciler {
	return &MultiRoleInferenceReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      log,
		Recorder: recorder,
	}
}

// +kubebuilder:rbac:groups=kaito.sh,resources=multiroleinferences,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kaito.sh,resources=multiroleinferences/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kaito.sh,resources=multiroleinferences/finalizers,verbs=update
// +kubebuilder:rbac:groups=kaito.sh,resources=inferencesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete

func (r *MultiRoleInferenceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("multiroleinference", req.NamespacedName)

	// Fetch the MultiRoleInference instance.
	mri := &kaitov1alpha1.MultiRoleInference{}
	if err := r.Get(ctx, req.NamespacedName, mri); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("MultiRoleInference not found, might be deleted already", "multiroleinference", req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion vs normal reconciliation.
	if mri.DeletionTimestamp.IsZero() {
		// Ensure finalizer is present.
		if err := r.ensureFinalizer(ctx, mri); err != nil {
			return ctrl.Result{}, err
		}
		return r.addOrUpdateMultiRoleInference(ctx, log, mri)
	}

	// MRI is being deleted — run garbage collection.
	return r.deleteMultiRoleInference(ctx, log, mri)
}

// ensureFinalizer adds the finalizer to the MRI if not already present.
func (r *MultiRoleInferenceReconciler) ensureFinalizer(ctx context.Context, mri *kaitov1alpha1.MultiRoleInference) error {
	if !controllerutil.ContainsFinalizer(mri, MultiRoleInferenceFinalizer) {
		controllerutil.AddFinalizer(mri, MultiRoleInferenceFinalizer)
		if err := r.Update(ctx, mri); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer on the multiroleinference", "multiroleinference", klog.KObj(mri))
			return err
		}
	}
	return nil
}

// deleteMultiRoleInference handles MRI deletion: sets Deleting condition, GCs children, removes finalizer.
func (r *MultiRoleInferenceReconciler) deleteMultiRoleInference(ctx context.Context, log logr.Logger, mri *kaitov1alpha1.MultiRoleInference) (ctrl.Result, error) {
	klog.InfoS("deleteMultiRoleInference", "multiroleinference", klog.KObj(mri))

	// Set Deleting condition.
	meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeDeleting,
		Status:             metav1.ConditionTrue,
		Reason:             "MultiRoleInferenceDeleted",
		Message:            "MultiRoleInference is being deleted",
		ObservedGeneration: mri.Generation,
	})
	if err := r.Status().Update(ctx, mri); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "failed to update deleting status", "multiroleinference", klog.KObj(mri))
	}

	return r.garbageCollectMultiRoleInference(ctx, log, mri)
}

// garbageCollectMultiRoleInference deletes all child InferenceSets, cleans up auto-generated
// ConfigMaps, and removes the finalizer. OCIRepository and HelmRelease are GC'd via ownerReferences.
func (r *MultiRoleInferenceReconciler) garbageCollectMultiRoleInference(ctx context.Context, log logr.Logger, mri *kaitov1alpha1.MultiRoleInference) (ctrl.Result, error) {
	// List all child InferenceSets owned by this MRI.
	isList := &kaitov1alpha1.InferenceSetList{}
	if err := r.List(ctx, isList, client.InNamespace(mri.Namespace), client.MatchingLabels{
		kaitov1alpha1.LabelMultiRoleInferenceParent: mri.Name,
	}); err != nil {
		klog.ErrorS(err, "failed to list child InferenceSets", "multiroleinference", klog.KObj(mri))
		return ctrl.Result{}, err
	}

	// Delete each child InferenceSet that hasn't been deleted yet.
	for i := range isList.Items {
		is := &isList.Items[i]
		if is.DeletionTimestamp.IsZero() {
			klog.InfoS("Deleting child InferenceSet", "inferenceset", klog.KObj(is), "multiroleinference", klog.KObj(mri))
			if err := r.Delete(ctx, is, &client.DeleteOptions{}); err != nil {
				if !apierrors.IsNotFound(err) {
					klog.ErrorS(err, "failed to delete child InferenceSet", "inferenceset", klog.KObj(is))
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Wait until all child InferenceSets are fully removed before removing the finalizer.
	if len(isList.Items) > 0 {
		klog.InfoS("Waiting for child InferenceSets to be fully deleted", "remaining", len(isList.Items), "multiroleinference", klog.KObj(mri))
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Clean up auto-generated EPP plugins ConfigMap (only if we created it).
	if mri.Spec.EPPPluginsConfig == "" {
		cmName := eppPluginsConfigMapName(mri.Name)
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: mri.Namespace}, cm); err == nil {
			klog.InfoS("Deleting auto-generated EPP plugins ConfigMap", "configmap", cmName, "multiroleinference", klog.KObj(mri))
			if err := r.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "failed to delete EPP plugins ConfigMap", "configmap", cmName)
				return ctrl.Result{}, err
			}
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	// Remove the finalizer.
	controllerutil.RemoveFinalizer(mri, MultiRoleInferenceFinalizer)
	if err := r.Update(ctx, mri); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "failed to update the multiroleinference to remove finalizer", "multiroleinference", klog.KObj(mri))
		return ctrl.Result{}, err
	}

	klog.InfoS("Successfully removed the multiroleinference finalizer", "multiroleinference", klog.KObj(mri))
	r.Recorder.Event(mri, "Normal", "Deleted", "MultiRoleInference deleted and child InferenceSets cleaned up")
	return ctrl.Result{}, nil
}

// addOrUpdateMultiRoleInference handles normal reconciliation: create/update child InferenceSets.
func (r *MultiRoleInferenceReconciler) addOrUpdateMultiRoleInference(ctx context.Context, log logr.Logger, mri *kaitov1alpha1.MultiRoleInference) (ctrl.Result, error) {
	log.Info("Reconciling MultiRoleInference", "name", mri.Name)

	// Create or update child InferenceSets for each role.
	for _, role := range mri.Spec.Roles {
		if err := r.reconcileInferenceSet(ctx, mri, role); err != nil {
			log.Error(err, "Failed to reconcile InferenceSet", "role", role.Type)
			r.Recorder.Eventf(mri, "Warning", "ReconcileFailed",
				"Failed to reconcile %s InferenceSet: %v", role.Type, err)

			meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
				Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypeReady),
				Status:             metav1.ConditionFalse,
				Reason:             "ReconcileFailed",
				Message:            fmt.Sprintf("Failed to reconcile %s InferenceSet: %v", role.Type, err),
				ObservedGeneration: mri.Generation,
			})
			if statusErr := r.Status().Update(ctx, mri); statusErr != nil {
				log.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	// Clean up stale InferenceSets (roles removed from spec).
	if err := r.cleanupStaleInferenceSets(ctx, mri); err != nil {
		log.Error(err, "Failed to cleanup stale InferenceSets")
		return ctrl.Result{}, err
	}

	// Reconcile EPP plugins ConfigMap (Step 5).
	if err := r.reconcileEPPPluginsConfigMap(ctx, mri); err != nil {
		log.Error(err, "Failed to reconcile EPP plugins ConfigMap")
		r.Recorder.Eventf(mri, "Warning", "ReconcileFailed",
			"Failed to reconcile EPP plugins ConfigMap: %v", err)
		meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
			Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypeReady),
			Status:             metav1.ConditionFalse,
			Reason:             "ReconcileFailed",
			Message:            fmt.Sprintf("Failed to reconcile EPP plugins ConfigMap: %v", err),
			ObservedGeneration: mri.Generation,
		})
		if statusErr := r.Status().Update(ctx, mri); statusErr != nil {
			log.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Reconcile InferencePool via Flux OCIRepository + HelmRelease (Step 4).
	if err := r.reconcileInferencePool(ctx, mri); err != nil {
		log.Error(err, "Failed to reconcile InferencePool")
		r.Recorder.Eventf(mri, "Warning", "ReconcileFailed",
			"Failed to reconcile InferencePool: %v", err)
		meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
			Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypeReady),
			Status:             metav1.ConditionFalse,
			Reason:             "ReconcileFailed",
			Message:            fmt.Sprintf("Failed to reconcile InferencePool: %v", err),
			ObservedGeneration: mri.Generation,
		})
		if statusErr := r.Status().Update(ctx, mri); statusErr != nil {
			log.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Aggregate status from child InferenceSets.
	if err := r.aggregateStatus(ctx, log, mri); err != nil {
		log.Error(err, "Failed to aggregate status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(mri, "Normal", "Reconciled", "MultiRoleInference reconciled successfully")
	return ctrl.Result{}, nil
}

// aggregateStatus reads child InferenceSet conditions and updates MRI status accordingly.
func (r *MultiRoleInferenceReconciler) aggregateStatus(ctx context.Context, log logr.Logger, mri *kaitov1alpha1.MultiRoleInference) error {
	// List all child InferenceSets.
	isList := &kaitov1alpha1.InferenceSetList{}
	if err := r.List(ctx, isList, client.InNamespace(mri.Namespace), client.MatchingLabels{
		kaitov1alpha1.LabelMultiRoleInferenceParent: mri.Name,
	}); err != nil {
		return err
	}

	// Build a map from role → InferenceSet.
	roleISMap := make(map[string]*kaitov1alpha1.InferenceSet)
	for i := range isList.Items {
		is := &isList.Items[i]
		if roleLabel, ok := is.Labels[kaitov1alpha1.LabelInferenceRole]; ok {
			roleISMap[roleLabel] = is
		}
	}

	// Check individual role readiness.
	prefillReady := r.isInferenceSetReady(roleISMap[string(kaitov1alpha1.MultiRoleInferenceRolePrefill)])
	decodeReady := r.isInferenceSetReady(roleISMap[string(kaitov1alpha1.MultiRoleInferenceRoleDecode)])

	// TODO: include inferencePoolReady once InferencePool creation is implemented (Step 4).
	// For now, InferencePool is always not ready.
	inferencePoolReady := r.isInferencePoolReady(ctx, mri)
	allReady := prefillReady && decodeReady && inferencePoolReady

	// Set prefill condition.
	condStatus := metav1.ConditionFalse
	reason := "PrefillNotReady"
	message := "Prefill InferenceSet is not ready"
	if prefillReady {
		condStatus = metav1.ConditionTrue
		reason = "PrefillReady"
		message = "Prefill InferenceSet is ready"
	} else {
		if _, exists := roleISMap[string(kaitov1alpha1.MultiRoleInferenceRolePrefill)]; !exists {
			reason = "PrefillNotFound"
			message = "Prefill InferenceSet not found"
		}
	}
	meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
		Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypePrefillReady),
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: mri.Generation,
	})

	// Check decode InferenceSet readiness.
	condStatus = metav1.ConditionFalse
	reason = "DecodeNotReady"
	message = "Decode InferenceSet is not ready"
	if decodeReady {
		condStatus = metav1.ConditionTrue
		reason = "DecodeReady"
		message = "Decode InferenceSet is ready"
	} else {
		if _, exists := roleISMap[string(kaitov1alpha1.MultiRoleInferenceRoleDecode)]; !exists {
			reason = "DecodeNotFound"
			message = "Decode InferenceSet not found"
		}
	}
	meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
		Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypeDecodeReady),
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: mri.Generation,
	})

	// Check InferencePool readiness.
	condStatus = metav1.ConditionFalse
	reason = "InferencePoolNotReady"
	message = "InferencePool is not ready"
	if inferencePoolReady {
		condStatus = metav1.ConditionTrue
		reason = "InferencePoolReady"
		message = "InferencePool is ready"
	}
	meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
		Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypeInferencePoolReady),
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: mri.Generation,
	})

	// Set overall Ready condition.
	overallStatus := metav1.ConditionFalse
	overallReason := "NotReady"
	overallMessage := "Not all components are ready"
	if allReady {
		overallStatus = metav1.ConditionTrue
		overallReason = "Ready"
		overallMessage = "All components are ready"
	}
	meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
		Type:               string(kaitov1alpha1.MultiRoleInferenceConditionTypeReady),
		Status:             overallStatus,
		Reason:             overallReason,
		Message:            overallMessage,
		ObservedGeneration: mri.Generation,
	})

	mri.Status.ObservedGeneration = mri.Generation
	return r.Status().Update(ctx, mri)
}

// isInferenceSetReady checks if an InferenceSet has the InferenceSetReady condition set to True.
func (r *MultiRoleInferenceReconciler) isInferenceSetReady(is *kaitov1alpha1.InferenceSet) bool {
	if is == nil {
		return false
	}
	for _, cond := range is.Status.Conditions {
		if cond.Type == string(kaitov1alpha1.InferenceSetConditionTypeReady) && cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// cleanupStaleInferenceSets deletes InferenceSets whose role has been removed from the MRI spec.
func (r *MultiRoleInferenceReconciler) cleanupStaleInferenceSets(ctx context.Context, mri *kaitov1alpha1.MultiRoleInference) error {
	// Build set of expected InferenceSet names.
	expectedNames := make(map[string]bool, len(mri.Spec.Roles))
	for _, role := range mri.Spec.Roles {
		expectedNames[fmt.Sprintf("%s-%s", mri.Name, role.Type)] = true
	}

	// List all child InferenceSets.
	isList := &kaitov1alpha1.InferenceSetList{}
	if err := r.List(ctx, isList, client.InNamespace(mri.Namespace), client.MatchingLabels{
		kaitov1alpha1.LabelMultiRoleInferenceParent: mri.Name,
	}); err != nil {
		return err
	}

	for i := range isList.Items {
		is := &isList.Items[i]
		if !expectedNames[is.Name] && is.DeletionTimestamp.IsZero() {
			klog.InfoS("Deleting stale InferenceSet", "inferenceset", klog.KObj(is), "multiroleinference", klog.KObj(mri))
			if err := r.Delete(ctx, is, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// reconcileInferenceSet creates or updates a child InferenceSet for the given role.
func (r *MultiRoleInferenceReconciler) reconcileInferenceSet(
	ctx context.Context,
	mri *kaitov1alpha1.MultiRoleInference,
	role kaitov1alpha1.MultiRoleInferenceRoleSpec,
) error {
	isName := fmt.Sprintf("%s-%s", mri.Name, role.Type)
	roleStr := string(role.Type)

	// Build the desired InferenceSet.
	desired := &kaitov1alpha1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      isName,
			Namespace: mri.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		// Set owner reference so the InferenceSet is garbage-collected with the MRI.
		if err := controllerutil.SetControllerReference(mri, desired, r.Scheme); err != nil {
			return err
		}

		// Labels on the InferenceSet metadata.
		if desired.Labels == nil {
			desired.Labels = make(map[string]string)
		}
		desired.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mri.Name
		desired.Labels[kaitov1alpha1.LabelInferenceRole] = roleStr

		// Spec — only reconcile replicas when explicitly set (non-nil).
		// When nil, autoscaling is assumed and the controller skips replica reconciliation.
		if role.Replicas != nil {
			desired.Spec.Replicas = int(*role.Replicas)
		}

		// LabelSelector — start from the MRI's labelSelector and inject role info.
		// The InferenceSet controller propagates Spec.Selector to workspace.Resource.LabelSelector,
		// so role-specific labels must be in the selector to ensure correct node selection.
		desired.Spec.Selector = mri.Spec.LabelSelector.DeepCopy()
		if desired.Spec.Selector == nil {
			desired.Spec.Selector = &metav1.LabelSelector{}
		}
		if desired.Spec.Selector.MatchLabels == nil {
			desired.Spec.Selector.MatchLabels = make(map[string]string)
		}
		desired.Spec.Selector.MatchLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mri.Name
		desired.Spec.Selector.MatchLabels[kaitov1alpha1.LabelInferenceRole] = roleStr

		// Template metadata labels: propagate selector matchLabels (includes role labels).
		templateLabels := make(map[string]string)
		if mri.Spec.LabelSelector != nil && mri.Spec.LabelSelector.MatchLabels != nil {
			for k, v := range mri.Spec.LabelSelector.MatchLabels {
				templateLabels[k] = v
			}
		}
		templateLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mri.Name
		templateLabels[kaitov1alpha1.LabelInferenceRole] = roleStr
		desired.Spec.Template.Labels = templateLabels

		// Resource.
		desired.Spec.Template.Resource = kaitov1alpha1.InferenceSetResourceSpec{
			InstanceType: role.InstanceType,
		}

		// Inference — preset with shared model config.
		desired.Spec.Template.Inference = kaitov1beta1.InferenceSpec{
			Preset: &kaitov1beta1.PresetSpec{
				PresetMeta: kaitov1beta1.PresetMeta{
					Name: kaitov1beta1.ModelName(mri.Spec.Model.Name),
				},
				PresetOptions: kaitov1beta1.PresetOptions{
					ModelAccessSecret: mri.Spec.Model.ModelAccessSecret,
				},
			},
		}

		// Role-specific runtime config.
		if role.RuntimeConfig != "" {
			desired.Spec.Template.Inference.Config = role.RuntimeConfig
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("CreateOrUpdate InferenceSet %s: %w", isName, err)
	}

	klog.V(2).InfoS("Reconciled InferenceSet",
		"name", isName,
		"role", roleStr,
		"result", result,
	)

	return nil
}

const (
	// eppPluginsConfigMapKey is the key used in the ConfigMap data for EPP plugins config.
	eppPluginsConfigMapKey = "plugins-config.yaml"

	// defaultPDPluginsConfig is the default EPP plugins YAML for P/D disaggregated serving.
	defaultPDPluginsConfig = `plugins:
  preSchedule:
    - name: disagg-profile-handler
  filter:
    - name: by-label-selector
      args:
        key: "kaito.sh/inference-role"
        value: "decode"
  scorer:
    - name: prefix-cache-scorer
      weight: 80
    - name: load-aware-scorer
      weight: 20
  postSchedule:
    - name: disagg-headers-handler
  prefillFilter:
    - name: by-label-selector
      args:
        key: "kaito.sh/inference-role"
        value: "prefill"
  prefillScorer:
    - name: prefix-cache-scorer
      weight: 50
    - name: load-aware-scorer
      weight: 50
`
	// portRoutingSidecar is the port the routing sidecar listens on for decode pods.
	portRoutingSidecar = int32(8080)
)

// eppPluginsConfigMapName returns the name of the auto-generated EPP plugins ConfigMap.
func eppPluginsConfigMapName(mriName string) string {
	return fmt.Sprintf("%s-epp-plugins", mriName)
}

// inferencePoolName returns the name of the InferencePool resources for the MRI.
func inferencePoolName(mriName string) string {
	return fmt.Sprintf("%s-inferencepool", mriName)
}

// reconcileEPPPluginsConfigMap creates or updates the default P/D EPP plugins ConfigMap
// when spec.eppPluginsConfig is not set. If the user has provided a custom ConfigMap name,
// this is a no-op.
func (r *MultiRoleInferenceReconciler) reconcileEPPPluginsConfigMap(
	ctx context.Context,
	mri *kaitov1alpha1.MultiRoleInference,
) error {
	// If the user provided a custom EPP plugins ConfigMap, skip auto-generation.
	if mri.Spec.EPPPluginsConfig != "" {
		return nil
	}

	cmName := eppPluginsConfigMapName(mri.Name)
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: mri.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		if err := controllerutil.SetControllerReference(mri, desired, r.Scheme); err != nil {
			return err
		}
		if desired.Labels == nil {
			desired.Labels = make(map[string]string)
		}
		desired.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mri.Name

		desired.Data = map[string]string{
			eppPluginsConfigMapKey: defaultPDPluginsConfig,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("CreateOrUpdate EPP plugins ConfigMap %s: %w", cmName, err)
	}

	klog.V(2).InfoS("Reconciled EPP plugins ConfigMap",
		"name", cmName,
		"result", result,
	)
	return nil
}

// reconcileInferencePool creates or updates the Flux OCIRepository and HelmRelease
// that render an InferencePool CR + EPP deployment, owned by the MRI.
func (r *MultiRoleInferenceReconciler) reconcileInferencePool(
	ctx context.Context,
	mri *kaitov1alpha1.MultiRoleInference,
) error {
	poolName := inferencePoolName(mri.Name)

	// --- OCIRepository ---
	ociRepo := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolName,
			Namespace: mri.Namespace,
		},
	}

	ociResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, ociRepo, func() error {
		if err := controllerutil.SetControllerReference(mri, ociRepo, r.Scheme); err != nil {
			return err
		}
		ociRepo.Spec = sourcev1.OCIRepositorySpec{
			URL: consts.InferencePoolChartURL,
			Reference: &sourcev1.OCIRepositoryRef{
				Tag: consts.InferencePoolChartVersion,
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("CreateOrUpdate OCIRepository %s: %w", poolName, err)
	}
	klog.V(2).InfoS("Reconciled InferencePool OCIRepository",
		"name", poolName,
		"result", ociResult,
	)

	// --- HelmRelease ---
	// Build matchLabels: MRI's labelSelector matchLabels + pod-index "0".
	matchLabels := make(map[string]string)
	if mri.Spec.LabelSelector != nil && mri.Spec.LabelSelector.MatchLabels != nil {
		for k, v := range mri.Spec.LabelSelector.MatchLabels {
			matchLabels[k] = v
		}
	}
	matchLabels[appsv1.PodIndexLabel] = "0"

	// Build EPP extension values with llm-d image and P/D plugins config.
	eppValues := map[string]any{
		"image": map[string]string{
			"hub":        consts.EPPImageHub,
			"name":       consts.EPPImageName,
			"tag":        consts.EPPImageTag,
			"pullPolicy": string(corev1.PullIfNotPresent),
		},
	}

	// Load plugins config: either from user-provided ConfigMap or auto-generated default.
	pluginsYAML := defaultPDPluginsConfig
	if mri.Spec.EPPPluginsConfig != "" {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: mri.Spec.EPPPluginsConfig, Namespace: mri.Namespace}, cm); err != nil {
			return fmt.Errorf("get user-provided EPP plugins ConfigMap %s: %w", mri.Spec.EPPPluginsConfig, err)
		}
		if data, ok := cm.Data[eppPluginsConfigMapKey]; ok {
			pluginsYAML = data
		} else {
			return fmt.Errorf("EPP plugins ConfigMap %s missing key %s", mri.Spec.EPPPluginsConfig, eppPluginsConfigMapKey)
		}
	}
	eppValues["pluginsConfigFile"] = eppPluginsConfigMapKey
	eppValues["pluginsCustomConfig"] = map[string]string{
		eppPluginsConfigMapKey: pluginsYAML,
	}

	helmValues := map[string]any{
		"inferenceExtension": eppValues,
		"inferencePool": map[string]any{
			"targetPorts": []map[string]any{{
				"number": portRoutingSidecar,
			}},
			"modelServers": map[string]any{
				"matchLabels": matchLabels,
			},
		},
	}
	rawHelmValues, err := json.Marshal(helmValues)
	if err != nil {
		return fmt.Errorf("marshal HelmRelease values: %w", err)
	}

	helmRelease := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolName,
			Namespace: mri.Namespace,
		},
	}

	helmResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmRelease, func() error {
		if err := controllerutil.SetControllerReference(mri, helmRelease, r.Scheme); err != nil {
			return err
		}
		helmRelease.Spec = helmv2.HelmReleaseSpec{
			ChartRef: &helmv2.CrossNamespaceSourceReference{
				Kind:      sourcev1.OCIRepositoryKind,
				Namespace: mri.Namespace,
				Name:      poolName,
			},
			Values: &apiextensionsv1.JSON{
				Raw: rawHelmValues,
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("CreateOrUpdate HelmRelease %s: %w", poolName, err)
	}
	klog.V(2).InfoS("Reconciled InferencePool HelmRelease",
		"name", poolName,
		"result", helmResult,
	)

	return nil
}

// isInferencePoolReady checks if the InferencePool HelmRelease is ready.
func (r *MultiRoleInferenceReconciler) isInferencePoolReady(ctx context.Context, mri *kaitov1alpha1.MultiRoleInference) bool {
	poolName := inferencePoolName(mri.Name)
	hr := &helmv2.HelmRelease{}
	if err := r.Get(ctx, client.ObjectKey{Name: poolName, Namespace: mri.Namespace}, hr); err != nil {
		return false
	}
	for _, cond := range hr.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *MultiRoleInferenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.MultiRoleInference{}).
		Owns(&kaitov1alpha1.InferenceSet{}).
		Owns(&sourcev1.OCIRepository{}).
		Owns(&helmv2.HelmRelease{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 3}).
		Complete(r)
}
