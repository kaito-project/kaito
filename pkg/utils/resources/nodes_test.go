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

func TestGetBringYourOwnNodes(t *testing.T) {
	t.Run("Should return all ready nodes when no label selector and node provisioning is disabled", func(t *testing.T) {
		// Save original feature gate value and restore after test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		readyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node2"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		notReadyNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node3"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*readyNode1, *readyNode2, *notReadyNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 2, "Expected 2 ready nodes")
		assert.Equal(t, nodes[0].Name, "node1", "Expected first node to be node1")
		assert.Equal(t, nodes[1].Name, "node2", "Expected second node to be node2")
	})

	t.Run("Should return only preferred nodes when specified and node provisioning enabled", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		preferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "preferred-node"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		nonPreferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "non-preferred-node"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*preferredNode, *nonPreferredNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{"preferred-node"},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 1, "Expected 1 preferred node")
		assert.Equal(t, nodes[0].Name, "preferred-node", "Expected preferred node")
	})

	t.Run("Should filter nodes by label selector", func(t *testing.T) {
		// Enable auto provisioning and ensure nodes aren't filtered by preferred nodes
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true // Disable provisioning so all ready nodes are returned
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		matchingNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "matching-node",
				Labels: map[string]string{"env": "production"},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*matchingNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "production"},
				},
				PreferredNodes: []string{},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 1, "Expected 1 matching node")
		assert.Equal(t, nodes[0].Name, "matching-node", "Expected matching node")
	})

	t.Run("Should return error when node list fails", func(t *testing.T) {
		mockClient := test.NewClient()

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
		}

		_, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Error(t, err, "list failed")
	})

	t.Run("Should skip not ready preferred nodes", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		notReadyPreferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "not-ready-preferred"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*notReadyPreferredNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{"not-ready-preferred"},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 0, "Expected no nodes since preferred node is not ready")
	})
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
