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
	"sort"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/pkg/workspace/estimator/advancednodesestimator"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
	"github.com/kaito-project/kaito/pkg/workspace/tuning"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	WorkspaceHashAnnotation = "workspace.kaito.io/hash"
	WorkspaceNameLabel      = "workspace.kaito.io/name"
	revisionHashSuffix      = 5
)

type WorkspaceReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	klogger             klog.Logger
	expectations        *utils.ControllerExpectations
	Estimator           estimator.NodesEstimator
	nodeClaimManager    *resource.NodeClaimManager
	nodeResourceManager *resource.NodeManager
}

func NewWorkspaceReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, Recorder record.EventRecorder) *WorkspaceReconciler {
	expectations := utils.NewControllerExpectations()
	return &WorkspaceReconciler{
		Client:              client,
		Scheme:              scheme,
		Log:                 log,
		klogger:             klog.NewKlogr().WithName("WorkspaceController"),
		Recorder:            Recorder,
		expectations:        expectations,
		Estimator:           &advancednodesestimator.AdvancedNodesEstimator{},
		nodeClaimManager:    resource.NewNodeClaimManager(client, Recorder, expectations),
		nodeResourceManager: resource.NewNodeManager(client),
	}
}

func (c *WorkspaceReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	workspaceObj := &kaitov1beta1.Workspace{}
	if err := c.Client.Get(ctx, req.NamespacedName, workspaceObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.klogger, req.String())
			return reconcile.Result{}, nil
		}
		klog.ErrorS(err, "failed to get workspace", "workspace", req.Name)
		return reconcile.Result{}, err
	}

	klog.InfoS("Reconciling", "workspace", req.NamespacedName)

	if workspaceObj.DeletionTimestamp.IsZero() {
		if err := c.ensureFinalizer(ctx, workspaceObj); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Handle deleting workspace, garbage collect all the resources.
		return c.deleteWorkspace(ctx, workspaceObj)
	}

	if err := c.syncControllerRevision(ctx, workspaceObj); err != nil {
		return reconcile.Result{}, err
	}

	// update targetNodeCount for the workspace
	if err := c.UpdateWorkspaceTargetNodeCount(ctx, workspaceObj); err != nil {
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

func (c *WorkspaceReconciler) reconcileNodes(ctx context.Context, wObj *kaitov1beta1.Workspace) (*reconcile.Result, error) {
	defer func() {
		if err := c.nodeResourceManager.SetResourceReadyConditionByStatus(ctx, wObj); err != nil {
			klog.ErrorS(err, "failed to update resource status", "workspace", klog.KObj(wObj))
		}
	}()

	nodeList, err := resources.ListNodes(ctx, c.Client, wObj.Resource.LabelSelector.MatchLabels)
	if err != nil {
		return &reconcile.Result{}, err
	}
	matchingNodes := []*corev1.Node{}
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		matchingNodes = append(matchingNodes, node)
	}

	readyNodes, err := resources.GetReadyNodes(ctx, c.Client, wObj)
	if err != nil {
		return &reconcile.Result{}, fmt.Errorf("failed to list ready nodes: %w", err)
	}

	existingNodeClaims := []*karpenterv1.NodeClaim{}
	if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		// diff node claims
		var numNodeClaimsToCreate int
		var err error
		numNodeClaimsToCreate, existingNodeClaims, err = c.nodeClaimManager.CheckNodeClaims(ctx, wObj, readyNodes)
		klog.Info("NodeClaims to create", "count", numNodeClaimsToCreate, "workspace", klog.KObj(wObj))
		if err != nil {
			return &reconcile.Result{}, err
		}

		// create nodeclaims
		if err := c.nodeClaimManager.CreateUpNodeClaims(ctx, wObj, numNodeClaimsToCreate); err != nil {
			return &reconcile.Result{}, err
		}

		// check nodeclaims meet the target count
		if ready, err := c.nodeClaimManager.EnsureNodeClaimsReady(ctx, wObj, readyNodes, existingNodeClaims); err != nil {
			return &reconcile.Result{}, err
		} else if !ready {
			// Not enough ready nodeclaims, requeue and wait for next reconcile.
			return &reconcile.Result{}, nil
		}

		// check node plugins ready
		if ready, err := c.nodeResourceManager.CheckIfNodePluginsReady(ctx, wObj, existingNodeClaims); err != nil {
			return &reconcile.Result{}, err
		} else if !ready {
			// The node resource changes can not trigger workspace controller reconcile, so we need to requeue reconcile when don't proceed because of node resource not ready.
			return &reconcile.Result{RequeueAfter: 2 * time.Second}, nil
		}
	}

	// Check if selected nodes are ready in both NAP and BYO scenarios.
	_, err = c.nodeResourceManager.EnsureNodesReady(ctx, wObj, matchingNodes, existingNodeClaims)

	return nil, err
}

