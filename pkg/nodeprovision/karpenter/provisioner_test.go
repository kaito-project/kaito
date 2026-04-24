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

package karpenter

import (
	"context"
	"errors"
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

var testConfig = NodeClassConfig{
	Group: "karpenter.azure.com",
	Kind:  "AKSNodeClass",
	Name:  "image-family-ubuntu",
	ImageFamilyNames: map[string]string{
		"ubuntu":     "image-family-ubuntu",
		"azurelinux": "image-family-azure-linux",
	},
}

// mockNodeClassReady sets up a mock Get call for an unstructured NodeClass that
// populates the object with a Ready=True condition.
func mockNodeClassReady(mockClient *test.MockClient, name string) {
	mockClient.On("Get", mock.IsType(context.Background()), mock.MatchedBy(func(key client.ObjectKey) bool {
		return key.Name == name
	}), mock.IsType(&unstructured.Unstructured{}), mock.Anything).
		Run(func(args mock.Arguments) {
			obj := args.Get(2).(*unstructured.Unstructured)
			obj.Object = map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			}
		}).
		Return(nil)
}

func TestKarpenterProvisionerImplementsInterface(t *testing.T) {
	mockClient := test.NewClient()
	var _ nodeprovision.NodeProvisioner = NewKarpenterProvisioner(mockClient, testConfig)
}

func TestName(t *testing.T) {
	p := NewKarpenterProvisioner(test.NewClient(), testConfig)
	assert.Equal(t, "KarpenterProvisioner", p.Name())
}

func TestStart_NoOp(t *testing.T) {
	p := NewKarpenterProvisioner(test.NewClient(), testConfig)
	err := p.Start(context.Background())
	assert.NoError(t, err)
}

// --- ProvisionNodes tests ---

func TestProvisionNodes_Success(t *testing.T) {
	mockClient := test.NewClient()
	mockNodeClassReady(mockClient, "image-family-ubuntu")
	mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestProvisionNodes_AlreadyExists(t *testing.T) {
	mockClient := test.NewClient()
	mockNodeClassReady(mockClient, "image-family-ubuntu")
	alreadyExistsErr := apierrors.NewAlreadyExists(schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, "default-ws1")
	mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(alreadyExistsErr)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	assert.NoError(t, err) // AlreadyExists is silently ignored
}

func TestProvisionNodes_CreateError(t *testing.T) {
	mockClient := test.NewClient()
	mockNodeClassReady(mockClient, "image-family-ubuntu")
	mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(errors.New("API server down"))

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API server down")
}

func TestProvisionNodes_UsesDefaultNodeClassName(t *testing.T) {
	mockClient := test.NewClient()
	mockNodeClassReady(mockClient, "image-family-ubuntu")
	mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	assert.NoError(t, err)

	found := false
	for _, call := range mockClient.Calls {
		if call.Method == "Create" {
			createdNP, ok := call.Arguments[1].(*karpenterv1.NodePool)
			if ok {
				assert.Equal(t, "image-family-ubuntu", createdNP.Spec.Template.Spec.NodeClassRef.Name)
				found = true
			}
		}
	}
	assert.True(t, found, "expected Create to be called with a NodePool")
}

func TestProvisionNodes_UsesAnnotationImageFamily(t *testing.T) {
	mockClient := test.NewClient()
	mockNodeClassReady(mockClient, "image-family-azure-linux")
	mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, map[string]string{
		kaitov1beta1.AnnotationNodeImageFamily: "AzureLinux",
	})

	err := p.ProvisionNodes(context.Background(), ws)
	assert.NoError(t, err)

	for _, call := range mockClient.Calls {
		if call.Method == "Create" {
			createdNP, ok := call.Arguments[1].(*karpenterv1.NodePool)
			if ok {
				assert.Equal(t, "image-family-azure-linux", createdNP.Spec.Template.Spec.NodeClassRef.Name)
			}
		}
	}
}

// --- DeleteNodes tests ---

