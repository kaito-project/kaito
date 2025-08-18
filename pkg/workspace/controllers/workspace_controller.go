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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gaiev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	"github.com/kaito-project/kaito/pkg/workspace/tuning"
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

	klogger      klog.Logger
	expectations *utils.ControllerExpectations
}

func NewWorkspaceReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, Recorder record.EventRecorder) *WorkspaceReconciler {
	return &WorkspaceReconciler{
		Client:       client,
		Scheme:       scheme,
		Log:          log,
		klogger:      klog.NewKlogr().WithName("WorkspaceController"),
		Recorder:     Recorder,
		expectations: utils.NewControllerExpectations(),
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

	result, err := c.addOrUpdateWorkspace(ctx, workspaceObj)
	if err != nil {
		return result, err
	}

	return result, nil
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

func (c *WorkspaceReconciler) addOrUpdateWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	reqKey := client.ObjectKeyFromObject(wObj).String()
	// nodeclaim don't meet expectation if no enough nodeclaim events are observed within the timeout period.
	if !c.expectations.SatisfiedExpectations(c.klogger, reqKey) {
		startTime := c.expectations.GetExpectationStartTime(reqKey)
		requeueTime := time.Second * 5
		if startTime != nil {
			requeueTime = startTime.Add(utils.ExpectationsTimeout).Sub(clock.RealClock{}.Now())
			if requeueTime < 0 {
				requeueTime = 100 * time.Millisecond
			}
		}
		return reconcile.Result{RequeueAfter: requeueTime}, nil
	}

	// Read ResourceSpec
	err := c.applyWorkspaceResource(ctx, wObj)
	if err != nil {
		if errors.Is(err, reconcile.TerminalError(nil)) {
			return reconcile.Result{}, nil
		}
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
			"workspaceFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, err
	}

	if wObj.Tuning != nil {
		if err = c.applyTuning(ctx, wObj); err != nil {
			if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
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
				if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue,
					"workspaceSucceeded", "workspace succeeds"); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
					return reconcile.Result{}, updateErr
				}
			} else { // The job is still running
				var readyPod int32
				if job.Status.Ready != nil {
					readyPod = *job.Status.Ready
				}
				if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
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
			if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
				"workspaceFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return reconcile.Result{}, updateErr
			}
			return reconcile.Result{}, err
		}
		if err = c.applyInference(ctx, wObj); err != nil {
			if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
				"workspaceFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return reconcile.Result{}, updateErr
			}
			return reconcile.Result{}, err
		}
		if err = c.ensureGatewayAPIInferenceExtension(ctx, wObj); err != nil {
			if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionFalse,
				"workspaceFailed", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
				return reconcile.Result{}, updateErr
			}
		}

		if err = c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeSucceeded, metav1.ConditionTrue,
			"workspaceSucceeded", "workspace succeeds"); err != nil {
			klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (c *WorkspaceReconciler) deleteWorkspace(ctx context.Context, wObj *kaitov1beta1.Workspace) (reconcile.Result, error) {
	klog.InfoS("deleteWorkspace", "workspace", klog.KObj(wObj))
	err := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeDeleting, metav1.ConditionTrue, "workspaceDeleted", "workspace is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return reconcile.Result{}, err
	}

	return c.garbageCollectWorkspace(ctx, wObj)
}
func (c *WorkspaceReconciler) syncControllerRevision(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	currentHash := computeHash(wObj)
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

	err = updateWorkspaceWithRetry(ctx, c.Client, wObj, func(ws *kaitov1beta1.Workspace) error {
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

func computeHash(w *kaitov1beta1.Workspace) string {
	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	encoder.Encode(w.Resource)
	encoder.Encode(w.Inference)
	encoder.Encode(w.Tuning)
	return hex.EncodeToString(hasher.Sum(nil))
}

// applyWorkspaceResource applies workspace resource spec.
func (c *WorkspaceReconciler) applyWorkspaceResource(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	// Wait for pending nodeClaims if any before we decide whether to create new node or not.
	if err := nodeclaim.WaitForPendingNodeClaims(ctx, wObj, c.Client); err != nil {
		return err
	}

	// Find all nodes that meet the requirements, they are not necessarily created by machines/nodeClaims.
	validNodes, err := c.getAllQualifiedNodes(ctx, wObj)
	if err != nil {
		return err
	}

	selectedNodes := utils.SelectNodes(validNodes, wObj.Resource.PreferredNodes, wObj.Status.WorkerNodes, lo.FromPtr(wObj.Resource.Count))

	newNodesCount := lo.FromPtr(wObj.Resource.Count) - len(selectedNodes)

	if newNodesCount > 0 {
		// Check if node auto-provisioning is disabled
		if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
			return fmt.Errorf("node auto-provisioning is disabled but insufficient nodes available: need %d nodes, have %d selected nodes", lo.FromPtr(wObj.Resource.Count), len(selectedNodes))
		}

		klog.InfoS("need to create more nodes", "NodeCount", newNodesCount)
		if err := c.updateStatusConditionIfNotMatch(ctx, wObj,
			kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionUnknown,
			"CreateNodeClaimPending", fmt.Sprintf("creating %d nodeClaims", newNodesCount)); err != nil {
			klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return err
		}

		err := c.createNewNodes(ctx, wObj, newNodesCount)
		if err != nil {
			return fmt.Errorf("failed to create new nodes: %w", err)
		}
		// terminate the current reconciliation and rely on NodeClaim creation event to trigger a new reconciliation
		return reconcile.TerminalError(errors.New("waiting for new nodes to be created"))
	}

	// Ensure all gpu plugins are running successfully.
	knownGPUConfig, _ := utils.GetGPUConfigBySKU(wObj.Resource.InstanceType)
	if len(wObj.Resource.PreferredNodes) == 0 && knownGPUConfig != nil {
		for i := range selectedNodes {
			err = c.ensureNodePlugins(ctx, wObj, selectedNodes[i])
			if err != nil {
				if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
					"workspaceResourceStatusFailed", err.Error()); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
					return updateErr
				}
				return err
			}
		}
	}

	if err = c.updateStatusConditionIfNotMatch(ctx, wObj,
		kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
		"installNodePluginsSuccess", "nodeClaim plugins have been installed successfully"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}

	// Add the valid nodes names to the WorkspaceStatus.WorkerNodes.
	err = c.updateStatusNodeListIfNotMatch(ctx, wObj, selectedNodes)
	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
			"workspaceResourceStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return updateErr
		}
		return err
	}

	if err = c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue,
		"workspaceResourceStatusSuccess", "workspace resource is ready"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}

	return nil
}

