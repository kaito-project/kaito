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

package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestEnsureNodeClaims(t *testing.T) {
	t.Run("Should return false and error when GetRequiredNodeClaimsCount fails", func(t *testing.T) {
		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{},
			},
		}

		// Mock GetRequiredNodeClaimsCount to return error
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false")
		assert.Error(t, err, "Expected error from GetRequiredNodeClaimsCount")
		assert.Contains(t, err.Error(), "failed to get required NodeClaims")
	})

	t.Run("Should return false when expectations not satisfied", func(t *testing.T) {
		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{},
			},
		}

		// Set expectations to not be satisfied
		workspaceKey := "default/test-workspace"
		expectations.ExpectCreations(manager.logger, workspaceKey, 1)

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false when expectations not satisfied")
		assert.NoError(t, err, "Expected no error when waiting for expectations")
	})

	t.Run("Should return true when no NodeClaims needed (BYO mode)", func(t *testing.T) {
		// Disable auto provisioning to use BYO nodes
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 1,
				},
			},
		}

		// Mock ready node (when auto provisioning disabled, all ready nodes count)
		readyNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "ready-node"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{Items: []corev1.Node{*readyNode}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock empty NodeClaim list (no NodeClaims needed in BYO mode)
		nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.True(t, ready, "Expected ready to be true when no NodeClaims needed")
		assert.NoError(t, err, "Expected no error")
	})

	t.Run("Should return false when creating NodeClaims", func(t *testing.T) {
		// Enable auto provisioning
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{}, // Empty preferred nodes means no BYO nodes
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2, // Need 2 NodeClaims
				},
			},
		}

		// Mock empty node list (no BYO nodes)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock empty NodeClaim list (no existing NodeClaims)
		nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock NodeClaim creation
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false when creating NodeClaims")
		assert.NoError(t, err, "Expected no error when creating NodeClaims")
		mockClient.AssertNumberOfCalls(t, "Create", 1) // Only 1 creation per invocation
		mockRecorder.AssertEventCount(t, 1)            // Should record 1 creation event
	})

	t.Run("Should return false when deleting excess NodeClaims", func(t *testing.T) {
		// Enable auto provisioning
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{}, // Empty preferred nodes
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 1, // Need only 1 NodeClaim, but have 2
				},
			},
		}

		// Mock empty node list (no BYO nodes)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock NodeClaim list with 2 NodeClaims (need to delete 1)
		nodeClaim1 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "nodeclaim-1",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
				CreationTimestamp: metav1.Now(),
			},
		}
		nodeClaim2 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "nodeclaim-2",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
				CreationTimestamp: metav1.Now(),
			},
		}

		nodeClaimList := &karpenterv1.NodeClaimList{
			Items: []karpenterv1.NodeClaim{*nodeClaim1, *nodeClaim2},
		}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock NodeClaim deletion
		mockClient.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false when deleting NodeClaims")
		assert.NoError(t, err, "Expected no error when deleting NodeClaims")
		mockClient.AssertNumberOfCalls(t, "Delete", 1) // Should delete 1 NodeClaim
		mockRecorder.AssertEventCount(t, 1)            // Should record 1 deletion event
	})

	t.Run("Should return true when all NodeClaims are ready", func(t *testing.T) {
		// Enable auto provisioning
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{}, // No BYO nodes, so all target nodes come from NodeClaims
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 1, // Need 1 NodeClaim (no BYO nodes)
				},
			},
		}

		// Mock empty node list (no BYO nodes)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock 1 ready NodeClaim (exactly what we need)
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{Type: "Ready", Status: "True"},
				},
			},
		}

		nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{*nodeClaim}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.True(t, ready, "Expected ready to be true when all NodeClaims are ready")
		assert.NoError(t, err, "Expected no error when all NodeClaims are ready")
		mockClient.AssertNotCalled(t, "Create") // Should not create any NodeClaims
		mockClient.AssertNotCalled(t, "Delete") // Should not delete any NodeClaims
		mockRecorder.AssertEventCount(t, 0)     // Should not record any events
	})

	t.Run("Should return false when NodeClaims are not ready", func(t *testing.T) {
		// Enable auto provisioning
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()
		mockRecorder := &MockEventRecorder{}
		expectations := utils.NewControllerExpectations()
		manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{}, // No BYO nodes
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 1, // Need 1 NodeClaim
				},
			},
		}

		// Mock empty node list (no BYO nodes)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock 1 not-ready NodeClaim
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{Type: "Ready", Status: "False"},
				},
			},
		}

		nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{*nodeClaim}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.EnsureNodeClaims(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false when NodeClaims are not ready")
		assert.NoError(t, err, "Expected no error when NodeClaims are not ready")
		mockClient.AssertNotCalled(t, "Create") // Should not create any NodeClaims
		mockClient.AssertNotCalled(t, "Delete") // Should not delete any NodeClaims
		mockRecorder.AssertEventCount(t, 0)     // Should not record any events
	})
}

// MockEventRecorder is a mock implementation of record.EventRecorder
type MockEventRecorder struct {
	events []string
}

func (m *MockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	m.events = append(m.events, reason)
}

func (m *MockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, reason)
}

func (m *MockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	m.events = append(m.events, reason)
}

func (m *MockEventRecorder) AssertEventCount(t *testing.T, expectedCount int) {
	assert.Equal(t, expectedCount, len(m.events), "Expected %d events, got %d", expectedCount, len(m.events))
}
