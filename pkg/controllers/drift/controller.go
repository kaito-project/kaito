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

package drift

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (
	// driftActiveRequeueInterval is used only when drift is actively in progress
	// (budget "1") and we're waiting for Karpenter to complete node replacement.
	driftActiveRequeueInterval = 30 * time.Second
)

// DriftReconciler orchestrates rolling drift upgrades for InferenceSet-managed Workspaces.
type DriftReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	Provisioner nodeprovision.NodeProvisioner
}

// NewDriftReconciler creates a DriftReconciler.
func NewDriftReconciler(c client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, provisioner nodeprovision.NodeProvisioner) *DriftReconciler {
	return &DriftReconciler{
		Client:      c,
		Scheme:      scheme,
		Recorder:    recorder,
		Provisioner: provisioner,
	}
}

// getDriftBudgetNodes extracts the Drifted budget Nodes value from an already-fetched NodePool.
// Returns an error if no budget entry with DisruptionReasonDrifted is found.
func getDriftBudgetNodes(np *karpenterv1.NodePool) (string, error) {
	for _, budget := range np.Spec.Disruption.Budgets {
		for _, reason := range budget.Reasons {
			if reason == karpenterv1.DisruptionReasonDrifted {
				return budget.Nodes, nil
			}
		}
	}
	return "", fmt.Errorf("NodePool %q has no budget entry with Drifted reason", np.Name)
}

// hasDriftedNodeClaimsInGroup checks whether any NodeClaim in the slice has the Drifted condition.
func hasDriftedNodeClaimsInGroup(nodeClaims []*karpenterv1.NodeClaim) bool {
	for _, nc := range nodeClaims {
		for _, condition := range nc.Status.Conditions {
			if condition.Type == karpenterv1.ConditionTypeDrifted && condition.Status == metav1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

// Reconcile implements the drift upgrade state machine for a single InferenceSet.
//
//  1. Get InferenceSet
//  2. List NodePools by InferenceSet labels
//  3. List NodeClaims by InferenceSet labels, group by NodePool name
//  4. For each NodePool, check drift budget and apply state machine
func (r *DriftReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// 1. Get InferenceSet.
	inferenceSet := &kaitov1alpha1.InferenceSet{}
	if err := r.Get(ctx, req.NamespacedName, inferenceSet); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. List NodePools for this InferenceSet.
	nodePoolList := &karpenterv1.NodePoolList{}
	if err := r.List(ctx, nodePoolList,
		client.MatchingLabels{
			consts.KarpenterInferenceSetKey:          inferenceSet.Name,
			consts.KarpenterInferenceSetNamespaceKey: inferenceSet.Namespace,
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing NodePools for InferenceSet %s/%s: %w",
			inferenceSet.Namespace, inferenceSet.Name, err)
	}
	if len(nodePoolList.Items) == 0 {
		return ctrl.Result{}, nil
	}

	// 3. List all NodeClaims for this InferenceSet and group by NodePool name.
	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaimList,
		client.MatchingLabels{
			consts.KarpenterInferenceSetKey:          inferenceSet.Name,
			consts.KarpenterInferenceSetNamespaceKey: inferenceSet.Namespace,
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing NodeClaims for InferenceSet %s/%s: %w",
			inferenceSet.Namespace, inferenceSet.Name, err)
	}

	// Group NodeClaims by their NodePool name label.
	nodeClaimsByPool := make(map[string][]*karpenterv1.NodeClaim)
	for i := range nodeClaimList.Items {
		nc := &nodeClaimList.Items[i]
		poolName := nc.Labels[karpenterv1.NodePoolLabelKey]
		if poolName != "" {
			nodeClaimsByPool[poolName] = append(nodeClaimsByPool[poolName], nc)
		}
	}

	// 4. State machine: find upgrading NodePool or next candidate.
	type nodePoolInfo struct {
		nodePoolName       string
		workspaceName      string
		workspaceNamespace string
	}

	var upgrading *nodePoolInfo
	var nextCandidate *nodePoolInfo

	for i := range nodePoolList.Items {
		np := &nodePoolList.Items[i]

		budgetNodes, err := getDriftBudgetNodes(np)
		if err != nil {
			klog.V(2).InfoS("NodePool has no Drifted budget, skipping",
				"nodePool", np.Name, "error", err)
			continue
		}

		wsName := np.Labels[consts.KarpenterWorkspaceNameKey]
		wsNamespace := np.Labels[consts.KarpenterWorkspaceNamespaceKey]

		if budgetNodes == "1" {
			upgrading = &nodePoolInfo{
				nodePoolName:       np.Name,
				workspaceName:      wsName,
				workspaceNamespace: wsNamespace,
			}
			break
		}

		// Check if this NodePool has drifted NodeClaims.
		if nextCandidate == nil {
			if hasDriftedNodeClaimsInGroup(nodeClaimsByPool[np.Name]) {
				nextCandidate = &nodePoolInfo{
					nodePoolName:       np.Name,
					workspaceName:      wsName,
					workspaceNamespace: wsNamespace,
				}
			}
		}
	}

	// Case A: One NodePool is upgrading (budget "1").
	if upgrading != nil {
		if hasDriftedNodeClaimsInGroup(nodeClaimsByPool[upgrading.nodePoolName]) {
			klog.V(2).InfoS("Workspace still has drifted NodeClaims, waiting",
				"workspace", klog.KRef(upgrading.workspaceNamespace, upgrading.workspaceName),
				"nodePool", upgrading.nodePoolName)
			return ctrl.Result{}, nil
		}

		// No drifted NodeClaims — check workspace readiness.
		ws := &kaitov1beta1.Workspace{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: upgrading.workspaceNamespace,
			Name:      upgrading.workspaceName,
		}, ws); err != nil {
			return ctrl.Result{}, fmt.Errorf("getting workspace %s/%s: %w",
				upgrading.workspaceNamespace, upgrading.workspaceName, err)
		}
		if !isWorkspaceReady(ws) {
			klog.V(2).InfoS("Workspace workload not yet ready after drift replacement, waiting",
				"workspace", klog.KRef(upgrading.workspaceNamespace, upgrading.workspaceName))
			return ctrl.Result{}, nil
		}

		// Workload ready — disable drift remediation.
		if err := r.Provisioner.DisableDriftRemediation(ctx, upgrading.workspaceNamespace, upgrading.workspaceName); err != nil {
			return ctrl.Result{}, fmt.Errorf("disabling drift remediation for workspace %s/%s: %w",
				upgrading.workspaceNamespace, upgrading.workspaceName, err)
		}
		klog.V(2).InfoS("Drift replacement complete, disabled drift remediation",
			"workspace", klog.KRef(upgrading.workspaceNamespace, upgrading.workspaceName),
			"nodePool", upgrading.nodePoolName)
		r.Recorder.Eventf(inferenceSet, "Normal", "DriftComplete",
			"Drift replacement complete for workspace %s/%s",
			upgrading.workspaceNamespace, upgrading.workspaceName)
		// Requeue to check if more NodePools need upgrading (no event will fire for
		// already-drifted NodeClaims sitting stable in other pools).
		return ctrl.Result{RequeueAfter: driftActiveRequeueInterval}, nil
	}

	// Case B: No NodePool is upgrading. Find next candidate.
	if nextCandidate == nil {
		return ctrl.Result{}, nil
	}

	// Enable drift remediation on the next candidate.
	if err := r.Provisioner.EnableDriftRemediation(ctx, nextCandidate.workspaceNamespace, nextCandidate.workspaceName); err != nil {
		return ctrl.Result{}, fmt.Errorf("enabling drift remediation for workspace %s/%s: %w",
			nextCandidate.workspaceNamespace, nextCandidate.workspaceName, err)
	}
	klog.V(2).InfoS("Enabled drift remediation",
		"workspace", klog.KRef(nextCandidate.workspaceNamespace, nextCandidate.workspaceName),
		"nodePool", nextCandidate.nodePoolName)
	r.Recorder.Eventf(inferenceSet, "Normal", "DriftStarted",
		"Started drift replacement for workspace %s/%s",
		nextCandidate.workspaceNamespace, nextCandidate.workspaceName)
	return ctrl.Result{}, nil
}

// isWorkspaceReady returns true if the workspace has WorkspaceSucceeded=True.
func isWorkspaceReady(ws *kaitov1beta1.Workspace) bool {
	for _, c := range ws.Status.Conditions {
		if c.Type == string(kaitov1beta1.WorkspaceConditionTypeSucceeded) {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// inferenceSetNodeClaimPredicate filters to only NodeClaims with both the
// InferenceSet name and namespace labels.
func inferenceSetNodeClaimPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		labels := obj.GetLabels()
		name, hasName := labels[consts.KarpenterInferenceSetKey]
		namespace, hasNamespace := labels[consts.KarpenterInferenceSetNamespaceKey]
		return hasName && hasNamespace && name != "" && namespace != ""
	})
}

// mapNodeClaimToInferenceSet extracts InferenceSet name/namespace from
// NodeClaim labels and returns a reconcile request.
func mapNodeClaimToInferenceSet(_ context.Context, o client.Object) []reconcile.Request {
	labels := o.GetLabels()
	name := labels[consts.KarpenterInferenceSetKey]
	ns := labels[consts.KarpenterInferenceSetNamespaceKey]
	if name == "" || ns == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: name, Namespace: ns},
	}}
}