func nodeIsReadyAndNotDeleting(node corev1.Node) bool {

	// skip nodes that are being deleted
	if node.DeletionTimestamp != nil {
		return false
	}

	// skip nodes that are not ready
	_, statusRunning := lo.Find(node.Status.Conditions, func(condition corev1.NodeCondition) bool {
		return condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue
	})

	return statusRunning
}

func (c *WorkspaceReconciler) getAllQualifiedNodes(ctx context.Context, wObj *kaitov1beta1.Workspace) ([]*corev1.Node, error) {
	var qualifiedNodes []*corev1.Node

	if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		for _, name := range wObj.Resource.PreferredNodes {
			// Get the node with a client call
			node, err := resources.GetNode(ctx, name, c.Client)
			if err != nil {
				klog.ErrorS(err, "failed to get preferred node", "node", name)
				continue
			}

			if nodeIsReadyAndNotDeleting(*node) {
				// Check that all labels in wObj.Resource.LabelSelector.MatchLabels are present in the node's labels.
				allLabelsMatch := true
				for k, v := range wObj.Resource.LabelSelector.MatchLabels {
					val, ok := node.Labels[k]
					if !ok || v != val {
						allLabelsMatch = false
						break
					}
				}
				if allLabelsMatch {
					qualifiedNodes = append(qualifiedNodes, node)
				}
			}
		}

		if len(qualifiedNodes) < *wObj.Resource.Count {
			return nil, fmt.Errorf("when node auto-provisioning is disabled, at least %d preferred nodes must match the label selector and be ready and not deleting, only have %d", *wObj.Resource.Count, len(qualifiedNodes))
		}
	} else {
		nodeList, err := resources.ListNodes(ctx, c.Client, wObj.Resource.LabelSelector.MatchLabels)
		if err != nil {
			return nil, err
		}

		if len(nodeList.Items) == 0 {
			klog.InfoS("no current nodes match the workspace resource spec", "workspace", klog.KObj(wObj))
			return nil, nil
		}

		preferredNodeSet := sets.New(wObj.Resource.PreferredNodes...)
		for index := range nodeList.Items {
			node := nodeList.Items[index]
			if nodeIsReadyAndNotDeleting(node) {
				// match the preferred node
				if preferredNodeSet.Has(node.Name) {
					qualifiedNodes = append(qualifiedNodes, lo.ToPtr(node))
					continue
				}

				// match the instanceType
				if node.Labels[corev1.LabelInstanceTypeStable] == wObj.Resource.InstanceType {
					qualifiedNodes = append(qualifiedNodes, lo.ToPtr(node))
				}
			}
		}
	}

	return qualifiedNodes, nil
}

