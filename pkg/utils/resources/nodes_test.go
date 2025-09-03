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

package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestUpdateNodeWithLabel(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		nodeObj       *corev1.Node
		expectedError error
	}{
		"Fail to update node because node cannot be updated": {
			callMocks: func(c *test.MockClient) {
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(errors.New("Cannot update node"))
			},
			nodeObj:       &corev1.Node{},
			expectedError: errors.New("Cannot update node"),
		},
		"Successfully updates node": {
			callMocks: func(c *test.MockClient) {
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			nodeObj:       &corev1.Node{},
			expectedError: nil,
		},
		"Skip update node because it already has the label": {
			callMocks: func(c *test.MockClient) {},
			nodeObj: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mockNode",
					Labels: map[string]string{
						LabelKeyNvidia: LabelValueNvidia,
					},
				},
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			err := UpdateNodeWithLabel(context.Background(), tc.nodeObj, "fakeKey", "fakeVal", mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestListNodes(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Fails to list nodes": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("Cannot retrieve node list"))
			},
			expectedError: errors.New("Cannot retrieve node list"),
		},
		"Successfully lists all nodes": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range test.MockNodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			labelSelector := client.MatchingLabels{}
			nodeList, err := ListNodes(context.Background(), mockClient, labelSelector)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
				assert.Check(t, nodeList != nil, "Response node list should not be nil")
				assert.Check(t, nodeList.Items != nil, "Response node list items should not be nil")
				assert.Check(t, len(nodeList.Items) == 3, "Response should contain 3 nodes")

			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestCheckNvidiaPlugin(t *testing.T) {
	testcases := map[string]struct {
		nodeObj        *corev1.Node
		isNvidiaPlugin bool
	}{
		"Is not nvidia plugin": {
			nodeObj:        &test.MockNodeList.Items[1],
			isNvidiaPlugin: false,
		},
		"Is nvidia plugin": {
			nodeObj:        &test.MockNodeList.Items[0],
			isNvidiaPlugin: true,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			result := CheckNvidiaPlugin(context.Background(), tc.nodeObj)

			assert.Equal(t, result, tc.isNvidiaPlugin)
		})
	}
}

func TestNodeIsReadyAndNotDeleting(t *testing.T) {
	t.Run("Should return true for ready node without deletion timestamp", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "ready-node",
				DeletionTimestamp: nil,
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

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == true, "Expected ready node without deletion timestamp to return true")
	})

	t.Run("Should return false for node with deletion timestamp", func(t *testing.T) {
		now := metav1.Now()
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deleting-node",
				DeletionTimestamp: &now,
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

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node with deletion timestamp to return false")
	})

	t.Run("Should return false for not ready node", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "not-ready-node",
				DeletionTimestamp: nil,
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

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected not ready node to return false")
	})

	t.Run("Should return false for node with unknown ready status", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "unknown-ready-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionUnknown,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node with unknown ready status to return false")
	})

	t.Run("Should return false for node without ready condition", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "no-condition-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeDiskPressure,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node without ready condition to return false")
	})

	t.Run("Should return false for node with empty conditions", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "empty-conditions-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node with empty conditions to return false")
	})

	t.Run("Should return false for both deleting and not ready node", func(t *testing.T) {
		now := metav1.Now()
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deleting-not-ready-node",
				DeletionTimestamp: &now,
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

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected deleting and not ready node to return false")
	})

	t.Run("Should return true when ready condition is among multiple conditions", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "multi-condition-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeMemoryPressure,
						Status: corev1.ConditionFalse,
					},
					{
						Type:   corev1.NodeDiskPressure,
						Status: corev1.ConditionFalse,
					},
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   corev1.NodePIDPressure,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == true, "Expected ready node among multiple conditions to return true")
	})

	t.Run("Should return true when ready condition appears multiple times with mixed statuses", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "mixed-ready-conditions-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionFalse,
					},
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		// The function uses lo.Find which returns true if ANY condition matches, so it will find the true condition
		assert.Check(t, result == true, "Expected node with mixed ready conditions to return true (finds any true condition)")
	})
}

