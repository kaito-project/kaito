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

package noderesource

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestNodeResourceReconcilerReconcile(t *testing.T) {
	test.RegisterTestModel()

	// Set required environment variable for SKU handler
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should reset ResourceStatus condition to false when NodeClaim condition is false", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{2}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
						Status: metav1.ConditionFalse,
						Reason: "NodeClaimNotReady",
					},
					{
						Type:   string(kaitov1beta1.ConditionTypeResourceStatus),
						Status: metav1.ConditionTrue,
						Reason: "ResourcesReady",
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update to reset ResourceStatus condition
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.Workspace)
			// Verify that ResourceStatus condition was set to false
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionFalse, condition.Status)
			assert.Equal(t, "NodeClaimNotReady", condition.Reason)
		}).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should not update ResourceStatus condition when it's already false", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{2}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
						Status: metav1.ConditionFalse,
						Reason: "NodeClaimNotReady",
					},
					{
						Type:   string(kaitov1beta1.ConditionTypeResourceStatus),
						Status: metav1.ConditionFalse,
						Reason: "ResourcesNotReady",
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// No status update should be called since ResourceStatus is already false

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})
}

func TestGetReadyNodesMatchingLabelSelector(t *testing.T) {
	test.RegisterTestModel()

	t.Run("Should return ready nodes matching label selector", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		// Create workspace with label selector
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{2}[0],
				InstanceType: "Standard_NC12s_v3",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kaito.sh/workspace":               "test-workspace",
						"node.kubernetes.io/instance-type": "Standard_NC12s_v3",
					},
				},
			},
		}

		// Create mock nodes - some ready, some not ready, some matching labels, some not
		readyMatchingNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ready-matching-node",
				Labels: map[string]string{
					"kaito.sh/workspace":               "test-workspace",
					"node.kubernetes.io/instance-type": "Standard_NC12s_v3",
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

		notReadyMatchingNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "not-ready-matching-node",
				Labels: map[string]string{
					"kaito.sh/workspace":               "test-workspace",
					"node.kubernetes.io/instance-type": "Standard_NC12s_v3",
				},
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

		// Mock List call to return filtered nodes based on label selector
		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*readyMatchingNode, *notReadyMatchingNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.MatchedBy(func(opts []client.ListOption) bool {
			// Verify that MatchingLabelsSelector is used
			for _, opt := range opts {
				if _, ok := opt.(client.MatchingLabelsSelector); ok {
					return true
				}
			}
			return false
		})).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		ctx := context.Background()
		nodes, err := reconciler.getReadyNodesMatchingLabelSelector(ctx, workspace)

		assert.NoError(t, err)
		assert.Len(t, nodes, 1) // Only the ready matching node should be returned
		assert.Equal(t, "ready-matching-node", nodes[0].Name)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should return all ready nodes when no label selector specified", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		// Create workspace without label selector
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:         &[]int{2}[0],
				InstanceType:  "Standard_NC12s_v3",
				LabelSelector: nil, // No label selector
			},
		}

		// Create mock ready nodes
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ready-node-1",
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

		readyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ready-node-2",
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

		// Mock List call to return all nodes (no label selector filter)
		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*readyNode1, *readyNode2, *notReadyNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.MatchedBy(func(opts []client.ListOption) bool {
			// Verify that no MatchingLabelsSelector is used
			for _, opt := range opts {
				if _, ok := opt.(client.MatchingLabelsSelector); ok {
					return false
				}
			}
			return true
		})).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		ctx := context.Background()
		nodes, err := reconciler.getReadyNodesMatchingLabelSelector(ctx, workspace)

		assert.NoError(t, err)
		assert.Len(t, nodes, 2) // Only the ready nodes should be returned
		nodeNames := []string{nodes[0].Name, nodes[1].Name}
		assert.Contains(t, nodeNames, "ready-node-1")
		assert.Contains(t, nodeNames, "ready-node-2")
		mockClient.AssertExpectations(t)
	})
}

func TestNewNodeResourceReconciler(t *testing.T) {
	mockClient := test.NewClient()

	reconciler := NewNodeResourceReconciler()
	reconciler.Client = mockClient

	assert.NotNil(t, reconciler)
	assert.Equal(t, mockClient, reconciler.Client)
}

