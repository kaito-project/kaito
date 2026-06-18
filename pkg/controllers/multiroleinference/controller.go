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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gaiev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gaiev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/apix/v1alpha2"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
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
	Log                          logr.Logger
	Scheme                       *runtime.Scheme
	Recorder                     record.EventRecorder
	EnableGatewayAPIInferenceExt bool
}

// NewMultiRoleInferenceReconciler creates a new reconciler.
func NewMultiRoleInferenceReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, recorder record.EventRecorder, enableGWIE bool) *MultiRoleInferenceReconciler {
	return &MultiRoleInferenceReconciler{
		Client:                       client,
		Scheme:                       scheme,
		Log:                          log,
		Recorder:                     recorder,
		EnableGatewayAPIInferenceExt: enableGWIE,
	}
}

// +kubebuilder:rbac:groups=kaito.sh,resources=multiroleinferences,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kaito.sh,resources=multiroleinferences/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kaito.sh,resources=multiroleinferences/finalizers,verbs=update
// +kubebuilder:rbac:groups=kaito.sh,resources=inferencesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
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

// garbageCollectMultiRoleInference deletes all child InferenceSets and removes
// the finalizer. OCIRepository and HelmRelease are GC'd via ownerReferences.
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

	// Reconcile InferencePool via Flux OCIRepository + HelmRelease — only when GWIE is enabled.
	// EPP plugins config is passed inline through Helm values (pluginsCustomConfig),
	// so no separate ConfigMap reconciliation is needed.
	if r.EnableGatewayAPIInferenceExt {
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
			desired.Spec.Replicas = role.Replicas
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
	// eppPluginsConfigKey is the filename key for EPP plugins config,
	// used both as the ConfigMap data key and the Helm pluginsConfigFile name.
	eppPluginsConfigKey = "config.yaml"
)

// defaultPDPluginsConfigTemplate is the default EPP plugins YAML template for P/D disaggregated serving.
// Uses the llm-d EndpointPickerConfig format with schedulingProfiles for prefill and decode.
// approx-prefix-cache-producer is used for prefix cache awareness (no tokenizer sidecar needed).
const defaultPDPluginsConfigTemplate = `apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
plugins:
  - type: disagg-headers-handler
  - type: approx-prefix-cache-producer
    parameters:
      blockSizeTokens: 64
      autoTune: true
  - type: prefix-based-pd-decider
    parameters:
      nonCachedTokens: 4
  - type: disagg-profile-handler
    parameters:
      deciders:
        prefill: prefix-based-pd-decider
  - type: by-label-selector
    name: prefill-filter
    parameters:
      matchLabels:
        kaito.sh/inference-role: prefill
  - type: by-label-selector
    name: decode-filter
    parameters:
      matchLabels:
        kaito.sh/inference-role: decode
  - type: load-aware-scorer
    parameters:
      threshold: 10
  - type: max-score-picker
schedulingProfiles:
  - name: prefill
    plugins:
      - pluginRef: prefill-filter
      - pluginRef: load-aware-scorer
        weight: 10
      - pluginRef: max-score-picker
  - name: decode
    plugins:
      - pluginRef: decode-filter
      - pluginRef: load-aware-scorer
        weight: 10
      - pluginRef: max-score-picker
`

// defaultPDPluginsConfig returns the default P/D plugins config.
func defaultPDPluginsConfig() string {
	return defaultPDPluginsConfigTemplate
}