// enqueueInferenceSetForNodeClaim maps NodeClaim events to the owning InferenceSet.
var enqueueInferenceSetForNodeClaim = handler.EnqueueRequestsFromMapFunc(mapNodeClaimToInferenceSet)

// inferenceSetWorkspacePredicate filters to only Workspaces created by an InferenceSet.
func inferenceSetWorkspacePredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		labels := obj.GetLabels()
		val, has := labels[consts.WorkspaceCreatedByInferenceSetLabel]
		return has && val != ""
	})
}

// mapWorkspaceToInferenceSet extracts InferenceSet name from the workspace's
// created-by label and returns a reconcile request using the workspace's namespace.
func mapWorkspaceToInferenceSet(_ context.Context, o client.Object) []reconcile.Request {
	labels := o.GetLabels()
	name := labels[consts.WorkspaceCreatedByInferenceSetLabel]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: name, Namespace: o.GetNamespace()},
	}}
}

// enqueueInferenceSetForWorkspace maps Workspace events to the owning InferenceSet.
var enqueueInferenceSetForWorkspace = handler.EnqueueRequestsFromMapFunc(mapWorkspaceToInferenceSet)

// SetupWithManager registers the controller with the manager.
func (r *DriftReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("drift").
		For(&kaitov1alpha1.InferenceSet{}).
		Watches(&karpenterv1.NodeClaim{},
			enqueueInferenceSetForNodeClaim,
			builder.WithPredicates(inferenceSetNodeClaimPredicate()),
		).
		Watches(&kaitov1beta1.Workspace{},
			enqueueInferenceSetForWorkspace,
			builder.WithPredicates(inferenceSetWorkspacePredicate()),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Complete(r)
}
