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
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
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

func (c *WorkspaceReconciler) reconcileNodes(ctx context.Context, wObj *kaitov1beta1.Workspace) (result *reconcile.Result, err error) {
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
	}

	// Check if selected nodes are ready in both NAP and BYO scenarios.
	ready, err := c.nodeResourceManager.EnsureNodesReady(ctx, wObj, matchingNodes, existingNodeClaims)
	if err != nil {
		return &reconcile.Result{}, err
	} else if !ready {
		// The node resource changes can not trigger workspace controller reconcile, so we need to requeue reconcile when don't proceed because of node resource not ready.
		return &reconcile.Result{RequeueAfter: 2 * time.Second}, nil
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

	if result, err := c.reconcileNodes(ctx, wObj); err != nil || result != nil {
		return *result, err
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
	if wObj.Tuning == nil || wObj.Tuning.Preset == nil {
		return nil
	}

	presetName := string(wObj.Tuning.Preset.Name)
	model := plugin.KaitoModelRegister.MustGet(presetName)
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

	_, err := tuning.CreatePresetTuning(ctx, wObj, revisionNum, model, c.Client)
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
	workloadObj, err := inference.GeneratePresetInference(ctx, wObj, revisionStr, model, c.Client)
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
	// If the current workload revision matches the one in Workspace, we do not need to update it.
	if ok && currentRevisionStr == revisionStr {
		return nil
	}

	desiredPodSpec := desiredStatefulSet.Spec.Template.Spec
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
	return c.Update(ctx, existingObj)
}

func (c *WorkspaceReconciler) syncWorkspaceStatus(ctx context.Context, key types.NamespacedName, reconcileErr error) error {
	wObj := &kaitov1beta1.Workspace{}
	if err := c.Get(ctx, key, wObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	inferenceReady := false
	tuningJobStarted := false
	tuningJobSucceeded := false
	tuningJobFailed := false
	jobActive := int32(0)
	jobReady := int32(0)

	matchingNodes := []*corev1.Node{}
	readyNodes := []*corev1.Node{}
	workerNodeNames := []string{}
	nodeReadyCount := 0
	targetNodeCount := int(wObj.Status.TargetNodeCount)
	nodeConditionStatus := metav1.ConditionFalse
	nodeConditionReason := "NodeNotReady"
	nodeConditionMessage := fmt.Sprintf("Not enough Nodes are ready (TargetNodes: %d, CurrentReadyNodes: %d, SelectedNodes: %d)", targetNodeCount, 0, 0)

	nodeClaimRequired := !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	nodeClaimConditionStatus := metav1.ConditionFalse
	nodeClaimConditionReason := "NodeClaimNotReady"
	nodeClaimConditionMessage := "Ready NodeClaims are not enough (TargetNodeClaims: 0, CurrentReadyNodeClaims: 0)"

	resourceConditionStatus := metav1.ConditionFalse
	resourceConditionReason := "workspaceResourceStatusNotReady"
	resourceConditionMessage := "node claim or node status condition not ready"

	var matchLabels client.MatchingLabels
	if wObj.Resource.LabelSelector != nil {
		matchLabels = wObj.Resource.LabelSelector.MatchLabels
	}

	nodeList, err := resources.ListNodes(ctx, c.Client, matchLabels)
	if err != nil {
		return err
	}
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		matchingNodes = append(matchingNodes, node)
		workerNodeNames = append(workerNodeNames, node.Name)
		if resources.NodeIsReadyAndNotDeleting(node) {
			readyNodes = append(readyNodes, node)
			if !nodeClaimRequired {
				nodeReadyCount++
				continue
			}
			instanceType, ok := node.Labels[corev1.LabelInstanceTypeStable]
			if ok && instanceType == wObj.Resource.InstanceType {
				nodeReadyCount++
			}
		}
	}
	sort.Strings(workerNodeNames)

	existingNodeClaims := []*karpenterv1.NodeClaim{}
	if nodeClaimRequired {
		ncList, err := nodeclaim.ListNodeClaim(ctx, wObj, c.Client)
		if err != nil {
			return fmt.Errorf("failed to list node claims: %w", err)
		}
		for i := range ncList.Items {
			existingNodeClaims = append(existingNodeClaims, &ncList.Items[i])
		}

		targetNodeClaimCount := c.nodeClaimManager.GetNumNodeClaimsNeeded(ctx, wObj, readyNodes)
		readyNodeClaimCount := 0
		for _, claim := range existingNodeClaims {
			if nodeclaim.IsNodeClaimReadyNotDeleting(claim) {
				readyNodeClaimCount++
			}
		}

		if readyNodeClaimCount >= targetNodeClaimCount {
			nodeClaimConditionStatus = metav1.ConditionTrue
			nodeClaimConditionReason = "NodeClaimsReady"
			nodeClaimConditionMessage = fmt.Sprintf("Enough NodeClaims are ready (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeClaimCount, readyNodeClaimCount)
		} else {
			nodeClaimConditionStatus = metav1.ConditionFalse
			nodeClaimConditionReason = "NodeClaimNotReady"
			nodeClaimConditionMessage = fmt.Sprintf("Ready NodeClaims are not enough (TargetNodeClaims: %d, CurrentReadyNodeClaims: %d)", targetNodeClaimCount, readyNodeClaimCount)
		}
	}

	if nodeReadyCount >= targetNodeCount {
		nodeConditionStatus = metav1.ConditionTrue
		nodeConditionReason = "NodesReady"
		nodeConditionMessage = fmt.Sprintf("Enough Nodes are ready (TargetNodes: %d, CurrentReadyNodes: %d, SelectedNodes: %d)", targetNodeCount, nodeReadyCount, len(matchingNodes))
		if nodeClaimRequired {
			pluginReady, pluginErr := c.nodeResourceManager.CheckIfNodePluginsReady(ctx, wObj, existingNodeClaims)
			if pluginErr != nil {
				nodeConditionStatus = metav1.ConditionFalse
				nodeConditionReason = "NodePluginsNotReady"
				nodeConditionMessage = pluginErr.Error()
			} else if !pluginReady {
				nodeConditionStatus = metav1.ConditionFalse
				nodeConditionReason = "NodePluginsNotReady"
				nodeConditionMessage = "waiting all node plugins to be ready"
			}
		}
	} else {
		nodeConditionStatus = metav1.ConditionFalse
		nodeConditionReason = "NodeNotReady"
		nodeConditionMessage = fmt.Sprintf("Not enough Nodes are ready (TargetNodes: %d, CurrentReadyNodes: %d, SelectedNodes: %d)", targetNodeCount, nodeReadyCount, len(matchingNodes))
	}

	if nodeConditionStatus == metav1.ConditionTrue && (!nodeClaimRequired || nodeClaimConditionStatus == metav1.ConditionTrue) {
		resourceConditionStatus = metav1.ConditionTrue
		resourceConditionReason = "workspaceResourceStatusSuccess"
		resourceConditionMessage = "workspace resource is ready"
	} else if nodeClaimRequired && nodeClaimConditionStatus != metav1.ConditionTrue {
		resourceConditionStatus = metav1.ConditionFalse
		resourceConditionReason = nodeClaimConditionReason
		resourceConditionMessage = nodeClaimConditionMessage
	} else {
		resourceConditionStatus = metav1.ConditionFalse
		resourceConditionReason = nodeConditionReason
		resourceConditionMessage = nodeConditionMessage
	}

	if wObj.Inference != nil {
		ss := &appsv1.StatefulSet{}
		if err := c.Get(ctx, types.NamespacedName{Name: wObj.Name, Namespace: wObj.Namespace}, ss); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		} else {
			replicas := int32(1)
			if ss.Spec.Replicas != nil {
				replicas = *ss.Spec.Replicas
			}
			inferenceReady = ss.Status.ReadyReplicas == replicas
		}
	}

	if wObj.Tuning != nil {
		job := &batchv1.Job{}
		if err := c.Get(ctx, types.NamespacedName{Name: wObj.Name, Namespace: wObj.Namespace}, job); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		} else {
			jobActive = job.Status.Active
			if job.Status.Ready != nil {
				jobReady = *job.Status.Ready
			}
			tuningJobFailed = job.Status.Failed > 0
			tuningJobSucceeded = job.Status.Succeeded > 0
			tuningJobStarted = tuningJobSucceeded || jobReady > 0 || jobActive > 0
		}
	}

	appendReconcileErrMessage := func(message string) string {
		if reconcileErr == nil {
			return message
		}
		return fmt.Sprintf("%s (last reconcile error: %s)", message, reconcileErr.Error())
	}

	return c.updateWorkspaceStatusIfChanged(ctx, key, func(status *kaitov1beta1.WorkspaceStatus) error {
		setCondition := func(conditionType kaitov1beta1.ConditionType, conditionStatus metav1.ConditionStatus, reason, message string) {
			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type:               string(conditionType),
				Status:             conditionStatus,
				Reason:             reason,
				Message:            appendReconcileErrMessage(message),
				ObservedGeneration: wObj.GetGeneration(),
				LastTransitionTime: metav1.Now(),
			})
		}

		if !wObj.DeletionTimestamp.IsZero() {
			setCondition(kaitov1beta1.WorkspaceConditionTypeDeleting, metav1.ConditionTrue, "workspaceDeleted", "workspace is being deleted")
			return nil
		}

		status.WorkerNodes = workerNodeNames
		setCondition(kaitov1beta1.ConditionTypeNodeStatus, nodeConditionStatus, nodeConditionReason, nodeConditionMessage)
		if nodeClaimRequired {
			setCondition(kaitov1beta1.ConditionTypeNodeClaimStatus, nodeClaimConditionStatus, nodeClaimConditionReason, nodeClaimConditionMessage)
		} else {
			meta.RemoveStatusCondition(&status.Conditions, string(kaitov1beta1.ConditionTypeNodeClaimStatus))
		}
		setCondition(kaitov1beta1.ConditionTypeResourceStatus, resourceConditionStatus, resourceConditionReason, resourceConditionMessage)

		if wObj.Tuning != nil {
			if tuningJobFailed {
				setCondition(kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionFalse, "WorkspaceTuningJobStatusFailed", "tuning job failed")
				setCondition(kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspaceFailed", "tuning job failed")
				status.State = kaitov1beta1.WorkspaceStateFailed
				return nil
			}

			if tuningJobStarted {
				setCondition(kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionTrue, "WorkspaceTuningJobStatusStarted", "Tuning job has started")
			} else {
				setCondition(kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionFalse, "WorkspaceTuningJobStatusPending", "Tuning job has not started")
			}

			if tuningJobSucceeded {
				setCondition(kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue, "workspaceSucceeded", "workspace succeeds")
				status.State = kaitov1beta1.WorkspaceStateSucceeded
			} else if tuningJobStarted {
				setCondition(kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspacePending", fmt.Sprintf("workspace has not completed, tuning job has %d active pod, %d ready pod", jobActive, jobReady))
				status.State = kaitov1beta1.WorkspaceStateRunning
			} else {
				setCondition(kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspacePending", "workspace is initializing, tuning job has not started")
				status.State = kaitov1beta1.WorkspaceStatePending
			}
			return nil
		}

		if wObj.Inference != nil {
			resourceReady := resourceConditionStatus == metav1.ConditionTrue
			isInferenceEstablished := status.State == kaitov1beta1.WorkspaceStateReady || status.State == kaitov1beta1.WorkspaceStateNotReady

			if inferenceReady && resourceReady {
				setCondition(kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionTrue, "WorkspaceInferenceStatusSuccess", "Inference has been deployed successfully")
				setCondition(kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue, "workspaceSucceeded", "workspace succeeds")
				status.State = kaitov1beta1.WorkspaceStateReady
			} else {
				setCondition(kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionFalse, "WorkspaceInferenceStatusPending", "Inference workload is not ready")
				setCondition(kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse, "workspacePending", "workspace is waiting for inference workload readiness")
				if isInferenceEstablished {
					status.State = kaitov1beta1.WorkspaceStateNotReady
				} else {
					status.State = kaitov1beta1.WorkspaceStatePending
				}
			}
			return nil
		}

		return nil
	})
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

			originalStatus := wObj.Status
			if modifyFn != nil {
				if err := modifyFn(&wObj.Status); err != nil {
					return err
				}
			}

			if apiequality.Semantic.DeepEqual(originalStatus, wObj.Status) {
				return nil
			}

			return c.Status().Update(ctx, wObj)
		})
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
