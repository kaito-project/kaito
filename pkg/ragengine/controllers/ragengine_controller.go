// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/go-logr/logr"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/ragengine/manifests"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/machine"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"knative.dev/pkg/apis"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
)

const (
	RAGEngineHashAnnotation = "ragengine.kaito.io/hash"
	RAGEngineNameLabel      = "ragengine.kaito.io/name"
	revisionHashSuffix      = 5
)

type RAGEngineReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func NewRAGEngineReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, Recorder record.EventRecorder) *RAGEngineReconciler {
	return &RAGEngineReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      log,
		Recorder: Recorder,
	}
}

func (c *RAGEngineReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ragEngineObj := &kaitov1alpha1.RAGEngine{}
	if err := c.Client.Get(ctx, req.NamespacedName, ragEngineObj); err != nil {
		if !errors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get RAG Engine", "RAG Engine", req.Name)
		}
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	klog.InfoS("Reconciling", "RAG Engine", req.NamespacedName)

	if err := c.ensureFinalizer(ctx, ragEngineObj); err != nil {
		return reconcile.Result{}, err
	}

	// Handle deleting ragengine, garbage collect all the resources.
	if !ragEngineObj.DeletionTimestamp.IsZero() {
		return c.deleteRAGEngine(ctx, ragEngineObj)
	}

	if err := c.syncControllerRevision(ctx, ragEngineObj); err != nil {
		return reconcile.Result{}, err
	}

	result, err := c.addRAGEngine(ctx, ragEngineObj)
	if err != nil {
		return result, err
	}

	return result, nil
}

func (c *RAGEngineReconciler) ensureFinalizer(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) error {
	if !controllerutil.ContainsFinalizer(ragEngineObj, consts.RAGEngineFinalizer) {
		patch := client.MergeFrom(ragEngineObj.DeepCopy())
		controllerutil.AddFinalizer(ragEngineObj, consts.RAGEngineFinalizer)
		if err := c.Client.Patch(ctx, ragEngineObj, patch); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer to the ragengine", "ragengine", klog.KObj(ragEngineObj))
			return err
		}
	}
	return nil
}

func (c *RAGEngineReconciler) addRAGEngine(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) (reconcile.Result, error) {
	err := c.applyRAGEngineResource(ctx, ragEngineObj)
	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGEngineConditionTypeSucceeded, metav1.ConditionFalse,
			"ragengineFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return reconcile.Result{}, updateErr
		}
		// if error is	due to machine/nodeClaim instance types unavailability, stop reconcile.
		if err.Error() == consts.ErrorInstanceTypesUnavailable {
			return reconcile.Result{Requeue: false}, err
		}
		return reconcile.Result{}, err
	}
	if err := c.ensureService(ctx, ragEngineObj); err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGEngineConditionTypeSucceeded, metav1.ConditionFalse,
			"ragEngineFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragEngine status", "ragEngine", klog.KObj(ragEngineObj))
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, err
	}
	if err = c.applyRAG(ctx, ragEngineObj); err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGEngineConditionTypeSucceeded, metav1.ConditionFalse,
			"ragengineFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return reconcile.Result{}, updateErr
		}
		return reconcile.Result{}, err
	}

	if err = c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGEngineConditionTypeSucceeded, metav1.ConditionTrue,
		"ragengineSucceeded", "ragengine succeeds"); err != nil {
		klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (c *RAGEngineReconciler) ensureService(ctx context.Context, ragObj *kaitov1alpha1.RAGEngine) error {
	serviceType := corev1.ServiceTypeClusterIP
	ragAnnotations := ragObj.GetAnnotations()

	if len(ragAnnotations) != 0 {
		val, found := ragAnnotations[kaitov1alpha1.AnnotationEnableLB]
		if found && val == "True" {
			serviceType = corev1.ServiceTypeLoadBalancer
		}
	}

	// Ensure Service for index and query
	// TODO: ServiceName currently does not accept customization for now

	serviceName := ragObj.Name

	existingSVC := &corev1.Service{}
	err := resources.GetResource(ctx, serviceName, ragObj.Namespace, c.Client, existingSVC)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		return nil
	}
	serviceObj := manifests.GenerateRAGServiceManifest(ctx, ragObj, serviceName, serviceType)
	if err := resources.CreateResource(ctx, serviceObj, c.Client); err != nil {
		return err
	}

	return nil
}

