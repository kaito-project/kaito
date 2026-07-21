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
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/api/v1beta1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/pkg/workspace/estimator/nodesestimator"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming/registry"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	"github.com/kaito-project/kaito/pkg/workspace/tuning"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	WorkspaceHashAnnotation = "workspace.kaito.io/hash"
	WorkspaceNameLabel      = "workspace.kaito.io/name"
	revisionHashSuffix      = 5

	// MaxAllowedNodeCount caps the per-replica node count produced by the node
	// estimator for inference workspaces. vLLM's Ray executor has a known bug
	// where pipeline_parallel_size > 3 fails to initialize the KV cache,
	// producing a KeyError on layer lookup; see
	// https://github.com/vllm-project/vllm/issues/30128. Until that is fixed
	// upstream, we refuse to provision more than this many nodes per replica
	// and ask the user to pick a larger GPU instance type (or shrink the
	// model / context size) instead.
	MaxAllowedNodeCount = 3
)

type WorkspaceReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	klogger         klog.Logger
	expectations    *utils.ControllerExpectations
	Estimator       estimator.NodesEstimator
	nodeProvisioner nodeprovision.NodeProvisioner
}

func NewWorkspaceReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, Recorder record.EventRecorder,
	provisioner nodeprovision.NodeProvisioner) *WorkspaceReconciler {
	expectations := utils.NewControllerExpectations()

	return &WorkspaceReconciler{
		Client:          client,
		Scheme:          scheme,
		Log:             log,
		klogger:         klog.NewKlogr().WithName("WorkspaceController"),
		Recorder:        Recorder,
		expectations:    expectations,
		Estimator:       &nodesestimator.NodeEstimator{},
		nodeProvisioner: provisioner,
	}
}

func (c *WorkspaceReconciler) Reconcile(ctx context.Context, req reconcile.Request) (result reconcile.Result, err error) {
	workspaceObj := &kaitov1beta1.Workspace{}
	if err = c.Client.Get(ctx, req.NamespacedName, workspaceObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.klogger, req.String())
			return reconcile.Result{}, nil
		}
		klog.ErrorS(err, "failed to get workspace", "workspace", req.Name)
		return reconcile.Result{}, err
	}

	defer func() {
		if syncErr := c.syncWorkspaceStatus(ctx, req.NamespacedName, err); syncErr != nil {
			klog.ErrorS(syncErr, "failed to sync workspace status", "workspace", req.NamespacedName)
			if err == nil {
				err = syncErr
			}
		}
	}()

	klog.InfoS("Reconciling", "workspace", req.NamespacedName)

	if workspaceObj.DeletionTimestamp.IsZero() {
		if err = c.ensureFinalizer(ctx, workspaceObj); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Handle deleting workspace, garbage collect all the resources.
		return c.deleteWorkspace(ctx, workspaceObj)
	}

	if err = c.syncControllerRevision(ctx, workspaceObj); err != nil {
		return reconcile.Result{}, err
	}

	// update targetNodeCount for the workspace
	if err = c.UpdateWorkspaceTargetNodeCount(ctx, workspaceObj); err != nil {
		return reconcile.Result{}, err
	}

	return c.addOrUpdateWorkspace(ctx, workspaceObj)
}

func (c *WorkspaceReconciler) ensureFinalizer(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) error {
	if !controllerutil.ContainsFinalizer(workspaceObj, consts.WorkspaceFinalizer) {
		patch := client.MergeFrom(workspaceObj.DeepCopy())
		controllerutil.AddFinalizer(workspaceObj, consts.WorkspaceFinalizer)
		if err := c.Client.Patch(ctx, workspaceObj, patch); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer to the workspace", "workspace", klog.KObj(workspaceObj))
			return err
		}
	}
	return nil
}

