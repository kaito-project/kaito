// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/machine"
	"github.com/kaito-project/kaito/pkg/nodeclaim"
	"github.com/kaito-project/kaito/pkg/resources"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
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

// applyRAGEngineResource applies RAGEngine resource spec.
func (c *RAGEngineReconciler) applyRAGEngineResource(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine) error {

	// Wait for pending machines if any before we decide whether to create new machine or not.
	if err := machine.WaitForPendingMachines(ctx, ragEngineObj, c.Client); err != nil {
		return err
	}

	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		// Wait for pending nodeClaims if any before we decide whether to create new node or not.
		if err := nodeclaim.WaitForPendingNodeClaims(ctx, ragEngineObj, c.Client); err != nil {
			return err
		}
	}

	// Find all nodes that match the labelSelector and instanceType, they are not necessarily created by machines/nodeClaims.
	validNodes, err := c.getAllQualifiedNodes(ctx, ragEngineObj)
	if err != nil {
		return err
	}

	selectedNodes := selectNodes(validNodes, ragEngineObj.Spec.Compute.PreferredNodes, ragEngineObj.Status.WorkerNodes, lo.FromPtr(ragEngineObj.Spec.Compute.Count))

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
	if strings.Contains(ragEngineObj.Spec.Compute.InstanceType, gpuSkuPrefix) { // GPU skus
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
	tick := timeClock.NewTicker(nodePluginInstallTimeout)
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
		Watches(&v1alpha5.Machine{}, c.watchMachines()).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})
	if featuregates.FeatureGates[consts.FeatureFlagKarpenter] {
		builder.
			Watches(&v1beta1.NodeClaim{}, c.watchNodeClaims()) // watches for nodeClaim with labels indicating ragengine name.
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
