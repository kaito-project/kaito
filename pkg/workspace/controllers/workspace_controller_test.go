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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/estimator/basicnodesestimator"
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
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&storagev1.StorageClass{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(test.NotFoundError()).Times(4)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
					ssObj := &appsv1.StatefulSet{}
					key := client.ObjectKey{Namespace: "kaito", Name: "testWorkspace"}
					c.GetObjectFromMap(ssObj, key)
					ssObj.Status.ReadyReplicas = 1
					c.CreateOrUpdateObjectInMap(ssObj)
				})
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)

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

		"Update statefulset with new configuration": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&storagev1.StorageClass{}), mock.Anything).Return(nil)
				// Mocking existing StatefulSet object
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ss := args.Get(2).(*appsv1.StatefulSet)
						*ss = test.MockStatefulSetUpdated
					}).
					Return(nil)

				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
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
				Client:    mockClient,
				Scheme:    test.NewTestScheme(),
				Estimator: &basicnodesestimator.BasicNodesEstimator{},
			}
			ctx := context.Background()

			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			if tc.workspace.Status.TargetNodeCount == 0 {
				cnt, err := reconciler.Estimator.EstimateNodeCount(t.Context(), &tc.workspace, mockClient)
				if err != nil {
					t.Errorf("Failed to estimate node count: %v", err)
					return
				}
				tc.workspace.Status.TargetNodeCount = cnt
			}

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
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(errors.New("Failed to create deployment"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(nil)
			},
			workspace:     *test.MockWorkspaceWithInferenceTemplate,
			expectedError: errors.New("Failed to create deployment"),
		},
		"Apply inference from workspace template": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
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
			depObj := &appsv1.StatefulSet{}

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

// mockEstimator is a mock implementation of estimator.NodesEstimator for testing
type mockEstimator struct {
	mock.Mock
}

func (m *mockEstimator) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockEstimator) EstimateNodeCount(ctx context.Context, workspace *v1beta1.Workspace, client client.Client) (int32, error) {
	args := m.Called(ctx, workspace, client)
	return args.Get(0).(int32), args.Error(1)
}

func TestUpdateWorkspaceTargetNodeCount(t *testing.T) {
	tests := map[string]struct {
		workspace      *v1beta1.Workspace
		setupMocks     func(*test.MockClient, *mockEstimator, *int32)
		expectedError  bool
		expectedTarget int32
	}{
		"should return false when targetNodeCount already set": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: v1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status:     v1beta1.WorkspaceStatus{TargetNodeCount: 2},
			},
			setupMocks:     func(c *test.MockClient, e *mockEstimator, _ *int32) {},
			expectedError:  false,
			expectedTarget: 2,
		},
		"should set targetNodeCount to 1 when zero and no inference": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: v1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Status:     v1beta1.WorkspaceStatus{TargetNodeCount: 0},
			},
			setupMocks: func(c *test.MockClient, e *mockEstimator, updatedTarget *int32) {
				// Mock Get used by UpdateWorkspaceStatus to populate the workspace object
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						ws.ObjectMeta = v1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
						ws.Status = v1beta1.WorkspaceStatus{TargetNodeCount: 0}
					}).Return(nil).Once()
				c.StatusMock.On("Update", mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(1).(*v1beta1.Workspace)
						*updatedTarget = ws.Status.TargetNodeCount
					}).Return(nil)
			},
			expectedError:  false,
			expectedTarget: 1,
		},
		"should use estimator when inference present and set returned value": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: v1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Inference:  &v1beta1.InferenceSpec{Preset: &v1beta1.PresetSpec{PresetMeta: v1beta1.PresetMeta{Name: "test-preset"}}},
				Status:     v1beta1.WorkspaceStatus{TargetNodeCount: 0},
			},
			setupMocks: func(c *test.MockClient, e *mockEstimator, updatedTarget *int32) {
				e.On("EstimateNodeCount", mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(int32(3), nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						ws.ObjectMeta = v1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
						ws.Status = v1beta1.WorkspaceStatus{TargetNodeCount: 0}
					}).Return(nil).Once()
				c.StatusMock.On("Update", mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(1).(*v1beta1.Workspace)
						*updatedTarget = ws.Status.TargetNodeCount
					}).Return(nil)
			},
			expectedError:  false,
			expectedTarget: 3,
		},
		"should return error when estimator fails": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: v1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Inference:  &v1beta1.InferenceSpec{Preset: &v1beta1.PresetSpec{PresetMeta: v1beta1.PresetMeta{Name: "test-preset"}}},
				Status:     v1beta1.WorkspaceStatus{TargetNodeCount: 0},
			},
			setupMocks: func(c *test.MockClient, e *mockEstimator, _ *int32) {
				e.On("EstimateNodeCount", mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).Return(int32(0), fmt.Errorf("estimator error"))
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&v1beta1.Workspace{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.Workspace)
						ws.ObjectMeta = v1.ObjectMeta{Name: "test-workspace", Namespace: "default"}
						ws.Status = v1beta1.WorkspaceStatus{TargetNodeCount: 0}
					}).Return(nil).Once()
			},
			expectedError:  true,
			expectedTarget: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockClient := test.NewClient()
			mockEst := &mockEstimator{}
			var updatedTarget int32

			if tt.setupMocks != nil {
				tt.setupMocks(mockClient, mockEst, &updatedTarget)
			}

			reconciler := &WorkspaceReconciler{Client: mockClient, Estimator: mockEst}
			err := reconciler.UpdateWorkspaceTargetNodeCount(context.Background(), tt.workspace)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if !tt.expectedError {
				assert.Equal(t, tt.expectedTarget, tt.workspace.Status.TargetNodeCount)
			}

			mockClient.AssertExpectations(t)
			mockEst.AssertExpectations(t)
		})
	}
}
