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
	"strings"
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCheckNodeClaims(t *testing.T) {
	// Define test cases in a table-driven approach
	testCases := []struct {
		name                       string
		workspace                  *kaitov1beta1.Workspace
		readyNodes                 []*corev1.Node
		setupMocks                 func(*test.MockClient)
		expectedAddedCount         int
		expectedExistingNodeClaims int
		expectedError              string
		featureFlagValue           bool
	}{
		{
			name: "get existing node claims fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			readyNodes: []*corev1.Node{}, // Empty ready nodes
			setupMocks: func(mockClient *test.MockClient) {
				// Mock NodeClaim list to fail
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("list nodeclaims failed"))
			},
			expectedAddedCount:         0,
			expectedExistingNodeClaims: 0,
			expectedError:              "failed to get existing NodeClaims",
			featureFlagValue:           false,
		},
		{
			name: "need to add node claims",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector:  &metav1.LabelSelector{},
					PreferredNodes: []string{},
				},
				Inference: &kaitov1beta1.InferenceSpec{}, // Need this for inference workload
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 3, // Target 3 nodes, no BYO = need 3 NodeClaims, have 1 = add 2
				},
			},
			readyNodes: []*corev1.Node{}, // Empty ready nodes - no ready nodes yet
			setupMocks: func(mockClient *test.MockClient) {
				// Mock existing NodeClaim list with 1 NodeClaim
				nodeClaim := &karpenterv1.NodeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      "test-workspace",
							kaitov1beta1.LabelWorkspaceNamespace: "default",
						},
					},
				}
				nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{*nodeClaim}}
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
					ncl := args.Get(1).(*karpenterv1.NodeClaimList)
					*ncl = *nodeClaimList
				}).Return(nil)
			},
			expectedAddedCount:         2, // Required 3, have 0 ready nodes without NodeClaim = need 3 NodeClaims
			expectedExistingNodeClaims: 1,
			expectedError:              "",
			featureFlagValue:           false,
		},
		{
			name: "node claims match target",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector:  &metav1.LabelSelector{},
					PreferredNodes: []string{},
				},
				Inference: &kaitov1beta1.InferenceSpec{}, // Need this for inference workload
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2, // Target 2, no BYO = need 2 NodeClaims, have 2 = perfect match
				},
			},
			readyNodes: []*corev1.Node{}, // Empty ready nodes - NodeClaims exist but not ready yet
			setupMocks: func(mockClient *test.MockClient) {
				// Mock existing NodeClaim list with 2 NodeClaims (matches target)
				nodeClaims := []karpenterv1.NodeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nodeclaim-1",
							Labels: map[string]string{
								kaitov1beta1.LabelWorkspaceName:      "test-workspace",
								kaitov1beta1.LabelWorkspaceNamespace: "default",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nodeclaim-2",
							Labels: map[string]string{
								kaitov1beta1.LabelWorkspaceName:      "test-workspace",
								kaitov1beta1.LabelWorkspaceNamespace: "default",
							},
						},
					},
				}
				nodeClaimList := &karpenterv1.NodeClaimList{Items: nodeClaims}
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
					ncl := args.Get(1).(*karpenterv1.NodeClaimList)
					*ncl = *nodeClaimList
				}).Return(nil)
			},
			expectedAddedCount:         0, // Target 2, have 0 ready nodes without NodeClaim = need 2 NodeClaims
			expectedExistingNodeClaims: 2,
			expectedError:              "",
			featureFlagValue:           false,
		},
		{
			name: "empty nodeclaim list with target nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector:  &metav1.LabelSelector{},
					PreferredNodes: []string{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			readyNodes: []*corev1.Node{}, // Empty ready nodes
			setupMocks: func(mockClient *test.MockClient) {
				// Mock empty NodeClaim list
				nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{}}
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
					ncl := args.Get(1).(*karpenterv1.NodeClaimList)
					*ncl = *nodeClaimList
				}).Return(nil)
			},
			expectedAddedCount:         1, // Target 1, have 0 = add 1
			expectedExistingNodeClaims: 0,
			expectedError:              "",
			featureFlagValue:           false,
		},
		{
			name: "mixed scenario: BYO nodes and target nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector:  &metav1.LabelSelector{},
					PreferredNodes: []string{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 3, // Target 3: have 1 BYO ready node = need 2 more NodeClaims
				},
			},
			readyNodes: []*corev1.Node{
				// 1 ready BYO node (does not have karpenter.sh/nodepool label)
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-ready-node-1",
						Labels: map[string]string{
							// No karpenter.sh/nodepool label - this is a BYO node
							"node.kubernetes.io/instance-type": "Standard_NC24ads_A100_v4",
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
				},
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock 2 NodeClaims: 1 that has a ready node, 1 that is still pending
				nodeClaims := []karpenterv1.NodeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nodeclaim-ready", // This one has a ready node
							Labels: map[string]string{
								kaitov1beta1.LabelWorkspaceName:      "test-workspace",
								kaitov1beta1.LabelWorkspaceNamespace: "default",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nodeclaim-pending", // This one is still pending
							Labels: map[string]string{
								kaitov1beta1.LabelWorkspaceName:      "test-workspace",
								kaitov1beta1.LabelWorkspaceNamespace: "default",
							},
						},
					},
				}
				nodeClaimList := &karpenterv1.NodeClaimList{Items: nodeClaims}
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
					ncl := args.Get(1).(*karpenterv1.NodeClaimList)
					*ncl = *nodeClaimList
				}).Return(nil)
			},
			expectedAddedCount:         0, // Target 3, have 1 BYO ready node = need 2 more NodeClaims
			expectedExistingNodeClaims: 2,
			expectedError:              "",
			featureFlagValue:           false,
		},
	}

	// Run all test cases using a for loop
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up feature flag
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tc.featureFlagValue
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

			// Set up mocks
			mockClient := test.NewClient()
			mockRecorder := record.NewFakeRecorder(100)
			expectations := utils.NewControllerExpectations()
			manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

			// Set up test-specific mocks
			if tc.setupMocks != nil {
				tc.setupMocks(mockClient)
			}

			// Execute the function under test
			addedCount, existingNodeClaims, err := manager.CheckNodeClaims(context.Background(), tc.workspace, tc.readyNodes)

			// Assertions
			assert.Equal(t, tc.expectedAddedCount, addedCount, "Added count mismatch")
			assert.Equal(t, tc.expectedExistingNodeClaims, len(existingNodeClaims), "Existing NodeClaims count mismatch")

			if tc.expectedError != "" {
				assert.Error(t, err, "Expected error but got none")
				assert.Contains(t, err.Error(), tc.expectedError, "Error message mismatch")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			// Verify mock expectations
			mockClient.AssertExpectations(t)
		})
	}
}

