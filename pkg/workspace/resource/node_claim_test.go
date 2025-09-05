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
	"time"

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

func TestDiffNodeClaims(t *testing.T) {
	// Define test cases in a table-driven approach
	testCases := []struct {
		name                       string
		workspace                  *kaitov1beta1.Workspace
		expectationsSatisfied      bool
		setupMocks                 func(*test.MockClient)
		expectedReady              bool
		expectedAddedCount         int
		expectedDeletedCount       int
		expectedExistingNodeClaims int
		expectedError              string
		featureFlagValue           bool
	}{
		{
			name: "expectations not satisfied",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			expectationsSatisfied:      false,
			expectedReady:              false,
			expectedAddedCount:         0,
			expectedDeletedCount:       0,
			expectedExistingNodeClaims: 0,
			expectedError:              "",
			featureFlagValue:           false,
		},
		{
			name: "get required node claims fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{},
				},
			},
			expectationsSatisfied: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock GetRequiredNodeClaimsCount to fail (mock node list to fail)
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list nodes failed"))
			},
			expectedReady:              false,
			expectedAddedCount:         0,
			expectedDeletedCount:       0,
			expectedExistingNodeClaims: 0,
			expectedError:              "failed to get required NodeClaims",
			featureFlagValue:           false,
		},
		{
			name: "get existing node claims fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector:  &metav1.LabelSelector{},
					PreferredNodes: []string{},
				},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 2,
					},
				},
			},
			expectationsSatisfied: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock node list to succeed (for GetRequiredNodeClaimsCount)
				nodeList := &corev1.NodeList{Items: []corev1.Node{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

				// Mock NodeClaim list to fail (for GetExistingNodeClaims)
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("list nodeclaims failed"))
			},
			expectedReady:              false,
			expectedAddedCount:         0,
			expectedDeletedCount:       0,
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
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 3, // Target 3 nodes, no BYO = need 3 NodeClaims, have 1 = add 2
					},
				},
			},
			expectationsSatisfied: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock empty node list (no BYO nodes)
				nodeList := &corev1.NodeList{Items: []corev1.Node{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

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
			expectedReady:              true,
			expectedAddedCount:         2, // Required 3, have 1 = add 2
			expectedDeletedCount:       0,
			expectedExistingNodeClaims: 1,
			expectedError:              "",
			featureFlagValue:           false,
		},
		{
			name: "need to delete node claims",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector:  &metav1.LabelSelector{},
					PreferredNodes: []string{},
				},
				Inference: &kaitov1beta1.InferenceSpec{}, // Need this for inference workload
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 1, // Target 1, no BYO = need 1 NodeClaim, have 3 = delete 2
					},
				},
			},
			expectationsSatisfied: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock empty node list (no BYO nodes)
				nodeList := &corev1.NodeList{Items: []corev1.Node{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

				// Mock existing NodeClaim list with 3 NodeClaims
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
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nodeclaim-3",
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
			expectedReady:              true,
			expectedAddedCount:         0,
			expectedDeletedCount:       2, // Required 1, have 3 = delete 2
			expectedExistingNodeClaims: 3,
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
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 2, // Target 2, no BYO = need 2 NodeClaims, have 2 = perfect match
					},
				},
			},
			expectationsSatisfied: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock empty node list (no BYO nodes)
				nodeList := &corev1.NodeList{Items: []corev1.Node{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

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
			expectedReady:              true,
			expectedAddedCount:         0,
			expectedDeletedCount:       0,
			expectedExistingNodeClaims: 2,
			expectedError:              "",
			featureFlagValue:           false,
		},
		{
			name: "BYO mode no node claims needed",
			workspace: &kaitov1beta1.Workspace{
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
			},
			expectationsSatisfied: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock empty node list (for ResolveReadyNodesAndRequiredNodeCount -> GetBYOAndReadyNodes -> ListNodes)
				nodeList := &corev1.NodeList{Items: []corev1.Node{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)

				// Mock empty NodeClaim list
				nodeClaimList := &karpenterv1.NodeClaimList{Items: []karpenterv1.NodeClaim{}}
				mockClient.On("List", mock.Anything, mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
					ncl := args.Get(1).(*karpenterv1.NodeClaimList)
					*ncl = *nodeClaimList
				}).Return(nil)
			},
			expectedReady:              true,
			expectedAddedCount:         0,
			expectedDeletedCount:       0,
			expectedExistingNodeClaims: 0,
			expectedError:              "",
			featureFlagValue:           true, // BYO mode
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

			// Set expectations
			if !tc.expectationsSatisfied {
				workspaceKey := client.ObjectKeyFromObject(tc.workspace).String()
				expectations.ExpectCreations(manager.logger, workspaceKey, 1)
			}

			// Set up test-specific mocks
			if tc.setupMocks != nil {
				tc.setupMocks(mockClient)
			}

			// Execute the function under test
			ready, addedCount, deletedCount, existingNodeClaims, _, err := manager.DiffNodeClaims(context.Background(), tc.workspace)

			// Assertions
			assert.Equal(t, tc.expectedReady, ready, "Ready status mismatch")
			assert.Equal(t, tc.expectedAddedCount, addedCount, "Added count mismatch")
			assert.Equal(t, tc.expectedDeletedCount, deletedCount, "Deleted count mismatch")
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

func TestScaleUpNodeClaims(t *testing.T) {
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
		expectedReady  bool
		expectedError  string
		expectedEvents []string
		presetWithDisk bool
	}{
		{
			name: "successful scale up with single node",
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
			expectedReady:  true,
			expectedError:  "",
			expectedEvents: []string{"NodeClaimCreated"},
			presetWithDisk: false,
		},
		{
			name: "successful scale up with multiple nodes",
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
			expectedReady:  true,
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
			expectedReady:  true,
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
			expectedReady:  false,
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
			expectedReady:  true,
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
			nodesToCreate: 0,
			setupMocks: func(mockClient *test.MockClient) {
				setupBaseMocks(mockClient, &kaitov1beta1.Workspace{
					ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
					Resource: kaitov1beta1.ResourceSpec{
						LabelSelector: &metav1.LabelSelector{},
					},
				}, nil)
				// No NodeClaim creation should happen
			},
			expectedReady:  true,
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
			ready, err := manager.ScaleUpNodeClaims(context.Background(), tc.workspace, tc.nodesToCreate)

			// Assertions
			assert.Equal(t, tc.expectedReady, ready, "Ready status mismatch")

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

func TestMeetReadyNodeClaimsTarget(t *testing.T) {
	// Helper function to create a NodeClaim with specified ready state
	createNodeClaim := func(name string, ready bool, deleting bool, hasNodeName bool) *karpenterv1.NodeClaim {
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
			Status: karpenterv1.NodeClaimStatus{},
		}

		if deleting {
			now := metav1.Now()
			nodeClaim.DeletionTimestamp = &now
		}

		if ready {
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

		if hasNodeName {
			nodeClaim.Status.NodeName = "node-" + name
		}

		return nodeClaim
	}

	// Define test cases in a table-driven approach
	testCases := []struct {
		name                    string
		workspace               *kaitov1beta1.Workspace
		existingNodeClaims      []*karpenterv1.NodeClaim
		setupMocks              func(*test.MockClient)
		expectedReady           bool
		expectedError           string
		expectedConditionType   string
		expectedConditionStatus metav1.ConditionStatus
		expectedReason          string
	}{
		{
			name: "target_met_with_default_count",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status:     kaitov1beta1.WorkspaceStatus{
					// No inference status = default target count of 1
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim", true, false, true),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock Get call for UpdateStatusConditionIfNotMatch
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				// Mock Status().Update() call for UpdateStatusConditionIfNotMatch
				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           true,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "NodeClaimsReady",
		},
		{
			name: "target_met_with_explicit_count",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 3,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim-1", true, false, true),
				createNodeClaim("ready-claim-2", true, false, true),
				createNodeClaim("ready-claim-3", true, false, true),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock successful status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           true,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "NodeClaimsReady",
		},
		{
			name: "target_exceeded",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 2,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim-1", true, false, true),
				createNodeClaim("ready-claim-2", true, false, true),
				createNodeClaim("ready-claim-3", true, false, true), // Extra ready claim
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock successful status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           true,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "NodeClaimsReady",
		},
		{
			name: "target_not_met_insufficient_ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 3,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim", true, false, true),
				createNodeClaim("not-ready-claim", false, false, false),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock successful status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           false,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          "NodeClaimNotReady",
		},
		{
			name: "ignores_deleting_nodeclaims",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 2,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim", true, false, true),
				createNodeClaim("deleting-claim", true, true, true), // Ready but deleting - should be ignored
				createNodeClaim("another-ready-claim", true, false, true),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock successful status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           true,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "NodeClaimsReady",
		},
		{
			name: "considers_nodeclaim_with_nodename_ready",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 1,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				func() *karpenterv1.NodeClaim {
					nodeClaim := &karpenterv1.NodeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nodename-claim",
							Labels: map[string]string{
								kaitov1beta1.LabelWorkspaceName:      "test-workspace",
								kaitov1beta1.LabelWorkspaceNamespace: "default",
							},
						},
						Status: karpenterv1.NodeClaimStatus{
							NodeName: "node-nodename-claim", // Has NodeName but NO Ready condition
						},
					}
					return nodeClaim
				}(),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock successful status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           true,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          "NodeClaimsReady",
		},
		{
			name: "status_update_fails_ready_condition",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 1,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim", true, false, true),
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock failing status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(errors.New("status update failed")).Maybe()
			},
			expectedReady: false,
			expectedError: "failed to update NodeClaim status condition(NodeClaimsReady): status update failed",
		},
		{
			name: "status_update_fails_not_ready_condition",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 2,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("ready-claim", true, false, true), // Only 1 ready, need 2
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock failing status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(errors.New("status update failed")).Maybe()
			},
			expectedReady: false,
			expectedError: "failed to update NodeClaim status condition(NodeClaimNotReady): status update failed",
		},
		{
			name: "empty_nodeclaims_list",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Inference: &kaitov1beta1.InferenceStatus{
						TargetNodeCount: 1,
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock successful status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedReady:           false,
			expectedError:           "",
			expectedConditionType:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          "NodeClaimNotReady",
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
			ready, err := manager.MeetReadyNodeClaimsTarget(context.Background(), tc.workspace, tc.existingNodeClaims)

			// Assertions
			assert.Equal(t, tc.expectedReady, ready, "Ready status mismatch")

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

func TestScaleDownNodeClaims(t *testing.T) {
	// Helper function to create a NodeClaim with specified properties
	createNodeClaim := func(name string, ready bool, deleting bool, hasNodeName bool, creationTime metav1.Time) *karpenterv1.NodeClaim {
		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: creationTime,
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
			Status: karpenterv1.NodeClaimStatus{},
		}

		if deleting {
			now := metav1.Now()
			nodeClaim.DeletionTimestamp = &now
		}

		if ready {
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

		if hasNodeName {
			nodeClaim.Status.NodeName = "node-" + name
		}

		return nodeClaim
	}

	// Define test cases in a table-driven approach
	testCases := []struct {
		name                      string
		workspace                 *kaitov1beta1.Workspace
		existingNodeClaims        []*karpenterv1.NodeClaim
		nodesToDelete             int
		setupMocks                func(*test.MockClient)
		expectedError             string
		expectedDeletedNodeClaims []string
		hasExistingCondition      bool
	}{
		{
			name: "no_nodes_to_delete_without_existing_condition",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status:     kaitov1beta1.WorkspaceStatus{Conditions: []metav1.Condition{}},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-1", true, false, true, metav1.Time{}),
			},
			nodesToDelete: 0,
			setupMocks: func(mockClient *test.MockClient) {
				// No status update expected when no existing condition
			},
			expectedError: "",
		},
		{
			name: "no_nodes_to_delete_with_existing_condition",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.ConditionTypeScalingDownStatus),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-1", true, false, true, metav1.Time{}),
			},
			nodesToDelete:        0,
			hasExistingCondition: true,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock status update for completion - may be called multiple times due to retry logic
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()
			},
			expectedError: "",
		},
		{
			name: "successful_scale_down_single_node",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-1", true, false, true, metav1.Time{}),
			},
			nodesToDelete: 1,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock status update for scaling down
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock pod list for HasPodRunningOnNode check (no pods running)
				emptyPodList := &corev1.PodList{Items: []corev1.Pod{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.PodList{}), mock.Anything).Run(func(args mock.Arguments) {
					pl := args.Get(1).(*corev1.PodList)
					*pl = *emptyPodList
				}).Return(nil).Maybe()

				// Mock NodeClaim deletion
				mockClient.On("Delete", mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Maybe()
			},
			expectedError:             "",
			expectedDeletedNodeClaims: []string{"claim-1"},
		},
		{
			name: "nodes_with_pods_are_not_deleted",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-with-pods", true, false, true, metav1.Time{}),
				createNodeClaim("claim-without-pods", true, false, true, metav1.Time{}),
			},
			nodesToDelete: 2,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock pod list - different responses for different calls
				call := 0
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.PodList{}), mock.Anything).Run(func(args mock.Arguments) {
					pl := args.Get(1).(*corev1.PodList)
					call++
					// Return pods for first call, no pods for second call
					if call == 1 {
						pl.Items = []corev1.Pod{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "test-pod",
									Namespace: "default",
									Labels: map[string]string{
										kaitov1beta1.LabelWorkspaceName:      "test-workspace",
										kaitov1beta1.LabelWorkspaceNamespace: "default",
									},
								},
								Spec: corev1.PodSpec{NodeName: "node-claim-with-pods"},
							},
						}
					} else {
						pl.Items = []corev1.Pod{}
					}
				}).Return(nil).Maybe()

				// Mock NodeClaim deletion only for the node without pods
				mockClient.On("Delete", mock.Anything, mock.MatchedBy(func(nc *karpenterv1.NodeClaim) bool {
					return nc.Name == "claim-without-pods"
				}), mock.Anything).Return(nil).Maybe()
			},
			expectedError:             "not enough NodeClaims can be deleted because some NodeClaims still have pods running or are being deleted",
			expectedDeletedNodeClaims: []string{"claim-without-pods"}, // Only the one without pods
		},
		{
			name: "already_deleting_nodes_skip_deletion",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("deleting-claim", true, true, true, metav1.Time{}), // Already deleting
				createNodeClaim("normal-claim", true, false, true, metav1.Time{}),
			},
			nodesToDelete: 2,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock pod list (no pods running)
				emptyPodList := &corev1.PodList{Items: []corev1.Pod{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.PodList{}), mock.Anything).Run(func(args mock.Arguments) {
					pl := args.Get(1).(*corev1.PodList)
					*pl = *emptyPodList
				}).Return(nil).Maybe()

				// Mock NodeClaim deletion only for the non-deleting node
				mockClient.On("Delete", mock.Anything, mock.MatchedBy(func(nc *karpenterv1.NodeClaim) bool {
					return nc.Name == "normal-claim"
				}), mock.Anything).Return(nil).Maybe()
			},
			expectedError:             "",
			expectedDeletedNodeClaims: []string{"normal-claim"}, // Only the non-deleting one
		},
		{
			name: "scale_down_multiple_nodes_with_sorting",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-ready-old", true, false, true, metav1.Time{Time: time.Now().Add(-2 * time.Hour)}),
				createNodeClaim("claim-not-ready-new", false, false, true, metav1.Time{Time: time.Now().Add(-1 * time.Hour)}),
				createNodeClaim("claim-ready-new", true, false, true, metav1.Time{Time: time.Now()}),
			},
			nodesToDelete: 2,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock pod list for HasPodRunningOnNode check (no pods running)
				emptyPodList := &corev1.PodList{Items: []corev1.Pod{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.PodList{}), mock.Anything).Run(func(args mock.Arguments) {
					pl := args.Get(1).(*corev1.PodList)
					*pl = *emptyPodList
				}).Return(nil).Maybe()

				// Mock NodeClaim deletion for multiple nodes
				mockClient.On("Delete", mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Maybe()
			},
			expectedError:             "",
			expectedDeletedNodeClaims: []string{"claim-not-ready-new", "claim-ready-new"}, // Not ready first, then newest
		},
		{
			name: "status_update_fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-1", true, false, true, metav1.Time{}),
			},
			nodesToDelete: 1,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock failing status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(errors.New("status update failed")).Maybe()
			},
			expectedError: "failed to update scaling down status condition(ScalingDownNodeClaims): status update failed",
		},
		{
			name: "nodeclaim_deletion_fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			},
			existingNodeClaims: []*karpenterv1.NodeClaim{
				createNodeClaim("claim-1", true, false, true, metav1.Time{}),
			},
			nodesToDelete: 1,
			setupMocks: func(mockClient *test.MockClient) {
				// Mock status update
				mockClient.On("Get", mock.Anything, mock.IsType(client.ObjectKey{}), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
					ws := args.Get(2).(*kaitov1beta1.Workspace)
					ws.ObjectMeta = metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
				}).Return(nil).Maybe()

				mockClient.On("Status").Return(&mockClient.StatusMock).Maybe()
				mockClient.StatusMock.On("Update", mock.Anything, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Maybe()

				// Mock pod list (no pods running)
				emptyPodList := &corev1.PodList{Items: []corev1.Pod{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.PodList{}), mock.Anything).Run(func(args mock.Arguments) {
					pl := args.Get(1).(*corev1.PodList)
					*pl = *emptyPodList
				}).Return(nil).Maybe()

				// Mock NodeClaim deletion failure
				mockClient.On("Delete", mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("deletion failed")).Maybe()
			},
			expectedError:             "",
			expectedDeletedNodeClaims: []string{}, // No successful deletions
		},
	}

	// Run all test cases using a for loop
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mocks
			mockClient := test.NewClient()
			mockRecorder := record.NewFakeRecorder(10)
			expectations := utils.NewControllerExpectations()
			manager := NewNodeClaimManager(mockClient, mockRecorder, expectations)

			// Setup test-specific mocks
			tc.setupMocks(mockClient)

			// Execute the function under test
			err := manager.ScaleDownNodeClaims(context.Background(), tc.workspace, tc.existingNodeClaims, tc.nodesToDelete)

			if tc.expectedError == "" {
				assert.NoError(t, err, "Expected no error")
			} else {
				assert.Error(t, err, "Expected error")
				assert.Contains(t, err.Error(), tc.expectedError, "Error message mismatch")
			}

			// Verify mock expectations
			mockClient.AssertExpectations(t)

			// Check recorded events
			close(mockRecorder.Events)
			recordedEvents := []string{}
			for event := range mockRecorder.Events {
				if strings.Contains(event, "NodeClaimDeleted") {
					recordedEvents = append(recordedEvents, "NodeClaimDeleted")
				} else if strings.Contains(event, "NodeClaimDeletionFailed") {
					recordedEvents = append(recordedEvents, "NodeClaimDeletionFailed")
				}
			}

			// Verify expected deletions happened based on successful deletion events
			expectedSuccessfulDeletions := len(tc.expectedDeletedNodeClaims)
			actualSuccessfulDeletions := 0
			for _, event := range recordedEvents {
				if event == "NodeClaimDeleted" {
					actualSuccessfulDeletions++
				}
			}

			if tc.expectedError == "" && tc.nodesToDelete > 0 && len(tc.expectedDeletedNodeClaims) > 0 {
				assert.Equal(t, expectedSuccessfulDeletions, actualSuccessfulDeletions, "Number of successful deletions mismatch")
			}
		})
	}
}