func (c *RAGEngineReconciler) applyRAG(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) error {
	var err error
	func() {

		deployment := &appsv1.Deployment{}
		revisionStr := ragEngineObj.Annotations[kaitov1alpha1.RAGEngineRevisionAnnotation]

		if err = resources.GetResource(ctx, ragEngineObj.Name, ragEngineObj.Namespace, c.Client, deployment); err == nil {
			klog.InfoS("An inference workload already exists for ragengine", "ragengine", klog.KObj(ragEngineObj))
			if deployment.Annotations[kaitov1alpha1.RAGEngineRevisionAnnotation] != revisionStr {

				envs := manifests.RAGSetEnv(ragEngineObj)

				spec := &deployment.Spec
				// Currently, all CRD changes are only passed through environment variables (env)
				spec.Template.Spec.Containers[0].Env = envs
				deployment.Annotations[kaitov1alpha1.RAGEngineRevisionAnnotation] = revisionStr

				if err := c.Update(ctx, deployment); err != nil {
					return
				}
			}
			if err = resources.CheckResourceStatus(deployment, c.Client, time.Duration(10)*time.Minute); err != nil {
				return
			}
		} else if apierrors.IsNotFound(err) {
			var workloadObj client.Object
			// Need to create a new workload
			workloadObj, err = CreatePresetRAG(ctx, ragEngineObj, revisionStr, c.Client)
			if err != nil {
				return
			}
			if err = resources.CheckResourceStatus(workloadObj, c.Client, time.Duration(10)*time.Minute); err != nil {
				return
			}
		}

	}()

	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGConditionTypeServiceStatus, metav1.ConditionFalse,
			"RAGEngineServiceStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return updateErr
		} else {
			return err
		}
	}

	if err := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGEneineConditionTypeServiceStatus, metav1.ConditionTrue,
		"RAGEngineServiceSuccess", "Inference has been deployed successfully"); err != nil {
		klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
		return err
	}

	return nil
}

func (c *RAGEngineReconciler) deleteRAGEngine(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) (reconcile.Result, error) {
	klog.InfoS("deleteRAGEngine", "ragengine", klog.KObj(ragEngineObj))
	err := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.RAGEngineConditionTypeDeleting, metav1.ConditionTrue, "ragengineDeleted", "ragengine is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
		return reconcile.Result{}, err
	}

	return c.garbageCollectRAGEngine(ctx, ragEngineObj)
}