func TestDeleteNodes_Success(t *testing.T) {
	mockClient := test.NewClient()
	np := &karpenterv1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"}}
	mockClient.CreateOrUpdateObjectInMap(np)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
	mockClient.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.NoError(t, err)
}

func TestDeleteNodes_NotFound(t *testing.T) {
	mockClient := test.NewClient()
	notFoundErr := apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, "default-ws1")
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(notFoundErr)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.NoError(t, err) // NotFound is silently ignored
}

func TestDeleteNodes_AlreadyDeleting(t *testing.T) {
	mockClient := test.NewClient()
	now := metav1.Now()
	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "default-ws1",
			DeletionTimestamp: &now,
			Finalizers:        []string{"karpenter.sh/termination"},
		},
	}
	mockClient.CreateOrUpdateObjectInMap(np)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.NoError(t, err)
	// Delete should NOT have been called
	mockClient.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything, mock.Anything)
}

func TestDeleteNodes_DeleteError(t *testing.T) {
	mockClient := test.NewClient()
	np := &karpenterv1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"}}
	mockClient.CreateOrUpdateObjectInMap(np)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
	mockClient.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(errors.New("forbidden"))

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

// --- EnableDriftRemediation tests ---

func TestEnableDriftRemediation_Success(t *testing.T) {
	mockClient := test.NewClient()

	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   "0",
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonDrifted},
					},
				},
			},
		},
	}
	mockClient.CreateOrUpdateObjectInMap(np)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
	mockClient.On("Update", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	err := p.EnableDriftRemediation(context.Background(), "default", "ws1")
	assert.NoError(t, err)

	found := false
	for _, call := range mockClient.Calls {
		if call.Method == "Update" {
			updatedNP := call.Arguments[1].(*karpenterv1.NodePool)
			assert.Equal(t, "1", updatedNP.Spec.Disruption.Budgets[0].Nodes)
			found = true
		}
	}
	assert.True(t, found, "expected Update to be called")
}

func TestEnableDriftRemediation_NodePoolNotFound(t *testing.T) {
	mockClient := test.NewClient()
	notFoundErr := apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, "default-ws1")
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(notFoundErr)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	err := p.EnableDriftRemediation(context.Background(), "default", "ws1")
	assert.Error(t, err)
}

func TestEnableDriftRemediation_NoDriftedBudgetEntry(t *testing.T) {
	mockClient := test.NewClient()

	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   "10%",
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonEmpty},
					},
				},
			},
		},
	}
	mockClient.CreateOrUpdateObjectInMap(np)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	err := p.EnableDriftRemediation(context.Background(), "default", "ws1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no budget entry with Drifted reason")
}

// --- DisableDriftRemediation tests ---

func TestDisableDriftRemediation_Success(t *testing.T) {
	mockClient := test.NewClient()

	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   "1",
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonDrifted},
					},
				},
			},
		},
	}
	mockClient.CreateOrUpdateObjectInMap(np)
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)
	mockClient.On("Update", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	err := p.DisableDriftRemediation(context.Background(), "default", "ws1")
	assert.NoError(t, err)

	found := false
	for _, call := range mockClient.Calls {
		if call.Method == "Update" {
			updatedNP := call.Arguments[1].(*karpenterv1.NodePool)
			assert.Equal(t, "0", updatedNP.Spec.Disruption.Budgets[0].Nodes)
			found = true
		}
	}
	assert.True(t, found, "expected Update to be called")
}

func TestDisableDriftRemediation_NodePoolNotFound(t *testing.T) {
	mockClient := test.NewClient()
	notFoundErr := apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, "default-ws1")
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodePool{}), mock.Anything).Return(notFoundErr)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	err := p.DisableDriftRemediation(context.Background(), "default", "ws1")
	assert.Error(t, err)
}

// --- EnsureNodesReady tests ---

func TestEnsureNodesReady_AllReady(t *testing.T) {
	mockClient := test.NewClient()

	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nc-1",
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: "default-ws1",
			},
		},
		Status: karpenterv1.NodeClaimStatus{
			Conditions: []status.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
		},
	}
	nodeClaimList := &karpenterv1.NodeClaimList{}
	relevantMap := mockClient.CreateMapWithType(nodeClaimList)
	relevantMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	assert.NoError(t, err)
	assert.True(t, ready)
	assert.False(t, needRequeue)
}