// ensureModelMirror creates the ModelMirror CR for the workspace's model if it doesn't exist.
// Returns nil if the CR exists (any phase) or was created successfully.
func (c *WorkspaceReconciler) ensureModelMirror(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	if err := modelstreaming.ValidateStaticModelMirrorAnnotations(wObj.Annotations); err != nil {
		return err
	}

	modelID := modelstreaming.ResolveHFModelID(wObj)
	crName := modelstreaming.ModelMirrorCRName(modelID)

	// Check if CR already exists
	existing := &kaitov1alpha1.ModelMirror{}
	err := c.Client.Get(ctx, client.ObjectKey{Name: crName}, existing)
	if err == nil {
		// CR exists — verify it's for the same model (collision check).
		if existing.Spec.Source != nil && existing.Spec.Source.ModelID != modelID {
			return fmt.Errorf("ModelMirror CR name collision: %s maps to both %q and %q",
				crName, existing.Spec.Source.ModelID, modelID)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get ModelMirror CR %s: %w", crName, err)
	}

	if modelstreaming.StaticModelMirrorEnabled(wObj.Annotations) {
		if err := registry.SelectModelStreamer(wObj).ValidateAuth(ctx, wObj, c.Client, modelstreaming.StreamingDefaults.ServiceAccount); err != nil {
			return err
		}
		staticCR := &kaitov1alpha1.ModelMirror{
			ObjectMeta: metav1.ObjectMeta{Name: crName},
			Spec:       kaitov1alpha1.ModelMirrorSpec{Mode: kaitov1alpha1.ModelMirrorModeStatic},
		}
		if err := c.Client.Create(ctx, staticCR); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil // Race condition
			}
			return fmt.Errorf("failed to create static ModelMirror CR %s: %w", crName, err)
		}
		klog.InfoS("Created static ModelMirror CR", "name", crName, "workspace", klog.KObj(wObj))
		return nil
	}

	// Managed mirror: validate the StorageClass exists and uses the correct CSI provisioner.
	storageClass, err := modelstreaming.ResolveStorageClass(wObj, modelstreaming.StreamingDefaults.StorageClass)
	if err != nil {
		return err
	}
	sc := &storagev1.StorageClass{}
	if err := c.Client.Get(ctx, client.ObjectKey{Name: storageClass}, sc); err != nil {
		return fmt.Errorf("StorageClass %q not found: %w", storageClass, err)
	}
	expectedCSIDriver := consts.CSIDriverNameForCloud(os.Getenv("CLOUD_PROVIDER"))
	if sc.Provisioner != expectedCSIDriver {
		return fmt.Errorf("StorageClass %q uses provisioner %q, but model streaming requires %q; "+
			"create a StorageClass with the correct provisioner",
			storageClass, sc.Provisioner, expectedCSIDriver)
	}

	// Validate ServiceAccount exists and has provider-specific identity configured.
	if err := registry.SelectModelStreamer(wObj).ValidateAuth(ctx, wObj, c.Client, modelstreaming.StreamingDefaults.ServiceAccount); err != nil {
		return err
	}

	serviceAccount, err := modelstreaming.ResolveStreamingServiceAccount(wObj, modelstreaming.StreamingDefaults.ServiceAccount)
	if err != nil {
		return err
	}

	// Resolve model metadata for DiskStorageRequirement
	presetName := string(wObj.Inference.Preset.Name)
	model, err := models.GetModelByName(ctx, presetName, wObj.Inference.Preset.PresetOptions.ModelAccessSecret, wObj.Namespace, c.Client)
	if err != nil {
		return fmt.Errorf("failed to resolve model for streaming: %w", err)
	}

	modelSize := model.GetInferenceParameters().DiskStorageRequirement
	if modelSize == "" {
		return fmt.Errorf("model %q has no DiskStorageRequirement; cannot create ModelMirror CR", modelID)
	}

	var accessSecret *corev1.ObjectReference
	if wObj.Inference.Preset.PresetOptions.ModelAccessSecret != "" {
		accessSecret = &corev1.ObjectReference{
			Name:      wObj.Inference.Preset.PresetOptions.ModelAccessSecret,
			Namespace: wObj.Namespace,
		}
	}

	cr := &kaitov1alpha1.ModelMirror{
		ObjectMeta: metav1.ObjectMeta{Name: crName},
		Spec: kaitov1alpha1.ModelMirrorSpec{
			Mode: kaitov1alpha1.ModelMirrorModeManaged,
			Source: &kaitov1alpha1.ModelMirrorSource{
				Registry:     kaitov1alpha1.RegistryHuggingFace,
				ModelID:      modelID,
				AccessSecret: accessSecret,
			},
			Storage: &kaitov1alpha1.ModelMirrorStorage{
				Size:             modelSize,
				StorageClassName: ptr.To(storageClass),
			},
			JobNamespace:       wObj.Namespace,
			ServiceAccountName: serviceAccount,
		},
	}

	if err := c.Client.Create(ctx, cr); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil // Race condition
		}
		return fmt.Errorf("failed to create ModelMirror CR %s: %w", crName, err)
	}

	klog.InfoS("Created ModelMirror CR", "name", crName, "modelID", modelID, "workspace", klog.KObj(wObj))
	return nil
}

// waitForModelMirror checks if the ModelMirror CR is Ready.
// Returns (nil, nil) to proceed, or (*Result, err) to stop — same pattern as reconcileNodes.
func (c *WorkspaceReconciler) waitForModelMirror(ctx context.Context, wObj *kaitov1beta1.Workspace) (result *reconcile.Result, err error) {
	modelID := modelstreaming.ResolveHFModelID(wObj)
	crName := modelstreaming.ModelMirrorCRName(modelID)

	cr := &kaitov1alpha1.ModelMirror{}
	if err := c.Client.Get(ctx, client.ObjectKey{Name: crName}, cr); err != nil {
		if apierrors.IsNotFound(err) {
			// CR was deleted externally; ensureModelMirror will recreate on next reconcile
			return &reconcile.Result{}, nil
		}
		return &reconcile.Result{}, fmt.Errorf("failed to get ModelMirror CR %s: %w", crName, err)
	}

	if cr.Status.Phase != kaitov1alpha1.ModelMirrorPhaseReady {
		klog.InfoS("ModelMirror CR not ready, gating inference", "name", crName, "phase", cr.Status.Phase)
		return &reconcile.Result{}, nil
	}
	return nil, nil
}

func (c *WorkspaceReconciler) reconcileNodes(ctx context.Context, wObj *kaitov1beta1.Workspace) (result *reconcile.Result, err error) {
	// Refuse to provision when the persisted target node count is over the limit.
	if err := c.guardTargetNodeCount(wObj); err != nil {
		return &reconcile.Result{}, err
	}

	// Provision nodes via the NodeProvisioner interface.
	// GpuProvisioner creates NodeClaims; BYOProvisioner (BYO mode) is a no-op.
	if err := c.nodeProvisioner.ProvisionNodes(ctx, wObj); err != nil {
		return &reconcile.Result{}, err
	}

	// Check if nodes are ready.
	ready, needRequeue, err := c.nodeProvisioner.EnsureNodesReady(ctx, wObj)
	if err != nil {
		return &reconcile.Result{}, err
	}
	if !ready {
		if needRequeue {
			return &reconcile.Result{RequeueAfter: 2 * time.Second}, nil
		}
		return &reconcile.Result{}, nil
	}

	return nil, nil
}

