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

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/awslabs/operatorpkg/status"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

func TestSelectWorkspaceNodes(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		qualified []*corev1.Node
		preferred []string
		previous  []string
		count     int
		expected  []string
	}{
		"two qualified nodes, need one": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
			},
			preferred: []string{},
			previous:  []string{},
			count:     1,
			expected:  []string{"node1"},
		},

		"three qualified nodes, prefer two of them": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
					},
				},
			},
			preferred: []string{"node3", "node2"},
			previous:  []string{},
			count:     2,
			expected:  []string{"node2", "node3"},
		},

		"three qualified nodes, two of them are selected previously, need two": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
					},
				},
			},
			preferred: []string{},
			previous:  []string{"node3", "node2"},
			count:     2,
			expected:  []string{"node2", "node3"},
		},

		"three qualified nodes, one preferred, one previous, need two": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
					},
				},
			},
			preferred: []string{"node3"},
			previous:  []string{"node2"},
			count:     2,
			expected:  []string{"node2", "node3"},
		},

		"three qualified nodes, one preferred, one previous, need one": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
					},
				},
			},
			preferred: []string{"node3"},
			previous:  []string{"node2"},
			count:     1,
			expected:  []string{"node3"},
		},

		"three qualified nodes, one is created by gpu-provisioner, need one": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							consts.LabelGPUProvisionerCustom: consts.GPUString,
						},
					},
				},
			},
			preferred: []string{},
			previous:  []string{},
			count:     1,
			expected:  []string{"node3"},
		},
		"three qualified nodes, one is created by gpu-provisioner, one is preferred, one is previous, need two": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							consts.LabelGPUProvisionerCustom: consts.GPUString,
						},
					},
				},
			},
			preferred: []string{"node2"},
			previous:  []string{"node1"},
			count:     2,
			expected:  []string{"node1", "node2"},
		},
		"three qualified nodes, one is created by gpu-provisioner, one is preferred, one is previous, need three": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							consts.LabelGPUProvisionerCustom: consts.GPUString,
						},
					},
				},
			},
			preferred: []string{"node2"},
			previous:  []string{"node1"},
			count:     3,
			expected:  []string{"node1", "node2", "node3"},
		},
		"three qualified nodes, one is created by gpu-provisioner (machine), the other created by karpenter (nodeClaim), one is preferred, need two": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							consts.LabelNodePool: consts.KaitoNodePoolName,
						},
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							consts.LabelGPUProvisionerCustom: consts.GPUString,
						},
					},
				},
			},
			preferred: []string{"node1"},
			previous:  []string{},
			count:     2,
			expected:  []string{"node1", "node3"},
		},
		"three qualified nodes, one is created by  by karpenter (nodeClaim), two is preferred, need two": {
			qualified: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							consts.LabelNodePool: consts.KaitoNodePoolName,
						},
					},
				},
			},
			preferred: []string{"node1"},
			previous:  []string{},
			count:     2,
			expected:  []string{"node1", "node3"},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			selectedNodes := utils.SelectNodes(tc.qualified, tc.preferred, tc.previous, tc.count)

			selectedNodesArray := []string{}

			for _, each := range selectedNodes {
				selectedNodesArray = append(selectedNodesArray, each.Name)
			}

			sort.Strings(selectedNodesArray)
			sort.Strings(tc.expected)

			if !reflect.DeepEqual(selectedNodesArray, tc.expected) {
				t.Errorf("%s: selected Nodes %+v are different from the expected %+v", k, selectedNodesArray, tc.expected)
			}
		})
	}
}

func TestEnsureService(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
		workspace     *v1beta1.Workspace
	}{
		"Existing service is found for workspace": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			workspace:     test.MockWorkspaceDistributedModel,
		},
		"Service creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(errors.New("cannot create service"))
			},
			expectedError: errors.New("cannot create service"),
			workspace:     test.MockWorkspaceDistributedModel,
		},
		"Successfully creates a new service": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			workspace:     test.MockWorkspaceDistributedModel,
		},
		"Successfully creates a new service for a custom model": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			workspace:     test.MockWorkspaceCustomModel,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.ensureService(ctx, tc.workspace)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}

}