func (c *RAGEngineReconciler) syncControllerRevision(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) error {
	currentHash := computeHash(ragEngineObj)

	annotations := ragEngineObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	} // nil checking.

	revisionNum := int64(1)

	revisions := &appsv1.ControllerRevisionList{}
	if err := c.List(ctx, revisions, client.InNamespace(ragEngineObj.Namespace), client.MatchingLabels{RAGEngineNameLabel: ragEngineObj.Name}); err != nil {
		return fmt.Errorf("failed to list revisions: %w", err)
	}
	sort.Slice(revisions.Items, func(i, j int) bool {
		return revisions.Items[i].Revision < revisions.Items[j].Revision
	})

	var latestRevision *appsv1.ControllerRevision

	jsonData, err := json.Marshal(ragEngineObj.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal selected fields: %w", err)
	}

	if len(revisions.Items) > 0 {
		latestRevision = &revisions.Items[len(revisions.Items)-1]

		revisionNum = latestRevision.Revision + 1
	}
	newRevision := &appsv1.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", ragEngineObj.Name, currentHash[:revisionHashSuffix]),
			Namespace: ragEngineObj.Namespace,
			Annotations: map[string]string{
				RAGEngineHashAnnotation: currentHash,
			},
			Labels: map[string]string{
				RAGEngineNameLabel: ragEngineObj.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ragEngineObj, kaitov1alpha1.GroupVersion.WithKind("RAGEngine")),
			},
		},
		Revision: revisionNum,
		Data:     runtime.RawExtension{Raw: jsonData},
	}

	annotations[RAGEngineHashAnnotation] = currentHash
	ragEngineObj.SetAnnotations(annotations)
	controllerRevision := &appsv1.ControllerRevision{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      newRevision.Name,
		Namespace: newRevision.Namespace,
	}, controllerRevision); err != nil {
		if errors.IsNotFound(err) {

			if err := c.Create(ctx, newRevision); err != nil {
				return fmt.Errorf("failed to create new ControllerRevision: %w", err)
			} else {
				annotations[kaitov1alpha1.RAGEngineRevisionAnnotation] = strconv.FormatInt(revisionNum, 10)
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
		if controllerRevision.Annotations[RAGEngineHashAnnotation] != newRevision.Annotations[RAGEngineHashAnnotation] {
			return fmt.Errorf("revision name conflicts, the hash values are different")
		}
		annotations[kaitov1alpha1.RAGEngineRevisionAnnotation] = strconv.FormatInt(controllerRevision.Revision, 10)
	}
	annotations[RAGEngineHashAnnotation] = currentHash
	ragEngineObj.SetAnnotations(annotations)

	if err := c.Update(ctx, ragEngineObj); err != nil {
		return fmt.Errorf("failed to update RAGEngine annotations: %w", err)
	}
	return nil
}

func computeHash(ragEngineObj *kaitov1alpha1.RAGEngine) string {
	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	encoder.Encode(ragEngineObj.Spec)
	return hex.EncodeToString(hasher.Sum(nil))
}

// applyRAGEngineResource applies RAGEngine resource spec.
func (c *RAGEngineReconciler) applyRAGEngineResource(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) error {
	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		// Wait for pending nodeClaims if any before we decide whether to create new node or not.
		if err := nodeclaim.WaitForPendingNodeClaims(ctx, ragEngineObj, c.Client); err != nil {
			return err
		}
	} else {
		// Wait for pending machines if any before we decide whether to create new machine or not.
		if err := machine.WaitForPendingMachines(ctx, ragEngineObj, c.Client); err != nil {
			return err
		}
	}

	// Find all nodes that match the labelSelector and instanceType, they are not necessarily created by machines/nodeClaims.
	validNodes, err := c.getAllQualifiedNodes(ctx, ragEngineObj)
	if err != nil {
		return err
	}

	selectedNodes := utils.SelectNodes(validNodes, ragEngineObj.Spec.Compute.PreferredNodes, ragEngineObj.Status.WorkerNodes, lo.FromPtr(ragEngineObj.Spec.Compute.Count))
	newNodesCount := lo.FromPtr(ragEngineObj.Spec.Compute.Count) - len(selectedNodes)

	if newNodesCount > 0 {
		klog.InfoS("need to create more nodes", "NodeCount", newNodesCount)
		if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
			if err := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj,
				kaitov1alpha1.ConditionTypeNodeClaimStatus, metav1.ConditionUnknown,
				"CreateNodeClaimPending", fmt.Sprintf("creating %d nodeClaims", newNodesCount)); err != nil {
				klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
				return err
			}
		} else if err := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj,
			kaitov1alpha1.ConditionTypeMachineStatus, metav1.ConditionUnknown,
			"CreateMachinePending", fmt.Sprintf("creating %d machines", newNodesCount)); err != nil {
			klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return err
		}

		for i := 0; i < newNodesCount; i++ {
			newNode, err := c.createAndValidateNode(ctx, ragEngineObj)
			if err != nil {
				if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionFalse,
					"ragengineResourceStatusFailed", err.Error()); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
					return updateErr
				}
				return err
			}
			selectedNodes = append(selectedNodes, newNode)
		}
	}

	// Ensure all gpu plugins are running successfully.
	if strings.Contains(ragEngineObj.Spec.Compute.InstanceType, consts.GpuSkuPrefix) { // GPU skus
		for i := range selectedNodes {
			err = c.ensureNodePlugins(ctx, ragEngineObj, selectedNodes[i])
			if err != nil {
				if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionFalse,
					"ragengineResourceStatusFailed", err.Error()); updateErr != nil {
					klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
					return updateErr
				}
				return err
			}
		}
	}

	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		if err = c.updateStatusConditionIfNotMatch(ctx, ragEngineObj,
			kaitov1alpha1.ConditionTypeNodeClaimStatus, metav1.ConditionTrue,
			"installNodePluginsSuccess", "nodeClaim plugins have been installed successfully"); err != nil {
			klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return err
		}
	} else if err = c.updateStatusConditionIfNotMatch(ctx, ragEngineObj,
		kaitov1alpha1.ConditionTypeMachineStatus, metav1.ConditionTrue,
		"installNodePluginsSuccess", "machines plugins have been installed successfully"); err != nil {
		klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
		return err
	}

	// Add the valid nodes names to the RAGEngineStatus.WorkerNodes.
	err = c.updateStatusNodeListIfNotMatch(ctx, ragEngineObj, selectedNodes)
	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionFalse,
			"ragengineResourceStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return updateErr
		}
		return err
	}

	if err = c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue,
		"ragengineResourceStatusSuccess", "ragengine resource is ready"); err != nil {
		klog.ErrorS(err, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
		return err
	}

	return nil
}

