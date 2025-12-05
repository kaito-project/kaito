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

package inferenceset

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gaiev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gaiev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/apix/v1alpha2"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/inferenceset"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

const (
	InferenceSetHashAnnotation = "inferenceset.kaito.io/hash"
	InferenceSetNameLabel      = "inferenceset.kaito.io/name"
	revisionHashSuffix         = 5
)

type InferenceSetReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	klogger      klog.Logger
	expectations *utils.ControllerExpectations
}

func NewInferenceSetReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, Recorder record.EventRecorder) *InferenceSetReconciler {
	expectations := utils.NewControllerExpectations()
	return &InferenceSetReconciler{
		Client:       client,
		Scheme:       scheme,
		Log:          log,
		klogger:      klog.NewKlogr().WithName("InferenceSetController"),
		Recorder:     Recorder,
		expectations: expectations,
	}
}

func (c *InferenceSetReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	iObj := &kaitov1alpha1.InferenceSet{}
	if err := c.Client.Get(ctx, req.NamespacedName, iObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.klogger, req.String())
			klog.InfoS("Inference set not found, might be deleted already", "inference set", req.Name)
			return reconcile.Result{}, nil
		}
		klog.ErrorS(err, "failed to get inference set", "inference set", req.Name)
		return reconcile.Result{}, err
	}

	klog.InfoS("Reconciling", "inference set", req.NamespacedName, "name", req.Name)
	if iObj.DeletionTimestamp.IsZero() {
		if err := c.ensureFinalizer(ctx, iObj); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Handle deleting inferenceset, garbage collect all the resources.
		return c.deleteInferenceSet(ctx, iObj)
	}

	if err := c.syncControllerRevision(ctx, iObj); err != nil {
		return reconcile.Result{}, err
	}

	return c.addOrUpdateInferenceSet(ctx, iObj)
}

func (c *InferenceSetReconciler) ensureFinalizer(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) error {
	if !controllerutil.ContainsFinalizer(iObj, consts.InferenceSetFinalizer) {
		patch := client.MergeFrom(iObj.DeepCopy())
		controllerutil.AddFinalizer(iObj, consts.InferenceSetFinalizer)
		if err := c.Client.Patch(ctx, iObj, patch); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer to the inference set", "inference set", klog.KObj(iObj))
			return err
		}
	}
	return nil
}

func (c *InferenceSetReconciler) deleteInferenceSet(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	klog.InfoS("deleteInferenceSet", "inferenceset", klog.KObj(iObj))
	err := inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeDeleting, metav1.ConditionTrue, "inferencesetDeleted", "inferenceset is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
		return reconcile.Result{}, err
	}

	return c.garbageCollectInferenceSet(ctx, iObj)
}

// garbageCollectInferenceSet remove finalizer associated with inferenceset object.
func (c *InferenceSetReconciler) garbageCollectInferenceSet(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) (ctrl.Result, error) {
	klog.InfoS("garbageCollectInferenceSet", "inferenceset", klog.KObj(iObj))
	// Check if there are any workspaces associated with this inferenceset.
	wsList, err := inferenceset.ListWorkspaces(ctx, iObj, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// We should delete all the workspaces that are created by this inferenceset
	for i := range wsList.Items {
		if wsList.Items[i].DeletionTimestamp.IsZero() {
			klog.InfoS("Deleting associated Workspace...", "workspace", wsList.Items[i].Name)
			if deleteErr := c.Delete(ctx, &wsList.Items[i], &client.DeleteOptions{}); deleteErr != nil {
				klog.ErrorS(deleteErr, "failed to delete the workspace", "workspace", klog.KObj(&wsList.Items[i]))
				return ctrl.Result{}, deleteErr
			}
		}
	}

	updateErr := inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, iObj, func(ws *kaitov1alpha1.InferenceSet) error {
		controllerutil.RemoveFinalizer(ws, consts.InferenceSetFinalizer)
		return nil
	})
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			return ctrl.Result{}, nil
		}
		klog.ErrorS(updateErr, "failed to update the inferenceset to remove finalizer", "inferenceset", klog.KObj(iObj))
		return ctrl.Result{}, updateErr
	}

	klog.InfoS("successfully removed the inferenceset finalizers", "inferenceset", klog.KObj(iObj))
	return ctrl.Result{}, nil
}