func (c *WorkspaceReconciler) addOrUpdateWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	workspaceKey := client.ObjectKeyFromObject(wObj).String()
	if !c.expectations.SatisfiedExpectations(c.Log, workspaceKey) {
		klog.V(4).InfoS("Waiting for NodeClaim expectations to be satisfied",
			"workspace", workspaceKey)
		return reconcile.Result{}, nil
	}

	if result, err := c.reconcileNodes(ctx, wObj); err != nil || result != nil {
		return *result, err
	}

	var err error

	if wObj.Tuning != nil {
		if err = c.applyTuning(ctx, wObj); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
				"workspaceFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return reconcile.Result{}, updateErr
			}
			return reconcile.Result{}, err
		}
		// Only mark workspace succeeded when job completes.
		job := &batchv1.Job{}
		if err = resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, job); err == nil {
			if job.Status.Succeeded > 0 {
				if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue,
					"workspaceSucceeded", "workspace succeeds"); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
					return reconcile.Result{}, updateErr
				}
			} else { // The job is still running
				var readyPod int32
				if job.Status.Ready != nil {
					readyPod = *job.Status.Ready
				}
				if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
					"workspacePending", fmt.Sprintf("workspace has not completed, tuning job has %d active pod, %d ready pod", job.Status.Active, readyPod)); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
					return reconcile.Result{}, updateErr
				}
			}
		} else {
			klog.ErrorS(err, "failed to get job resource", "workspace", klog.KObj(wObj))
			return reconcile.Result{}, err
		}
	} else if wObj.Inference != nil {
		if err := c.ensureService(ctx, wObj); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
				"workspaceFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return reconcile.Result{}, updateErr
			}
			return reconcile.Result{}, err
		}
		if err = c.applyInference(ctx, wObj); err != nil {
			if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
				"workspaceFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return reconcile.Result{}, updateErr
			}
			return reconcile.Result{}, err
		}
		if err = workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue,
			"workspaceSucceeded", "workspace succeeds"); err != nil {
			klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (c *WorkspaceReconciler) deleteWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	klog.InfoS("deleteWorkspace", "workspace", klog.KObj(wObj))
	err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeDeleting, metav1.ConditionTrue, "workspaceDeleted", "workspace is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return reconcile.Result{}, err
	}

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
	if err := resources.CreateResource(ctx, serviceObj, c.Client); err != nil {
		return err
	}

	// headless service for worker pod to discover the leader pod
	headlessService := manifests.GenerateHeadlessServiceManifest(wObj)
	if err := resources.CreateResource(ctx, headlessService, c.Client); err != nil {
		return err
	}

	return nil
}