func (c *WorkspaceReconciler) addOrUpdateWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	if !c.expectations.SatisfiedExpectations(c.Log, workspaceKey) {
		klog.V(4).InfoS("Waiting for NodeClaim expectations to be satisfied",
			"workspace", workspaceKey)
		return reconcile.Result{}, nil
	}

	// Ensure ModelMirror CR exists (starts download in parallel with node provisioning).
	if modelstreaming.ModelStreamingEnabled(wObj) && wObj.Inference != nil && wObj.Inference.Preset != nil {
		if err := c.ensureModelMirror(ctx, wObj); err != nil {
			return reconcile.Result{}, err
		}
	}

	if result, err := c.reconcileNodes(ctx, wObj); err != nil || result != nil {
		return *result, err
	}

	// Wait for ModelMirror CR to be Ready (gate inference pod creation).
	if modelstreaming.ModelStreamingEnabled(wObj) && wObj.Inference != nil && wObj.Inference.Preset != nil {
		if result, err := c.waitForModelMirror(ctx, wObj); err != nil || result != nil {
			return *result, err
		}
	}

	if wObj.Tuning != nil {
		if err := c.applyTuning(ctx, wObj); err != nil {
			return reconcile.Result{}, err
		}
	} else if wObj.Inference != nil {
		if err := c.ensureService(ctx, wObj); err != nil {
			return reconcile.Result{}, err
		}
		if err := c.applyInference(ctx, wObj); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (c *WorkspaceReconciler) deleteWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	klog.InfoS("deleteWorkspace", "workspace", klog.KObj(wObj))
	return c.garbageCollectWorkspace(ctx, wObj)
}
func (c *WorkspaceReconciler) syncControllerRevision(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	currentHash := ComputeHash(wObj)
	annotations := wObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	} // nil checking.

	revisionNum := int64(1)

	revisions := &appsv1.ControllerRevisionList{}
	if err := c.List(ctx, revisions, client.InNamespace(wObj.Namespace), client.MatchingLabels{WorkspaceNameLabel: wObj.Name}); err != nil {
		return fmt.Errorf("failed to list revisions: %w", err)
	}
	sort.Slice(revisions.Items, func(i, j int) bool {
		return revisions.Items[i].Revision < revisions.Items[j].Revision
	})

	var latestRevision *appsv1.ControllerRevision

	jsonData, err := marshalSelectedFields(wObj)
	if err != nil {
		return fmt.Errorf("failed to marshal revision data: %w", err)
	}

	if len(revisions.Items) > 0 {
		latestRevision = &revisions.Items[len(revisions.Items)-1]

		revisionNum = latestRevision.Revision + 1
	}
	newRevision := &appsv1.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", wObj.Name, currentHash[:revisionHashSuffix]),
			Namespace: wObj.Namespace,
			Annotations: map[string]string{
				WorkspaceHashAnnotation: currentHash,
			},
			Labels: map[string]string{
				WorkspaceNameLabel: wObj.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(wObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Revision: revisionNum,
		Data:     runtime.RawExtension{Raw: jsonData},
	}

	annotations[WorkspaceHashAnnotation] = currentHash
	wObj.SetAnnotations(annotations)
	controllerRevision := &appsv1.ControllerRevision{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      newRevision.Name,
		Namespace: newRevision.Namespace,
	}, controllerRevision); err != nil {
		if apierrors.IsNotFound(err) {

			if err := c.Create(ctx, newRevision); err != nil {
				return fmt.Errorf("failed to create new ControllerRevision: %w", err)
			} else {
				annotations[kaitov1beta1.WorkspaceRevisionAnnotation] = strconv.FormatInt(revisionNum, 10)
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
		if controllerRevision.Annotations[WorkspaceHashAnnotation] != newRevision.Annotations[WorkspaceHashAnnotation] {
			return fmt.Errorf("revision name conflicts, the hash values are different")
		}
		annotations[kaitov1beta1.WorkspaceRevisionAnnotation] = strconv.FormatInt(controllerRevision.Revision, 10)
	}
	annotations[WorkspaceHashAnnotation] = currentHash

	err = workspace.UpdateWorkspaceWithRetry(ctx, c.Client, wObj, func(ws *kaitov1beta1.Workspace) error {
		ws.SetAnnotations(annotations)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update Workspace annotations: %w", err)
	}
	return nil
}

func marshalSelectedFields(wObj *kaitov1beta1.Workspace) ([]byte, error) {
	partialMap := map[string]interface{}{
		"resource":  wObj.Resource,
		"inference": wObj.Inference,
		"tuning":    wObj.Tuning,
	}

	jsonData, err := json.Marshal(partialMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal selected fields: %w", err)
	}

	return jsonData, nil
}

func ComputeHash(w *kaitov1beta1.Workspace) string {
	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	encoder.Encode(w.Resource)
	encoder.Encode(w.Inference)
	encoder.Encode(w.Tuning)
	return hex.EncodeToString(hasher.Sum(nil))
}

func (c *WorkspaceReconciler) ensureService(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	serviceType := corev1.ServiceTypeClusterIP
	wAnnotation := wObj.GetAnnotations()

	if len(wAnnotation) != 0 {
		val, found := wAnnotation[kaitov1beta1.AnnotationEnableLB]
		if found && val == "True" {
			serviceType = corev1.ServiceTypeLoadBalancer
		}
	}

	serviceObj := manifests.GenerateServiceManifest(wObj, serviceType)
	if err := resources.GetResource(ctx, serviceObj.Name, serviceObj.Namespace, c.Client, &corev1.Service{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := resources.CreateResource(ctx, serviceObj, c.Client); err != nil {
			return err
		}
	}

	// headless service for worker pod to discover the leader pod
	headlessService := manifests.GenerateHeadlessServiceManifest(wObj)
	if err := resources.GetResource(ctx, headlessService.Name, headlessService.Namespace, c.Client, &corev1.Service{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		if err := resources.CreateResource(ctx, headlessService, c.Client); err != nil {
			return err
		}
	}

	return nil
}

func (c *WorkspaceReconciler) applyTuning(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	if wObj.Tuning == nil || wObj.Tuning.Preset == nil {
		return nil
	}

	presetName := string(wObj.Tuning.Preset.Name)
	model, err := models.GetModelByName(ctx, presetName, "", wObj.Namespace, c.Client)
	if err != nil {
		klog.ErrorS(err, "failed to get model by name", "model", presetName, "workspace", klog.KObj(wObj))
		return err
	}
	revisionNum := wObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation]

	existingObj := &batchv1.Job{}
	if err := resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, existingObj); err != nil {
		if apierrors.IsNotFound(err) {
			_, err = tuning.CreatePresetTuning(ctx, wObj, revisionNum, model, c.Client)
			return err
		}
		return err
	}

	klog.InfoS("A tuning workload already exists for workspace", "workspace", klog.KObj(wObj))
	if existingObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation] == revisionNum {
		return nil
	}

	deletePolicy := metav1.DeletePropagationForeground
	if err := c.Delete(ctx, existingObj, &client.DeleteOptions{PropagationPolicy: &deletePolicy}); err != nil {
		return err
	}

	_, err = tuning.CreatePresetTuning(ctx, wObj, revisionNum, model, c.Client)
	return err
}

// applyInference applies inference spec.
func (c *WorkspaceReconciler) applyInference(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	// From v0.8.0 onwards, StatefulSet is the default workload for all workspaces.
	// This block purges existing Deployments and migrates them to StatefulSets later.
	// WARNING: This migration will cause a few minutes of service downtime.
	existingDeploy := appsv1.Deployment{}
	if err := resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, &existingDeploy); err == nil {
		c.Recorder.Eventf(wObj, "Warning", "WorkloadMigration",
			"Migrating inference workload from Deployment to StatefulSet, this will cause a few minutes of downtime.")
		klog.InfoS("Delete existing deployment workload for workspace", "workspace", klog.KObj(wObj))
		err = c.Delete(ctx, &existingDeploy, &client.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID: &existingDeploy.UID,
			},
		})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete old inference deployment: %w", err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get existing inference deployment: %w", err)
	}

	if wObj.Inference == nil {
		return nil
	}

	if wObj.Inference.Template != nil {
		// TODO: handle update
		_, err := inference.CreateTemplateInference(ctx, wObj, c.Client)
		return err
	}

	if wObj.Inference.Preset == nil {
		return nil
	}

	presetName := string(wObj.Inference.Preset.Name)
	model, err := models.GetModelByName(ctx, presetName, wObj.Inference.Preset.PresetOptions.ModelAccessSecret, wObj.Namespace, c.Client)
	if err != nil {
		klog.ErrorS(err, "failed to get model by name", "model", presetName, "workspace", klog.KObj(wObj))
		return err
	}

	revisionStr := wObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation]
	workloadObj, err := inference.GeneratePresetInference(ctx, wObj, revisionStr, model, c.Client, c.nodeProvisioner)
	if err != nil {
		return err
	}

	desiredStatefulSet, ok := workloadObj.(*appsv1.StatefulSet)
	if !ok {
		return fmt.Errorf("failed to generate statefulset workload for inference")
	}

	existingObj := &appsv1.StatefulSet{}
	if err := resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, existingObj); err != nil {
		if apierrors.IsNotFound(err) {
			return resources.CreateResource(ctx, workloadObj, c.Client)
		}
		return err
	}

	klog.InfoS("An inference workload already exists for workspace", "workspace", klog.KObj(wObj))
	annotations := existingObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	currentRevisionStr, ok := annotations[kaitov1beta1.WorkspaceRevisionAnnotation]
	baseImageUpgrade := shouldUpgradeBaseImage(wObj, existingObj, desiredStatefulSet)

	// If the current workload revision matches the one in Workspace and no upgrade is pending,
	// we do not need to update it.
	if ok && currentRevisionStr == revisionStr && !baseImageUpgrade {
		return nil
	}

	if baseImageUpgrade {
		// On base image upgrade, update all mutable fields of the StatefulSet
		// https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/apps/validation/validation.go#L268C1-L269C1
		existingObj.Spec.Template = desiredStatefulSet.Spec.Template
		existingObj.Spec.Replicas = desiredStatefulSet.Spec.Replicas
		existingObj.Spec.Ordinals = desiredStatefulSet.Spec.Ordinals
		existingObj.Spec.UpdateStrategy = desiredStatefulSet.Spec.UpdateStrategy
		existingObj.Spec.MinReadySeconds = desiredStatefulSet.Spec.MinReadySeconds
		existingObj.Spec.PersistentVolumeClaimRetentionPolicy = desiredStatefulSet.Spec.PersistentVolumeClaimRetentionPolicy
	} else {
		// Selectively update the pod spec fields that are relevant to inference,
		// and leave the rest unchanged in case user has customized them.
		desiredPodSpec := desiredStatefulSet.Spec.Template.Spec
		spec := &existingObj.Spec.Template.Spec
		spec.Containers[0].Env = desiredPodSpec.Containers[0].Env
		spec.Containers[0].VolumeMounts = desiredPodSpec.Containers[0].VolumeMounts
		spec.InitContainers = desiredPodSpec.InitContainers
		spec.Volumes = desiredPodSpec.Volumes
	}

	annotations[kaitov1beta1.WorkspaceRevisionAnnotation] = revisionStr
	existingObj.SetAnnotations(annotations)

	// Update it with the latest one generated above.
	if err := c.Update(ctx, existingObj); err != nil {
		return err
	}

	// After a base image auto-upgrade, clear the recorded benchmark result and reset
	// the BenchmarkCompleted condition so the post-load benchmark re-runs against the
	// new image on the next reconcile, re-recording all metrics (including the runtime
	// metadata such as engineVersion) with fresh values.
	if baseImageUpgrade {
		if err := c.resetBenchmarkOnUpgrade(ctx, wObj); err != nil {
			return err
		}
	}
	return nil
}

