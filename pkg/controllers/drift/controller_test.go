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
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

// --- Test helpers ---

// mockProvisioner records EnableDriftRemediation/DisableDriftRemediation calls.
type mockProvisioner struct {
	mock.Mock
}

func (m *mockProvisioner) Name() string { return "mock" }
func (m *mockProvisioner) Start(_ context.Context) error {
	return nil
}
func (m *mockProvisioner) ProvisionNodes(_ context.Context, _ *kaitov1beta1.Workspace) error {
	return nil
}
func (m *mockProvisioner) DeleteNodes(_ context.Context, _ *kaitov1beta1.Workspace) error {
	return nil
}
func (m *mockProvisioner) EnsureNodesReady(_ context.Context, _ *kaitov1beta1.Workspace) (bool, bool, error) {
	return false, false, nil
}
func (m *mockProvisioner) CollectNodeStatusInfo(_ context.Context, _ *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	return nil, nil
}
func (m *mockProvisioner) EnableDriftRemediation(ctx context.Context, ns, name string) error {
	args := m.Called(ctx, ns, name)
	return args.Error(0)
}
func (m *mockProvisioner) DisableDriftRemediation(ctx context.Context, ns, name string) error {
	args := m.Called(ctx, ns, name)
	return args.Error(0)
}

func newNodePoolWithDriftBudget(name, nodes, wsName, wsNamespace, infSetName, infSetNamespace string) *karpenterv1.NodePool {
	return &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				consts.KarpenterLabelManagedBy:           consts.KarpenterManagedByValue,
				consts.KarpenterWorkspaceNameKey:         wsName,
				consts.KarpenterWorkspaceNamespaceKey:    wsNamespace,
				consts.KarpenterInferenceSetKey:          infSetName,
				consts.KarpenterInferenceSetNamespaceKey: infSetNamespace,
			},
		},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   nodes,
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonDrifted},
					},
				},
			},
		},
	}
}

func newInferenceSet(ns, name string) *kaitov1alpha1.InferenceSet {
	return &kaitov1alpha1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}

func newWorkspaceForInferenceSet(ns, name, infSetName string) *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: infSetName,
			},
		},
	}
}

func newNodeClaimWithDriftCondition(name, nodePoolName, infSetName, infSetNamespace string, drifted bool) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey:             nodePoolName,
				consts.KarpenterInferenceSetKey:          infSetName,
				consts.KarpenterInferenceSetNamespaceKey: infSetNamespace,
			},
		},
	}
	if drifted {
		nc.Status.Conditions = []status.Condition{
			{Type: karpenterv1.ConditionTypeDrifted, Status: metav1.ConditionTrue},
		}
	} else {
		nc.Status.Conditions = []status.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		}
	}
	return nc
}

// --- getDriftBudgetNodes tests ---

func TestGetDriftBudgetNodes_Found(t *testing.T) {
	np := newNodePoolWithDriftBudget("test-np", "0", "ws", "default", "infset", "default")
	nodes, err := getDriftBudgetNodes(np)
	assert.NilError(t, err)
	assert.Equal(t, "0", nodes)
}

func TestGetDriftBudgetNodes_NotFound(t *testing.T) {
	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-np"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				Budgets: []karpenterv1.Budget{},
			},
		},
	}
	_, err := getDriftBudgetNodes(np)
	assert.Assert(t, err != nil)
}

// --- Predicate tests ---

func TestInferenceSetNodeClaimPredicate_WithLabel(t *testing.T) {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				consts.KarpenterInferenceSetKey:          "my-infset",
				consts.KarpenterInferenceSetNamespaceKey: "default",
			},
		},
	}
	p := inferenceSetNodeClaimPredicate()
	assert.Assert(t, p.Generic(event.GenericEvent{Object: nc}))
}

func TestInferenceSetNodeClaimPredicate_WithoutLabel(t *testing.T) {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
		},
	}
	p := inferenceSetNodeClaimPredicate()
	assert.Assert(t, !p.Generic(event.GenericEvent{Object: nc}))
}

// --- Mapper tests ---