func (c *InferenceSetReconciler) addOrUpdateInferenceSet(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	if iObj == nil {
		return reconcile.Result{}, nil
	}

	isKey := client.ObjectKeyFromObject(iObj).String()
	if !c.expectations.SatisfiedExpectations(c.Log, isKey) {
		klog.V(4).InfoS("Waiting for expectations to be satisfied", "inferenceset", isKey)
		return reconcile.Result{}, nil
	}

	// Check if there are any existing workspaces associated with this inferenceset.
	wsList, err := inferenceset.ListWorkspaces(ctx, iObj, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	klog.InfoS("Found workspaces for inference set", "name", iObj.Name, "current", len(wsList.Items), "desired", iObj.Spec.Replicas)

	replicaNumToDelete := len(wsList.Items) - iObj.Spec.Replicas
	var deletingWorkspaces []string
	if replicaNumToDelete > 0 {
		klog.InfoS("Found extra workspaces, deleting...", "current", len(wsList.Items), "desired", iObj.Spec.Replicas)
		// first delete workspace that is not in ready state
		for _, ws := range wsList.Items {
			if !ws.DeletionTimestamp.IsZero() {
				deletingWorkspaces = append(deletingWorkspaces, ws.Name)
				replicaNumToDelete--
				klog.InfoS("Skipping workspace that is already being deleted...", "workspace", klog.KObj(&ws))
			} else if controllers.DetermineWorkspacePhase(&ws) != "succeeded" {
				klog.InfoS("Deleting non-ready workspace...", "workspace", klog.KObj(&ws))
				if err := c.Client.Delete(ctx, &ws, &client.DeleteOptions{}); err != nil {
					klog.ErrorS(err, "failed to delete non-ready workspace", "workspace", klog.KObj(&ws))
					return ctrl.Result{}, err
				}
				deletingWorkspaces = append(deletingWorkspaces, ws.Name)
				replicaNumToDelete--
			}
			if replicaNumToDelete <= 0 {
				break
			}
		}

		// delete rest of extra workspaces
		if replicaNumToDelete > 0 {
			for _, ws := range wsList.Items {
				// check whether ws.Name is already in deletingWorkspaces
				if slices.Contains(deletingWorkspaces, ws.Name) {
					continue
				}

				if !ws.DeletionTimestamp.IsZero() {
					replicaNumToDelete--
					klog.InfoS("Skipping workspace that is already being deleted...", "workspace", klog.KObj(&ws))
				} else {
					klog.InfoS("Deleting extra workspace...", "workspace", klog.KObj(&ws))
					if err := c.Client.Delete(ctx, &ws, &client.DeleteOptions{}); err != nil {
						klog.ErrorS(err, "failed to delete extra workspace", "workspace", klog.KObj(&ws))
						return ctrl.Result{}, err
					}
					replicaNumToDelete--
				}
				if replicaNumToDelete <= 0 {
					break
				}
			}
		}

		// After deleting the extra workspaces, we should requeue to wait for the deletion to complete
		if wsList, err = inferenceset.ListWorkspaces(ctx, iObj, c.Client); err != nil {
			return ctrl.Result{}, err
		}
	}

	replicaNumToCreate := iObj.Spec.Replicas - len(wsList.Items)
	if replicaNumToCreate > 0 {
		klog.InfoS("Need to create more workspaces...", "current", len(wsList.Items), "desired", iObj.Spec.Replicas)
		for i := range replicaNumToCreate {
			workspaceObj := &kaitov1beta1.Workspace{}
			workspaceObj.GenerateName = iObj.Name + "-"
			workspaceObj.Namespace = iObj.Namespace
			workspaceObj.Labels = map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: iObj.Name,
			}
			workspaceObj.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(iObj, kaitov1alpha1.GroupVersion.WithKind("InferenceSet")),
			}
			workspaceObj.Resource = kaitov1beta1.ResourceSpec{
				InstanceType:  iObj.Spec.Template.Resource.InstanceType,
				LabelSelector: iObj.Spec.Selector,
			}
			workspaceObj.Inference = &iObj.Spec.Template.Inference

			klog.InfoS("creating workspace", "workspace", workspaceObj.Name, "index", i)
			if err := c.Client.Create(ctx, workspaceObj); err != nil {
				klog.ErrorS(err, "failed to create workspace", "workspace", workspaceObj.Name)
				return reconcile.Result{}, err
			}
		}
	}

	// check whether all the workspaces are ready
	readyReplicas := 0
	for _, ws := range wsList.Items {
		if controllers.DetermineWorkspacePhase(&ws) == "succeeded" {
			readyReplicas++
		}
	}

	// update the replicas in the status
	if err = inferenceset.UpdateInferenceSetStatus(ctx, c.Client, &client.ObjectKey{Name: iObj.Name, Namespace: iObj.Namespace}, func(status *kaitov1alpha1.InferenceSetStatus) error {
		status.Replicas = iObj.Spec.Replicas
		status.ReadyReplicas = readyReplicas
		// set selector for HPA/VPA
		status.Selector = fmt.Sprintf("%s=%s", consts.WorkspaceCreatedByInferenceSetLabel, iObj.Name)
		return nil
	}); err != nil {
		klog.ErrorS(err, "failed to update inferenceset replicas", "inferenceset", klog.KObj(iObj))
		return reconcile.Result{}, err
	}

	if readyReplicas == iObj.Spec.Replicas {
		if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeReady, metav1.ConditionTrue,
			"inferencesetReady", "inferenceset is ready"); err != nil {
			klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, err
		}
	} else {
		if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeReady, metav1.ConditionFalse,
			"inferencesetNotReady", fmt.Sprintf("inferenceset is not ready, %d/%d replicas are ready", readyReplicas, iObj.Spec.Replicas)); err != nil {
			klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, err
		}
	}

	if err = c.ensureGatewayAPIInferenceExtension(ctx, iObj); err != nil {
		if updateErr := inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeReady, metav1.ConditionFalse,
			"inferencesetFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, updateErr
		}
	}

	return reconcile.Result{}, nil
}

