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
	"maps"
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
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
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
	iObj := &kaitov1beta1.InferenceSet{}
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

func (c *InferenceSetReconciler) ensureFinalizer(ctx context.Context, iObj *kaitov1beta1.InferenceSet) error {
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

func (c *InferenceSetReconciler) deleteInferenceSet(ctx context.Context, iObj *kaitov1beta1.InferenceSet) (reconcile.Result, error) {
	klog.InfoS("deleteInferenceSet", "inferenceset", klog.KObj(iObj))
	err := inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1beta1.InferenceSetConditionTypeDeleting, metav1.ConditionTrue, "inferencesetDeleted", "inferenceset is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
		return reconcile.Result{}, err
	}

	return c.garbageCollectInferenceSet(ctx, iObj)
}

// garbageCollectInferenceSet remove finalizer associated with inferenceset object.
func (c *InferenceSetReconciler) garbageCollectInferenceSet(ctx context.Context, iObj *kaitov1beta1.InferenceSet) (ctrl.Result, error) {
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

	updateErr := inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, iObj, func(ws *kaitov1beta1.InferenceSet) error {
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

// aggregateBenchmarkResults scans workspaces and returns:
//   - totalTPM: sum of peakTokensPerMinute across all succeeded workspaces that have a valid result
//   - readyReplicas: count of succeeded workspaces
//   - benchmarkedReplicas: count of those workspaces
//   - hasBenchmarkTPMResult: true if at least one workspace contributed a TPM value
func aggregateBenchmarkResults(workspaces []kaitov1beta1.Workspace) (totalTPM float64, readyReplicas, benchmarkedReplicas int, hasBenchmarkTPMResult bool) {
	for _, ws := range workspaces {
		if controllers.DetermineWorkspacePhase(&ws) == "succeeded" {
			readyReplicas++
			if ws.Status.Performance != nil {
				if m, ok := ws.Status.Performance.Metrics[controllers.BenchmarkMetricPeakTPM]; ok {
					if v, err := strconv.ParseFloat(m.Value, 64); err == nil && v > 0 {
						totalTPM += v
						hasBenchmarkTPMResult = true
						benchmarkedReplicas++
					}
				}
			}
		}
	}
	return
}

func (c *InferenceSetReconciler) addOrUpdateInferenceSet(ctx context.Context, iObj *kaitov1beta1.InferenceSet) (reconcile.Result, error) {
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
	desiredReplicas := int32(1)
	if iObj.Spec.Replicas != nil {
		desiredReplicas = *iObj.Spec.Replicas
	}
	klog.InfoS("Found workspaces for inference set", "name", iObj.Name, "current", len(wsList.Items), "desired", desiredReplicas)

	replicaNumToDelete := len(wsList.Items) - int(desiredReplicas)
	if replicaNumToDelete > 0 {
		klog.InfoS("Found extra workspaces, deleting...", "current", len(wsList.Items), "desired", desiredReplicas)

		// Partition workspaces into those already being deleted, those that are
		// not ready, and those that are ready. Workspaces already being deleted
		// count toward the target without issuing a new delete; among the rest,
		// prefer deleting non-ready workspaces before ready ones.
		var notReady, ready []*kaitov1beta1.Workspace
		for i := range wsList.Items {
			ws := &wsList.Items[i]
			if !ws.DeletionTimestamp.IsZero() {
				replicaNumToDelete--
				klog.InfoS("Skipping workspace that is already being deleted...", "workspace", klog.KObj(ws))
			} else if controllers.DetermineWorkspacePhase(ws) != "succeeded" {
				notReady = append(notReady, ws)
			} else {
				ready = append(ready, ws)
			}
		}

		var toDelete []*kaitov1beta1.Workspace
		for _, ws := range append(notReady, ready...) {
			if replicaNumToDelete <= 0 {
				break
			}
			toDelete = append(toDelete, ws)
			replicaNumToDelete--
		}

		if len(toDelete) > 0 {
			// Set deletion expectations before issuing any delete so that stale
			// cache reads in subsequent reconciles do not over-delete workspaces.
			// The expectation is lowered when the delete event is observed by the
			// workspace event handler, or here if the delete call does not result
			// in an eventual delete event (already-gone or failed).
			if err := c.expectations.ExpectDeletions(c.klogger, isKey, len(toDelete)); err != nil {
				klog.ErrorS(err, "failed to set deletion expectations", "inferenceset", isKey)
				return ctrl.Result{}, err
			}
			for _, ws := range toDelete {
				klog.InfoS("Deleting extra workspace...", "workspace", klog.KObj(ws))
				if err := c.Client.Delete(ctx, ws, &client.DeleteOptions{}); err != nil {
					// No delete event will be observed for this workspace, so lower
					// the expectation to avoid stalling until it expires.
					c.expectations.DeletionObserved(c.klogger, isKey)
					if !apierrors.IsNotFound(err) {
						klog.ErrorS(err, "failed to delete extra workspace", "workspace", klog.KObj(ws))
						return ctrl.Result{}, err
					}
				}
			}
		}

		// After deleting the extra workspaces, we should requeue to wait for the deletion to complete
		if wsList, err = inferenceset.ListWorkspaces(ctx, iObj, c.Client); err != nil {
			return ctrl.Result{}, err
		}
	}

	replicaNumToCreate := int(desiredReplicas) - len(wsList.Items)
	if replicaNumToCreate > 0 {
		klog.InfoS("Need to create more workspaces...", "current", len(wsList.Items), "desired", desiredReplicas)
		// Set creation expectations before issuing any create so that a stale
		// cache read in a subsequent reconcile does not create duplicate
		// workspaces. The expectation is lowered when the create event is
		// observed by the workspace event handler, or here if the create fails.
		if err := c.expectations.ExpectCreations(c.klogger, isKey, replicaNumToCreate); err != nil {
			klog.ErrorS(err, "failed to set creation expectations", "inferenceset", isKey)
			return reconcile.Result{}, err
		}
		for i := range replicaNumToCreate {
			workspaceObj := &kaitov1beta1.Workspace{}
			workspaceObj.GenerateName = iObj.Name + "-"
			workspaceObj.Namespace = iObj.Namespace

			// Start with labels from the template metadata, then add controller labels.
			workspaceLabels := maps.Clone(iObj.Spec.Template.Labels)
			if workspaceLabels == nil {
				workspaceLabels = make(map[string]string)
			}
			// Also propagate select labels from the InferenceSet's own metadata,
			// in case template.metadata.labels was pruned by the API server.
			if role, ok := iObj.Labels[kaitov1beta1.LabelInferenceRole]; ok {
				workspaceLabels[kaitov1beta1.LabelInferenceRole] = role
			}
			if mriParent, ok := iObj.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent]; ok {
				workspaceLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mriParent
			}
			workspaceLabels[consts.WorkspaceCreatedByInferenceSetLabel] = iObj.Name
			workspaceObj.Labels = workspaceLabels

			// Start with annotations from the template metadata.
			workspaceAnnotations := maps.Clone(iObj.Spec.Template.Annotations)
			// Propagate the disable-benchmark opt-out so each child workspace inherits it.
			// Benchmark is on by default; only propagate when explicitly disabled.
			if !kaitov1beta1.IsInferenceSetBenchmarkEnabled(iObj) {
				if workspaceAnnotations == nil {
					workspaceAnnotations = make(map[string]string)
				}
				workspaceAnnotations[kaitov1beta1.AnnotationDisableBenchmark] = "true"
			}
			workspaceObj.Annotations = workspaceAnnotations
			workspaceObj.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(iObj, kaitov1beta1.GroupVersion.WithKind("InferenceSet")),
			}
			workspaceObj.Resource = kaitov1beta1.ResourceSpec{
				InstanceType:  iObj.Spec.Template.Resource.InstanceType,
				LabelSelector: iObj.Spec.Selector,
				Partition:     iObj.Spec.Template.Resource.Partition,
			}
			workspaceObj.Inference = &iObj.Spec.Template.Inference

			klog.InfoS("creating workspace", "workspace", workspaceObj.Name, "index", i)
			if err := c.Client.Create(ctx, workspaceObj); err != nil {
				// The create failed, so no create event will be observed for it;
				// lower the expectation to avoid stalling until it expires.
				c.expectations.CreationObserved(c.klogger, isKey)
				klog.ErrorS(err, "failed to create workspace", "workspace", workspaceObj.Name)
				return reconcile.Result{}, err
			}
		}
	}

	// Reconcile labels on existing workspaces by additively propagating InferenceSet metadata labels.
	// Note: this only adds/updates desired labels; it does not remove stale labels to avoid
	// conflicting with labels managed by other controllers.
	// This ensures label changes (e.g., adding kaito.sh/inference-role) propagate
	// to workspaces that were created before the label was set.
	desiredLabels := make(map[string]string)
	for k, v := range iObj.Spec.Template.Labels {
		desiredLabels[k] = v
	}
	// Propagate inference-role from InferenceSet metadata (reliable even if template labels are pruned).
	if role, ok := iObj.Labels[kaitov1beta1.LabelInferenceRole]; ok {
		desiredLabels[kaitov1beta1.LabelInferenceRole] = role
	}
	if mriParent, ok := iObj.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent]; ok {
		desiredLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mriParent
	}
	if len(desiredLabels) > 0 {
		for i := range wsList.Items {
			ws := &wsList.Items[i]
			needsUpdate := false
			if ws.Labels == nil {
				ws.Labels = make(map[string]string)
			}
			for k, v := range desiredLabels {
				if ws.Labels[k] != v {
					ws.Labels[k] = v
					needsUpdate = true
				}
			}
			if needsUpdate {
				klog.InfoS("Reconciling workspace labels", "workspace", klog.KObj(ws))
				if err := c.Client.Update(ctx, ws); err != nil {
					klog.ErrorS(err, "failed to update workspace labels", "workspace", klog.KObj(ws))
					return ctrl.Result{}, err
				}
			}
		}
	}

	// check whether all the workspaces are ready
	totalTPM, readyReplicas, benchmarkedReplicas, hasBenchmarkTPMResult := aggregateBenchmarkResults(wsList.Items)

	// update the replicas in the status
	if err = inferenceset.UpdateInferenceSetStatus(ctx, c.Client, &client.ObjectKey{Name: iObj.Name, Namespace: iObj.Namespace}, func(status *kaitov1beta1.InferenceSetStatus) error {
		status.Replicas = int(desiredReplicas)
		status.ReadyReplicas = readyReplicas
		// set selector for HPA/VPA
		status.Selector = fmt.Sprintf("%s=%s", consts.WorkspaceCreatedByInferenceSetLabel, iObj.Name)
		runtimeName := kaitov1beta1.GetInferenceSetRuntimeName(iObj)
		var presetName string
		if iObj.Spec.Template.Inference.Preset != nil {
			presetName = string(iObj.Spec.Template.Inference.Preset.Name)
		}

		if kaitov1beta1.ShouldRunInferenceSetBenchmark(iObj) {
			if hasBenchmarkTPMResult {
				if status.Performance == nil {
					status.Performance = &kaitov1beta1.Performance{}
				}
				if status.Performance.Metrics == nil {
					status.Performance.Metrics = make(map[string]kaitov1beta1.Metric)
				}
				status.Performance.Metrics[controllers.BenchmarkMetricAggregatedPeakTPM] = kaitov1beta1.Metric{
					Description: controllers.BenchmarkDesc,
					Value:       strconv.FormatFloat(totalTPM, 'f', 2, 64),
					Unit:        controllers.BenchmarkMetricUnit,
					Config:      controllers.RuntimeMetadataConfig(runtimeName, presetName),
				}
			} else {
				// No ready replica has a TPM result — clear the TPM key so the profile
				// doesn't reflect a previous generation of workspaces.
				// Other metric keys are left intact to be cleared by their own logic.
				if status.Performance != nil {
					delete(status.Performance.Metrics, controllers.BenchmarkMetricAggregatedPeakTPM)
					if len(status.Performance.Metrics) == 0 {
						status.Performance = nil
					}
				}
			}
		} else {
			// Feature flag is off — clear any TPM value that may have been written
			// when the flag was previously enabled (e.g. annotation removed).
			if status.Performance != nil {
				delete(status.Performance.Metrics, controllers.BenchmarkMetricAggregatedPeakTPM)
				if len(status.Performance.Metrics) == 0 {
					status.Performance = nil
				}
			}
		}

		return nil
	}); err != nil {
		klog.ErrorS(err, "failed to update inferenceset replicas", "inferenceset", klog.KObj(iObj))
		return reconcile.Result{}, err
	}

	if readyReplicas == int(desiredReplicas) {
		if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1beta1.InferenceSetConditionTypeReady, metav1.ConditionTrue,
			"inferencesetReady", "inferenceset is ready"); err != nil {
			klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, err
		}
	} else {
		if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1beta1.InferenceSetConditionTypeReady, metav1.ConditionFalse,
			"inferencesetNotReady", fmt.Sprintf("inferenceset is not ready, %d/%d replicas are ready", readyReplicas, desiredReplicas)); err != nil {
			klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, err
		}
	}

	// Surface benchmark progress when the annotation is set.
	if kaitov1beta1.ShouldRunInferenceSetBenchmark(iObj) {
		if benchmarkedReplicas == int(desiredReplicas) && desiredReplicas > 0 {
			if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1beta1.InferenceSetConditionTypeBenchmarkCompleted, metav1.ConditionTrue,
				"BenchmarkCompleted", fmt.Sprintf("%d/%d replicas benchmarked", benchmarkedReplicas, desiredReplicas)); err != nil {
				klog.ErrorS(err, "failed to update inferenceset benchmark status", "inferenceset", klog.KObj(iObj))
				return reconcile.Result{}, err
			}
		} else {
			if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1beta1.InferenceSetConditionTypeBenchmarkCompleted, metav1.ConditionFalse,
				"BenchmarkPending", fmt.Sprintf("%d/%d replicas benchmarked", benchmarkedReplicas, desiredReplicas)); err != nil {
				klog.ErrorS(err, "failed to update inferenceset benchmark status", "inferenceset", klog.KObj(iObj))
				return reconcile.Result{}, err
			}
		}
	}

	if err = c.ensureGatewayAPIInferenceExtension(ctx, iObj); err != nil {
		if updateErr := inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1beta1.InferenceSetConditionTypeReady, metav1.ConditionFalse,
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
func (c *InferenceSetReconciler) ensureGatewayAPIInferenceExtension(ctx context.Context, iObj *kaitov1beta1.InferenceSet) error {
	if iObj == nil {
		return fmt.Errorf("InferenceSet object is nil")
	}

	// Skip GWIE for child InferenceSets managed by MultiRoleInference.
	// The MRI controller creates a shared InferencePool + EPP for all child InferenceSets.
	// Use OwnerReferences (controller-managed) instead of labels (easily user-modifiable)
	// to prevent accidental GWIE bypass on standalone InferenceSets.
	for _, owner := range iObj.OwnerReferences {
		if owner.Controller != nil && *owner.Controller &&
			owner.Kind == "MultiRoleInference" &&
			owner.APIVersion == kaitov1alpha1.GroupVersion.String() {
			return nil
		}
	}

	runtimeName := kaitov1beta1.GetInferenceSetRuntimeName(iObj)
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

	ociRepository := manifests.GenerateInferencePoolOCIRepository(iObj)
	helmRelease, err := manifests.GenerateInferencePoolHelmRelease(iObj)
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

func (c *InferenceSetReconciler) syncControllerRevision(ctx context.Context, iObj *kaitov1beta1.InferenceSet) error {
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
				*metav1.NewControllerRef(iObj, kaitov1beta1.GroupVersion.WithKind("InferenceSet")),
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
				annotations[kaitov1beta1.InferenceSetRevisionAnnotation] = strconv.FormatInt(revisionNum, 10)
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
		annotations[kaitov1beta1.InferenceSetRevisionAnnotation] = strconv.FormatInt(controllerRevision.Revision, 10)
	}
	annotations[InferenceSetHashAnnotation] = currentHash

	err = inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, iObj, func(ws *kaitov1beta1.InferenceSet) error {
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
		For(&kaitov1beta1.InferenceSet{}).
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