// inferencePoolName returns the name of the InferencePool resources for the MRI.
// Delegates to the shared utils.InferencePoolName helper.
func inferencePoolName(mriName string) string {
	return utils.InferencePoolName(mriName)
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
			URL:      consts.InferencePoolChartURL,
			Interval: metav1.Duration{Duration: 10 * time.Minute},
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
	// InferencePool selects ALL MRI pods (prefill + decode). EPP's internal
	// prefill-filter / decode-filter plugins handle role-based selection.
	// targetPort=5000 (PortInferenceServer) — on decode pods the routing sidecar
	// listens on 5000 and forwards to vLLM on 5001; on prefill pods vLLM
	// listens directly on 5000. EPP routes user requests to decode pods.
	matchLabels := map[string]string{
		kaitov1alpha1.LabelMultiRoleInferenceParent: mri.Name,
		appsv1.PodIndexLabel:                        "0", // Only leader pod (ordinal 0) serves inference traffic
	}

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
	pluginsYAML := defaultPDPluginsConfig()
	if mri.Spec.EPPPluginsConfig != "" {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: mri.Spec.EPPPluginsConfig, Namespace: mri.Namespace}, cm); err != nil {
			return fmt.Errorf("get user-provided EPP plugins ConfigMap %s: %w", mri.Spec.EPPPluginsConfig, err)
		}
		if data, ok := cm.Data[eppPluginsConfigKey]; ok {
			pluginsYAML = data
		} else {
			return fmt.Errorf("EPP plugins ConfigMap %s missing key %s", mri.Spec.EPPPluginsConfig, eppPluginsConfigKey)
		}
	}
	eppValues["pluginsConfigFile"] = eppPluginsConfigKey
	eppValues["pluginsCustomConfig"] = map[string]string{
		eppPluginsConfigKey: pluginsYAML,
	}
	// EPP scrapes vLLM metrics from each pod via PortInferenceServer (5000).
	// On prefill pods vLLM listens directly on 5000; on decode pods the routing
	// sidecar listens on 5000 and transparently proxies /metrics to vLLM on 5001.
	// This keeps a single metrics port across roles, avoiding per-role EPP config.
	// Disable secure-serving so the Gateway can reach EPP over plaintext gRPC
	// without requiring TLS DestinationRules or certificate bootstrapping.
	eppValues["flags"] = map[string]string{
		"secure-serving":            "false",
		"model-server-metrics-port": fmt.Sprintf("%d", consts.PortInferenceServer),
	}
	// No tokenizer sidecar: the EPP plugin pipeline (approx-prefix-cache-producer
	// + prefix-based-pd-decider) does not require a token-producer plugin, so a
	// GPU-less vLLM render process would only add ~500m CPU / 1Gi memory per MRI
	// without any benefit. If/when a future EPP version requires a token producer,
	// re-introduce the sidecar guarded on plugin presence rather than enabling
	// it unconditionally.

	helmValues := map[string]any{
		"inferenceExtension": eppValues,
		"inferencePool": map[string]any{
			"targetPorts": []map[string]any{
				{"number": consts.PortInferenceServer}, // sidecar (decode) or vLLM (prefill) on port 5000
			},
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
			Interval: metav1.Duration{Duration: 10 * time.Minute},
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
// When Gateway API Inference Extension is disabled, no pool is reconciled so we return true.
func (r *MultiRoleInferenceReconciler) isInferencePoolReady(ctx context.Context, mri *kaitov1alpha1.MultiRoleInference) bool {
	if !r.EnableGatewayAPIInferenceExt {
		return true
	}
	poolName := inferencePoolName(mri.Name)
	hr := &helmv2.HelmRelease{}
	if err := r.Get(ctx, client.ObjectKey{Name: poolName, Namespace: mri.Namespace}, hr); err != nil {
		return false
	}
	for _, cond := range hr.Status.Conditions {
		if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue &&
			hr.Status.ObservedGeneration >= hr.Generation {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *MultiRoleInferenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.MultiRoleInference{}).
		Owns(&kaitov1alpha1.InferenceSet{})

	// Only watch Flux resources when Gateway API Inference Extension is enabled,
	// because the Flux CRDs are only installed under that feature gate.
	if r.EnableGatewayAPIInferenceExt {
		// Verify prerequisite CRDs exist before configuring watches.
		for _, gvk := range []schema.GroupVersionKind{
			helmv2.GroupVersion.WithKind(helmv2.HelmReleaseKind),
			sourcev1.GroupVersion.WithKind(sourcev1.OCIRepositoryKind),
			gaiev1.SchemeGroupVersion.WithKind("InferencePool"),
			gaiev1alpha2.SchemeGroupVersion.WithKind("InferenceObjective"),
		} {
			found, err := utils.EnsureKindExists(mgr.GetConfig(), gvk)
			if err != nil {
				return fmt.Errorf("failed to ensure kind %s exists: %w", gvk.Kind, err)
			}
			if !found {
				return fmt.Errorf("%s not found in the cluster, please ensure the Gateway API Inference Extension and Flux are installed", gvk.String())
			}
		}
		builder = builder.
			Owns(&sourcev1.OCIRepository{}).
			Owns(&helmv2.HelmRelease{})
	}

	return builder.
		WithOptions(controller.Options{MaxConcurrentReconciles: 3}).
		Complete(r)
}
