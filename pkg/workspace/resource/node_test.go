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
	"os"
	"reflect"
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestNewNodeResourceManager(t *testing.T) {
	mockClient := test.NewClient()

	manager := NewNodeManager(mockClient)

	assert.NotNil(t, manager)
	assert.Equal(t, mockClient, manager.Client)
}

func TestSetNodePluginsReadyCondition_SetsToTrue(t *testing.T) {
	tests := []struct {
		name               string
		workspace          *kaitov1beta1.Workspace
		existingNodeClaims []*karpenterv1.NodeClaim
		setup              func(*test.MockClient)
		expectedReady      bool
		expectedError      bool
	}{
		{
			name: "Should set NodePluginsReady condition to true when all plugins are ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3", // GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						Conditions: []status.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a ready node with GPU capacity and correct labels
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
							resources.LabelKeyNvidia:       resources.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("1"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Create workspace object in the mock object map for Get calls
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)

				// Mock status update for workspace condition update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should set NodePluginsReady condition to true for non-GPU instance type",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_D2s_v3", // Non-GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Create workspace object in the mock object map for status updates
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Mock status update for NodePluginsReady condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should set NodePluginsReady condition to false when checkNodePlugin fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3", // GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						Conditions: []status.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create workspace object in the mock object map for Get calls
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)

				// Don't create the node in ObjectMap to simulate Get failure
				// Mock Get to return an error for node operations
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("node get failed"))

				// Mock status update for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name: "Should set NodePluginsReady condition to false when nodes are not ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3", // GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						Conditions: []status.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a node with zero GPU capacity (device plugins not ready)
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
							resources.LabelKeyNvidia:       resources.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("0"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Create workspace object in the mock object map for Get calls
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)

				// Mock status update for NodePluginsNotReady condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			// set env CLOUD_PROVIDER to "azure" for AzureSKUHandler
			os.Setenv("CLOUD_PROVIDER", "azure")
			defer os.Unsetenv("CLOUD_PROVIDER")

			manager := NewNodeManager(mockClient)
			ready, err := manager.CheckIfNodePluginsReady(context.Background(), tt.workspace, tt.existingNodeClaims)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedReady, ready)
		})
	}
}

func TestSetNodePluginsReadyCondition_AdditionalCases(t *testing.T) {
	tests := []struct {
		name               string
		workspace          *kaitov1beta1.Workspace
		existingNodeClaims []*karpenterv1.NodeClaim
		setup              func(*test.MockClient)
		expectedReady      bool
		expectedError      bool
	}{
		{
			name: "Should succeed when instance type has no known GPU config",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_D2s_v3", // Non-GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"node1"},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// For non-GPU instance types, the function should set NodePluginsReady to true directly
				// Create workspace object in the mock object map for Get calls
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						InstanceType:  "Standard_D2s_v3", // Non-GPU instance type
						LabelSelector: &metav1.LabelSelector{},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should succeed when GPU instance type and device plugins are ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3", // GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						Conditions: []status.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a ready node with GPU capacity and correct labels
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
							resources.LabelKeyNvidia:       resources.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("1"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Node and workspace exist in ObjectMap for status updates
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Mock status update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should fail when device plugins check fails due to node get error",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3", // GPU instance type
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						Conditions: []status.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock failing node Get - this should fail when getting the node, not from ObjectMap
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("node get failed"))

				// Mock status update for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			// set env CLOUD_PROVIDER to "azure" for AzureSKUHandler
			os.Setenv("CLOUD_PROVIDER", "azure")
			defer os.Unsetenv("CLOUD_PROVIDER")

			manager := NewNodeManager(mockClient)
			ready, err := manager.CheckIfNodePluginsReady(context.Background(), tt.workspace, tt.existingNodeClaims)

			assert.Equal(t, tt.expectedError, err != nil)
			assert.Equal(t, tt.expectedReady, ready)

		})
	}
}