func TestNodeResourceReconcilerReconcileComprehensive(t *testing.T) {
	test.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should handle workspace not found gracefully", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "non-existent-workspace",
				Namespace: "default",
			},
		}

		// Mock Client.Get to return NotFound error
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{}, "workspace"))

		result, err := reconciler.Reconcile(context.Background(), req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should return error when workspace get fails", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-workspace",
				Namespace: "default",
			},
		}

		// Mock Client.Get to return a generic error
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(fmt.Errorf("network error"))

		result, err := reconciler.Reconcile(context.Background(), req)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network error")
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should requeue when NVIDIA device plugins are not ready", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{1}[0],
				InstanceType: "Standard_NC12s_v3", // GPU instance type
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kaito.sh/workspace": "test-workspace",
					},
				},
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
						Status: metav1.ConditionTrue,
						Reason: "NodeClaimsReady",
					},
				},
			},
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock workspace get
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock all NodeList calls (GetBringYourOwnNodes, updateWorkspaceStatusIfNeeded)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No BYO nodes initially
		}).Return(nil)

		// Mock all NodeClaim List calls
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			}
		}).Return(nil)

		// Mock node get - return node without GPU capacity
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
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
						resources.CapacityNvidiaGPU: resource.MustParse("0"), // Zero GPU capacity
					},
				},
			}
		}).Return(nil)

		// Mock workspace get for status update
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-workspace", Namespace: "default"}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update for GPU capacity not ready
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(context.Background(), req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{RequeueAfter: 1 * time.Second}, result)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should complete successfully when all resources are ready", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{
			Client: mockClient,
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{1}[0],
				InstanceType: "Standard_NC12s_v3",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kaito.sh/workspace": "test-workspace",
					},
				},
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(kaitov1beta1.ConditionTypeNodeClaimStatus),
						Status: metav1.ConditionTrue,
						Reason: "NodeClaimsReady",
					},
				},
			},
		}

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock workspace get
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock all NodeList calls (GetBringYourOwnNodes, updateWorkspaceStatusIfNeeded)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"kaito.sh/workspace": "test-workspace",
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
			}
		}).Return(nil)

		// Mock all NodeClaim List calls
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "test-node",
					},
				},
			}
		}).Return(nil)

		// Mock node get - return ready node with GPU capacity
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
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
		}).Return(nil)

		// Mock workspace get for status update
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-workspace", Namespace: "default"}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update for successful completion
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(context.Background(), req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})
}

