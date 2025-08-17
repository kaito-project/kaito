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

package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestGetBringYourOwnNodes(t *testing.T) {
	t.Run("Should return empty list when no preferred nodes are specified", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{}, // No preferred nodes
			},
		}

		// Mock the List call to return an empty node list
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Empty(t, result)
		assert.Len(t, result, 0)
	})

	t.Run("Should return error when label selector is invalid", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2"},
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "key",
							Operator: "InvalidOperator", // Invalid operator
							Values:   []string{"value"},
						},
					},
				},
			},
		}

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to convert label selector")
	})

	t.Run("Should return error when listing nodes fails", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2"},
			},
		}

		// Mock the List call to return an error
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(assert.AnError)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to list nodes")
	})

	t.Run("Should return only ready preferred nodes", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		// Use default feature gate setting (false) to get the preferred-nodes-only behavior
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2", "node3"},
			},
		}

		// Create test nodes
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
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

		notReadyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
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

		readyNode3 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
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

		// Node that's not in preferred list
		notPreferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node4",
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

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*readyNode1, *notReadyNode2, *readyNode3, *notPreferredNode},
		}

		// Mock the List call to return our test nodes
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Len(t, result, 2) // Only node1 and node3 should be returned

		// Check that only ready preferred nodes are returned
		nodeNames := make([]string, len(result))
		for i, node := range result {
			nodeNames[i] = node.Name
		}
		assert.Contains(t, nodeNames, "node1")
		assert.Contains(t, nodeNames, "node3")
		assert.NotContains(t, nodeNames, "node2") // Not ready
		assert.NotContains(t, nodeNames, "node4") // Not in preferred list
	})

	t.Run("Should filter nodes by label selector when provided", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2"},
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"node-type": "gpu",
					},
				},
			},
		}

		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					"node-type": "gpu",
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

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*readyNode1}, // Only node1 should match the label selector
		}

		// Mock the List call to return filtered nodes (simulating label selector behavior)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "node1", result[0].Name)
	})

	t.Run("Should return empty list when no nodes match preferred nodes list", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		// Use default feature gate setting (false) to get the preferred-nodes-only behavior
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2"},
			},
		}

		// Create nodes that are not in the preferred list
		nodeList := &corev1.NodeList{
			Items: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node4",
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
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Empty(t, result)
		assert.Len(t, result, 0)
	})

	t.Run("Should handle node without ready condition", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1"},
			},
		}

		// Create a node without NodeReady condition
		nodeWithoutReadyCondition := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
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

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*nodeWithoutReadyCondition},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Empty(t, result) // Node should be filtered out as it's not ready
		assert.Len(t, result, 0)
	})

	t.Run("Should respect feature gate FeatureFlagDisableNodeAutoProvisioning when enabled", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		// Enable the feature gate to ignore preferred nodes filtering
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2"},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node2"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node3"}, // Not in preferred nodes
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Len(t, result, 3) // All ready nodes should be returned when feature gate is enabled

		nodeNames := make([]string, len(result))
		for i, node := range result {
			nodeNames[i] = node.Name
		}
		assert.Contains(t, nodeNames, "node1")
		assert.Contains(t, nodeNames, "node2")
		assert.Contains(t, nodeNames, "node3") // node3 should be included when feature gate is enabled
	})

	t.Run("Should return all ready nodes when feature gate FeatureFlagDisableNodeAutoProvisioning is disabled", func(t *testing.T) {
		mockClient := test.NewClient()
		ctx := context.Background()

		// Disable the feature gate (default behavior) to filter by preferred nodes
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				PreferredNodes: []string{"node1", "node2"},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node2"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node3"}, // Not in preferred nodes but should be excluded
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			list := args.Get(1).(*corev1.NodeList)
			*list = *nodeList
		}).Return(nil)

		result, err := GetBringYourOwnNodes(ctx, mockClient, workspace)

		assert.NoError(t, err)
		assert.Len(t, result, 2) // Only preferred ready nodes should be returned when feature gate is disabled

		nodeNames := make([]string, len(result))
		for i, node := range result {
			nodeNames[i] = node.Name
		}
		assert.Contains(t, nodeNames, "node1")
		assert.Contains(t, nodeNames, "node2")
		assert.NotContains(t, nodeNames, "node3") // node3 should be excluded when feature gate is disabled
	})
}
