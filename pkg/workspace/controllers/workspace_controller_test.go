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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

// createMockWorkspaceWithStatus creates a workspace with proper inference status
func createMockWorkspaceWithStatus(workspace v1beta1.Workspace, targetNodeCount int) v1beta1.Workspace {
	workspace.Status.Inference = &v1beta1.InferenceStatus{
		TargetNodeCount: int32(targetNodeCount),
	}
	return workspace
}

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
			workspace: func() *v1beta1.Workspace {
				ws := createMockWorkspaceWithStatus(*test.MockWorkspaceDistributedModel, 2)
				return &ws
			}(),
		},
		"Service creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(errors.New("cannot create service"))
			},
			expectedError: errors.New("cannot create service"),
			workspace: func() *v1beta1.Workspace {
				ws := createMockWorkspaceWithStatus(*test.MockWorkspaceDistributedModel, 2)
				return &ws
			}(),
		},
		"Successfully creates a new service": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			workspace: func() *v1beta1.Workspace {
				ws := createMockWorkspaceWithStatus(*test.MockWorkspaceDistributedModel, 2)
				return &ws
			}(),
		},
		"Successfully creates a new service for a custom model": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			workspace: func() *v1beta1.Workspace {
				ws := createMockWorkspaceWithStatus(*test.MockWorkspaceCustomModel, 1)
				return &ws
			}(),
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
			workspace:     createMockWorkspaceWithStatus(*test.MockWorkspaceDistributedModel, 2),
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

				// Mocks for updateWorkspaceInferenceStatus function
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     createMockWorkspaceWithStatus(*test.MockWorkspaceWithPreset, 1),
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
				// Mock for ScaleDeploymentIfNeeded - deployment doesn't exist (StatefulSet scenario)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(test.NotFoundError())
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     createMockWorkspaceWithStatus(*test.MockWorkspaceDistributedModel, 2),
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
			workspace:     createMockWorkspaceWithStatus(*test.MockWorkspaceWithPreset, 1),
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
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(errors.New("Failed to create deployment"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     createMockWorkspaceWithStatus(*test.MockWorkspaceWithInferenceTemplate, 1),
			expectedError: errors.New("Failed to create deployment"),
		},
		"Apply inference from workspace template": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     createMockWorkspaceWithStatus(*test.MockWorkspaceWithInferenceTemplate, 1),
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

func TestUpdateWorkspaceInferenceStatus(t *testing.T) {
	testcases := map[string]struct {
		callMocks          func(c *test.MockClient)
		workspace          *v1beta1.Workspace
		expectedError      error
		shouldUpdateStatus bool
	}{
		"Deployment not found - no error, no update": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Return(test.NotFoundError())
			},
			workspace:          test.MockWorkspaceWithPreset.DeepCopy(),
			expectedError:      nil,
			shouldUpdateStatus: false,
		},
		"Get deployment fails with non-NotFound error": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Return(errors.New("api server error"))
			},
			workspace:          test.MockWorkspaceWithPreset.DeepCopy(),
			expectedError:      errors.New("failed to get deployment: api server error"),
			shouldUpdateStatus: false,
		},
		"Initialize inference status when nil and update with deployment data": {
			callMocks: func(c *test.MockClient) {
				// Mock deployment get
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = appsv1.Deployment{
							ObjectMeta: v1.ObjectMeta{
								Name:      "testWorkspace",
								Namespace: "kaito",
							},
							Spec: appsv1.DeploymentSpec{
								Selector: &v1.LabelSelector{
									MatchLabels: map[string]string{
										"app":                     "testWorkspace",
										"workspace.kaito.io/name": "testWorkspace",
									},
								},
							},
							Status: appsv1.DeploymentStatus{
								Replicas: 2,
							},
						}
					}).
					Return(nil)

				// Mock workspace get and update in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						// Simulate the workspace without inference status
						*ws = *test.MockWorkspaceWithPreset.DeepCopy()
						ws.Status.Inference = nil
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace: func() *v1beta1.Workspace {
				ws := test.MockWorkspaceWithPreset.DeepCopy()
				ws.Status.Inference = nil
				return ws
			}(),
			expectedError:      nil,
			shouldUpdateStatus: true,
		},
		"Update replicas when different from deployment": {
			callMocks: func(c *test.MockClient) {
				// Mock deployment get
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = appsv1.Deployment{
							ObjectMeta: v1.ObjectMeta{
								Name:      "testWorkspace",
								Namespace: "kaito",
							},
							Spec: appsv1.DeploymentSpec{
								Selector: &v1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "testWorkspace",
									},
								},
							},
							Status: appsv1.DeploymentStatus{
								Replicas: 5, // Different from workspace (3)
							},
						}
					}).
					Return(nil)

				// Mock workspace get and update in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = *test.MockWorkspaceWithPreset.DeepCopy()
						// Set initial inference status with different replicas
						ws.Status.Inference = &v1beta1.InferenceStatus{
							Replicas: 3, // Different from deployment (5)
							Selector: "app=testWorkspace",
						}
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace: func() *v1beta1.Workspace {
				ws := test.MockWorkspaceWithPreset.DeepCopy()
				ws.Status.Inference = &v1beta1.InferenceStatus{
					Replicas: 3,
					Selector: "app=testWorkspace",
				}
				return ws
			}(),
			expectedError:      nil,
			shouldUpdateStatus: true,
		},
		"Update selector when different from deployment": {
			callMocks: func(c *test.MockClient) {
				// Mock deployment get
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = appsv1.Deployment{
							ObjectMeta: v1.ObjectMeta{
								Name:      "testWorkspace",
								Namespace: "kaito",
							},
							Spec: appsv1.DeploymentSpec{
								Selector: &v1.LabelSelector{
									MatchLabels: map[string]string{
										"app":     "testWorkspace",
										"version": "v2", // New label
									},
								},
							},
							Status: appsv1.DeploymentStatus{
								Replicas: 2,
							},
						}
					}).
					Return(nil)

				// Mock workspace get and update in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = *test.MockWorkspaceWithPreset.DeepCopy()
						ws.Status.Inference = &v1beta1.InferenceStatus{
							Replicas: 2,
							Selector: "app=testWorkspace", // Different from deployment
						}
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace: func() *v1beta1.Workspace {
				ws := test.MockWorkspaceWithPreset.DeepCopy()
				ws.Status.Inference = &v1beta1.InferenceStatus{
					Replicas: 2,
					Selector: "app=testWorkspace",
				}
				return ws
			}(),
			expectedError:      nil,
			shouldUpdateStatus: true,
		},
		"No update needed when values are identical": {
			callMocks: func(c *test.MockClient) {
				// Mock deployment get
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = appsv1.Deployment{
							ObjectMeta: v1.ObjectMeta{
								Name:      "testWorkspace",
								Namespace: "kaito",
							},
							Spec: appsv1.DeploymentSpec{
								Selector: &v1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "testWorkspace",
									},
								},
							},
							Status: appsv1.DeploymentStatus{
								Replicas: 2,
							},
						}
					}).
					Return(nil)
			},
			workspace: func() *v1beta1.Workspace {
				ws := test.MockWorkspaceWithPreset.DeepCopy()
				ws.Status.Inference = &v1beta1.InferenceStatus{
					Replicas: 2,                   // Same as deployment
					Selector: "app=testWorkspace", // Same as deployment
				}
				return ws
			}(),
			expectedError:      nil,
			shouldUpdateStatus: false,
		},
		"Handle deployment with nil selector": {
			callMocks: func(c *test.MockClient) {
				// Mock deployment get
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = appsv1.Deployment{
							ObjectMeta: v1.ObjectMeta{
								Name:      "testWorkspace",
								Namespace: "kaito",
							},
							Spec: appsv1.DeploymentSpec{
								Selector: nil, // Nil selector
							},
							Status: appsv1.DeploymentStatus{
								Replicas: 1,
							},
						}
					}).
					Return(nil)

				// Mock workspace get and update in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						*ws = *test.MockWorkspaceWithPreset.DeepCopy()
						ws.Status.Inference = &v1beta1.InferenceStatus{
							Replicas: 1,
							Selector: "old-selector",
						}
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(nil)
			},
			workspace: func() *v1beta1.Workspace {
				ws := test.MockWorkspaceWithPreset.DeepCopy()
				ws.Status.Inference = &v1beta1.InferenceStatus{
					Replicas: 1,
					Selector: "old-selector",
				}
				return ws
			}(),
			expectedError:      nil,
			shouldUpdateStatus: true,
		},
		"Update workspace retry fails": {
			callMocks: func(c *test.MockClient) {
				// Mock deployment get
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = appsv1.Deployment{
							ObjectMeta: v1.ObjectMeta{
								Name:      "testWorkspace",
								Namespace: "kaito",
							},
							Spec: appsv1.DeploymentSpec{
								Selector: &v1.LabelSelector{
									MatchLabels: map[string]string{
										"app": "testWorkspace",
									},
								},
							},
							Status: appsv1.DeploymentStatus{
								Replicas: 3,
							},
						}
					}).
					Return(nil)

				// Mock workspace get in updateWorkspaceWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Return(errors.New("failed to get workspace"))
			},
			workspace: func() *v1beta1.Workspace {
				ws := test.MockWorkspaceWithPreset.DeepCopy()
				ws.Status.Inference = &v1beta1.InferenceStatus{
					Replicas: 1, // Different from deployment
					Selector: "app=testWorkspace",
				}
				return ws
			}(),
			expectedError:      errors.New("failed to get workspace"),
			shouldUpdateStatus: true, // Will attempt update but fail
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			reconciler := &WorkspaceReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.updateWorkspaceInferenceStatus(ctx, tc.workspace)

			if tc.expectedError != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