// determineNodeOSDiskSize returns the appropriate OS disk size for the workspace
func (c *WorkspaceReconciler) determineNodeOSDiskSize(wObj *kaitov1beta1.Workspace) string {
	var nodeOSDiskSize string
	if wObj.Inference != nil && wObj.Inference.Preset != nil && wObj.Inference.Preset.Name != "" {
		presetName := string(wObj.Inference.Preset.Name)
		nodeOSDiskSize = plugin.KaitoModelRegister.MustGet(presetName).
			GetInferenceParameters().DiskStorageRequirement
	}
	if nodeOSDiskSize == "" {
		nodeOSDiskSize = "1024Gi" // The default OS size is used
	}
	return nodeOSDiskSize
}

// createAllNodeClaims creates multiple NodeClaims in parallel and returns the created NodeClaim objects
func (c *WorkspaceReconciler) createAllNodeClaims(ctx context.Context, wObj *kaitov1beta1.Workspace, count int, nodeOSDiskSize string) ([]*karpenterv1.NodeClaim, error) {
	klog.InfoS("Creating multiple NodeClaims", "count", count, "workspace", klog.KObj(wObj))

	nodeClaims := make([]*karpenterv1.NodeClaim, 0, count)

	for range count {
		var newNodeClaim *karpenterv1.NodeClaim

		// Set expectations before creating the NodeClaim in case event handler
		// observes the NodeClaim creation event before the controller does.
		c.expectations.ExpectCreations(c.klogger, client.ObjectKeyFromObject(wObj).String(), 1)
		newNodeCreated := false
		err := retry.OnError(retry.DefaultRetry, func(err error) bool {
			return apierrors.IsAlreadyExists(err)
		}, func() error {
			newNodeClaim = nodeclaim.GenerateNodeClaimManifest(nodeOSDiskSize, wObj)
			err0 := nodeclaim.CreateNodeClaim(ctx, newNodeClaim, c.Client)
			if err0 == nil {
				newNodeCreated = true
			}
			return err0
		})
		if !newNodeCreated {
			// Decrement the expected number of creates because the informer won't observe this nodeclaim
			c.expectations.CreationObserved(c.klogger, client.ObjectKeyFromObject(wObj).String())
		}

		if err != nil {
			klog.ErrorS(err, "failed to create nodeClaim", "nodeClaim", newNodeClaim.Name)
			return nil, fmt.Errorf("failed to create nodeClaim %s: %w", newNodeClaim.Name, err)
		}
		klog.InfoS("NodeClaim created successfully", "nodeClaim", newNodeClaim.Name, "workspace", klog.KObj(wObj))

		nodeClaims = append(nodeClaims, newNodeClaim)
	}

	return nodeClaims, nil
}

func (c *WorkspaceReconciler) createNewNodes(ctx context.Context, wObj *kaitov1beta1.Workspace, newNodesCount int) error {
	// Create all node claims at once
	nodeOSDiskSize := c.determineNodeOSDiskSize(wObj)
	_, err := c.createAllNodeClaims(ctx, wObj, newNodesCount, nodeOSDiskSize)
	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
			"workspaceResourceStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return updateErr
		}
		return err
	}
	if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionFalse,
		"workspaceResourceCreated", "nodeclaims created successfully"); updateErr != nil {
		klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return updateErr
	}
	return nil
}