func TestGetBYOAndReadyNodes(t *testing.T) {
	testcases := []struct {
		name                          string
		workspace                     *kaitov1beta1.Workspace
		nodeList                      *corev1.NodeList
		listNodesError                error
		disableNodeAutoProvisioning   bool
		expectedAvailableBYONodes     int
		expectedReadyNodes            int
		expectedError                 string
		expectedAvailableBYONodeNames []string
		expectedReadyNodeNames        []string
	}{
		{
			name: "successful_retrieval_with_preferred_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{"ready-node-1", "ready-node-2"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node-1", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node-2", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("not-preferred-node", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			disableNodeAutoProvisioning:   false,
			expectedAvailableBYONodes:     2,
			expectedReadyNodes:            3,
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{"ready-node-1", "ready-node-2"},
			expectedReadyNodeNames:        []string{"ready-node-1", "ready-node-2", "not-preferred-node"},
		},
		{
			name: "disable_node_auto_provisioning_ignores_preferred_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{"ready-node-1"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node-1", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node-2", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			disableNodeAutoProvisioning:   true,
			expectedAvailableBYONodes:     2,
			expectedReadyNodes:            2,
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{"ready-node-1", "ready-node-2"},
			expectedReadyNodeNames:        []string{"ready-node-1", "ready-node-2"},
		},
		{
			name: "skips_not_ready_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{"not-ready-node", "ready-node"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("not-ready-node", false, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			disableNodeAutoProvisioning:   false,
			expectedAvailableBYONodes:     1,
			expectedReadyNodes:            1,
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{"ready-node"},
			expectedReadyNodeNames:        []string{"ready-node"},
		},
		{
			name: "skips_deleting_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{"deleting-node", "ready-node"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("deleting-node", true, true, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			disableNodeAutoProvisioning:   false,
			expectedAvailableBYONodes:     1,
			expectedReadyNodes:            1,
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{"ready-node"},
			expectedReadyNodeNames:        []string{"ready-node"},
		},
		{
			name: "empty_node_list",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{"node-1"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{},
			},
			disableNodeAutoProvisioning:   false,
			expectedAvailableBYONodes:     0,
			expectedReadyNodes:            0,
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{},
			expectedReadyNodeNames:        []string{},
		},
		{
			name: "list_nodes_fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			listNodesError:            errors.New("failed to list nodes"),
			expectedAvailableBYONodes: 0,
			expectedReadyNodes:        0,
			expectedError:             "failed to list nodes",
		},
		{
			name: "no_preferred_nodes_with_auto_provisioning_enabled",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{}, // Empty preferred nodes
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node-1", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node-2", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			disableNodeAutoProvisioning:   false,
			expectedAvailableBYONodes:     0, // No nodes added to BYO because they're not in preferred list
			expectedReadyNodes:            2, // But they're counted as ready
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{},
			expectedReadyNodeNames:        []string{"ready-node-1", "ready-node-2"},
		},
		{
			name: "mixed_ready_and_not_ready_preferred_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					PreferredNodes: []string{"ready-node", "not-ready-node", "deleting-node"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"workload": "gpu",
						},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("not-ready-node", false, false, map[string]string{"workload": "gpu"}),
					createMockNode("deleting-node", true, true, map[string]string{"workload": "gpu"}),
				},
			},
			disableNodeAutoProvisioning:   false,
			expectedAvailableBYONodes:     1,
			expectedReadyNodes:            1,
			expectedError:                 "",
			expectedAvailableBYONodeNames: []string{"ready-node"},
			expectedReadyNodeNames:        []string{"ready-node"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up feature gate for disable node auto provisioning
			originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tc.disableNodeAutoProvisioning
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFeatureGate
			}()

			mockClient := test.NewClient()

			if tc.listNodesError != nil {
				mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(tc.listNodesError)
			} else {
				mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nodeList := args.Get(1).(*corev1.NodeList)
					*nodeList = *tc.nodeList
				}).Return(nil)
			}

			availableBYONodes, readyNodes, err := GetBYOAndReadyNodes(context.Background(), mockClient, tc.workspace)

			if tc.expectedError != "" {
				assert.Check(t, err != nil, "Expected an error")
				assert.Check(t, err.Error() == tc.expectedError, "Expected error message: %s, got: %s", tc.expectedError, err.Error())
				return
			}

			assert.Check(t, err == nil, "Expected no error, got: %v", err)
			assert.Check(t, len(availableBYONodes) == tc.expectedAvailableBYONodes, "Expected %d available BYO nodes, got %d", tc.expectedAvailableBYONodes, len(availableBYONodes))
			assert.Check(t, len(readyNodes) == tc.expectedReadyNodes, "Expected %d ready nodes, got %d", tc.expectedReadyNodes, len(readyNodes))

			// Check actual node names
			actualBYONodeNames := make([]string, len(availableBYONodes))
			for i, node := range availableBYONodes {
				actualBYONodeNames[i] = node.Name
			}

			assert.DeepEqual(t, actualBYONodeNames, tc.expectedAvailableBYONodeNames)
			assert.DeepEqual(t, readyNodes, tc.expectedReadyNodeNames)

			mockClient.AssertExpectations(t)
		})
	}
}

// Helper function to create mock nodes for testing
func createMockNode(name string, ready bool, deleting bool, labels map[string]string) corev1.Node {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	if deleting {
		now := metav1.Now()
		node.ObjectMeta.DeletionTimestamp = &now
	}

	if ready {
		node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		})
	} else {
		node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionFalse,
		})
	}

	return node
}
