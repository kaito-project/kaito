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

func newNodePoolWithDriftBudget(name, nodes string) *karpenterv1.NodePool {
	return &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
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

func newNodeClaimWithDriftCondition(name, nodePoolName string, drifted bool) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{karpenterv1.NodePoolLabelKey: nodePoolName},
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
	np := newNodePoolWithDriftBudget("test-np", "0")
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
				consts.KarpenterInferenceSetKey: "my-infset",
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

// --- Mapper tests (test the actual function) ---

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
				// Missing namespace label
			},
		},
	}

	reqs := mapNodeClaimToInferenceSet(context.Background(), nc)
	assert.Assert(t, len(reqs) == 0)
}

// --- Reconcile state machine tests ---

// These tests use the mock client from pkg/utils/test/mock_client.go.
// Important: mock List does NOT apply label selectors — it returns all objects
// of that type. Tests must be designed with this in mind.
// Important: mock Update does NOT write back to ObjectMap — verify updates by
// inspecting mockClient.Calls arguments.

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

func TestReconcile_NoWorkspaces(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{}) // empty

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)

	r := NewDriftReconciler(mockClient, nil, record.NewFakeRecorder(10), &mockProvisioner{})
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_NoDriftedWorkspaces(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	np := newNodePoolWithDriftBudget("default-ws-0", "0")
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", false) // NOT drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws)] = ws
	mockClient.CreateOrUpdateObjectInMap(np) // Get-accessed: use CreateOrUpdateObjectInMap
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
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
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	np := newNodePoolWithDriftBudget("default-ws-0", "0")
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", true) // drifted

	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws)] = ws
	mockClient.CreateOrUpdateObjectInMap(np) // Get-accessed
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
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
	assert.Equal(t, driftRequeueInterval, result.RequeueAfter)

	// Verify EnableDriftRemediation was called exactly once.
	mockProv.AssertNumberOfCalls(t, "EnableDriftRemediation", 1)
}

func TestReconcile_UpgradingStillDrifted_Requeues(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	np := newNodePoolWithDriftBudget("default-ws-0", "1")              // upgrading
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", true) // still drifted

	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws)] = ws
	mockClient.CreateOrUpdateObjectInMap(np) // Get-accessed
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	recorder := record.NewFakeRecorder(10)
	mockProv := &mockProvisioner{}
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, driftRequeueInterval, result.RequeueAfter)

	// Verify neither Enable nor Disable was called (still waiting for drift to complete).
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
	np := newNodePoolWithDriftBudget("default-ws-0", "1")               // upgrading
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", false) // no longer drifted

	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws)] = ws
	mockClient.CreateOrUpdateObjectInMap(ws) // also store for Get-by-type
	mockClient.CreateOrUpdateObjectInMap(np) // Get-accessed
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
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
	assert.Equal(t, driftRequeueInterval, result.RequeueAfter)

	// Verify DisableDriftRemediation was called for the correct workspace.
	mockProv.AssertCalled(t, "DisableDriftRemediation", mock.Anything, "default", "ws-0")
	mockProv.AssertNumberOfCalls(t, "DisableDriftRemediation", 1)
}

func TestReconcile_UpgradingNoLongerDrifted_WorkloadNotReady_Requeues(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	// Workspace NOT ready — no WorkspaceSucceeded condition
	np := newNodePoolWithDriftBudget("default-ws-0", "1")               // upgrading
	nc := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", false) // no longer drifted

	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws)] = ws
	mockClient.CreateOrUpdateObjectInMap(ws) // also store for Get-by-type
	mockClient.CreateOrUpdateObjectInMap(np)
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

	mockProv := &mockProvisioner{}
	// DisableDriftRemediation should NOT be called

	recorder := record.NewFakeRecorder(10)
	r := NewDriftReconciler(mockClient, nil, recorder, mockProv)
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	assert.NilError(t, err)
	assert.Equal(t, driftRequeueInterval, result.RequeueAfter)

	// Verify DisableDriftRemediation was NOT called — workload not ready yet.
	mockProv.AssertNumberOfCalls(t, "DisableDriftRemediation", 0)
}

func TestReconcile_MultipleDrifted_OnlyOneEnabled(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws0 := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	ws1 := newWorkspaceForInferenceSet("default", "ws-1", "my-infset")
	np0 := newNodePoolWithDriftBudget("default-ws-0", "0")
	np1 := newNodePoolWithDriftBudget("default-ws-1", "0")
	nc0 := newNodeClaimWithDriftCondition("nc-0", "default-ws-0", true)
	nc1 := newNodeClaimWithDriftCondition("nc-1", "default-ws-1", true)

	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws0)] = ws0
	wsMap[client.ObjectKeyFromObject(ws1)] = ws1
	mockClient.CreateOrUpdateObjectInMap(np0) // Get-accessed
	mockClient.CreateOrUpdateObjectInMap(np1) // Get-accessed
	ncMap := mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	ncMap[client.ObjectKeyFromObject(nc0)] = nc0
	ncMap[client.ObjectKeyFromObject(nc1)] = nc1

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
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
	assert.Equal(t, driftRequeueInterval, result.RequeueAfter)

	// Verify exactly one EnableDriftRemediation was called (serial: only first candidate enabled).
	mockProv.AssertNumberOfCalls(t, "EnableDriftRemediation", 1)
}

func TestReconcile_NodePoolNotFound_SkipsWorkspace(t *testing.T) {
	mockClient := test.NewClient()

	infSet := newInferenceSet("default", "my-infset")
	ws := newWorkspaceForInferenceSet("default", "ws-0", "my-infset")
	// No NodePool seeded — it will be NotFound.

	mockClient.CreateMapWithType(&kaitov1alpha1.InferenceSetList{})
	mockClient.CreateOrUpdateObjectInMap(infSet)
	wsMap := mockClient.CreateMapWithType(&kaitov1beta1.WorkspaceList{})
	wsMap[client.ObjectKeyFromObject(ws)] = ws
	mockClient.CreateMapWithType(&karpenterv1.NodePoolList{})  // empty
	mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{}) // empty

	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&kaitov1beta1.WorkspaceList{}), mock.Anything).Return(nil)
	notFoundErr := k8serrors.NewNotFound(
		schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, "default-ws-0")
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything,
		mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(notFoundErr)

	recorder := record.NewFakeRecorder(10)
	r := NewDriftReconciler(mockClient, nil, recorder, &mockProvisioner{})
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-infset", Namespace: "default"},
	})
	// Should succeed (skip the workspace) and not requeue.
	assert.NilError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}
