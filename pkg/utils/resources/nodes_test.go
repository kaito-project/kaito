// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestUpdateNodeWithLabel(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Fail to update node because it cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "mockNode"}, mock.IsType(&corev1.Node{}), mock.Anything).Return(errors.New("Cannot retrieve node"))
			},
			expectedError: errors.New("Cannot retrieve node"),
		},
		"Fail to update node because node cannot be updated": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "mockNode"}, mock.Anything, mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(errors.New("Cannot update node"))
			},
			expectedError: errors.New("Cannot update node"),
		},
		"Successfully updates node": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "mockNode"}, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			err := UpdateNodeWithLabel(context.Background(), "mockNode", "fakeKey", "fakeVal", mockClient)
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