// shouldUpgradeBaseImage checks if an auto-upgrade has been requested via the upgrade label
// and the image hasn't been updated yet. The label value must match the controller's
// current desired base image tag to prevent stale labels from triggering upgrades.
func shouldUpgradeBaseImage(wObj *kaitov1beta1.Workspace, existingObj, desiredStatefulSet *appsv1.StatefulSet) bool {
	upgradeVersion := wObj.Labels[kaitov1alpha1.LabelUpgradeToVersion]
	return upgradeVersion != "" &&
		upgradeVersion == inference.GetBaseImageTag() &&
		workspace.GetInferenceContainerImage(existingObj) != workspace.GetInferenceContainerImage(desiredStatefulSet)
}

func (c *WorkspaceReconciler) syncWorkspaceStatus(ctx context.Context, key types.NamespacedName, reconcileErr error) error {
	wObj := &kaitov1beta1.Workspace{}
	if err := c.Get(ctx, key, wObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	nodeSnapshot, err := c.collectNodeStatusSnapshot(ctx, wObj)
	if err != nil {
		return err
	}

	inferenceReady, hasBenchmarkProbe, infFailReason, infFailMsg, err := c.collectInferenceReadyStatus(ctx, wObj)
	if err != nil {
		return err
	}

	tuningSnapshot, err := c.collectTuningStatusSnapshot(ctx, wObj)
	if err != nil {
		return err
	}

	// benchmarkApplicable gates the benchmark on the *running* pod: it requires both
	// that the workspace should benchmark and that the StatefulSet actually
	// carries the benchmark startup probe. Legacy workspaces created before the
	// benchmark feature have no probe (backward compatibility).
	benchmarkApplicable := kaitov1beta1.ShouldRunBenchmark(wObj) && hasBenchmarkProbe

	appendReconcileErrMessage := buildReconcileErrMessageAppender(reconcileErr)

	return c.updateWorkspaceStatusIfChanged(ctx, key, func(status *kaitov1beta1.WorkspaceStatus) error {
		if !wObj.DeletionTimestamp.IsZero() {
			setWorkspaceCondition(status, wObj.GetGeneration(), appendReconcileErrMessage,
				kaitov1beta1.WorkspaceConditionTypeDeleting, metav1.ConditionTrue, "workspaceDeleted", "workspace is being deleted")
			return nil
		}

		status.WorkerNodes = nodeSnapshot.workerNodeNames

		// Merge node conditions from provisioner: set returned conditions,
		// remove any known node condition type that was not returned.
		returnedTypes := make(map[string]struct{}, len(nodeSnapshot.conditions))
		for i := range nodeSnapshot.conditions {
			cond := nodeSnapshot.conditions[i]
			returnedTypes[cond.Type] = struct{}{}
			cond.Message = appendReconcileErrMessage(cond.Message)
			cond.ObservedGeneration = wObj.GetGeneration()
			meta.SetStatusCondition(&status.Conditions, cond)
		}
		for _, t := range nodeConditionTypes {
			if _, ok := returnedTypes[t]; !ok {
				meta.RemoveStatusCondition(&status.Conditions, t)
			}
		}

		// Extract ResourceStatus condition status for downstream use.
		resourceConditionStatus := metav1.ConditionFalse
		if rc := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus)); rc != nil {
			resourceConditionStatus = rc.Status
		}

		if wObj.Tuning != nil {
			applyTuningWorkspaceStatus(status, wObj.GetGeneration(), appendReconcileErrMessage, tuningSnapshot)
			return nil
		}

		if wObj.Inference != nil {
			if modelstreaming.ModelStreamingEnabled(wObj) && wObj.Inference.Preset != nil {

				modelID := modelstreaming.ResolveHFModelID(wObj)
				crName := modelstreaming.ModelMirrorCRName(modelID)

				cr := &kaitov1alpha1.ModelMirror{}
				if err := c.Get(ctx, client.ObjectKey{Name: crName}, cr); err != nil {
					if !apierrors.IsNotFound(err) {
						klog.ErrorS(err, "failed to get ModelMirror CR for status sync", "cr", crName)
					}
					// CR not found or error — model weights not ready, override ResourceReady
					resourceConditionStatus = metav1.ConditionFalse
					setWorkspaceCondition(status, wObj.GetGeneration(), appendReconcileErrMessage,
						kaitov1beta1.ConditionTypeResourceStatus,
						metav1.ConditionFalse, "ModelMirrorNotReady", "Model download has not started")
				} else {
					if cr.Status.Phase == kaitov1alpha1.ModelMirrorPhaseReady {
						setWorkspaceCondition(status, wObj.GetGeneration(), appendReconcileErrMessage,
							kaitov1beta1.WorkspaceConditionTypeModelMirrorReady,
							metav1.ConditionTrue, "ModelMirrorReady", "Model download complete")
					} else {
						msg := "Model download in progress"
						if cr.Status.FailureMessage != "" {
							msg = cr.Status.FailureMessage
						}
						setWorkspaceCondition(status, wObj.GetGeneration(), appendReconcileErrMessage,
							kaitov1beta1.WorkspaceConditionTypeModelMirrorReady,
							metav1.ConditionFalse, "ModelMirrorPending", msg)
						// Model weights not ready — override ResourceReady
						resourceConditionStatus = metav1.ConditionFalse
						setWorkspaceCondition(status, wObj.GetGeneration(), appendReconcileErrMessage,
							kaitov1beta1.ConditionTypeResourceStatus,
							metav1.ConditionFalse, "ModelMirrorNotReady", msg)
					}
				}
			}

			applyInferenceWorkspaceStatus(ctx, status, wObj, appendReconcileErrMessage, inferenceReady, resourceConditionStatus, benchmarkApplicable, infFailReason, infFailMsg)
			return nil
		}

		return nil
	})
}