// ensureNodePlugins ensures node plugins are installed.
func (c *WorkspaceReconciler) ensureNodePlugins(ctx context.Context, wObj *kaitov1beta1.Workspace, nodeObj *corev1.Node) error {
	timeClock := clock.RealClock{}
	tick := timeClock.NewTicker(consts.NodePluginInstallTimeout)
	defer tick.Stop()

	hasUpdatedLabel := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C():
			return fmt.Errorf("node plugin installation timed out. node %s is not ready", nodeObj.Name)
		default:
			// get fresh node object
			freshNode, err := resources.GetNode(ctx, nodeObj.Name, c.Client)
			if err != nil {
				klog.ErrorS(err, "cannot get node", "node", nodeObj.Name)
				return err
			}

			//Nvidia Plugin
			if found := resources.CheckNvidiaPlugin(ctx, freshNode); found {
				return nil
			}

			if !hasUpdatedLabel {
				err = resources.UpdateNodeWithLabel(ctx, freshNode, resources.LabelKeyNvidia, resources.LabelValueNvidia, c.Client)
				if apierrors.IsNotFound(err) {
					klog.ErrorS(err, "nvidia plugin cannot be installed, node not found", "node", freshNode.Name)
					if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
						"checkNodeClaimStatusFailed", err.Error()); updateErr != nil {
						klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
						return updateErr
					}
					return err
				}
				hasUpdatedLabel = true
			}

			time.Sleep(1 * time.Second)
		}
	}
}

// getPresetName returns the preset name from wObj if available
func getPresetName(wObj *kaitov1beta1.Workspace) string {
	if wObj.Inference != nil && wObj.Inference.Preset != nil {
		return string(wObj.Inference.Preset.Name)
	}
	if wObj.Tuning != nil && wObj.Tuning.Preset != nil {
		return string(wObj.Tuning.Preset.Name)
	}
	return ""
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

	existingSVC := &corev1.Service{}
	err := resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, existingSVC)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		return nil
	}

	isStatefulSet := false
	if presetName := getPresetName(wObj); presetName != "" {
		model := plugin.KaitoModelRegister.MustGet(presetName)
		// Dry-run the inference workload generation to determine if it will be a StatefulSet or not.
		workloadObj, _ := inference.GeneratePresetInference(ctx, wObj, "", model, c.Client)
		_, isStatefulSet = workloadObj.(*appsv1.StatefulSet)
	}

	serviceObj := manifests.GenerateServiceManifest(wObj, serviceType, isStatefulSet)
	if err := resources.CreateResource(ctx, serviceObj, c.Client); err != nil {
		return err
	}

	if isStatefulSet {
		headlessService := manifests.GenerateHeadlessServiceManifest(wObj)
		if err := resources.CreateResource(ctx, headlessService, c.Client); err != nil {
			return err
		}
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
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionFalse,
			"WorkspaceTuningJobStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return updateErr
		}
		return err
	}

	if err := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeTuningJobStatus, metav1.ConditionTrue,
		"WorkspaceTuningJobStatusStarted", "Tuning job has started"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}

	return nil
}

// applyInference applies inference spec.
func (c *WorkspaceReconciler) applyInference(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
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
			model := plugin.KaitoModelRegister.MustGet(presetName)
			inferenceParam := model.GetInferenceParameters()
			revisionStr := wObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation]

			// Generate the inference workload (including adapters and their associated
			// volumes) ahead of time. This is important to ensure we are modifying the
			// correct type of workload (Deployment or StatefulSet) based on the model's
			// inference parameters.
			var workloadObj client.Object
			workloadObj, err = inference.GeneratePresetInference(ctx, wObj, revisionStr, model, c.Client)
			if err != nil {
				return
			}

			// Assign the correct type to existingObj based on the type of workloadObj.
			var existingObj client.Object
			var desiredPodSpec *corev1.PodSpec
			switch workloadObj := workloadObj.(type) {
			case *appsv1.StatefulSet:
				existingObj = &appsv1.StatefulSet{}
				desiredPodSpec = &workloadObj.Spec.Template.Spec
			case *appsv1.Deployment:
				existingObj = &appsv1.Deployment{}
				desiredPodSpec = &workloadObj.Spec.Template.Spec
			}

			if err = resources.GetResource(ctx, wObj.Name, wObj.Namespace, c.Client, existingObj); err == nil {
				klog.InfoS("An inference workload already exists for workspace", "workspace", klog.KObj(wObj))
				annotations := existingObj.GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}

				currentRevisionStr, ok := annotations[kaitov1beta1.WorkspaceRevisionAnnotation]
				// If the current workload revision matches the one in Workspace, we do not need to update it.
				if ok && currentRevisionStr == revisionStr {
					return
				}

				var spec *corev1.PodSpec
				switch existingObj := existingObj.(type) {
				case *appsv1.StatefulSet:
					spec = &existingObj.Spec.Template.Spec
				case *appsv1.Deployment:
					spec = &existingObj.Spec.Template.Spec
				}

				// Selectively update the pod spec fields that are relevant to inference,
				// and leave the rest unchanged in case user has customized them.
				spec.Containers[0].Env = desiredPodSpec.Containers[0].Env
				spec.Containers[0].VolumeMounts = desiredPodSpec.Containers[0].VolumeMounts
				spec.InitContainers = desiredPodSpec.InitContainers
				spec.Volumes = desiredPodSpec.Volumes

				annotations[kaitov1beta1.WorkspaceRevisionAnnotation] = revisionStr
				existingObj.SetAnnotations(annotations)

				// Update it with the latest one generated above.
				err = c.Update(ctx, existingObj)
				return
			} else if !apierrors.IsNotFound(err) {
				return
			}

			err = resources.CreateResource(ctx, workloadObj, c.Client)
			if client.IgnoreAlreadyExists(err) != nil {
				return
			}
			if err = resources.CheckResourceStatus(workloadObj, c.Client, inferenceParam.ReadinessTimeout); err != nil {
				return
			}
		}
	}()

	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionFalse,
			"WorkspaceInferenceStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update workspace status", "workspace", klog.KObj(wObj))
			return updateErr
		} else {
			return err
		}
	}

	if err := c.updateStatusConditionIfNotMatch(ctx, wObj, kaitov1beta1.WorkspaceConditionTypeInferenceStatus, metav1.ConditionTrue,
		"WorkspaceInferenceStatusSuccess", "Inference has been deployed successfully"); err != nil {
		klog.ErrorS(err, "failed to update workspace status", "workspace", klog.KObj(wObj))
		return err
	}
	return nil
}

