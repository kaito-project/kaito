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
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	karpenterpkg "github.com/kaito-project/kaito/pkg/nodeprovision/karpenter"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	inferencesetutil "github.com/kaito-project/kaito/pkg/utils/inferenceset"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

const (
	driftRequeueInterval = 30 * time.Second
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

// Reconcile implements the drift upgrade state machine for a single InferenceSet.
//
// State machine:
//  1. List all Workspaces for this InferenceSet
//  2. For each Workspace, get its NodePool and read the drift budget
//  3. If one has budget "1" (upgrade in progress):
//     - If it still has drifted NodeClaims: requeue (wait)
//     - If no drifted NodeClaims: set budget back to "0", requeue
//  4. If none has budget "1" (no upgrade in progress):
//     - Find next workspace with drifted NodeClaims
//     - Set its budget to "1" (enable drift), requeue
//     - If no drifted workspaces: done (no requeue)
func (r *DriftReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// 1. Get InferenceSet.
	inferenceSet := &kaitov1alpha1.InferenceSet{}
	if err := r.Get(ctx, req.NamespacedName, inferenceSet); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. List Workspaces for this InferenceSet.
	wsList, err := inferencesetutil.ListWorkspaces(ctx, inferenceSet, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing workspaces for InferenceSet %s/%s: %w",
			inferenceSet.Namespace, inferenceSet.Name, err)
	}
	if len(wsList.Items) == 0 {
		return ctrl.Result{}, nil
	}

	// 3. For each Workspace, get its NodePool and check the budget.
	//    Track the workspace with budget "1" (if any) and workspaces with drifted NodeClaims.
	type workspaceDriftInfo struct {
		workspaceNamespace string
		workspaceName      string
		nodePoolName       string
	}

	var upgrading *workspaceDriftInfo
	var driftedCandidates []workspaceDriftInfo

	for i := range wsList.Items {
		ws := &wsList.Items[i]
		nodePoolName := karpenterpkg.NodePoolName(ws.Namespace, ws.Name)

		np := &karpenterv1.NodePool{}
		if err := r.Get(ctx, types.NamespacedName{Name: nodePoolName}, np); err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).InfoS("NodePool not found, skipping workspace",
					"workspace", klog.KRef(ws.Namespace, ws.Name),
					"nodePool", nodePoolName)
				continue
			}
			return ctrl.Result{}, fmt.Errorf("getting NodePool %q: %w", nodePoolName, err)
		}

		budgetNodes, err := getDriftBudgetNodes(np)
		if err != nil {
			klog.V(2).InfoS("NodePool has no Drifted budget, skipping",
				"nodePool", nodePoolName, "error", err)
			continue
		}

		if budgetNodes == "1" {
			upgrading = &workspaceDriftInfo{
				workspaceNamespace: ws.Namespace,
				workspaceName:      ws.Name,
				nodePoolName:       nodePoolName,
			}
			break // Only one should be upgrading at a time
		}

		// Check if this workspace has drifted NodeClaims.
		hasDrifted, err := nodeclaim.HasDriftedNodeClaims(ctx, r.Client, nodePoolName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("checking drifted NodeClaims for NodePool %q: %w", nodePoolName, err)
		}
		if hasDrifted {
			driftedCandidates = append(driftedCandidates, workspaceDriftInfo{
				workspaceNamespace: ws.Namespace,
				workspaceName:      ws.Name,
				nodePoolName:       nodePoolName,
			})
		}
	}

	// 4. Handle the state machine transitions.

	// Case A: One workspace is upgrading (budget "1").
	if upgrading != nil {
		hasDrifted, err := nodeclaim.HasDriftedNodeClaims(ctx, r.Client, upgrading.nodePoolName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("checking drifted NodeClaims for upgrading NodePool %q: %w",
				upgrading.nodePoolName, err)
		}
		if hasDrifted {
			// Still drifted — requeue and wait for karpenter to finish replacement.
			klog.V(2).InfoS("Workspace still has drifted NodeClaims, waiting",
				"workspace", klog.KRef(upgrading.workspaceNamespace, upgrading.workspaceName),
				"nodePool", upgrading.nodePoolName)
			return ctrl.Result{RequeueAfter: driftRequeueInterval}, nil
		}

		// No longer drifted — disable drift remediation (set budget back to "0").
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
		// Requeue to check if more workspaces need upgrading.
		return ctrl.Result{RequeueAfter: driftRequeueInterval}, nil
	}

	// Case B: No workspace is upgrading. Find next candidate.
	if len(driftedCandidates) == 0 {
		// Nothing drifted — done.
		return ctrl.Result{}, nil
	}

	// Enable drift remediation on the first candidate.
	candidate := driftedCandidates[0]
	if err := r.Provisioner.EnableDriftRemediation(ctx, candidate.workspaceNamespace, candidate.workspaceName); err != nil {
		return ctrl.Result{}, fmt.Errorf("enabling drift remediation for workspace %s/%s: %w",
			candidate.workspaceNamespace, candidate.workspaceName, err)
	}
	klog.V(2).InfoS("Enabled drift remediation",
		"workspace", klog.KRef(candidate.workspaceNamespace, candidate.workspaceName),
		"nodePool", candidate.nodePoolName)
	r.Recorder.Eventf(inferenceSet, "Normal", "DriftStarted",
		"Started drift replacement for workspace %s/%s",
		candidate.workspaceNamespace, candidate.workspaceName)
	return ctrl.Result{RequeueAfter: driftRequeueInterval}, nil
}

// inferenceSetNodeClaimPredicate filters to only NodeClaims with the
// InferenceSet label. This prevents the controller from receiving events
// for standalone workspace NodeClaims, RAGEngine NodeClaims, etc.
func inferenceSetNodeClaimPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[consts.KarpenterInferenceSetKey]
		return ok
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

// SetupWithManager registers the controller with the manager.
func (r *DriftReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("drift").
		For(&kaitov1alpha1.InferenceSet{}).
		Watches(&karpenterv1.NodeClaim{},
			enqueueInferenceSetForNodeClaim,
			builder.WithPredicates(inferenceSetNodeClaimPredicate()),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Complete(r)
}