type nodeStatusSnapshot struct {
	workerNodeNames []string
	conditions      []metav1.Condition
}

// nodeConditionTypes is the complete set of node-related condition types
// managed by NodeProvisioner. Conditions returned by CollectNodeStatusInfo
// are set; any type in this set not returned is removed from status.
var nodeConditionTypes = []string{
	string(kaitov1beta1.ConditionTypeNodeStatus),
	string(kaitov1beta1.ConditionTypeNodeClaimStatus),
	string(kaitov1beta1.ConditionTypeResourceStatus),
}

func (c *WorkspaceReconciler) collectNodeStatusSnapshot(ctx context.Context, wObj *kaitov1beta1.Workspace) (*nodeStatusSnapshot, error) {
	snapshot := &nodeStatusSnapshot{
		workerNodeNames: []string{},
	}

	// Collect worker node names for status.
	matchLabels := client.MatchingLabels(kaitov1beta1.SanitizedMatchLabels(wObj.Resource.LabelSelector))
	nodeList, err := nodes.ListNodes(ctx, c.Client, matchLabels)
	if err != nil {
		return nil, err
	}
	for i := range nodeList.Items {
		snapshot.workerNodeNames = append(snapshot.workerNodeNames, nodeList.Items[i].Name)
	}
	sort.Strings(snapshot.workerNodeNames)

	// Delegate status condition collection to the NodeProvisioner.
	snapshot.conditions, err = c.nodeProvisioner.CollectNodeStatusInfo(ctx, wObj)
	if err != nil {
		return nil, err
	}

	return snapshot, nil
}

