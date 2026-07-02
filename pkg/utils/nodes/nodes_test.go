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

package nodes

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