func TestMapNodeClaimToInferenceSet_WithLabels(t *testing.T) {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				consts.KarpenterInferenceSetKey:          "my-infset",
				consts.KarpenterInferenceSetNamespaceKey: "prod",
			},
		},
	}

	reqs := mapNodeClaimToInferenceSet(context.Background(), nc)
	assert.Equal(t, 1, len(reqs))
	assert.Equal(t, "my-infset", reqs[0].Name)
	assert.Equal(t, "prod", reqs[0].Namespace)
}

func TestMapNodeClaimToInferenceSet_WithoutLabels(t *testing.T) {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
		},
	}

	reqs := mapNodeClaimToInferenceSet(context.Background(), nc)
	assert.Assert(t, len(reqs) == 0)
}

func TestMapNodeClaimToInferenceSet_PartialLabels(t *testing.T) {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				consts.KarpenterInferenceSetKey: "my-infset",
			},
		},
	}

	reqs := mapNodeClaimToInferenceSet(context.Background(), nc)
	assert.Assert(t, len(reqs) == 0)
}

// --- Workspace mapper tests ---

func TestMapWorkspaceToInferenceSet_WithLabel(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws-0",
			Namespace: "default",
			Labels: map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: "my-infset",
			},
		},
	}

	reqs := mapWorkspaceToInferenceSet(context.Background(), ws)
	assert.Equal(t, 1, len(reqs))
	assert.Equal(t, "my-infset", reqs[0].Name)
	assert.Equal(t, "default", reqs[0].Namespace)
}

func TestMapWorkspaceToInferenceSet_WithoutLabel(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws-0",
			Namespace: "default",
			Labels:    map[string]string{},
		},
	}

	reqs := mapWorkspaceToInferenceSet(context.Background(), ws)
	assert.Assert(t, len(reqs) == 0)
}

func TestInferenceSetWorkspacePredicate_WithLabel(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: "my-infset",
			},
		},
	}
	p := inferenceSetWorkspacePredicate()
	assert.Assert(t, p.Generic(event.GenericEvent{Object: ws}))
}

func TestInferenceSetWorkspacePredicate_WithoutLabel(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
		},
	}
	p := inferenceSetWorkspacePredicate()
	assert.Assert(t, !p.Generic(event.GenericEvent{Object: ws}))
}

// --- Reconcile state machine tests ---
// Note: mock List does NOT apply label selectors — it returns all objects
// of that type. Tests must be designed with this in mind.

func TestReconcile_InferenceSetNotFound(t *testing.T) {
	mockClient := test.NewClient()
	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})

	notFoundErr := k8serrors.NewNotFound(
		schema.GroupResource{Group: "kaito.sh", Resource: "inferencesets"}, "missing")
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(notFoundErr)

	r := NewDriftReconciler(mockClient, nil, record.NewFakeRecorder(10), &mockProvisioner{})
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "missing", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_NoNodePools(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	mockClient.CreateMapWithType(&karpenterv1.NodePoolList{}) // empty

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)

	r := NewDriftReconciler(mockClient, nil, record.NewFakeRecorder(10), &mockProvisioner{})
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_NoDriftedNodeClaims(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	np := newNodePoolWithDriftBudget("default-ws-0", "0", "ws-0", "default", "my-infset", "default")
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", "my-infset", "default", false) // NOT drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	npMap := mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})
	npMap[client.ObjectKeyFromObject(np)] = np
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	r := NewDriftReconciler(mockClient, nil, record.NewFakeRecorder(10), &mockProvisioner{})
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_OneDrifted_EnablesDrift(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	np := newNodePoolWithDriftBudget("default-ws-0", "0", "ws-0", "default", "my-infset", "default")
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", "my-infset", "default", true) // drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	npMap := mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})
	npMap[client.ObjectKeyFromObject(np)] = np
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	mockProv := &mockProvisioner{}
	mockProv.On("EnableDriftRemediation", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	recorder := record.NewFakeRecorder(10)
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	mockProv.AssertNumberOfCalls(t, "EnableDriftRemediation", 1)
}

func TestReconcile_UpgradingStillDrifted_Requeues(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	np := newNodePoolWithDriftBudget("default-ws-0", "1", "ws-0", "default", "my-infset", "default") // upgrading
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", "my-infset", "default", true)       // still drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	npMap := mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})
	npMap[client.ObjectKeyFromObject(np)] = np
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	recorder := record.NewFakeRecorder(10)
	mockProv := &mockProvisioner{}
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	mockProv.AssertNotCalled(t, "EnableDriftRemediation", mock.Anything, mock.Anything, mock.Anything)
	mockProv.AssertNotCalled(t, "DisableDriftRemediation", mock.Anything, mock.Anything, mock.Anything)
}

func TestReconcile_UpgradingNoLongerDrifted_DisablesDrift(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	ws.Status.Conditions = []metav1.Condition{
		{Type: string(kaitov1beta1.WorkspaceConditionTypeSucceeded), Status: metav1.ConditionTrue},
	}
	np := newNodePoolWithDriftBudget("default-ws-0", "1", "ws-0", "default", "my-infset", "default") // upgrading
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", "my-infset", "default", false)      // no longer drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	mockClient.CreateOrUpdateObjectInMap(ws) // for Get workspace
	npMap := mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})
	npMap[client.ObjectKeyFromObject(np)] = np
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

	mockProv := &mockProvisioner{}
	mockProv.On("DisableDriftRemediation", mock.Anything, "default", "ws-0").Return(nil)

	recorder := record.NewFakeRecorder(10)
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, driftActiveRequeueInterval, result.RequeueAfter)

	mockProv.AssertCalled(t, "DisableDriftRemediation", mock.Anything, "default", "ws-0")
	mockProv.AssertNumberOfCalls(t, "DisableDriftRemediation", 1)
}