// Helper function to setup workspace status mocks
func setupWorkspaceStatusMock(mockClient *test.MockClient, workspace *kaitov1beta1.Workspace, statusUpdateError error) {
	// Mock Get call for status update
	mockClient.On("Get", mock.IsType(context.Background()), mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
		ws := args.Get(2).(*kaitov1beta1.Workspace)
		*ws = *workspace
	}).Return(nil).Maybe()

	// Mock List call for nodes (needed by GetBYOAndReadyNodes)
	mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
		nodeList := args.Get(1).(*corev1.NodeList)
		nodeList.Items = []corev1.Node{} // Return empty node list for test
	}).Return(nil).Maybe()

	// Mock status update
	mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
	if statusUpdateError != nil {
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(statusUpdateError).Maybe()
	} else {
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
	}
}

func TestCreateNodeClaims(t *testing.T) {
	// Helper function to setup common mocks
	setupBaseMocks := func(mockClient *test.MockClient, workspace *kaitov1beta1.Workspace, statusUpdateError error) {
		// Mock Get call for status update
		mockClient.On("Get", mock.IsType(context.Background()), mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update - the Status() method returns a StatusMock, so we need to mock that
		// Use Maybe() to handle potential multiple calls
		mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
		if statusUpdateError != nil {
			mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(statusUpdateError).Maybe()
		} else {
			mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
		}
	}

	// Define test cases in a table-driven approach
	testCases := []struct {
		name           string
		workspace      *kaitov1beta1.Workspace
		nodesToCreate  int
		setupMocks     func(*test.MockClient)
		expectedError  string
		expectedEvents []string
		presetWithDisk bool
	}{
		{
			name: "successful create with single nodeclaim",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			nodesToCreate: 1,
			setupMocks: func(mockClient *test.MockClient) {
				setupBaseMocks(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						LabelSelector: &metav1.LabelSelector{},
					},
				}, nil)

				// Mock NodeClaim creation
				mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			expectedError:  "",
			expectedEvents: []string{"NodeClaimCreated"},
			presetWithDisk: false,
		},
		{
			name: "successful create with multiple nodeclaims",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			nodesToCreate: 3,
			setupMocks: func(mockClient *test.MockClient) {
				setupBaseMocks(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						LabelSelector: &metav1.LabelSelector{},
					},
				}, nil)

				// Mock NodeClaim creation (3 times)
				mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Times(3)
			},
			expectedError:  "",
			expectedEvents: []string{"NodeClaimCreated", "NodeClaimCreated", "NodeClaimCreated"},
			presetWithDisk: false,
		},
		{
			name: "workspace without preset - uses default disk size",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
				Inference: &kaitov1beta1.InferenceSpec{
					// No preset specified, should use default disk size
				},
			},
			nodesToCreate: 1,
			setupMocks: func(mockClient *test.MockClient) {
				setupBaseMocks(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						LabelSelector: &metav1.LabelSelector{},
					},
					Inference: &kaitov1beta1.InferenceSpec{},
				}, nil)

				// Mock NodeClaim creation
				mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			expectedError:  "",
			expectedEvents: []string{"NodeClaimCreated"},
			presetWithDisk: false,
		},
		{
			name: "status update fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			nodesToCreate: 1,
			setupMocks: func(mockClient *test.MockClient) {
				setupBaseMocks(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						LabelSelector: &metav1.LabelSelector{},
					},
				}, errors.New("status update failed"))
			},
			expectedError:  "failed to update NodeClaim status condition",
			expectedEvents: []string{},
			presetWithDisk: false,
		},
		{
			name: "nodeclaim creation fails for some nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			nodesToCreate: 2,
			setupMocks: func(mockClient *test.MockClient) {
				setupBaseMocks(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						LabelSelector: &metav1.LabelSelector{},
					},
				}, nil)

				// Mock NodeClaim creation - first succeeds, second fails
				mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Once()
				mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("creation failed")).Once()
			},
			expectedError:  "",
			expectedEvents: []string{"NodeClaimCreated", "NodeClaimCreationFailed"},
			presetWithDisk: false,
		},
		{
			name: "zero nodes to create",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			nodesToCreate:  0,
			expectedError:  "",
			expectedEvents: []string{},
			presetWithDisk: false,
		},
	}

	// Run all test cases using a for loop
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mocks
			mockClient := test.NewClient()
			mockRecorder := record.NewFakeRecorder(100)
			expectations := utils.NewControllerExpectations()
			manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

			// Set up test-specific mocks
			if tc.setupMocks != nil {
				tc.setupMocks(mockClient)
			}

			// Execute the function under test
			err := manager.CreateUpNodeClaims(context.Background(), tc.workspace, tc.nodesToCreate)

			if tc.expectedError != "" {
				assert.Error(t, err, "Expected error but got none")
				assert.Contains(t, err.Error(), tc.expectedError, "Error message mismatch")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			// Verify events were recorded correctly
			close(mockRecorder.Events)
			recordedEvents := []string{}
			for event := range mockRecorder.Events {
				// Extract the event reason from the event string
				// Event format is typically "Warning|Normal <reason> <message>"
				parts := strings.Fields(event)
				if len(parts) >= 2 {
					recordedEvents = append(recordedEvents, parts[1])
				}
			}

			assert.ElementsMatch(t, tc.expectedEvents, recordedEvents, "Event recording mismatch")

			// Verify mock expectations
			mockClient.AssertExpectations(t)
		})
	}
}