func TestEnsureNvidiaDevicePluginsReady(t *testing.T) {
	test.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should fail when GetRequiredNodeClaimsCount fails", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{Count: &[]int{1}[0], InstanceType: "Standard_NC12s_v3"},
		}

		// Mock GetBringYourOwnNodes call (happens first in GetRequiredNodeClaimsCount)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No BYO nodes
		}).Return(nil)

		// Mock failure when listing NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(fmt.Errorf("failed to list nodeclaims"))

		// Mock workspace get for status update (called by UpdateStatusConditionIfNotMatch)
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-workspace", Namespace: "default"}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update for the error condition
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := reconciler.ensureNvidiaDevicePluginsReady(context.Background(), workspace)

		assert.False(t, ready)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get nodes for workspace")
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should return false when not enough ready nodes", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{Count: &[]int{2}[0], InstanceType: "Standard_NC12s_v3"},
			Inference:  &kaitov1beta1.InferenceSpec{}, // Make it an inference workload
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2, // Set target to 2 nodes
				},
			},
		}

		// Mock GetRequiredNodeClaimsCount to return 2 (expecting 2 total nodes)
		// First call: GetBringYourOwnNodes
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No preferred nodes
		}).Return(nil).Once()

		// Second call: GetExistingNodeClaims in GetRequiredNodeClaimsCount
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{} // No existing NodeClaims, so need full target count (2)
		}).Return(nil).Once()

		// Third call: getReadyNodesFromNodeClaims - this should return 1 ready node
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{NodeName: "test-node"},
				},
			}
		}).Return(nil).Once()

		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
						resources.LabelKeyNvidia:       resources.LabelValueNvidia, // Already has accelerator
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
					Capacity:   corev1.ResourceList{resources.CapacityNvidiaGPU: resource.MustParse("1")},
				},
			}
		}).Return(nil)

		// Mock workspace get for status update
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-workspace", Namespace: "default"}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update for node count mismatch
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := reconciler.ensureNvidiaDevicePluginsReady(context.Background(), workspace)

		assert.False(t, ready) // Should return false because only 1 ready node but need 2
		assert.NoError(t, err)
		// Don't check all mock expectations since the function exits early due to node count mismatch
	})

	t.Run("Should add accelerator label to node when missing", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{Count: &[]int{1}[0], InstanceType: "Standard_NC12s_v3"},
		}

		// Mock GetBringYourOwnNodes call (happens first in GetRequiredNodeClaimsCount)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No BYO nodes
		}).Return(nil)

		// Mock NodeClaims count
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{{Status: karpenterv1.NodeClaimStatus{NodeName: "test-node"}}}
		}).Return(nil)

		// Mock getReadyNodesFromNodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.MatchedBy(func(opts []client.ListOption) bool {
			return len(opts) > 0
		})).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{NodeName: "test-node"},
				},
			}
		}).Return(nil)

		// Mock node get - node without accelerator label
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: map[string]string{corev1.LabelInstanceTypeStable: "Standard_NC12s_v3"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
					Capacity:   corev1.ResourceList{resources.CapacityNvidiaGPU: resource.MustParse("1")},
				},
			}
		}).Return(nil)

		// Mock node update to add accelerator label
		mockClient.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(1).(*corev1.Node)
			// Verify the accelerator label was added
			assert.Equal(t, resources.LabelValueNvidia, node.Labels[resources.LabelKeyNvidia])
		}).Return(nil)

		ready, err := reconciler.ensureNvidiaDevicePluginsReady(context.Background(), workspace)

		assert.True(t, ready)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should return false when node has zero GPU capacity", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{Count: &[]int{1}[0], InstanceType: "Standard_NC12s_v3"},
		}

		// Mock GetBringYourOwnNodes call (happens first in GetRequiredNodeClaimsCount)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No BYO nodes
		}).Return(nil)

		// Mock NodeClaims count
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{{Status: karpenterv1.NodeClaimStatus{NodeName: "test-node"}}}
		}).Return(nil)

		// Mock getReadyNodesFromNodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.MatchedBy(func(opts []client.ListOption) bool {
			return len(opts) > 0
		})).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{NodeName: "test-node"},
				},
			}
		}).Return(nil)

		// Mock node get - node with zero GPU capacity
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
						resources.LabelKeyNvidia:       resources.LabelValueNvidia,
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
					Capacity:   corev1.ResourceList{resources.CapacityNvidiaGPU: resource.MustParse("0")},
				},
			}
		}).Return(nil)

		// Mock workspace get for status update
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-workspace", Namespace: "default"}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update for GPU capacity not ready
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ready, err := reconciler.ensureNvidiaDevicePluginsReady(context.Background(), workspace)

		assert.False(t, ready)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})
}