func TestEnsureNodesReady_SomeNotReady(t *testing.T) {
	mockClient := test.NewClient()

	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nc-1",
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: "default-ws1",
			},
		},
		Status: karpenterv1.NodeClaimStatus{
			Conditions: []status.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
			},
		},
	}
	nodeClaimList := &karpenterv1.NodeClaimList{}
	relevantMap := mockClient.CreateMapWithType(nodeClaimList)
	relevantMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	assert.NoError(t, err)
	assert.False(t, ready)
	assert.False(t, needRequeue)
}

func TestEnsureNodesReady_CountBelowTarget(t *testing.T) {
	mockClient := test.NewClient()

	mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	assert.NoError(t, err)
	assert.False(t, ready)
	assert.False(t, needRequeue)
}

func TestEnsureNodesReady_ListError(t *testing.T) {
	mockClient := test.NewClient()
	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("API error"))

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	_, _, err := p.EnsureNodesReady(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestEnsureNodesReady_NodeClaimBeingDeleted(t *testing.T) {
	mockClient := test.NewClient()

	now := metav1.Now()
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "nc-1",
			DeletionTimestamp: &now,
			Finalizers:        []string{"karpenter.sh/termination"},
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: "default-ws1",
			},
		},
		Status: karpenterv1.NodeClaimStatus{
			Conditions: []status.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
		},
	}
	nodeClaimList := &karpenterv1.NodeClaimList{}
	relevantMap := mockClient.CreateMapWithType(nodeClaimList)
	relevantMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	assert.NoError(t, err)
	assert.False(t, ready)
	assert.False(t, needRequeue)
}

// --- CollectNodeStatusInfo tests ---

func TestCollectNodeStatusInfo_AllReady(t *testing.T) {
	mockClient := test.NewClient()

	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nc-1",
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: "default-ws1",
			},
		},
		Status: karpenterv1.NodeClaimStatus{
			Conditions: []status.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
		},
	}
	nodeClaimList := &karpenterv1.NodeClaimList{}
	relevantMap := mockClient.CreateMapWithType(nodeClaimList)
	relevantMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(conditions))

	for _, cond := range conditions {
		assert.Equal(t, metav1.ConditionTrue, cond.Status, "condition %s should be True", cond.Type)
	}
}

func TestCollectNodeStatusInfo_NotEnoughNodeClaims(t *testing.T) {
	mockClient := test.NewClient()

	mockClient.CreateMapWithType(&karpenterv1.NodeClaimList{})
	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(conditions))

	for _, cond := range conditions {
		assert.Equal(t, metav1.ConditionFalse, cond.Status, "condition %s should be False", cond.Type)
	}
}

func TestCollectNodeStatusInfo_ListError(t *testing.T) {
	mockClient := test.NewClient()
	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("API error"))

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	_, err := p.CollectNodeStatusInfo(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestCollectNodeStatusInfo_ConditionTypes(t *testing.T) {
	mockClient := test.NewClient()

	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nc-1",
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: "default-ws1",
			},
		},
		Status: karpenterv1.NodeClaimStatus{
			Conditions: []status.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
		},
	}
	nodeClaimList := &karpenterv1.NodeClaimList{}
	relevantMap := mockClient.CreateMapWithType(nodeClaimList)
	relevantMap[client.ObjectKeyFromObject(nc)] = nc

	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

	p := NewKarpenterProvisioner(mockClient, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	assert.NoError(t, err)

	typeSet := map[string]bool{}
	for _, cond := range conditions {
		typeSet[cond.Type] = true
	}
	assert.True(t, typeSet[string(kaitov1beta1.ConditionTypeNodeStatus)])
	assert.True(t, typeSet[string(kaitov1beta1.ConditionTypeNodeClaimStatus)])
	assert.True(t, typeSet[string(kaitov1beta1.ConditionTypeResourceStatus)])
}