// updateWorkspaceWithRetry gets the latest workspace object, applies the modify function, and retries on conflict
func updateWorkspaceWithRetry(ctx context.Context, c client.Client, wObj *kaitov1beta1.Workspace, modifyFn func(*kaitov1beta1.Workspace) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestWorkspace := &kaitov1beta1.Workspace{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(wObj), latestWorkspace); err != nil {
			return err
		}
		if err := modifyFn(latestWorkspace); err != nil {
			return err
		}
		return c.Update(ctx, latestWorkspace)
	})
}

// ensureGatewayAPIInferenceExtension reconciles Gateway API Inference Extension components for a Workspace.
//
// How it works:
// 1) Dry-runs preset inference generation to determine if the target workload is a StatefulSet.
// 2) Renders a Flux OCIRepository and a HelmRelease for the InferencePool chart.
// 3) Creates the resources if absent; updates them if the desired spec differs.
// 4) Waits for resources to become ready using the model's inference readiness timeout.
// 5) Aggregates and returns any errors.
//
// Idempotent and safe to call on every reconcile; no-op if preconditions are not met.
func (c *WorkspaceReconciler) ensureGatewayAPIInferenceExtension(ctx context.Context, wObj *kaitov1beta1.Workspace) error {
	runtimeName := kaitov1beta1.GetWorkspaceRuntimeName(wObj)
	isPresetInference := wObj.Inference != nil && wObj.Inference.Preset != nil

	// Gateway API Inference Extension is specifically designed to work with vLLM and preset-based inference workloads.
	if !featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] ||
		runtimeName != pkgmodel.RuntimeNameVLLM || !isPresetInference {
		return nil
	}

	model := plugin.KaitoModelRegister.MustGet(string(wObj.Inference.Preset.Name))

	// Dry-run the inference workload generation to determine if it will be a StatefulSet or not.
	workloadObj, _ := inference.GeneratePresetInference(ctx, wObj, "", model, c.Client)
	_, isStatefulSet := workloadObj.(*appsv1.StatefulSet)

	ociRepository := manifests.GenerateInferencePoolOCIRepository(wObj)
	helmRelease, err := manifests.GenerateInferencePoolHelmRelease(wObj, isStatefulSet)
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

// SetupWithManager sets up the controller with the Manager.
func (c *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("Workspace")

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1beta1.Workspace{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.ControllerRevision{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{}).
		Watches(&karpenterv1.NodeClaim{},
			&nodeClaimEventHandler{
				logger:         c.klogger,
				expectations:   c.expectations,
				enqueueHandler: enqueueWorkspaceForNodeClaim,
			},
			builder.WithPredicates(nodeclaim.NodeClaimPredicate),
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
			gaiev1alpha2.SchemeGroupVersion.WithKind("InferencePool"),
			gaiev1alpha2.SchemeGroupVersion.WithKind("InferenceModel"),
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

	go monitorWorkspaces(context.Background(), c.Client)

	return builder.Complete(c)
}
