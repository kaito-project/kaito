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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestNewNodeResourceManager(t *testing.T) {
	mockClient := test.NewClient()

	manager := NewNodeResourceManager(mockClient)

	assert.NotNil(t, manager)
	assert.Equal(t, mockClient, manager.Client)
}

func TestEnsureNodeResource(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedReady bool
		expectedError bool
	}{
		{
			name: "Should succeed when instance type has no known GPU config",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_D2s_v3", // Non-GPU instance type
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock Get for workspace status update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock list nodes for label selector matching
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// Mock status update for updateWorkspaceStatusIfNeeded
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
					InstanceType: "Standard_NC12s_v3", // GPU instance type
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock nodeclaim.GetRequiredNodeClaimsCount
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// Mock nodeclaim.GetExistingNodeClaims
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

				// Mock Get for workspace status update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				// Mock status update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should fail when device plugins check fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3", // GPU instance type
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock nodeclaim.GetRequiredNodeClaimsCount to fail
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))
			},
			expectedReady: false,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeResourceManager(mockClient)
			ready, err := manager.EnsureNodeResource(context.Background(), tt.workspace)

			assert.Equal(t, tt.expectedReady, ready)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnsureNvidiaDevicePluginsReady(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedReady bool
		expectedError bool
	}{
		{
			name: "Should fail when GetRequiredNodeClaimsCount fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name: "Should fail when getReadyNodesFromNodeClaims fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock successful GetRequiredNodeClaimsCount (returns 0 for no BYO nodes)
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// Mock failing GetExistingNodeClaims
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("nodeclaim list failed"))

				// Mock status update for error condition
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name: "Should wait when node count does not match target",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
				},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 1,
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock GetRequiredNodeClaimsCount returns 1 (1 required, 0 BYO)
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// Mock GetExistingNodeClaims returns empty list (0 nodes)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

				// Mock status update for node count mismatch
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: false,
			expectedError: false,
		},
		{
			name: "Should add accelerator label and succeed when node lacks it",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
				},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 1,
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock BYO nodes (empty list for GetRequiredNodeClaimsCount to return 1)
				nodeList := &corev1.NodeList{}
				mockClient.CreateMapWithType(nodeList)

				// Mock GetExistingNodeClaims returns one NodeClaim
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				}
				nodeClaimList := &karpenterv1.NodeClaimList{}
				nodeClaimMap := mockClient.CreateMapWithType(nodeClaimList)
				objKey := client.ObjectKeyFromObject(nodeClaim)
				nodeClaimMap[objKey] = nodeClaim

				// Set up List mock for both node and nodeclaim lists
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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

				// Mock status update
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "Should wait when node has zero GPU capacity",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock GetRequiredNodeClaimsCount returns 0
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// Mock GetExistingNodeClaims returns one NodeClaim
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				}
				mockClient.CreateOrUpdateObjectInMap(nodeClaim)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeResourceManager(mockClient)
			ready, err := manager.ensureNvidiaDevicePluginsReady(context.Background(), tt.workspace)

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
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedNodes int
		expectedError bool
	}{
		{
			name: "Should return error when GetExistingNodeClaims fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("list failed"))
			},
			expectedNodes: 0,
			expectedError: true,
		},
		{
			name: "Should skip NodeClaim without assigned node",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock NodeClaim without NodeName
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "", // No node assigned
					},
				}
				mockClient.CreateOrUpdateObjectInMap(nodeClaim)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
			},
			expectedNodes: 0,
			expectedError: false,
		},
		{
			name: "Should skip when node doesn't exist",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock NodeClaim with NodeName
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "nonexistent-node",
					},
				}
				mockClient.CreateOrUpdateObjectInMap(nodeClaim)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock NodeClaim with NodeName
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				}

				// Create map and add NodeClaim
				nodeClaimList := &karpenterv1.NodeClaimList{}
				relevantMap := mockClient.CreateMapWithType(nodeClaimList)
				objKey := client.ObjectKeyFromObject(nodeClaim)
				relevantMap[objKey] = nodeClaim

				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock NodeClaim
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				}
				mockClient.CreateOrUpdateObjectInMap(nodeClaim)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock NodeClaim
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				}
				mockClient.CreateOrUpdateObjectInMap(nodeClaim)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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
					InstanceType: "Standard_NC12s_v3",
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock NodeClaim
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				}

				// Create map and add NodeClaim
				nodeClaimList := &karpenterv1.NodeClaimList{}
				relevantMap := mockClient.CreateMapWithType(nodeClaimList)
				objKey := client.ObjectKeyFromObject(nodeClaim)
				relevantMap[objKey] = nodeClaim

				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)

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

			manager := NewNodeResourceManager(mockClient)
			nodes, err := manager.getReadyNodesFromNodeClaims(context.Background(), tt.workspace)

			assert.Len(t, nodes, tt.expectedNodes)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetReadyNodesMatchingLabelSelector(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedNodes int
		expectedError bool
	}{
		{
			name: "Should return error when label selector is invalid",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "invalid",
								Operator: metav1.LabelSelectorOperator("invalid-operator"),
							},
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// No setup needed for invalid label selector
			},
			expectedNodes: 0,
			expectedError: true,
		},
		{
			name: "Should return error when node list fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))
			},
			expectedNodes: 0,
			expectedError: true,
		},
		{
			name: "Should return ready nodes without label selector",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: nil,
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create ready and not ready nodes
				readyNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ready-node",
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
				notReadyNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-ready-node",
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

				// Create map and add nodes
				nodeList := &corev1.NodeList{}
				relevantMap := mockClient.CreateMapWithType(nodeList)
				readyKey := client.ObjectKeyFromObject(readyNode)
				notReadyKey := client.ObjectKeyFromObject(notReadyNode)
				relevantMap[readyKey] = readyNode
				relevantMap[notReadyKey] = notReadyNode

				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			expectedNodes: 1, // Only ready node should be returned
			expectedError: false,
		},
		{
			name: "Should return ready nodes matching label selector",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create ready node with matching label
				matchingNode := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "matching-node",
						Labels: map[string]string{
							"app": "test",
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

				// Create map and add node
				nodeList := &corev1.NodeList{}
				relevantMap := mockClient.CreateMapWithType(nodeList)
				objKey := client.ObjectKeyFromObject(matchingNode)
				relevantMap[objKey] = matchingNode

				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			expectedNodes: 1,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeResourceManager(mockClient)
			nodes, err := manager.getReadyNodesMatchingLabelSelector(context.Background(), tt.workspace)

			assert.Len(t, nodes, tt.expectedNodes)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUpdateWorkspaceStatusIfNeeded(t *testing.T) {
	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setup         func(*test.MockClient)
		expectedError bool
	}{
		{
			name: "Should return error when getting ready nodes fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			setup: func(mockClient *test.MockClient) {
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))
			},
			expectedError: true,
		},
		{
			name: "Should return early when no updates needed",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{}, // Empty worker nodes
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeResourceStatus),
							Status: metav1.ConditionTrue,
							Reason: "ResourcesReady",
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Mock empty node list - no ready nodes found
				nodeList := &corev1.NodeList{}
				mockClient.CreateMapWithType(nodeList)
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			expectedError: false,
		},
		{
			name: "Should update when worker nodes differ",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					WorkerNodes: []string{"old-node"},
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeResourceStatus),
							Status: metav1.ConditionTrue,
							Reason: "ResourcesReady",
						},
					},
				},
			},
			setup: func(mockClient *test.MockClient) {
				// Create a different ready node
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "new-node",
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
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// Mock the retry update
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeResourceManager(mockClient)
			err := manager.updateWorkspaceStatusIfNeeded(context.Background(), tt.workspace)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUpdateWorkspaceStatusWithBothUpdates(t *testing.T) {
	tests := []struct {
		name                 string
		workerNodes          []string
		updateWorkerNodes    bool
		updateResourceStatus bool
		setup                func(*test.MockClient)
		expectedError        bool
	}{
		{
			name:                 "Should succeed with both updates",
			workerNodes:          []string{"node1", "node2"},
			updateWorkerNodes:    true,
			updateResourceStatus: true,
			setup: func(mockClient *test.MockClient) {
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedError: false,
		},
		{
			name:                 "Should succeed with only worker nodes update",
			workerNodes:          []string{"node1"},
			updateWorkerNodes:    true,
			updateResourceStatus: false,
			setup: func(mockClient *test.MockClient) {
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedError: false,
		},
		{
			name:                 "Should return early when workspace not found",
			workerNodes:          []string{"node1"},
			updateWorkerNodes:    true,
			updateResourceStatus: true,
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "workspaces"}, "test-workspace"))
			},
			expectedError: false,
		},
		{
			name:                 "Should return error when Get fails with non-NotFound error",
			workerNodes:          []string{"node1"},
			updateWorkerNodes:    true,
			updateResourceStatus: true,
			setup: func(mockClient *test.MockClient) {
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("api server error"))
			},
			expectedError: true,
		},
		{
			name:                 "Should return error when status update fails",
			workerNodes:          []string{"node1"},
			updateWorkerNodes:    true,
			updateResourceStatus: true,
			setup: func(mockClient *test.MockClient) {
				workspace := &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				}
				mockClient.CreateOrUpdateObjectInMap(workspace)
				mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				mockClient.StatusMock.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("update failed"))
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			tt.setup(mockClient)

			manager := NewNodeResourceManager(mockClient)
			nameKey := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
			err := manager.updateWorkspaceStatusWithBothUpdates(context.Background(), nameKey, tt.workerNodes, tt.updateWorkerNodes, tt.updateResourceStatus)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
