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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestNewNodeResourceManager(t *testing.T) {
	mockClient := test.NewClient()

	manager := NewNodeManager(mockClient)

	assert.NotNil(t, manager)
	assert.Equal(t, mockClient, manager.Client)
}

func TestEnsureNodeResource(t *testing.T) {
	tests := []struct {
		name               string
		workspace          *kaitov1beta1.Workspace
		existingNodeClaims []*karpenterv1.NodeClaim
		workerNodes        []string
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
			workerNodes:        []string{"node1", "node2"},
			setup: func(mockClient *test.MockClient) {
				// Mock Get for workspace status update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock status update for worker nodes and resource status
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
			workerNodes: []string{"test-node"},
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

				// Mock Get for node and workspace status update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock status update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
			workerNodes: []string{"test-node"},
			setup: func(mockClient *test.MockClient) {
				// Mock Get for workspace status update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("node get failed"))

				// Mock status update for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name: "Should update worker nodes when they differ",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType:  "Standard_D2s_v3",
					LabelSelector: &metav1.LabelSelector{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"old-node"},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{},
			workerNodes:        []string{"new-node1", "new-node2"},
			setup: func(mockClient *test.MockClient) {
				// Mock Get for workspace status update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock status update for worker nodes and resource status
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: true,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			ready, err := manager.EnsureNodeResource(context.Background(), tt.workspace, tt.existingNodeClaims, tt.workerNodes)

			assert.Equal(t, tt.expectedReady, ready)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnsureNodePlugin(t *testing.T) {
	tests := []struct {
		name            string
		workspace       *kaitov1beta1.Workspace
		readyNodeClaims []*karpenterv1.NodeClaim
		setup           func(*test.MockClient)
		expectedReady   bool
		expectedError   bool
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
			readyNodeClaims: []*karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock failing node Get
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("node get failed"))

				// Mock status update for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
			readyNodeClaims: []*karpenterv1.NodeClaim{
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
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock node update
				mockClient.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
			readyNodeClaims: []*karpenterv1.NodeClaim{
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
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock status update for GPU capacity not ready
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
			readyNodeClaims: []*karpenterv1.NodeClaim{
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
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: true,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeManager(mockClient)
			ready, err := manager.ensureNodePlugin(context.Background(), tt.workspace, tt.readyNodeClaims)

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
			name: "Should skip NodeClaim without assigned node",
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
			name: "Should skip when node doesn't exist",
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
			expectedError: false,
		},
		{
			name: "Should return error when node Get fails with non-NotFound error",
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
				// Mock Get to return a different error
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("api server error"))
			},
			expectedNodes: 0,
			expectedError: true,
		},
		{
			name: "Should skip node that is not ready",
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
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedNodes: 0,
			expectedError: false,
		},
		{
			name: "Should skip node with different instance type",
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
				// Create a ready node with different instance type
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_D2s_v3", // Different instance type
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
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
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