func TestReconcile_UpgradingNoLongerDrifted_WorkloadNotReady_Requeues(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	// Workspace NOT ready — no WorkspaceSucceeded condition
	np := newNodePoolWithDriftBudget("default-ws-0", "1", "ws-0", "default", "my-infset", "default") // upgrading
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", "my-infset", "default", false)      // no longer drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	mockClient.CreateOrUpdateObjectInMap(ws) // for Get workspace
	npMap := mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})
	npMap[client.ObjectKeyFromObject(np)] = np
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

	mockProv := &mockProvisioner{}

	recorder := record.NewFakeRecorder(10)
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	mockProv.AssertNumberOfCalls(t, "DisableDriftRemediation", 0)
}

func TestReconcile_MultipleDrifted_OnlyOneEnabled(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	np0 := newNodePoolWithDriftBudget("default-ws-0", "0", "ws-0", "default", "my-infset", "default")
	np1 := newNodePoolWithDriftBudget("default-ws-1", "0", "ws-1", "default", "my-infset", "default")
	nc0 := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", "my-infset", "default", true)
	nc1 := newNodeClaimWithDriftCondition("nc-1", "default-ws-1", "my-infset", "default", true)

	mockClient.CreateOrUpdateObjectInMap(infSet)
	npMap := mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})
	npMap[client.ObjectKeyFromObject(np0)] = np0
	npMap[client.ObjectKeyFromObject(np1)] = np1
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc0)] = nc0
	ncMap[client.ObjectKeyFromObject(nc1)] = nc1

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodePoolList{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	mockProv := &mockProvisioner{}
	mockProv.On("EnableDriftRemediation", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	recorder := record.NewFakeRecorder(10)
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Only one should be enabled at a time.
	mockProv.AssertNumberOfCalls(t, "EnableDriftRemediation", 1)
}

// --- hasDriftedNodeClaimsInGroup tests ---

func TestHasDriftedNodeClaimsInGroup_Empty(t *testing.T) {
	assert.Assert(t, !hasDriftedNodeClaimsInGroup(nil))
}

func TestHasDriftedNodeClaimsInGroup_NoDrifted(t *testing.T) {
	nc := newNodeClaimWithDriftCondition("nc-0", "pool", "inf", "ns", false)
	assert.Assert(t, !hasDriftedNodeClaimsInGroup([]*karpenterv1.NodeClaim{nc}))
}

func TestHasDriftedNodeClaimsInGroup_HasDrifted(t *testing.T) {
	nc := newNodeClaimWithDriftCondition("nc-0", "pool", "inf", "ns", true)
	assert.Assert(t, hasDriftedNodeClaimsInGroup([]*karpenterv1.NodeClaim{nc}))
}