// getAllQualifiedNodes returns all nodes that match the labelSelector and instanceType.
func (c *RAGEngineReconciler) getAllQualifiedNodes(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) ([]*corev1.Node, error) {
	var qualifiedNodes []*corev1.Node

	nodeList, err := resources.ListNodes(ctx, c.Client, ragEngineObj.Spec.Compute.LabelSelector.MatchLabels)
	if err != nil {
		return nil, err
	}

	if len(nodeList.Items) == 0 {
		klog.InfoS("no current nodes match the ragengine resource spec", "ragengine", klog.KObj(ragEngineObj))
		return nil, nil
	}

	preferredNodeSet := sets.New(ragEngineObj.Spec.Compute.PreferredNodes...)

	for index := range nodeList.Items {
		nodeObj := nodeList.Items[index]
		// skip nodes that are being deleted
		if nodeObj.DeletionTimestamp != nil {
			continue
		}

		// skip nodes that are not ready
		_, statusRunning := lo.Find(nodeObj.Status.Conditions, func(condition corev1.NodeCondition) bool {
			return condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue
		})
		if !statusRunning {
			continue
		}

		// match the preferred node
		if preferredNodeSet.Has(nodeObj.Name) {
			qualifiedNodes = append(qualifiedNodes, lo.ToPtr(nodeObj))
			continue
		}

		// match the instanceType
		if nodeObj.Labels[corev1.LabelInstanceTypeStable] == ragEngineObj.Spec.Compute.InstanceType {
			qualifiedNodes = append(qualifiedNodes, lo.ToPtr(nodeObj))
		}
	}

	return qualifiedNodes, nil
}

// createAndValidateNode creates a new node and validates status.
func (c *RAGEngineReconciler) createAndValidateNode(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) (*corev1.Node, error) {
	var nodeOSDiskSize string

	if nodeOSDiskSize == "" {
		nodeOSDiskSize = "0" // The default OS size is used
	}

	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		return c.CreateNodeClaim(ctx, ragEngineObj, nodeOSDiskSize)
	} else {
		return c.CreateMachine(ctx, ragEngineObj, nodeOSDiskSize)
	}
}

func (c *RAGEngineReconciler) CreateMachine(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine, nodeOSDiskSize string) (*corev1.Node, error) {
RetryWithDifferentName:
	newMachine := machine.GenerateMachineManifest(ctx, nodeOSDiskSize, ragEngineObj)

	if err := machine.CreateMachine(ctx, newMachine, c.Client); err != nil {
		if apierrors.IsAlreadyExists(err) {
			klog.InfoS("A machine exists with the same name, retry with a different name", "machine", klog.KObj(newMachine))
			goto RetryWithDifferentName
		} else {

			klog.ErrorS(err, "failed to create machine", "machine", newMachine.Name)
			if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeMachineStatus, metav1.ConditionFalse,
				"machineFailedCreation", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
				return nil, updateErr
			}
			return nil, err
		}
	}

	// check machine status until it is ready
	err := machine.CheckMachineStatus(ctx, newMachine, c.Client)
	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeMachineStatus, metav1.ConditionFalse,
			"checkMachineStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return nil, updateErr
		}
		return nil, err
	}

	// get the node object from the machine status nodeName.
	return resources.GetNode(ctx, newMachine.Status.NodeName, c.Client)
}

func (c *RAGEngineReconciler) CreateNodeClaim(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine, nodeOSDiskSize string) (*corev1.Node, error) {
RetryWithDifferentName:
	newNodeClaim := nodeclaim.GenerateNodeClaimManifest(ctx, nodeOSDiskSize, ragEngineObj)

	if err := nodeclaim.CreateNodeClaim(ctx, newNodeClaim, c.Client); err != nil {
		if apierrors.IsAlreadyExists(err) {
			klog.InfoS("There exists a nodeClaim with the same name, retry with a different name", "nodeClaim", klog.KObj(newNodeClaim))
			goto RetryWithDifferentName
		} else {

			klog.ErrorS(err, "failed to create nodeClaim", "nodeClaim", newNodeClaim.Name)
			if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
				"nodeClaimFailedCreation", err.Error()); updateErr != nil {
				klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
				return nil, updateErr
			}
			return nil, err
		}
	}

	// check nodeClaim status until it is ready
	err := nodeclaim.CheckNodeClaimStatus(ctx, newNodeClaim, c.Client)
	if err != nil {
		if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
			"checkNodeClaimStatusFailed", err.Error()); updateErr != nil {
			klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
			return nil, updateErr
		}
		return nil, err
	}

	// get the node object from the nodeClaim status nodeName.
	return resources.GetNode(ctx, newNodeClaim.Status.NodeName, c.Client)
}