func (c *WorkspaceReconciler) applyTuning(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	var err error
	func() {
		if wObj.Tuning.Preset != nil {
			presetName := string(wObj.Tuning.Preset.Name)
			model := plugin.KaitoModelRegister.MustGet(presetName)

			tuningParam := model.GetTuningParameters()
			existingObj := &batchv1.Job{}
			revisionNum := wObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation]
			if err = resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, existingObj); err == nil {
				klog.InfoS("A tuning workload already exists for workspace", "workspace", klog.KObj(wObj))

				if existingObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation] != revisionNum {
					deletePolicy := metav1.DeletePropagationForeground
					if err := c.Delete(ctx, existingObj, &client.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						return
					}

					var workloadObj client.Object
					workloadObj, err = tuning.CreatePresetTuning(ctx, wObj, revisionNum, model, c.Client)
					if err != nil {
						return
					}
					existingObj = workloadObj.(*batchv1.Job)
				}

				if err = resources.CheckResourceStatus(existingObj, c.Client, tuningParam.ReadinessTimeout); err != nil {
					return
				}
			} else if apierrors.IsNotFound(err) {
				var workloadObj client.Object
				// Need to create a new workload
				workloadObj, err = tuning.CreatePresetTuning(ctx, wObj, revisionNum, model, c.Client)
				if err != nil {
					return
				}
				if err = resources.CheckResourceStatus(workloadObj, c.Client, tuningParam.ReadinessTimeout); err != nil {
					return
				}
			}
		}
	}()

	if err != nil {
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionFalse,
			"WorkspaceTuningJobStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return updateErr
		}
		return err
	}

	if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionTrue,
		"WorkspaceTuningJobStatusStarted", "Tuning job has started"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}

	return nil
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
		if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionFalse,
			"WorkspaceInferenceMigration", "Migrating inference workload from Deployment to StatefulSet"); err != nil {
			klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return err
		}
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

	var err error
	func() {
		if wObj.Inference.Template != nil {
			var workloadObj client.Object
			// TODO: handle update
			workloadObj, err = inference.CreateTemplateInference(ctx, wObj, c.Client)
			if err != nil {
				return
			}
			if err = resources.CheckResourceStatus(workloadObj, c.Client, time.Duration(10)*time.Minute); err != nil {
				return
			}
		} else if wObj.Inference != nil && wObj.Inference.Preset != nil {
			presetName := string(wObj.Inference.Preset.Name)
			var model pkgmodel.Model
			model, err = models.GetModelByName(ctx, presetName, wObj.Inference.Preset.PresetOptions.ModelAccessSecret, wObj.Namespace, c.Client)
			if err != nil {
				klog.ErrorS(err, "failed to get model by name", "model", presetName, "workspace", klog.KObj(wObj))
				return
			}
			inferenceParam := model.GetInferenceParameters()
			revisionStr := wObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation]

			var workloadObj client.Object
			workloadObj, err = inference.GeneratePresetInference(ctx, wObj, revisionStr, model, c.Client)
			if err != nil {
				return
			}

			existingObj := &appsv1.StatefulSet{}
			desiredPodSpec := workloadObj.(*appsv1.StatefulSet).Spec.Template.Spec

			if err = resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, existingObj); err == nil {
				klog.InfoS("An inference workload already exists for workspace", "workspace", klog.KObj(wObj))
				annotations := existingObj.GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}

				currentRevisionStr, ok := annotations[kaitov1beta1.WorkspaceRevisionAnnotation]
				// If the current workload revision matches the one in Workspace, we do not need to update it.
				if ok && currentRevisionStr == revisionStr {
					err = resources.CheckResourceStatus(workloadObj, c.Client, inferenceParam.ReadinessTimeout)
					return
				}

				spec := &existingObj.Spec.Template.Spec

				// Selectively update the pod spec fields that are relevant to inference,
				// and leave the rest unchanged in case user has customized them.
				spec.Containers[0].Env = desiredPodSpec.Containers[0].Env
				spec.Containers[0].VolumeMounts = desiredPodSpec.Containers[0].VolumeMounts
				spec.InitContainers = desiredPodSpec.InitContainers
				spec.Volumes = desiredPodSpec.Volumes

				annotations[kaitov1beta1.WorkspaceRevisionAnnotation] = revisionStr
				existingObj.SetAnnotations(annotations)

				// Update it with the latest one generated above.
				if err = c.Update(ctx, existingObj); err != nil {
					return
				}

				err = resources.CheckResourceStatus(workloadObj, c.Client, inferenceParam.ReadinessTimeout)
				return
			} else if !apierrors.IsNotFound(err) {
				return
			}

			if err = resources.CreateResource(ctx, workloadObj, c.Client); err != nil {
				return
			}
			if err = resources.CheckResourceStatus(workloadObj, c.Client, inferenceParam.ReadinessTimeout); err != nil {
				return
			}
		}
	}()

	if err != nil {
		if updateErr := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionFalse,
			"WorkspaceInferenceStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return updateErr
		} else {
			return err
		}
	}

	if err := workspace.UpdateStatusConditionIfNotMatch(ctx, c.Client, wObj, kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionTrue,
		"WorkspaceInferenceStatusSuccess", "Inference has been deployed successfully"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}
	return nil
}

// UpdateWorkspaceTargetNodeCount is used for updating the targetNodeCount in workspace status when it is 0.
func (c *WorkspaceReconciler) UpdateWorkspaceTargetNodeCount(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	var err error
	targetNodeCount := int32(1)
	if wObj.Status.TargetNodeCount == 0 {
		if err := workspace.UpdateWorkspaceStatus(ctx, c.Client, &client.ObjectKey{Name: wObj.Name, Namespace: wObj.Namespace}, func(status *kaitov1beta1.WorkspaceStatus) error {
			if wObj.Inference != nil {
				if v1beta1.GetWorkspaceRuntimeName(wObj) == pkgmodel.RuntimeNameVLLM {
					targetNodeCount, err = c.Estimator.EstimateNodeCount(ctx, wObj, c.Client)
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

	bldr = bldr.WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	go monitorWorkspaces(context.Background(), c.Client)

	return bldr.Complete(c)
}