// collectInferenceReadyStatus reports whether the inference workload is ready and
// whether its StatefulSet carries the benchmark startup probe (false for legacy,
// pre-benchmark-feature workspaces).
func (c *WorkspaceReconciler) collectInferenceReadyStatus(ctx context.Context, wObj *kaitov1beta1.Workspace) (inferenceReady bool, hasBenchmarkProbe bool, failReason, failMsg string, err error) {
	if wObj.Inference == nil {
		return false, false, "", "", nil
	}

	ss := &appsv1.StatefulSet{}
	if err := c.Get(ctx, types.NamespacedName{Name: wObj.Name, Namespace: wObj.Namespace}, ss); err != nil {
		if apierrors.IsNotFound(err) {
			return false, false, "", "", nil
		}
		return false, false, "", "", err
	}

	replicas := int32(1)
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}

	ready := ss.Status.ReadyReplicas == replicas
	// When not ready, surface a SAS-specific reason if the streaming init container
	// (fetch-sas) is failing
	if !ready {
		failReason, failMsg = c.detectSASInitFailure(ctx, wObj)
	}

	return ready, hasBenchmarkStartupProbe(ss), failReason, failMsg, nil
}

// detectSASInitFailure returns a reason/message when a workspace pod's SAS-fetch
// init container has failed or is crash-looping. Returns empty strings when no
// such failure is observed.
func (c *WorkspaceReconciler) detectSASInitFailure(ctx context.Context, wObj *kaitov1beta1.Workspace) (reason, message string) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(wObj.Namespace),
		client.MatchingLabels{kaitov1beta1.LabelWorkspaceName: wObj.Name}); err != nil {
		return "", ""
	}
	for i := range pods.Items {
		for _, ics := range pods.Items[i].Status.InitContainerStatuses {
			if ics.Name != modelstreaming.SASFetchInitContainerName {
				continue
			}
			if t := ics.LastTerminationState.Terminated; t != nil && t.ExitCode != 0 {
				return "SASTokenFetchFailed", "SAS token fetch failed: the streaming init container could not obtain a SAS token; check the fetch-sas init container logs"
			}
			if w := ics.State.Waiting; w != nil && w.Reason == "CrashLoopBackOff" {
				return "SASTokenFetchFailed", "SAS token fetch failed: the streaming init container could not obtain a SAS token; check the fetch-sas init container logs"
			}
		}
	}
	return "", ""
}

// benchmarkEntrypointMarker is the script basename that uniquely identifies the
// benchmark startup probe. Kept in sync with buildBenchmarkStartupProbe in
// pkg/workspace/inference/preset_inferences.go.
const benchmarkEntrypointMarker = "benchmark_entrypoint.py"

// hasBenchmarkStartupProbe reports whether the StatefulSet's pod template has the
// benchmark startup probe on any container. This is used to determine if it is
// a legacy workspace without the probe (backward compatibility)
func hasBenchmarkStartupProbe(ss *appsv1.StatefulSet) bool {
	if ss == nil {
		return false
	}
	for i := range ss.Spec.Template.Spec.Containers {
		p := ss.Spec.Template.Spec.Containers[i].StartupProbe
		if p == nil || p.Exec == nil || len(p.Exec.Command) == 0 {
			continue
		}
		if strings.Contains(strings.Join(p.Exec.Command, " "), benchmarkEntrypointMarker) {
			return true
		}
	}
	return false
}

type tuningStatusSnapshot struct {
	started   bool
	succeeded bool
	failed    bool
	active    int32
	ready     int32
}

func (c *WorkspaceReconciler) collectTuningStatusSnapshot(ctx context.Context, wObj *kaitov1beta1.Workspace) (*tuningStatusSnapshot, error) {
	snapshot := &tuningStatusSnapshot{}
	if wObj.Tuning == nil {
		return snapshot, nil
	}

	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Name: wObj.Name, Namespace: wObj.Namespace}, job); err != nil {
		if apierrors.IsNotFound(err) {
			return snapshot, nil
		}
		return nil, err
	}

	snapshot.active = job.Status.Active
	if job.Status.Ready != nil {
		snapshot.ready = *job.Status.Ready
	}
	snapshot.failed = job.Status.Failed > 0
	snapshot.succeeded = job.Status.Succeeded > 0
	snapshot.started = snapshot.succeeded || snapshot.ready > 0 || snapshot.active > 0

	return snapshot, nil
}

func buildReconcileErrMessageAppender(reconcileErr error) func(message string) string {
	return func(message string) string {
		if reconcileErr == nil {
			return message
		}
		return fmt.Sprintf("%s (last reconcile error: %s)", message, reconcileErr.Error())
	}
}

func setWorkspaceCondition(status *kaitov1beta1.WorkspaceStatus, generation int64, appendMessage func(string) string,
	conditionType kaitov1beta1.ConditionType, conditionStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             conditionStatus,
		Reason:             reason,
		Message:            appendMessage(message),
		ObservedGeneration: generation,
	})
}

func applyTuningWorkspaceStatus(status *kaitov1beta1.WorkspaceStatus, generation int64, appendMessage func(string) string, snapshot *tuningStatusSnapshot) {
	if snapshot.failed {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionFalse, "WorkspaceTuningJobStatusFailed", "tuning job failed")
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspaceFailed", "tuning job failed")
		status.State = kaitov1beta1.WorkspaceStateFailed
		return
	}

	if snapshot.started {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionTrue, "WorkspaceTuningJobStatusStarted", "Tuning job has started")
	} else {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionFalse, "WorkspaceTuningJobStatusPending", "Tuning job has not started")
	}

	if snapshot.succeeded {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue, "workspaceSucceeded", "workspace succeeds")
		status.State = kaitov1beta1.WorkspaceStateSucceeded
	} else if snapshot.started {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspacePending", "workspace has not completed yet, tuning job is running")
		status.State = kaitov1beta1.WorkspaceStateRunning
	} else {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspacePending", "workspace is initializing, tuning job has not started")
		status.State = kaitov1beta1.WorkspaceStatePending
	}
}

