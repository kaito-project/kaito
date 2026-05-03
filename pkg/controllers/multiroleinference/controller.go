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
	"fmt"

	"github.com/go-logr/logr"
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
)

const (
	// ConditionTypeReady is the condition type for overall readiness.
	ConditionTypeReady = "Ready"

	// ConditionTypeInferenceSetsReady indicates whether child InferenceSets are created.
	ConditionTypeInferenceSetsReady = "InferenceSetsReady"
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

func (r *MultiRoleInferenceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("multiroleinference", req.NamespacedName)

	// Fetch the MultiRoleInference instance.
	mri := &kaitov1alpha1.MultiRoleInference{}
	if err := r.Get(ctx, req.NamespacedName, mri); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciling MultiRoleInference", "name", mri.Name)

	// Create or update child InferenceSets for each role.
	for _, role := range mri.Spec.Roles {
		if err := r.reconcileInferenceSet(ctx, mri, role); err != nil {
			log.Error(err, "Failed to reconcile InferenceSet", "role", role.Type)
			r.Recorder.Eventf(mri, "Warning", "ReconcileFailed",
				"Failed to reconcile %s InferenceSet: %v", role.Type, err)

			meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeInferenceSetsReady,
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

	// Update status: InferenceSets are ready.
	meta.SetStatusCondition(&mri.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeInferenceSetsReady,
		Status:             metav1.ConditionTrue,
		Reason:             "InferenceSetsCreated",
		Message:            "All child InferenceSets have been created or updated",
		ObservedGeneration: mri.Generation,
	})
	mri.Status.ObservedGeneration = mri.Generation

	if err := r.Status().Update(ctx, mri); err != nil {
		log.Error(err, "Failed to update MultiRoleInference status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(mri, "Normal", "Reconciled", "MultiRoleInference reconciled successfully")
	return ctrl.Result{}, nil
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

		// Spec.
		desired.Spec.Replicas = int(role.Replicas)

		// LabelSelector — use the MRI's labelSelector.
		desired.Spec.Selector = mri.Spec.LabelSelector.DeepCopy()

		// Template metadata labels: propagate MRI labelSelector matchLabels + role labels.
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

// SetupWithManager sets up the controller with the Manager.
func (r *MultiRoleInferenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.MultiRoleInference{}).
		Owns(&kaitov1alpha1.InferenceSet{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 3}).
		Complete(r)
}