func TestSetNodeClaimsReadyCondition_SetsToTrue(t *testing.T) {
	// Helper function to create NodeClaim with ready condition
	createNodeClaim := func(name string, isReady bool, isDeleting bool) *karpenterv1.NodeClaim {
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: karpenterv1.NodeClaimStatus{
				NodeName: "test-node-" + name,
			},
		}

		if isDeleting {
			now := metav1.Now()
			nodeClaim.DeletionTimestamp = &now
		}

		if isReady {
			nodeClaim.Status.Conditions = []status.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			}
		} else {
			nodeClaim.Status.Conditions = []status.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionFalse,
				},
			}
		}

		return nodeClaim
	}

	// Define test cases specifically for verifying condition is set to True
	testCases := []struct {
		name               string
		workspace          *kaitov1beta1.Workspace
		existingNodeClaims []*karpenterv1.NodeClaim
		setupMocks         func(*test.MockClient)
		expectedReady      bool
		expectedError      string
	}{
		{
			name: "Should set NodeClaimsReady condition to true when enough ready node claims",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", true, false),
				createNodeClaim("nodeclaim-2", true, false),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 2},
				}, nil)

				// Specifically verify the condition is set to True with correct reason
				mockClient.StatusMock.On("Update", mock.Anything, mock.MatchedBy(func(ws *kaitov1beta1.Workspace) bool {
					// Find the NodeClaimStatus condition and verify it's set to True
					for _, condition := range ws.Status.Conditions {
						if condition.Type == string(kaitov1beta1.ConditionTypeNodeClaimStatus) {
							return condition.Status == metav1.ConditionTrue && condition.Reason == "NodeClaimsReady"
						}
					}
					return false
				}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady: true,
			expectedError: "",
		},
	}

	// Run all test cases using a for loop
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mocks
			mockClient := test.NewClient()
			mockRecorder := record.NewFakeRecorder(100)
			expectations := utils.NewControllerExpectations()
			manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

			// Set up test-specific mocks
			if tc.setupMocks != nil {
				tc.setupMocks(mockClient)
			}

			// Execute the function under test
			ready, err := manager.EnsureNodeClaimsReady(context.Background(), tc.workspace, []*corev1.Node{}, tc.existingNodeClaims)

			// Assertions
			assert.Equal(t, tc.expectedReady, ready, "Ready state mismatch")

			if tc.expectedError != "" {
				assert.Error(t, err, "Expected error but got none")
				assert.Contains(t, err.Error(), tc.expectedError, "Error message mismatch")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			// Verify mock expectations
			mockClient.AssertExpectations(t)
		})
	}
}