func applyInferenceWorkspaceStatus(ctx context.Context, status *kaitov1beta1.WorkspaceStatus, wObj *kaitov1beta1.Workspace, appendMessage func(string) string,
	inferenceReady bool, resourceConditionStatus metav1.ConditionStatus, benchmarkApplicable bool, notReadyReason, notReadyMessage string) {
	generation := wObj.GetGeneration()
	resourceReady := resourceConditionStatus == metav1.ConditionTrue
	isInferenceEstablished := status.State == kaitov1beta1.WorkspaceStateReady || status.State == kaitov1beta1.WorkspaceStateNotReady

	if inferenceReady && resourceReady {
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionTrue, "WorkspaceInferenceStatusSuccess", "Inference has been deployed successfully")

		if benchmarkApplicable {
			if err := applyBenchmarkStatus(ctx, status, wObj, generation, appendMessage); err != nil {
				setWorkspaceCondition(status, generation, appendMessage,
					kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "BenchmarkFailed", err.Error())
				status.State = kaitov1beta1.WorkspaceStateNotReady
				return
			}
		}

		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue, "workspaceSucceeded", "workspace succeeds")
		status.State = kaitov1beta1.WorkspaceStateReady
		return
	}

	// Default to a generic pending reason; override with a specific one (e.g. a failed
	// SAS token fetch) when the caller detected the cause.
	inferenceReason, inferenceMessage := "WorkspaceInferenceStatusPending", "Inference workload is not ready"
	if notReadyReason != "" {
		inferenceReason, inferenceMessage = notReadyReason, notReadyMessage
	}
	setWorkspaceCondition(status, generation, appendMessage,
		kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionFalse, inferenceReason, inferenceMessage)
	setWorkspaceCondition(status, generation, appendMessage,
		kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspacePending", "workspace is waiting for inference workload readiness")

	if isInferenceEstablished {
		status.State = kaitov1beta1.WorkspaceStateNotReady
	} else {
		status.State = kaitov1beta1.WorkspaceStatePending
	}
}

// applyBenchmarkStatus reads and parses the benchmark result from pod logs,
// then sets the BenchmarkCompleted condition on the workspace status.
// Returns nil on success (or when already recorded), non-nil on terminal failure.
// The result is write-once: the skip guard below returns early once
// BenchmarkCompleted is True, so a recorded result is never re-read or overwritten.
func applyBenchmarkStatus(ctx context.Context, status *kaitov1beta1.WorkspaceStatus, wObj *kaitov1beta1.Workspace, generation int64, appendMessage func(string) string) error {
	// Skip once the benchmark is done (write-once). Nothing clears BenchmarkCompleted on a
	// readiness transition, so a recorded result survives transient flaps and pod restarts.
	if c := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted)); c != nil && c.Status == metav1.ConditionTrue {
		return nil
	}

	result, err := reconcileBenchmarkResult(ctx, wObj)
	// These errors are terminal
	if err != nil {
		klog.ErrorS(err, "benchmark failed", "workspace", klog.KObj(wObj))
		setWorkspaceCondition(status, generation, appendMessage,
			kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted, metav1.ConditionFalse,
			"BenchmarkFailed", err.Error())
		return err
	}

	status.Performance = result
	setWorkspaceCondition(status, generation, appendMessage,
		kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted, metav1.ConditionTrue,
		"BenchmarkCompleted", "benchmark result has been recorded")
	return nil
}

// resetBenchmarkOnUpgrade clears the recorded benchmark result and removes the
// BenchmarkCompleted condition, so the post-load benchmark re-runs against the new
// image on the next reconcile.
func (c *WorkspaceReconciler) resetBenchmarkOnUpgrade(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	return workspace.UpdateWorkspaceStatus(ctx, c.Client, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace},
		func(status *kaitov1beta1.WorkspaceStatus) error {
			status.Performance = nil
			meta.RemoveStatusCondition(&status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted))
			return nil
		})
}

// RuntimeMetadataConfig returns the serving runtime metadata (engine, engine
// version, quantization) as a Metric.Config map, so external systems (e.g. a
// portal) can read it. Shared by the Workspace and InferenceSet controllers.
func RuntimeMetadataConfig(runtimeName pkgmodel.RuntimeName, presetName string) map[string]string {
	return map[string]string{
		ConfigKeyEngine:        string(runtimeName),
		ConfigKeyEngineVersion: inference.GetBaseRuntimeVersion(runtimeName),
		ConfigKeyQuantization:  inference.GetPresetQuantization(presetName),
	}
}

func (c *WorkspaceReconciler) updateWorkspaceStatusIfChanged(ctx context.Context, key types.NamespacedName, modifyFn func(*kaitov1beta1.WorkspaceStatus) error) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			wObj := &kaitov1beta1.Workspace{}
			if err := c.Get(ctx, key, wObj); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}

			originalStatus := *wObj.Status.DeepCopy()
			if modifyFn != nil {
				if err := modifyFn(&wObj.Status); err != nil {
					return err
				}
			}

			if apiequality.Semantic.DeepEqual(originalStatus, wObj.Status) {
				return nil
			}

			if klog.V(4).Enabled() {
				klog.InfoS("Workspace status changed",
					"workspace", key.String(),
					"changes", formatWorkspaceStatusChanges(originalStatus, wObj.Status))
			}

			return c.Status().Update(ctx, wObj)
		})
}