func TestApplyInferenceWithPreset(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		workspace     v1beta1.Workspace
		expectedError error
	}{
		"Fail to get inference because associated workload with workspace cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(errors.New("Failed to get resource"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceDistributedModel,
			expectedError: errors.New("Failed to get resource"),
		},
		"Create preset inference because inference workload did not exist": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(test.NotFoundError()).Times(4)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
					depObj := &appsv1.Deployment{}
					key := client.ObjectKey{Namespace: "kaito", Name: "testWorkspace"}
					c.GetObjectFromMap(depObj, key)
					depObj.Status.ReadyReplicas = 1
					c.CreateOrUpdateObjectInMap(depObj)
				})
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceWithPreset,
			expectedError: nil,
		},
		"Apply inference from existing workload": {
			callMocks: func(c *test.MockClient) {
				numRep := int32(1)
				relevantMap := c.CreateMapWithType(&appsv1.StatefulSet{})
				relevantMap[client.ObjectKey{Namespace: "kaito", Name: "testWorkspace"}] = &appsv1.StatefulSet{
					ObjectMeta: v1.ObjectMeta{
						Name:      "testWorkspace",
						Namespace: "kaito",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: &numRep,
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "inference-container",
									Image: "inference-image:latest",
								}},
							},
						},
					},
					Status: appsv1.StatefulSetStatus{
						ReadyReplicas: 1,
					},
				}
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
				c.On("Update", mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceDistributedModel,
			expectedError: nil,
		},

		"Update deployment with new configuration": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				// Mocking existing Deployment object
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = test.MockDeploymentUpdated
					}).
					Return(nil)

				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceWithPreset,
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

			err := reconciler.applyInference(ctx, &tc.workspace)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestApplyInferenceWithTemplate(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		workspace     v1beta1.Workspace
		expectedError error
	}{
		"Fail to apply inference from workspace template": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(errors.New("Failed to create deployment"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceWithInferenceTemplate,
			expectedError: errors.New("Failed to create deployment"),
		},
		"Apply inference from workspace template": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceWithInferenceTemplate,
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			depObj := &appsv1.Deployment{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(depObj, key)
				depObj.Status.ReadyReplicas = 1
				mockClient.CreateOrUpdateObjectInMap(depObj)
			}

			tc.callMocks(mockClient)

			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.applyInference(ctx, &tc.workspace)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestGetAllQualifiedNodes(t *testing.T) {
	// Save and restore feature gate state
	originalNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalNAP }()

	deletedNode := corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: "node4",
			Labels: map[string]string{
				corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
			},
			DeletionTimestamp: &v1.Time{Time: time.Now()},
		},
	}
	mockWorkspaceWithPreferredNodes := test.MockWorkspaceWithPreferredNodes.DeepCopy()
	mockWorkspaceWithPreferredNodes.Resource.PreferredNodes = []string{"node-p1", "node-p2"}
	mockWorkspaceWithPreferredNodes.Resource.Count = ptr.To(2)

	mockWorkspaceWithSinglePreferredNode := test.MockWorkspaceWithPreferredNodes.DeepCopy()

	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		workspace     *v1beta1.Workspace
		expectedError error
		expectedNodes []string
		disableNAP    bool
	}{
		"Fails to get qualified nodes because can't list nodes": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("Failed to list nodes"))
			},
			workspace:     test.MockWorkspaceDistributedModel,
			expectedError: errors.New("Failed to list nodes"),
			expectedNodes: nil,
			disableNAP:    false,
		},
		"Gets all qualified nodes": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList

				nodeList.Items = append(nodeList.Items, deletedNode)

				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range test.MockNodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			workspace:     test.MockWorkspaceDistributedModel,
			expectedError: nil,
			expectedNodes: []string{"node1"},
			disableNAP:    false,
		},
		"Gets all qualified nodes with preferred": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList

				nodeList.Items = append(nodeList.Items, deletedNode)

				nodesFromOtherVendor := []corev1.Node{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p1",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor1",
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
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p2",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor2",
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
					},
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p3",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor1",
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
				nodeList.Items = append(nodeList.Items, nodesFromOtherVendor...)

				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range test.MockNodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			workspace:     mockWorkspaceWithSinglePreferredNode,
			expectedError: nil,
			expectedNodes: []string{"node-p1"},
		},
		"NAP disabled: all preferred nodes present and ready, returns all": {
			callMocks: func(c *test.MockClient) {
				nodeList := &corev1.NodeList{Items: []corev1.Node{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p1",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor1",
								"apps":                         "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							}},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p2",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor2",
								"apps":                         "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							}},
						},
					},
				}}

				c.CreateOrUpdateObjectInMap(&nodeList.Items[0])
				c.CreateOrUpdateObjectInMap(&nodeList.Items[1])

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

			},
			workspace:     mockWorkspaceWithPreferredNodes.DeepCopy(),
			expectedError: nil,
			expectedNodes: []string{"node-p1", "node-p2"},
			disableNAP:    true,
		},
		"NAP disabled: one preferred node not ready, returns error": {
			callMocks: func(c *test.MockClient) {
				nodeList := &corev1.NodeList{Items: []corev1.Node{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p1",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor1",
								"apps":                         "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							}},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p2",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor2",
								"apps":                         "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionFalse,
							}},
						},
					},
				}}

				c.CreateOrUpdateObjectInMap(&nodeList.Items[0])
				c.CreateOrUpdateObjectInMap(&nodeList.Items[1])

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			workspace:     mockWorkspaceWithPreferredNodes.DeepCopy(),
			expectedError: errors.New("when node auto-provisioning is disabled, at least 2 preferred nodes must match the label selector and be ready and not deleting, only have 1"),
			expectedNodes: nil,
			disableNAP:    true,
		},
		"NAP disabled: one preferred node deleting, returns error": {
			callMocks: func(c *test.MockClient) {
				deletingTime := v1.Time{Time: time.Now()}
				nodeList := &corev1.NodeList{Items: []corev1.Node{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p1",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor1",
								"apps":                         "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							}},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p2",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor2",
								"apps":                         "test",
							},
							DeletionTimestamp: &deletingTime,
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							}},
						},
					},
				}}

				c.CreateOrUpdateObjectInMap(&nodeList.Items[0])
				c.CreateOrUpdateObjectInMap(&nodeList.Items[1])

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			workspace:     mockWorkspaceWithPreferredNodes.DeepCopy(),
			expectedError: errors.New("when node auto-provisioning is disabled, at least 2 preferred nodes must match the label selector and be ready and not deleting, only have 1"),
			expectedNodes: nil,
			disableNAP:    true,
		},
		"NAP disabled: missing preferred node, returns error": {
			callMocks: func(c *test.MockClient) {
				nodeList := &corev1.NodeList{Items: []corev1.Node{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "node-p1",
							Labels: map[string]string{
								corev1.LabelInstanceTypeStable: "vendor1",
								"apps":                         "test",
							},
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							}},
						},
					},
				}}

				c.CreateOrUpdateObjectInMap(&nodeList.Items[0])

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(errors.New("nodes \"node-p2\" not found"))
			},
			workspace:     mockWorkspaceWithPreferredNodes.DeepCopy(),
			expectedError: errors.New("when node auto-provisioning is disabled, at least 2 preferred nodes must match the label selector and be ready and not deleting, only have 1"),
			expectedNodes: nil,
			disableNAP:    true,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tc.disableNAP

			mockClient := test.NewClient()
			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			tc.callMocks(mockClient)

			nodes, err := reconciler.getAllQualifiedNodes(ctx, tc.workspace)

			if tc.expectedError != nil {
				assert.NotNil(t, err)
				assert.Equal(t, tc.expectedError, err)
				assert.Nil(t, nodes)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, nodes)
			assert.Equal(t, len(tc.expectedNodes), len(nodes))
		})
	}
}

