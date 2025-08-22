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
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestSyncNodeClaims(t *testing.T) {
	t.Run("Should return false and error when ensureNodeClaims fails", func(t *testing.T) {
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

		ready, err := manager.SyncNodeClaims(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false")
		assert.Error(t, err, "Expected error from ensureNodeClaims")
		assert.Contains(t, err.Error(), "failed to get required NodeClaims")
	})

	t.Run("Should return true when BYO nodes are sufficient", func(t *testing.T) {
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

		// Mock ready node
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

		// Mock empty NodeClaim list (required for BYO mode)
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

		ready, err := manager.SyncNodeClaims(context.Background(), workspace)

		assert.True(t, ready, "Expected ready to be true when enough BYO nodes")
		assert.NoError(t, err, "Expected no error")
	})
}

func TestEnsureNodeClaims(t *testing.T) {
	t.Run("Should wait when expectations not satisfied", func(t *testing.T) {
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
		workspaceKey := client.ObjectKeyFromObject(workspace).String()
		expectations.ExpectCreations(manager.logger, workspaceKey, 1)

		err := manager.ensureNodeClaims(context.Background(), workspace)

		assert.NoError(t, err, "Expected no error when waiting for expectations")
	})

	t.Run("Should return error when GetRequiredNodeClaimsCount fails", func(t *testing.T) {
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

		// Mock GetBringYourOwnNodes to fail
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))

		err := manager.ensureNodeClaims(context.Background(), workspace)

		assert.Error(t, err, "Expected error from GetRequiredNodeClaimsCount")
		assert.Contains(t, err.Error(), "failed to get required NodeClaims")
	})

	t.Run("Should create NodeClaims when current count is less than required", func(t *testing.T) {
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
				PreferredNodes: []string{}, // Empty preferred nodes means no BYO nodes when auto provisioning enabled
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2, // Need 2 NodeClaims (since no BYO nodes match)
				},
			},
		}

		// Mock empty node list (no BYO nodes match because PreferredNodes is empty)
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

		// Mock NodeClaim creation - called once per loop iteration
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		err := manager.ensureNodeClaims(context.Background(), workspace)

		assert.NoError(t, err, "Expected no error when creating NodeClaims")
		// Should create 2 NodeClaims (target=2, BYO=0, so need 2)
		// But the algorithm creates one at a time to ensure re-calculation after each creation
		mockClient.AssertNumberOfCalls(t, "Create", 1) // Only 1 creation per invocation
		mockRecorder.AssertEventCount(t, 1)            // Should record 1 creation event
	})

	t.Run("Should take no action when current count matches required", func(t *testing.T) {
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
			Inference: &kaitov1beta1.InferenceSpec{}, // Add inference spec
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2, // Match the number of existing NodeClaims
				},
			},
		}

		// Mock empty node list (no BYO nodes match because PreferredNodes is empty)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock NodeClaim list with exactly 2 NodeClaims (matches required)
		nodeClaim1 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					"kaito.sh/workspace": "test-workspace",
				},
			},
		}
		nodeClaim2 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeclaim-2",
				Namespace: "default",
				Labels: map[string]string{
					"kaito.sh/workspace": "test-workspace",
				},
			},
		}

		nodeClaimList := &karpenterv1.NodeClaimList{
			Items: []karpenterv1.NodeClaim{*nodeClaim1, *nodeClaim2},
		}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		// Mock Get for workspace (needed for status update in delete path)
		mockClient.On("Get", mock.IsType(context.Background()), mock.MatchedBy(func(key client.ObjectKey) bool {
			return key.Name == "test-workspace" && key.Namespace == "default"
		}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Status subresource
		statusClient := &test.MockStatusClient{}
		statusClient.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mockClient.On("Status").Return(statusClient)

		err := manager.ensureNodeClaims(context.Background(), workspace)

		assert.NoError(t, err, "Expected no error when counts match")
		mockClient.AssertNotCalled(t, "Create") // Should not create any NodeClaims
		mockClient.AssertNotCalled(t, "Delete") // Should not delete any NodeClaims
		mockRecorder.AssertEventCount(t, 0)     // Should not record any events
	})

	t.Run("Should continue creating when individual NodeClaim creation fails", func(t *testing.T) {
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
					TargetNodeCount: 2, // Need 2 NodeClaims (since no BYO nodes match)
				},
			},
		}

		// Mock empty node list (no BYO nodes match)
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

		// Mock NodeClaim creation to fail
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("creation failed"))

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		err := manager.ensureNodeClaims(context.Background(), workspace)

		// Should continue and not return an error even though individual creation failed
		assert.NoError(t, err, "Expected no error even when individual NodeClaim creation fails")
		// Should attempt to create the NodeClaim
		mockClient.AssertNumberOfCalls(t, "Create", 1)
		// Should record a failure event
		mockRecorder.AssertEventCount(t, 1) // Should record 1 failure event
	})

	t.Run("Should continue deleting when individual NodeClaim deletion fails", func(t *testing.T) {
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
					TargetNodeCount: 1, // Need only 1 NodeClaim, but have 2 (need to delete 1)
				},
			},
		}

		// Mock empty node list (no BYO nodes match)
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

		// Mock NodeClaim deletion to fail
		mockClient.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("deletion failed"))

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		err := manager.ensureNodeClaims(context.Background(), workspace)

		// Should continue and not return an error even though individual deletion failed
		assert.NoError(t, err, "Expected no error even when individual NodeClaim deletion fails")
		// Should attempt to delete a NodeClaim
		mockClient.AssertNumberOfCalls(t, "Delete", 1)
		// Should record a failure event
		mockRecorder.AssertEventCount(t, 1) // Should record 1 failure event
	})
}

func TestAreNodeClaimsReady(t *testing.T) {
	t.Run("Should return error when GetBringYourOwnNodes fails", func(t *testing.T) {
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

		// Mock GetBringYourOwnNodes to fail
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.areNodeClaimsReady(context.Background(), workspace)

		assert.False(t, ready, "Expected ready to be false")
		assert.Error(t, err, "Expected error from GetBringYourOwnNodes")
		assert.Contains(t, err.Error(), "failed to get available BYO nodes")
	})

	t.Run("Should return true when BYO nodes sufficient and auto provisioning disabled", func(t *testing.T) {
		// Disable auto provisioning
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
				LabelSelector: &metav1.LabelSelector{},
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2,
				},
			},
		}

		// Mock 2 BYO nodes (sufficient)
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "ready-node1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		readyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "ready-node2"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{Items: []corev1.Node{*readyNode1, *readyNode2}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		// Mock Get for status updates
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			w := args.Get(2).(*kaitov1beta1.Workspace)
			*w = *workspace
		}).Return(nil)

		// Mock status updates
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := manager.areNodeClaimsReady(context.Background(), workspace)

		assert.True(t, ready, "Expected ready to be true")
		assert.NoError(t, err, "Expected no error")
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
				PreferredNodes: []string{"ready-node"}, // Include BYO node in preferred nodes
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2,
				},
			},
		}

		// Mock 1 BYO node
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

		// Mock 1 ready NodeClaim (1 BYO + 1 NodeClaim = 2 total)
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					"kaito.sh/workspace": "test-workspace",
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

		ready, err := manager.areNodeClaimsReady(context.Background(), workspace)

		assert.True(t, ready, "Expected ready to be true")
		assert.NoError(t, err, "Expected no error")
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