func formatWorkspaceStatusChanges(oldStatus, newStatus kaitov1beta1.WorkspaceStatus) string {
	changes := make([]string, 0)

	if !apiequality.Semantic.DeepEqual(oldStatus.WorkerNodes, newStatus.WorkerNodes) {
		changes = append(changes, fmt.Sprintf("workerNodes: %v -> %v", oldStatus.WorkerNodes, newStatus.WorkerNodes))
	}

	if oldStatus.State != newStatus.State {
		changes = append(changes, fmt.Sprintf("state: %q -> %q", oldStatus.State, newStatus.State))
	}

	oldConditionByType := make(map[string]metav1.Condition, len(oldStatus.Conditions))
	newConditionByType := make(map[string]metav1.Condition, len(newStatus.Conditions))
	conditionTypes := make([]string, 0, len(oldStatus.Conditions)+len(newStatus.Conditions))

	for i := range oldStatus.Conditions {
		condition := oldStatus.Conditions[i]
		oldConditionByType[condition.Type] = condition
		conditionTypes = append(conditionTypes, condition.Type)
	}

	for i := range newStatus.Conditions {
		condition := newStatus.Conditions[i]
		newConditionByType[condition.Type] = condition
		if _, exists := oldConditionByType[condition.Type]; !exists {
			conditionTypes = append(conditionTypes, condition.Type)
		}
	}

	sort.Strings(conditionTypes)
	seenType := make(map[string]struct{}, len(conditionTypes))
	for _, conditionType := range conditionTypes {
		if _, seen := seenType[conditionType]; seen {
			continue
		}
		seenType[conditionType] = struct{}{}

		oldCondition, oldExists := oldConditionByType[conditionType]
		newCondition, newExists := newConditionByType[conditionType]

		switch {
		case !oldExists && newExists:
			changes = append(changes, fmt.Sprintf("condition[%s]: added(status=%s, reason=%s, message=%q)",
				conditionType, newCondition.Status, newCondition.Reason, newCondition.Message))
		case oldExists && !newExists:
			changes = append(changes, fmt.Sprintf("condition[%s]: removed(status=%s, reason=%s, message=%q)",
				conditionType, oldCondition.Status, oldCondition.Reason, oldCondition.Message))
		case oldExists && newExists && !apiequality.Semantic.DeepEqual(oldCondition, newCondition):
			changes = append(changes, fmt.Sprintf("condition[%s]: status=%s->%s, reason=%s->%s, message=%q->%q, observedGeneration=%d->%d",
				conditionType,
				oldCondition.Status, newCondition.Status,
				oldCondition.Reason, newCondition.Reason,
				oldCondition.Message, newCondition.Message,
				oldCondition.ObservedGeneration, newCondition.ObservedGeneration))
		}
	}

	if len(changes) == 0 {
		return "status changed"
	}

	return strings.Join(changes, "; ")
}

// UpdateWorkspaceTargetNodeCount is used for updating the targetNodeCount in workspace status when it is 0.
func (c *WorkspaceReconciler) UpdateWorkspaceTargetNodeCount(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	var err error
	targetNodeCount := int32(1)
	if wObj.Status.TargetNodeCount == 0 {
		// Build the estimate request once, outside the status-update closure.
		req, reqErr := workspace.NodeEstimateRequestFromWorkspace(ctx, wObj, c.Client)
		if reqErr != nil {
			return fmt.Errorf("failed to build node estimate request: %w", reqErr)
		}

		// Resolve the context window size from the workspace's inference ConfigMap (if any)
		// and pass it through RuntimeProfile so the estimator does not need to do I/O.
		if wObj.Inference != nil && wObj.Inference.Config != "" {
			configMap := &corev1.ConfigMap{}
			if cmErr := resources.GetResource(ctx, wObj.Inference.Config, wObj.Namespace, c.Client, configMap); cmErr != nil {
				klog.Warningf("[UpdateWorkspaceTargetNodeCount] workspace=%s: failed to get ConfigMap %s: %v, using estimator default context size",
					wObj.Name, wObj.Inference.Config, cmErr)
			} else if configData, exists := configMap.Data["inference_config.yaml"]; exists {
				if contextSize, found := utils.ParseExplicitMaxModelLen(configData); found {
					req.RuntimeProfile = estimator.RuntimeProfile{ContextSize: contextSize}
				}
			}
		}

		if err := workspace.UpdateWorkspaceStatus(ctx, c.Client, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, func(status *kaitov1beta1.WorkspaceStatus) error {
			if wObj.Inference != nil {
				if v1beta1.GetWorkspaceRuntimeName(wObj) == pkgmodel.RuntimeNameVLLM {
					targetNodeCount, err = c.Estimator.EstimateNodeCount(ctx, req, c.Client)
					if err != nil {
						return fmt.Errorf("failed to calculate target node count: %w", err)
					}
					if targetNodeCount < 1 {
						targetNodeCount = 1
					}
				} else {
					// For non-vLLM runtime, use the Resource.Count directly
					//nolint:staticcheck //SA1019: deprecate Resource.Count field
					targetNodeCount = int32(*wObj.Resource.Count)
					klog.Infof("[EstimateNodeCount] workspace=%s using Resource.Count=%d for non-vLLM runtime", wObj.Name, targetNodeCount)
				}
			}
			status.TargetNodeCount = int32(targetNodeCount)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update Workspace status targetNodeCount: %w", err)
		}
		// Update the wObj to reflect the latest status change.
		wObj.Status.TargetNodeCount = targetNodeCount
	}

	return nil
}

// guardTargetNodeCount blocks provisioning when the persisted target node
// count exceeds MaxAllowedNodeCount. Only enforced for inference; tuning
// paths set Resource.Count directly and do not go through the estimator.
func (c *WorkspaceReconciler) guardTargetNodeCount(wObj *kaitov1beta1.Workspace) error {
	if wObj.Inference == nil || wObj.Status.TargetNodeCount <= MaxAllowedNodeCount {
		return nil
	}
	msg := fmt.Sprintf("estimated node count %d exceeds the maximum allowed %d; "+
		"node provisioning halted. Use a larger GPU instance type or reduce model/context size.",
		wObj.Status.TargetNodeCount, MaxAllowedNodeCount)
	if c.Recorder != nil {
		c.Recorder.Eventf(wObj, corev1.EventTypeWarning, "NodeCountExceedsLimit", msg)
	}
	return fmt.Errorf("%s", msg)
}

// SetupWithManager sets up the controller with the Manager.
func (c *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("Workspace")

	bldr := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1beta1.Workspace{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.ControllerRevision{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{})

	// Only watch NodeClaim resources if node auto-provisioning is enabled
	if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		bldr = bldr.Watches(&karpenterv1.NodeClaim{},
			&nodeClaimEventHandler{
				logger:         c.klogger,
				expectations:   c.expectations,
				enqueueHandler: enqueueWorkspaceForNodeClaim,
			},
			builder.WithPredicates(nodeclaim.NodeClaimPredicate),
		)
	}

	// Watch ModelMirror CRs to immediately reconcile workspaces when downloads complete.
	if featuregates.FeatureGates[consts.FeatureFlagModelStreaming] {
		bldr = bldr.Watches(&kaitov1alpha1.ModelMirror{},
			enqueueWorkspacesForModelMirror(c.Client),
		)
	}

	bldr = bldr.WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	go monitorWorkspaces(context.Background(), c.Client)

	return bldr.Complete(c)
}