func TestApplyWorkspaceResource(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
		workspace     v1beta1.Workspace
	}{
		"Fail to apply workspace because associated nodeClaim cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("failed to retrieve nodeClaims"))

			},
			workspace:     *test.MockWorkspaceBaseModel,
			expectedError: errors.New("failed to retrieve nodeClaims"),
		},
		"Fail to apply workspace with nodeClaims because can't get qualified nodes": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&test.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range nodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("failed to list nodes"))
			},
			workspace:     *test.MockWorkspaceBaseModel,
			expectedError: errors.New("failed to list nodes"),
		},
		"Successfully apply workspace resource with nodeClaim": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				node1 := test.MockNodeList.Items[0]
				//insert node object into the map
				c.CreateOrUpdateObjectInMap(&node1)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceBaseModel,
			expectedError: nil,
		},
		"Successfully apply workspace resource with nodeClaim and preferred nodes": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				node1 := test.MockNodeList.Items[0]
				//insert node object into the map
				c.CreateOrUpdateObjectInMap(&node1)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				// no get node query is needed here as we are not updating the node

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceWithPreferredNodes,
			expectedError: nil,
		},
		"Update node Failed with NotFound error": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(apierrors.NewNotFound(corev1.Resource("Node"), "node1"))

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)

			},
			workspace:     *test.MockWorkspaceBaseModel,
			expectedError: apierrors.NewNotFound(corev1.Resource("Node"), "node1"),
		},
	}

	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockNodeClaim := &karpenterv1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockNodeClaim, key)
				mockNodeClaim.Status.Conditions = []status.Condition{
					{
						Type:   string(apis.ConditionReady),
						Status: v1.ConditionTrue,
					},
				}
				mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
			}

			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.applyWorkspaceResource(ctx, &tc.workspace)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestSyncControllerRevision(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		workspace     v1beta1.Workspace
		expectedError error
		verifyCalls   func(c *test.MockClient)
	}{

		"No new revision needed": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.ControllerRevision)
						*dep = appsv1.ControllerRevision{
							ObjectMeta: v1.ObjectMeta{
								Annotations: map[string]string{
									WorkspaceHashAnnotation: "1171dc5d15043c92e684c8f06689eb241763a735181fdd2b59c8bd8fd6eecdd4",
								},
							},
							Revision: 1,
						}
					}).
					Return(nil)
				// Add mock for workspace retrieval in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = test.MockWorkspaceWithComputeHash
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace:     test.MockWorkspaceWithComputeHash,
			expectedError: nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for Workspace
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Fail to create ControllerRevision": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(errors.New("failed to create ControllerRevision"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockWorkspaceFailToCreateCR.Name))
			},
			workspace:     test.MockWorkspaceFailToCreateCR,
			expectedError: errors.New("failed to create new ControllerRevision: failed to create ControllerRevision"),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Successfully create new ControllerRevision": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockWorkspaceFailToCreateCR.Name))
				// Add mock for workspace retrieval in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = test.MockWorkspaceSuccessful
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace:     test.MockWorkspaceSuccessful,
			expectedError: nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for Workspace
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Successfully delete old ControllerRevision": {
			callMocks: func(c *test.MockClient) {
				revisions := &appsv1.ControllerRevisionList{}
				jsonData, _ := json.Marshal(test.MockWorkspaceWithUpdatedDeployment)

				for i := 0; i <= consts.MaxRevisionHistoryLimit; i++ {
					revision := &appsv1.ControllerRevision{
						ObjectMeta: v1.ObjectMeta{
							Name: fmt.Sprintf("revision-%d", i),
						},
						Revision: int64(i),
						Data:     runtime.RawExtension{Raw: jsonData},
					}
					revisions.Items = append(revisions.Items, *revision)
				}
				relevantMap := c.CreateMapWithType(revisions)

				for _, obj := range revisions.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockWorkspaceFailToCreateCR.Name))
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				// Add mock for workspace retrieval in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = test.MockWorkspaceWithDeleteOldCR
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace:     test.MockWorkspaceWithDeleteOldCR,
			expectedError: nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for Workspace
				c.AssertNumberOfCalls(t, "Delete", 1)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Fail to update Workspace annotations": {
			callMocks: func(c *test.MockClient) {
				revisions := &appsv1.ControllerRevisionList{}
				jsonData, _ := json.Marshal(test.MockWorkspaceWithUpdatedDeployment)

				for i := 0; i <= consts.MaxRevisionHistoryLimit; i++ {
					revision := &appsv1.ControllerRevision{
						ObjectMeta: v1.ObjectMeta{
							Name: fmt.Sprintf("revision-%d", i),
						},
						Revision: int64(i),
						Data:     runtime.RawExtension{Raw: jsonData},
					}
					revisions.Items = append(revisions.Items, *revision)
				}
				relevantMap := c.CreateMapWithType(revisions)

				for _, obj := range revisions.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockWorkspaceFailToCreateCR.Name))
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				// Add mock for workspace retrieval in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = test.MockWorkspaceUpdateCR
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(fmt.Errorf("failed to update Workspace annotations"))
			},
			workspace:     test.MockWorkspaceUpdateCR,
			expectedError: fmt.Errorf("failed to update Workspace annotations: %w", fmt.Errorf("failed to update Workspace annotations")),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for Workspace
				c.AssertNumberOfCalls(t, "Delete", 1)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
	}
	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.syncControllerRevision(ctx, &tc.workspace)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
			if tc.verifyCalls != nil {
				tc.verifyCalls(mockClient)
			}
		})
	}
}