// ensureGatewayAPIInferenceExtension reconciles Gateway API Inference Extension components for a InferenceSet.
//
// How it works:
// 1) Dry-runs preset inference generation to determine if the target workload is a StatefulSet.
// 2) Renders a Flux OCIRepository and a HelmRelease for the InferencePool chart.
// 3) Creates the resources if absent; updates them if the desired spec differs.
// 4) Waits for resources to become ready using the model's inference readiness timeout.
// 5) Aggregates and returns any errors.
//
// Idempotent and safe to call on every reconcile; no-op if preconditions are not met.
func (c *InferenceSetReconciler) ensureGatewayAPIInferenceExtension(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) error {
	if iObj == nil {
		return fmt.Errorf("InferenceSet object is nil")
	}
	runtimeName := kaitov1alpha1.GetInferenceSetRuntimeName(iObj)
	isPresetInference := iObj.Spec.Template.Inference.Preset != nil

	// Gateway API Inference Extension is specifically designed to work with vLLM and preset-based inference workloads.
	if !featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] ||
		runtimeName != pkgmodel.RuntimeNameVLLM || !isPresetInference {
		return nil
	}

	wsList, err := inferenceset.ListWorkspaces(ctx, iObj, c.Client)
	if err != nil {
		return err
	}
	if len(wsList.Items) == 0 {
		klog.InfoS("No workspaces found for inferenceset(%s), skipping Gateway API Inference Extension reconciliation", "inferenceset", iObj.Name)
		return nil
	}

	model := plugin.KaitoModelRegister.MustGet(string(iObj.Spec.Template.Inference.Preset.Name))

	// Dry-run the inference workload generation to determine if it will be a StatefulSet or not.
	workloadObj, err := inference.GeneratePresetInference(ctx, &wsList.Items[0], "", model, c.Client)
	if err != nil {
		return fmt.Errorf("failed to generate preset inference workload for Gateway API Inference Extension: %w", err)
	}
	_, isStatefulSet := workloadObj.(*appsv1.StatefulSet)

	ociRepository := manifests.GenerateInferencePoolOCIRepository(iObj)
	helmRelease, err := manifests.GenerateInferencePoolHelmRelease(iObj, isStatefulSet)
	if err != nil {
		return err
	}

	// Create or update OCIRepository
	existingOCIRepo := &sourcev1.OCIRepository{}
	err = resources.GetResource(ctx, ociRepository.Name, ociRepository.Namespace, c.Client, existingOCIRepo)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := resources.CreateResource(ctx, ociRepository, c.Client); client.IgnoreAlreadyExists(err) != nil {
			return err
		}
	} else {
		equal, err := utils.ClientObjectSpecEqual(ociRepository, existingOCIRepo)
		if err != nil {
			return err
		}
		if !equal {
			existingOCIRepo.Spec = ociRepository.Spec
			if err := c.Update(ctx, existingOCIRepo); err != nil {
				return err
			}
		}
	}

	// Check if HelmRelease exists
	existingHelmRelease := &helmv2.HelmRelease{}
	err = resources.GetResource(ctx, helmRelease.Name, helmRelease.Namespace, c.Client, existingHelmRelease)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := resources.CreateResource(ctx, helmRelease, c.Client); client.IgnoreAlreadyExists(err) != nil {
			return err
		}
	} else {
		equal, err := utils.ClientObjectSpecEqual(helmRelease, existingHelmRelease)
		if err != nil {
			return err
		}
		if !equal {
			existingHelmRelease.Spec = helmRelease.Spec
			if err := c.Update(ctx, existingHelmRelease); err != nil {
				return err
			}
		}
	}

	for _, resource := range []client.Object{ociRepository, helmRelease} {
		if err := resources.CheckResourceStatus(resource, c.Client, 5*time.Minute); err != nil {
			return err
		}
	}

	return nil
}