// ensureNodePlugins ensures node plugins are installed.
func (c *RAGEngineReconciler) ensureNodePlugins(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine, nodeObj *corev1.Node) error {
	timeClock := clock.RealClock{}
	tick := timeClock.NewTicker(consts.NodePluginInstallTimeout)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C():
			return fmt.Errorf("node plugin installation timed out. node %s is not ready", nodeObj.Name)
		default:
			//Nvidia Plugin
			if found := resources.CheckNvidiaPlugin(ctx, nodeObj); !found {
				if err := resources.UpdateNodeWithLabel(ctx, nodeObj.Name, resources.LabelKeyNvidia, resources.LabelValueNvidia, c.Client); err != nil {
					if apierrors.IsNotFound(err) {
						klog.ErrorS(err, "nvidia plugin cannot be installed, node not found", "node", nodeObj.Name)
						if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
							if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeNodeClaimStatus, metav1.ConditionFalse,
								"checkNodeClaimStatusFailed", err.Error()); updateErr != nil {
								klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
								return updateErr
							}
						} else {
							if updateErr := c.updateStatusConditionIfNotMatch(ctx, ragEngineObj, kaitov1alpha1.ConditionTypeMachineStatus, metav1.ConditionFalse,
								"checkMachineStatusFailed", err.Error()); updateErr != nil {
								klog.ErrorS(updateErr, "failed to update ragengine status", "ragengine", klog.KObj(ragEngineObj))
								return updateErr
							}
						}
						return err
					}
				}
				time.Sleep(1 * time.Second)
			}
			return nil
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (c *RAGEngineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("RAGEngine")
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{},
		"spec.nodeName", func(rawObj client.Object) []string {
			pod := rawObj.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		}); err != nil {
		return err
	}
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.RAGEngine{}).
		Owns(&appsv1.ControllerRevision{}).
		Owns(&appsv1.Deployment{}).
		Watches(&v1alpha5.Machine{}, c.watchMachines()).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})
	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		builder.Watches(&v1beta1.NodeClaim{}, c.watchNodeClaims()) // watches for nodeClaim with labels indicating ragengine name.
	} else {
		builder.Watches(&v1alpha5.Machine{}, c.watchMachines())
	}
	return builder.Complete(c)
}

// watches for machine with labels indicating RAGEngine name.
func (c *RAGEngineReconciler) watchMachines() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, o client.Object) []reconcile.Request {
			machineObj := o.(*v1alpha5.Machine)
			name, ok := machineObj.Labels[kaitov1alpha1.LabelRAGEngineName]
			if !ok {
				return nil
			}
			namespace, ok := machineObj.Labels[kaitov1alpha1.LabelRAGEngineNamespace]
			if !ok {
				return nil
			}
			_, conditionFound := lo.Find(machineObj.GetConditions(), func(condition apis.Condition) bool {
				return condition.Type == apis.ConditionReady &&
					condition.Status == v1.ConditionTrue
			})
			if conditionFound && machineObj.DeletionTimestamp.IsZero() {
				// No need to reconcile ragengine if the machine is in READY state unless machine is deleted.
				return nil
			}
			return []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Name:      name,
						Namespace: namespace,
					},
				},
			}
		})
}

// watches for nodeClaim with labels indicating RAGEngine name.
func (c *RAGEngineReconciler) watchNodeClaims() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, o client.Object) []reconcile.Request {
			nodeClaimObj := o.(*v1beta1.NodeClaim)
			name, ok := nodeClaimObj.Labels[kaitov1alpha1.LabelRAGEngineName]
			if !ok {
				return nil
			}
			namespace, ok := nodeClaimObj.Labels[kaitov1alpha1.LabelRAGEngineNamespace]
			if !ok {
				return nil
			}
			return []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Name:      name,
						Namespace: namespace,
					},
				},
			}
		})
}