func TestGetReadyNodesFromNodeClaims(t *testing.T) {
	test.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should return empty list when no NodeClaims exist", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_NC12s_v3"},
		}

		// Mock empty NodeClaims list
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{}
		}).Return(nil)

		nodes, err := reconciler.getReadyNodesFromNodeClaims(context.Background(), workspace)

		assert.NoError(t, err)
		assert.Empty(t, nodes)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should skip NodeClaims without assigned nodes", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_NC12s_v3"},
		}

		// Mock NodeClaims without node names
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "", // No node assigned
					},
				},
			}
		}).Return(nil)

		nodes, err := reconciler.getReadyNodesFromNodeClaims(context.Background(), workspace)

		assert.NoError(t, err)
		assert.Empty(t, nodes)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should skip nodes that don't exist", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_NC12s_v3"},
		}

		// Mock NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "non-existent-node",
					},
				},
			}
		}).Return(nil)

		// Mock node get to return NotFound
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "non-existent-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{}, "node"))

		nodes, err := reconciler.getReadyNodesFromNodeClaims(context.Background(), workspace)

		assert.NoError(t, err)
		assert.Empty(t, nodes)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should skip nodes that are not ready", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_NC12s_v3"},
		}

		// Mock NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "not-ready-node",
					},
				},
			}
		}).Return(nil)

		// Mock node get - return not ready node
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "not-ready-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "not-ready-node",
					Labels: map[string]string{corev1.LabelInstanceTypeStable: "Standard_NC12s_v3"},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse, // Not ready
						},
					},
				},
			}
		}).Return(nil)

		nodes, err := reconciler.getReadyNodesFromNodeClaims(context.Background(), workspace)

		assert.NoError(t, err)
		assert.Empty(t, nodes)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should skip nodes with mismatched instance type", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_NC12s_v3"},
		}

		// Mock NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "mismatched-node",
					},
				},
			}
		}).Return(nil)

		// Mock node get - return node with different instance type
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "mismatched-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "mismatched-node",
					Labels: map[string]string{corev1.LabelInstanceTypeStable: "Standard_D4s_v3"}, // Different instance type
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
		}).Return(nil)

		nodes, err := reconciler.getReadyNodesFromNodeClaims(context.Background(), workspace)

		assert.NoError(t, err)
		assert.Empty(t, nodes)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should return ready nodes with matching instance type", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_NC12s_v3"},
		}

		// Mock NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-nodeclaim",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName:      workspace.Name,
							kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
						},
					},
					Status: karpenterv1.NodeClaimStatus{
						NodeName: "ready-node",
					},
				},
			}
		}).Return(nil)

		// Mock node get - return ready node with matching instance type
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "ready-node"}, mock.IsType(&corev1.Node{}), mock.Anything).Run(func(args mock.Arguments) {
			node := args.Get(2).(*corev1.Node)
			*node = corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "ready-node",
					Labels: map[string]string{corev1.LabelInstanceTypeStable: "Standard_NC12s_v3"},
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
		}).Return(nil)

		nodes, err := reconciler.getReadyNodesFromNodeClaims(context.Background(), workspace)

		assert.NoError(t, err)
		assert.Len(t, nodes, 1)
		assert.Equal(t, "ready-node", nodes[0].Name)
		mockClient.AssertExpectations(t)
	})
}

func TestUpdateWorkspaceStatusIfNeeded(t *testing.T) {
	test.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should fail when getReadyNodesMatchingLabelSelector fails", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
		}

		// Mock node list failure
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(fmt.Errorf("failed to list nodes"))

		err := reconciler.updateWorkspaceStatusIfNeeded(context.Background(), workspace)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get ready nodes for workspace")
		mockClient.AssertExpectations(t)
	})

	t.Run("Should return early when no updates are needed", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
			Status: kaitov1beta1.WorkspaceStatus{
				WorkerNodes: []string{"node1", "node2"},
				Conditions: []metav1.Condition{
					{
						Type:   string(kaitov1beta1.ConditionTypeResourceStatus),
						Status: metav1.ConditionTrue,
						Reason: "ResourcesReady",
					},
				},
			},
		}

		// Mock node list - return same nodes as status
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"app": "test"}},
					Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"app": "test"}},
					Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				},
			}
		}).Return(nil)

		err := reconciler.updateWorkspaceStatusIfNeeded(context.Background(), workspace)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		// No status updates should be called
	})

	t.Run("Should update workspace status when nodes change", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &NodeResourceReconciler{Client: mockClient}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
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
		}

		// Mock node list - return different nodes
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "new-node", Labels: map[string]string{"app": "test"}},
					Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				},
			}
		}).Return(nil)

		// Mock workspace get for status update
		mockClient.On("Get", mock.IsType(context.Background()), client.ObjectKey{Name: "test-workspace", Namespace: "default"}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.Workspace)
			assert.Equal(t, []string{"new-node"}, ws.Status.WorkerNodes)
		}).Return(nil)

		err := reconciler.updateWorkspaceStatusIfNeeded(context.Background(), workspace)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})
}