func TestEnsureGatewayAPIInferenceExtension(t *testing.T) {
	test.RegisterTestModel()
	// Ensure GPU SKU lookup works inside inference dry-run
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		featureGate   bool
		runtimeName   model.RuntimeName
		isPreset      bool
		expectedError error
	}{
		"feature gate off returns nil": {
			callMocks:     func(c *test.MockClient) {},
			featureGate:   false,
			runtimeName:   model.RuntimeNameVLLM,
			isPreset:      true,
			expectedError: nil,
		},
		"runtime not vllm returns nil": {
			callMocks:     func(c *test.MockClient) {},
			featureGate:   true,
			runtimeName:   model.RuntimeNameHuggingfaceTransformers,
			isPreset:      true,
			expectedError: nil,
		},
		"not preset returns nil": {
			callMocks:     func(c *test.MockClient) {},
			featureGate:   true,
			runtimeName:   model.RuntimeNameVLLM,
			isPreset:      false,
			expectedError: nil,
		},
		"OCIRepository and HelmRelease found and up-to-date": {
			callMocks: func(c *test.MockClient) {
				// Default inference template ConfigMap exists in target namespace
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&sourcev1.OCIRepository{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&helmv2.HelmRelease{}), mock.Anything).Return(nil)

				ociRepository := manifests.GenerateInferencePoolOCIRepository(test.MockWorkspaceWithPresetVLLM)
				ociRepository.Status.Conditions = []v1.Condition{{Type: consts.ConditionReady, Status: v1.ConditionTrue}}
				c.CreateOrUpdateObjectInMap(ociRepository)

				helmRelease, _ := manifests.GenerateInferencePoolHelmRelease(test.MockWorkspaceWithPresetVLLM, false)
				helmRelease.Status.Conditions = []v1.Condition{{Type: consts.ConditionReady, Status: v1.ConditionTrue}}
				c.CreateOrUpdateObjectInMap(helmRelease)
			},
			featureGate:   true,
			runtimeName:   model.RuntimeNameVLLM,
			isPreset:      true,
			expectedError: nil,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension]
			featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] = tc.featureGate
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] = originalFeatureGate
			}()

			wObj := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			if !tc.isPreset {
				wObj.Inference.Preset = nil
			}
			// Ensure runtime selection aligns with the test case
			if tc.runtimeName != model.RuntimeNameVLLM {
				if wObj.Annotations == nil {
					wObj.Annotations = map[string]string{}
				}
				wObj.Annotations[v1beta1.AnnotationWorkspaceRuntime] = string(tc.runtimeName)
			}

			mockClient := test.NewClient()
			if tc.callMocks != nil {
				tc.callMocks(mockClient)
			}

			reconciler := &WorkspaceReconciler{Client: mockClient}
			err := reconciler.ensureGatewayAPIInferenceExtension(context.Background(), wObj)
			if tc.expectedError != nil {
				assert.ErrorContains(t, err, tc.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