func TestCheckNodePlugin(t *testing.T) {
	tests := []struct {
		name               string
		workspace          *kaitov1beta1.Workspace
		existingNodeClaims []*karpenterv1.NodeClaim
		setup              func(*test.MockClient)
		expectedReady      bool
		expectedError      bool
	}{
		{
			name: "Should fail when getReadyNodesFromNodeClaims fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock failing node Get - this should fail when getting the node, not from ObjectMap
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("node get failed"))

				// Mock status update for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name: "Should add accelerator label and succeed when node lacks it",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a node without accelerator label but with GPU capacity
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("1"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Mock node update
				mockClient.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should wait when node has zero GPU capacity",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a node with accelerator label but zero GPU capacity
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
							resources.LabelKeyNvidia:       resources.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("0"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Create workspace object in the mock object map for status updates
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)

				// Mock status update for GPU capacity not ready
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: false,
		},
		{
			name: "Should wait when node has incorrect instance type label",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a node with incorrect instance type label
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v2",
							resources.LabelKeyNvidia:       resources.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("1"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: false,
		},
		{
			name: "Should succeed when all nodes have GPU capacity and labels",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a ready node with GPU capacity and correct labels
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
							resources.LabelKeyNvidia:       resources.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("1"),
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should return error when node update fails while adding label",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a node that lacks the accelerator label but has GPU capacity
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
						Capacity:   corev1.ResourceList{resources.CapacityNvidiaGPU: resource.MustParse("1")},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Node exists in ObjectMap
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

				// Simulate Update failing when adding the label
				mockClient.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("update failed"))

				// Create workspace object in the mock object map for status updates
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)

				// Expect status update attempt for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			areReady, err := manager.checkNodePlugin(context.Background(), tt.workspace, tt.existingNodeClaims)

			assert.Equal(t, tt.expectedReady, areReady)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetNodesReadyCondition_SetsToTrue(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		nodes         []*corev1.Node
		nodeClaims    []*karpenterv1.NodeClaim
		setup         func(*test.MockClient)
		expectedReady bool
		expectedError bool
	}{
		{
			name: "Should set NodesReady condition to true when enough nodes are ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload": "test",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
						Labels: map[string]string{
							"workload": "test",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock status update calls - verify condition is set to True
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock the condition update to verify it's set to True with "NodesReady" reason
				mockClient.StatusMock.On("Update", mock.Anything, mock.MatchedBy(func(ws *kaitov1beta1.Workspace) bool {
					// Find the NodeStatus condition and verify it's set to True
					for _, condition := range ws.Status.Conditions {
						if condition.Type == string(kaitov1beta1.ConditionTypeNodeStatus) {
							return condition.Status == metav1.ConditionTrue && condition.Reason == "NodesReady"
						}
					}
					return false
				}), mock.Anything).Return(nil).Maybe()

				// Mock worker nodes update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			// Default to empty slices if not specified
			nodes := tt.nodes
			if nodes == nil {
				nodes = []*corev1.Node{}
			}
			nodeClaims := tt.nodeClaims
			if nodeClaims == nil {
				nodeClaims = []*karpenterv1.NodeClaim{}
			}

			manager := NewNodeManager(mockClient)
			ready, err := manager.EnsureNodesReady(context.Background(), tt.workspace, nodes, nodeClaims)

			assert.Equal(t, tt.expectedReady, ready)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnsureNodesReady(t *testing.T) {
	tests := []struct {
		name                        string
		workspace                   *kaitov1beta1.Workspace
		nodes                       []*corev1.Node
		nodeClaims                  []*karpenterv1.NodeClaim
		setup                       func(*test.MockClient)
		disableNodeAutoProvisioning bool
		expectedReady               bool
		expectedError               bool
	}{
		{
			name: "Should return true when enough nodes are ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload": "test",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
						Labels: map[string]string{
							"workload": "test",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock status update calls
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should return false when not enough nodes are ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 3,
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Only 1 ready node, but target is 3
				nodes := []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
							Labels: map[string]string{
								"workload": "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-2",
							Labels: map[string]string{
								"workload": "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
							},
						},
					},
				}

				nodeList := &corev1.NodeList{Items: nodes}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

				// Mock status update calls
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false,
			expectedError: false,
		},
		{
			name: "Should exclude nodes being deleted",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			setup: func(mockClient *test.MockClient) {
				now := metav1.Now()
				nodes := []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
							Labels: map[string]string{
								"workload": "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-2",
							Labels: map[string]string{
								"workload": "test",
							},
							DeletionTimestamp: &now, // Being deleted
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
							},
						},
					},
				}

				nodeList := &corev1.NodeList{Items: nodes}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

				// Mock status update calls
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedReady: false, // Only 1 ready node (excluding deleting), but target is 2
			expectedError: false,
		},
		{
			name: "Should return error when UpdateWorkerNodesInStatus fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload": "test",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock successful condition update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				// Mock UpdateWorkerNodesInStatus to fail (this happens in the defer block)
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("worker nodes update failed")).Maybe()
			},
			disableNodeAutoProvisioning: true, // If NAP is disabled, we don't want to fail on updating the status due to the missing instance type label as that check should be skipped.
			expectedReady:               true, // Function succeeds initially
			expectedError:               true, // But returns error due to UpdateWorkerNodesInStatus failing in defer
		},
		{
			name: "Should return error when status update fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			setup: func(mockClient *test.MockClient) {
				nodes := []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
							Labels: map[string]string{
								"workload": "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
							},
						},
					},
				}

				nodeList := &corev1.NodeList{Items: nodes}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

				// Mock status update calls - fail the condition update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("status update failed")).Once()
				// Mock successful worker nodes status update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name: "NAP enabled - Should succeed but log warning when node missing instance type label",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload": "test",
							// Missing instance type label - logged but doesn't fail
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock status update calls - multiple updates expected (false then true)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			disableNodeAutoProvisioning: false, // NAP enabled
			expectedReady:               true,  // Current behavior: succeeds despite warning
			expectedError:               false,
		},
		{
			name: "NAP enabled - Should succeed but log warning when node has mismatched instance type label",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload":                     "test",
							corev1.LabelInstanceTypeStable: "Standard_NC6s_v3", // Wrong instance type - logged but doesn't fail
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock status update calls - multiple updates expected (false then true)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			disableNodeAutoProvisioning: false, // NAP enabled
			expectedReady:               true,  // Current behavior: succeeds despite warning
			expectedError:               false,
		},
		{
			name: "NAP enabled - Should succeed when node has correct instance type label",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload":                     "test",
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3", // Correct instance type
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock status update calls for success condition
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			disableNodeAutoProvisioning: false, // NAP enabled
			expectedReady:               true,
			expectedError:               false,
		},
		{
			name: "NAP disabled - Should succeed even when node missing instance type label",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "test",
						},
					},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"workload": "test",
							// Missing instance type label - should be OK when NAP is disabled
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			nodeClaims: []*karpenterv1.NodeClaim{},
			setup: func(mockClient *test.MockClient) {
				// Mock status update calls for success condition
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			disableNodeAutoProvisioning: true, // NAP disabled
			expectedReady:               true,
			expectedError:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup feature gate
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tt.disableNodeAutoProvisioning
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

			mockClient := test.NewClient()
			tt.setup(mockClient)

			// Default to empty slices if not specified
			nodes := tt.nodes
			if nodes == nil {
				nodes = []*corev1.Node{}
			}
			nodeClaims := tt.nodeClaims
			if nodeClaims == nil {
				nodeClaims = []*karpenterv1.NodeClaim{}
			}

			manager := NewNodeManager(mockClient)
			ready, err := manager.EnsureNodesReady(context.Background(), tt.workspace, nodes, nodeClaims)

			assert.Equal(t, tt.expectedReady, ready)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetReadyNodesFromNodeClaims(t *testing.T) {
	tests := []struct {
		name            string
		workspace       *kaitov1beta1.Workspace
		readyNodeClaims []*karpenterv1.NodeClaim
		setup           func(*test.MockClient)
		expectedNodes   int
		expectedError   bool
	}{
		{
			name: "Should return error when NodeClaim without assigned node",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			readyNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "", // No node assigned
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// No setup needed - will skip NodeClaim without NodeName
			},
			expectedNodes: 0,
			expectedError: false,
		},
		{
			name: "Should return error when fail to get node",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			readyNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "nonexistent-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock Get to return NotFound error
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "nodes"}, "nonexistent-node"))
			},
			expectedNodes: 0,
			expectedError: true,
		},
		{
			name: "Should return error when node is not ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			readyNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a node that is not ready
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionFalse,
							},
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedNodes: 0,
			expectedError: false,
		},
		{
			name: "Should return ready node with matching instance type",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_NC12s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			readyNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a ready node with matching instance type
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				}
				mockClient.CreateOrUpdateObjectInMap(node)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			expectedNodes: 1,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			nodes, err := manager.getReadyNodesFromNodeClaims(context.Background(), tt.workspace, tt.readyNodeClaims)

			assert.Len(t, nodes, tt.expectedNodes)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPropagateOwnedConditions(t *testing.T) {
	tests := []struct {
		name              string
		workspace         *kaitov1beta1.Workspace
		condition         kaitov1beta1.ConditionType
		conditionTypes    []kaitov1beta1.ConditionType
		setup             func(*test.MockClient)
		expectedError     bool
		expectedCondition metav1.ConditionStatus
	}{
		{
			name: "Should set condition to false when first owned condition is false",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status:  metav1.ConditionFalse,
							Reason:  "NodeClaimNotReady",
							Message: "Not enough NodeClaims are ready",
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			condition: kaitov1beta1.ConditionTypeResourceStatus,
			conditionTypes: []kaitov1beta1.ConditionType{
				kaitov1beta1.ConditionTypeNodeClaimStatus,
				kaitov1beta1.ConditionTypeNodeStatus,
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError:     false,
			expectedCondition: metav1.ConditionFalse,
		},
		{
			name: "Should not update condition when all owned conditions are true",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			condition: kaitov1beta1.ConditionTypeResourceStatus,
			conditionTypes: []kaitov1beta1.ConditionType{
				kaitov1beta1.ConditionTypeNodeClaimStatus,
				kaitov1beta1.ConditionTypeNodeStatus,
			},
			setup:         func(mockClient *test.MockClient) {},
			expectedError: false,
		},
		{
			name: "Should handle missing owned conditions gracefully",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			condition: kaitov1beta1.ConditionTypeResourceStatus,
			conditionTypes: []kaitov1beta1.ConditionType{
				kaitov1beta1.ConditionTypeNodeClaimStatus, // Missing
				kaitov1beta1.ConditionTypeNodeStatus,
			},
			setup:         func(mockClient *test.MockClient) {},
			expectedError: false,
		},
		{
			name: "Should return error when status update fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status:  metav1.ConditionFalse,
							Reason:  "NodeClaimNotReady",
							Message: "Not enough NodeClaims are ready",
						},
					},
				},
			},
			condition: kaitov1beta1.ConditionTypeResourceStatus,
			conditionTypes: []kaitov1beta1.ConditionType{
				kaitov1beta1.ConditionTypeNodeClaimStatus,
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("status update failed"))
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			_, err := manager.VerifyOwnedConditions(context.Background(), tt.workspace, tt.condition, tt.conditionTypes)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetResourceReadyCondition_SetsToTrue(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedError bool
	}{
		{
			name: "Should set ResourceReady condition to true when all owned conditions are true",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock status update - verify ResourceReady condition is set to True
				mockClient.StatusMock.On("Update", mock.Anything, mock.MatchedBy(func(ws *kaitov1beta1.Workspace) bool {
					// Find the ResourceStatus condition and verify it's set to True
					for _, condition := range ws.Status.Conditions {
						if condition.Type == string(kaitov1beta1.ConditionTypeResourceStatus) {
							return condition.Status == metav1.ConditionTrue && condition.Reason == "workspaceResourceStatusSuccess"
						}
					}
					return false
				}), mock.Anything).Return(nil)
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			err := manager.SetResourceReadyConditionByStatus(context.Background(), tt.workspace)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetResourceReadyCondition(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedError bool
	}{
		{
			name: "Should set resource condition to true when all owned conditions are true",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError: false,
		},
		{
			name: "Should set resource condition to false when NodeClaim condition is false",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status:  metav1.ConditionFalse,
							Reason:  "NodeClaimNotReady",
							Message: "Not enough NodeClaims are ready",
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError: false,
		},
		{
			name: "Should set resource condition to false when Node condition is false",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status: metav1.ConditionTrue,
						},
						{
							Type:    string(kaitov1beta1.ConditionTypeNodeStatus),
							Status:  metav1.ConditionFalse,
							Reason:  "NodeNotReady",
							Message: "Not enough nodes are ready",
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError: false,
		},
		{
			name: "Should set resource condition to false when NodePlugin condition is false",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError: false,
		},
		{
			name: "Should return error when propagateOwnedConditions fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(kaitov1beta1.ConditionTypeNodeStatus),
							Status:  metav1.ConditionFalse,
							Reason:  "NodeNotReady",
							Message: "Not enough nodes are ready",
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("status update failed"))
			},
			expectedError: true,
		},
		{
			name: "Should return error when final status update fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.ConditionTypeNodeStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("final status update failed"))
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			err := manager.SetResourceReadyConditionByStatus(context.Background(), tt.workspace)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUpdateWorkerNodesInStatus(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		readyNodes    []corev1.Node
		setup         func(*test.MockClient)
		expectedError bool
	}{
		{
			name: "Should update worker nodes when list has changed",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"old-node-1"},
				},
			},
			readyNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "new-node-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "new-node-2"}},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError: false,
		},
		{
			name: "Should not update when worker nodes list is unchanged",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"node-1", "node-2"},
				},
			},
			readyNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
			},
			setup:         func(mockClient *test.MockClient) {},
			expectedError: false,
		},
		{
			name: "Should handle empty node list",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"old-node-1"},
				},
			},
			readyNodes: []corev1.Node{},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe().Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe().Maybe()
			},
			expectedError: false,
		},
		{
			name: "Should return error when status update fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"old-node-1"},
				},
			},
			readyNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "new-node-1"}},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("status update failed"))
			},
			expectedError: true,
		},
		{
			name: "Should sort node names alphabetically",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{},
				},
			},
			readyNodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-z"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-m"}},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
				// Expect the update to be called with sorted node names
				mockClient.StatusMock.On("Update", mock.Anything, mock.MatchedBy(func(ws *kaitov1beta1.Workspace) bool {
					expectedNodes := []string{"node-a", "node-m", "node-z"}
					return reflect.DeepEqual(ws.Status.WorkerNodes, expectedNodes)
				}), mock.Anything).Return(nil)
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			// Convert []Node to []*Node
			nodePointers := make([]*corev1.Node, len(tt.readyNodes))
			for i := range tt.readyNodes {
				nodePointers[i] = &tt.readyNodes[i]
			}
			err := manager.UpdateWorkerNodesInStatus(context.Background(), tt.workspace, nodePointers)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