func (c *InferenceSetReconciler) syncControllerRevision(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) error {
	currentHash := inferenceset.ComputeInferenceSetHash(iObj)
	annotations := iObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	} // nil checking.

	revisionNum := int64(1)

	revisions := &appsv1.ControllerRevisionList{}
	if err := c.List(ctx, revisions, client.InNamespace(iObj.Namespace), client.MatchingLabels{InferenceSetNameLabel: iObj.Name}); err != nil {
		return fmt.Errorf("failed to list revisions: %w", err)
	}
	sort.Slice(revisions.Items, func(i, j int) bool {
		return revisions.Items[i].Revision < revisions.Items[j].Revision
	})

	var latestRevision *appsv1.ControllerRevision

	jsonData, err := inferenceset.MarshalInferenceSetFields(iObj)
	if err != nil {
		return fmt.Errorf("failed to marshal revision data: %w", err)
	}

	if len(revisions.Items) > 0 {
		latestRevision = &revisions.Items[len(revisions.Items)-1]
		revisionNum = latestRevision.Revision + 1
	}
	newRevision := &appsv1.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", iObj.Name, currentHash[:revisionHashSuffix]),
			Namespace: iObj.Namespace,
			Annotations: map[string]string{
				InferenceSetHashAnnotation: currentHash,
			},
			Labels: map[string]string{
				InferenceSetNameLabel: iObj.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(iObj, kaitov1alpha1.GroupVersion.WithKind("InferenceSet")),
			},
		},
		Revision: revisionNum,
		Data:     runtime.RawExtension{Raw: jsonData},
	}

	annotations[InferenceSetHashAnnotation] = currentHash
	iObj.SetAnnotations(annotations)
	controllerRevision := &appsv1.ControllerRevision{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      newRevision.Name,
		Namespace: newRevision.Namespace,
	}, controllerRevision); err != nil {
		if apierrors.IsNotFound(err) {
			if err := c.Create(ctx, newRevision); err != nil {
				return fmt.Errorf("failed to create new ControllerRevision: %w", err)
			} else {
				annotations[kaitov1alpha1.InferenceSetRevisionAnnotation] = strconv.FormatInt(revisionNum, 10)
			}

			if len(revisions.Items) > consts.MaxRevisionHistoryLimit {
				if err := c.Delete(ctx, &revisions.Items[0]); err != nil {
					return fmt.Errorf("failed to delete old revision: %w", err)
				}
			}
		} else {
			return fmt.Errorf("failed to get controller revision: %w", err)
		}
	} else {
		if controllerRevision.Annotations[InferenceSetHashAnnotation] != newRevision.Annotations[InferenceSetHashAnnotation] {
			return fmt.Errorf("revision name conflicts, the hash values are different, old hash: %s, new hash: %s", controllerRevision.Annotations[InferenceSetHashAnnotation], newRevision.Annotations[InferenceSetHashAnnotation])
		}
		annotations[kaitov1alpha1.InferenceSetRevisionAnnotation] = strconv.FormatInt(controllerRevision.Revision, 10)
	}
	annotations[InferenceSetHashAnnotation] = currentHash

	err = inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, iObj, func(ws *kaitov1alpha1.InferenceSet) error {
		ws.SetAnnotations(annotations)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update InferenceSet annotations: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *InferenceSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("InferenceSet")

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.InferenceSet{}).
		Owns(&appsv1.ControllerRevision{}).
		Watches(&kaitov1beta1.Workspace{},
			&workspaceEventHandler{
				logger:         c.klogger,
				expectations:   c.expectations,
				enqueueHandler: enqueueInferenceSetForWorkspace,
			},
			builder.WithPredicates(workspace.WorkspacePredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	if featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] {
		// Verify that all prerequisite CRDs exist before configuring watches that depend on them.
		// - FluxCD HelmRelease / OCIRepository: required for installing and reconciling the InferencePool Helm chart.
		// - Gateway API Inference Extension InferencePool / InferenceModel: required runtime CRDs that the Workspace
		//   controller indirectly relies on (Helm chart renders resources referencing them).
		// Failing fast here provides a clear, actionable error instead of deferred reconcile failures later.
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
				return fmt.Errorf("%s not found in the cluster, please ensure the Gateway API Inference Extension is installed", gvk.String())
			}
		}

		// We don't need to own InferencePool and InferenceModel because they are managed by Flux's HelmRelease
		builder = builder.
			Owns(&helmv2.HelmRelease{}).
			Owns(&sourcev1.OCIRepository{})
	}

	go monitorInferenceSets(context.Background(), c.Client)
	return builder.Complete(c)
}