func TestAreNodeClaimsReady(t *testing.T) {
	// Helper function to create NodeClaim with ready condition
	createNodeClaim := func(name string, isReady bool, isDeleting bool) *karpenterv1.NodeClaim {
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: karpenterv1.NodeClaimStatus{
				NodeName: "test-node-" + name,
			},
		}

		if isDeleting {
			now := metav1.Now()
			nodeClaim.DeletionTimestamp = &now
		}

		if isReady {
			nodeClaim.Status.Conditions = []status.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			}
		} else {
			nodeClaim.Status.Conditions = []status.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionFalse,
				},
			}
		}

		return nodeClaim
	}

	// Define test cases in a table-driven approach
	testCases := []struct {
		name               string
		workspace          *kaitov1beta1.Workspace
		existingNodeClaims []*karpenterv1.NodeClaim
		setupMocks         func(*test.MockClient)
		expectedReady      bool // true means ready, false means not ready (needs more waiting)
		expectedError      string
	}{
		{
			name: "enough ready node claims - should return ready (true)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", true, false),
				createNodeClaim("nodeclaim-2", true, false),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 2},
				}, nil)
			},
			expectedReady: true, // true means ready
			expectedError: "",
		},
		{
			name: "not enough ready node claims - should return not ready (false)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 3,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", true, false),
				createNodeClaim("nodeclaim-2", false, false), // not ready
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 3},
				}, nil)
			},
			expectedReady: false, // false means not ready
			expectedError: "",
		},
		{
			name: "some node claims are being deleted - should not count them",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", true, false),  // ready and not deleting
				createNodeClaim("nodeclaim-2", true, true),   // ready but deleting - should not count
				createNodeClaim("nodeclaim-3", false, false), // not ready
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 2},
				}, nil)
			},
			expectedReady: false, // false means not ready (only 1 ready, need 2)
			expectedError: "",
		},
		{
			name: "zero target node count - should always be ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 0,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", false, false),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 0},
				}, nil)
			},
			expectedReady: true, // true means ready
			expectedError: "",
		},
		{
			name: "status update fails when ready - should return error",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 1,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", true, false),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls with error
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 1},
				}, errors.New("status update failed"))
			},
			expectedReady: false,
			expectedError: "failed to update NodeClaim status condition(NodeClaimsReady)",
		},
		{
			name: "status update fails when not ready - should return error",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					TargetNodeCount: 2,
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("nodeclaim-1", true, false),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock workspace Get and Status update calls with error
				setupWorkspaceStatusMock(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 2},
				}, errors.New("status update failed"))
			},
			expectedReady: false,
			expectedError: "failed to update NodeClaim status condition(NodeClaimNotReady)",
		},
	}

	// Run all test cases using a for loop
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mocks
			mockClient := test.NewClient()
			mockRecorder := record.NewFakeRecorder(100)
			expectations := utils.NewControllerExpectations()
			manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

			// Set up test-specific mocks
			if tc.setupMocks != nil {
				tc.setupMocks(mockClient)
			}

			// Execute the function under test
			ready, err := manager.EnsureNodeClaimsReady(context.Background(), tc.workspace, []*corev1.Node{}, tc.existingNodeClaims)

			// Assertions
			assert.Equal(t, tc.expectedReady, ready, "Ready state mismatch")

			if tc.expectedError != "" {
				assert.Error(t, err, "Expected error but got none")
				assert.Contains(t, err.Error(), tc.expectedError, "Error message mismatch")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}

			// Verify mock expectations
			mockClient.AssertExpectations(t)
		})
	}
}

func TestDetermineNodeOSDiskSize(t *testing.T) {
	tests := []struct {
		name             string
		workspace        *kaitov1beta1.Workspace
		expectedDiskSize string
	}{
		{
			name: "Should return default disk size when workspace has no inference spec",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			expectedDiskSize: "1024Gi",
		},
		{
			name: "Should return default disk size when inference spec has no preset",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
				Inference: &kaitov1beta1.InferenceSpec{
					// No preset specified
				},
			},
			expectedDiskSize: "1024Gi",
		},
		{
			name: "Should return default disk size when preset name is empty",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "", // Empty preset name
						},
					},
				},
			},
			expectedDiskSize: "1024Gi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := test.NewClient()
			mockRecorder := record.NewFakeRecorder(100)
			expectations := utils.NewControllerExpectations()
			manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

			diskSize := manager.determineNodeOSDiskSize(context.Background(), tt.workspace)

			// When no preset is specified, we expect the default
			assert.Equal(t, tt.expectedDiskSize, diskSize)
		})
	}
}
